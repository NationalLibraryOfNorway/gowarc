package gowarc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/nationallibraryofnorway/gowarc/v3/internal/diskbuffer"
)

func Test_httpRequestBlock_BlockDigest(t *testing.T) {
	content := "GET / HTTP/1.0\n" +
		"Host: example.com\n" +
		"Accept-Language: en-US,en;q=0.8,ru;q=0.6\n" +
		"Referer: http://example.com/foo.html\n" +
		"Connection: close\n" +
		"User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/56.0.2924.87 Safari/537.36\n\n"
	digest := "sha1:a3781ff1fc3fb52318f623e22c85d63d74c12932"
	payloadDigest := "sha1:da39a3ee5e6b4b0d3255bfef95601890afd80709"

	tests := []blockDigestTest{
		{
			"strings.Reader",
			strings.NewReader(content),
		},
		{
			"diskbuffer.Buffer",
			func() io.Reader { d := diskbuffer.New(); _, _ = d.WriteString(content); return d }(),
		},
		{
			"iotest.HalfReader",
			iotest.HalfReader(strings.NewReader(content)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, val, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, tt.data, blockDigest, pDigest)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := val; len(got) != 0 {
				t.Fatalf("val = %v, want empty", got)
			}

			validateBlockDigestTest(t, block, digest)
			validatePayloadDigestTest(t, block, payloadDigest)
		})
	}
}

func Test_httpRequestBlock_Cache(t *testing.T) {
	content := "GET / HTTP/1.0\n" +
		"Host: example.com\n" +
		"Accept-Language: en-US,en;q=0.8,ru;q=0.6\n" +
		"Referer: http://example.com/foo.html\n" +
		"Connection: close\n" +
		"User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/56.0.2924.87 Safari/537.36\n\n"
	digest := "sha1:a3781ff1fc3fb52318f623e22c85d63d74c12932"

	tests := []cacheTest{
		{
			"strings.Reader",
			strings.NewReader(content),
			false,
		},
		{
			"diskbuffer.Buffer",
			func() io.Reader { d := diskbuffer.New(); _, _ = d.WriteString(content); return d }(),
			false,
		},
		{
			"iotest.HalfReader",
			iotest.HalfReader(strings.NewReader(content)),
			false,
		},
		{
			"ReplaceErrReader",
			ReplaceErrReader(strings.NewReader(content), io.ErrUnexpectedEOF),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, val, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, tt.data, blockDigest, pDigest)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := val; len(got) != 0 {
				t.Fatalf("val = %v, want empty", got)
			}

			validateCacheTest(t, block, content, digest, tt.wantCacheErr)
		})
	}
}

func Test_httpRequestBlock_IsCached(t *testing.T) {
	content := "GET / HTTP/1.0\n" +
		"Host: example.com\n" +
		"Accept-Language: en-US,en;q=0.8,ru;q=0.6\n" +
		"Referer: http://example.com/foo.html\n" +
		"Connection: close\n" +
		"User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/56.0.2924.87 Safari/537.36\n\n"

	tests := []isCachedTest{
		{
			"strings.Reader",
			strings.NewReader(content),
			false,
		},
		{
			"diskbuffer.Buffer",
			func() io.Reader { d := diskbuffer.New(); _, _ = d.WriteString(content); return d }(),
			true,
		},
		{
			"iotest.HalfReader",
			iotest.HalfReader(strings.NewReader(content)),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, val, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, tt.data, blockDigest, pDigest)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := val; len(got) != 0 {
				t.Fatalf("val = %v, want empty", got)
			}

			got := block.IsCached()
			if got, want := got, tt.want; got != want {
				t.Errorf("got = %#v, want %#v", got, want)
			}
		})
	}
}

func Test_httpRequestBlock_RawBytes(t *testing.T) {
	content := "GET / HTTP/1.0\n" +
		"Host: example.com\n" +
		"Accept-Language: en-US,en;q=0.8,ru;q=0.6\n" +
		"Referer: http://example.com/foo.html\n" +
		"Connection: close\n" +
		"User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/56.0.2924.87 Safari/537.36\n\n"
	digest := "sha1:a3781ff1fc3fb52318f623e22c85d63d74c12932"

	tests := []rawBytesTest{
		{
			"strings.Reader",
			strings.NewReader(content),
			false,
		},
		{
			"diskbuffer.Buffer",
			func() io.Reader { d := diskbuffer.New(); _, _ = d.WriteString(content); return d }(),
			false,
		},
		{
			"iotest.HalfReader",
			iotest.HalfReader(strings.NewReader(content)),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, val, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, tt.data, blockDigest, pDigest)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := val; len(got) != 0 {
				t.Fatalf("val = %v, want empty", got)
			}

			validateRawBytesTest(t, tt, block, content, digest)
		})
	}
}

func Test_httpResponseBlock_BlockDigest(t *testing.T) {
	content := "HTTP/1.1 200 OK\nDate: Tue, 19 Sep 2016 17:18:40 GMT\nServer: Apache/2.0.54 (Ubuntu)\n" +
		"Last-Modified: Mon, 16 Jun 2013 22:28:51 GMT\nETag: \"3e45-67e-2ed02ec0\"\nAccept-Ranges: bytes\n" +
		"Content-Length: 19\nConnection: close\nContent-Type: text/plain\n\nThis is the content"
	digest := "sha1:b285747ad7cc57aa74bce2e30b453c8d1cb71ba4"
	payloadDigest := "sha1:c37ffb221569c553a2476c22c7dad429f3492977"

	tests := []blockDigestTest{
		{
			"strings.Reader",
			strings.NewReader(content),
		},
		{
			"diskbuffer.Buffer",
			func() io.Reader { d := diskbuffer.New(); _, _ = d.WriteString(content); return d }(),
		},
		{
			"iotest.HalfReader",
			iotest.HalfReader(strings.NewReader(content)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, val, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, tt.data, blockDigest, pDigest)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := val; len(got) != 0 {
				t.Fatalf("val = %v, want empty", got)
			}

			validateBlockDigestTest(t, block, digest)
			validatePayloadDigestTest(t, block, payloadDigest)
		})
	}
}

func Test_httpResponseBlock_Cache(t *testing.T) {
	content := "HTTP/1.1 200 OK\nDate: Tue, 19 Sep 2016 17:18:40 GMT\nServer: Apache/2.0.54 (Ubuntu)\n" +
		"Last-Modified: Mon, 16 Jun 2013 22:28:51 GMT\nETag: \"3e45-67e-2ed02ec0\"\nAccept-Ranges: bytes\n" +
		"Content-Length: 19\nConnection: close\nContent-Type: text/plain\n\nThis is the content"
	digest := "sha1:b285747ad7cc57aa74bce2e30b453c8d1cb71ba4"

	tests := []cacheTest{
		{
			"strings.Reader",
			strings.NewReader(content),
			false,
		},
		{
			"diskbuffer.Buffer",
			func() io.Reader { d := diskbuffer.New(); _, _ = d.WriteString(content); return d }(),
			false,
		},
		{
			"iotest.HalfReader",
			iotest.HalfReader(strings.NewReader(content)),
			false,
		},
		{
			"ReplaceErrReader",
			ReplaceErrReader(strings.NewReader(content), io.ErrUnexpectedEOF),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, val, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, tt.data, blockDigest, pDigest)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := val; len(got) != 0 {
				t.Fatalf("val = %v, want empty", got)
			}

			validateCacheTest(t, block, content, digest, tt.wantCacheErr)
		})
	}
}

func Test_httpResponseBlock_IsCached(t *testing.T) {
	content := "HTTP/1.1 200 OK\nDate: Tue, 19 Sep 2016 17:18:40 GMT\nServer: Apache/2.0.54 (Ubuntu)\n" +
		"Last-Modified: Mon, 16 Jun 2013 22:28:51 GMT\nETag: \"3e45-67e-2ed02ec0\"\nAccept-Ranges: bytes\n" +
		"Content-Length: 19\nConnection: close\nContent-Type: text/plain\n\nThis is the content"

	tests := []isCachedTest{
		{
			"strings.Reader",
			strings.NewReader(content),
			false,
		},
		{
			"diskbuffer.Buffer",
			func() io.Reader { d := diskbuffer.New(); _, _ = d.WriteString(content); return d }(),
			true,
		},
		{
			"iotest.HalfReader",
			iotest.HalfReader(strings.NewReader(content)),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, val, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, tt.data, blockDigest, pDigest)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := val; len(got) != 0 {
				t.Fatalf("val = %v, want empty", got)
			}

			got := block.IsCached()
			if got, want := got, tt.want; got != want {
				t.Errorf("got = %#v, want %#v", got, want)
			}
		})
	}
}

func Test_httpResponseBlock_RawBytes(t *testing.T) {
	content := "HTTP/1.1 200 OK\nDate: Tue, 19 Sep 2016 17:18:40 GMT\nServer: Apache/2.0.54 (Ubuntu)\n" +
		"Last-Modified: Mon, 16 Jun 2013 22:28:51 GMT\nETag: \"3e45-67e-2ed02ec0\"\nAccept-Ranges: bytes\n" +
		"Content-Length: 19\nConnection: close\nContent-Type: text/plain\n\nThis is the content"
	digest := "sha1:b285747ad7cc57aa74bce2e30b453c8d1cb71ba4"

	tests := []rawBytesTest{
		{
			"strings.Reader",
			strings.NewReader(content),
			false,
		},
		{
			"diskbuffer.Buffer",
			func() io.Reader { d := diskbuffer.New(); _, _ = d.WriteString(content); return d }(),
			false,
		},
		{
			"iotest.HalfReader",
			iotest.HalfReader(strings.NewReader(content)),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, val, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, tt.data, blockDigest, pDigest)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := val; len(got) != 0 {
				t.Fatalf("val = %v, want empty", got)
			}

			validateRawBytesTest(t, tt, block, content, digest)
		})
	}
}

func Test_httpResponseBlock_Accessors(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		wantStatusLine string
		wantStatusCode int
		wantHeaders    map[string]string
	}{
		{
			name:           "200 OK response",
			content:        "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 5\r\n\r\nHello",
			wantStatusLine: "200 OK",
			wantStatusCode: 200,
			wantHeaders:    map[string]string{"Content-Type": "text/plain", "Content-Length": "5"},
		},
		{
			name:           "404 Not Found response",
			content:        "HTTP/1.1 404 Not Found\r\nContent-Type: text/html\r\nContent-Length: 0\r\n\r\n",
			wantStatusLine: "404 Not Found",
			wantStatusCode: 404,
			wantHeaders:    map[string]string{"Content-Type": "text/html", "Content-Length": "0"},
		},
		{
			name:           "301 redirect",
			content:        "HTTP/1.1 301 Moved Permanently\r\nLocation: http://example.com/new\r\nContent-Length: 0\r\n\r\n",
			wantStatusLine: "301 Moved Permanently",
			wantStatusCode: 301,
			wantHeaders:    map[string]string{"Location": "http://example.com/new"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, strings.NewReader(tt.content), blockDigest, pDigest)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			respBlock, ok := block.(HttpResponseBlock)
			if got := ok; !got {
				t.Fatalf("ok = %v, want true: %s", got, "expected HttpResponseBlock")
			}

			if got, want := respBlock.HttpStatusLine(), tt.wantStatusLine; got != want {
				t.Errorf("respBlock.HttpStatusLine() = %#v, want %#v", got, want)
			}
			if got, want := respBlock.HttpStatusCode(), tt.wantStatusCode; got != want {
				t.Errorf("respBlock.HttpStatusCode() = %#v, want %#v", got, want)
			}

			hdr := respBlock.HttpHeader()
			if got := hdr; got == nil {
				t.Fatalf("hdr = %v, want non-nil", got)
			}
			for key, wantVal := range tt.wantHeaders {
				if got, want := hdr.Get(key), wantVal; got != want {
					t.Errorf("hdr.Get(key) = %#v, want %#v", got, want)
				}
			}

			if got := respBlock.ProtocolHeaderBytes(); len(got) == 0 {
				t.Errorf("respBlock.ProtocolHeaderBytes() = %v, want nonempty", got)
			}
		})
	}
}

func Test_httpRequestBlock_Accessors(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		wantRequestLine string
		wantHeaders     map[string]string
	}{
		{
			name:            "simple GET",
			content:         "GET /index.html HTTP/1.1\r\nHost: example.com\r\n\r\n",
			wantRequestLine: "/index.html",
			wantHeaders:     map[string]string{},
		},
		{
			name:            "POST with content-type",
			content:         "POST /api/data HTTP/1.1\r\nHost: api.example.com\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}",
			wantRequestLine: "/api/data",
			wantHeaders:     map[string]string{"Content-Type": "application/json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			pDigest, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{
				&nameValue{Name: ContentType, Value: "application/http;msgtype=request"},
			}, strings.NewReader(tt.content), blockDigest, pDigest)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			reqBlock, ok := block.(HttpRequestBlock)
			if got := ok; !got {
				t.Fatalf("ok = %v, want true: %s", got, "expected HttpRequestBlock")
			}

			if got, want := reqBlock.HttpRequestLine(), tt.wantRequestLine; got != want {
				t.Errorf("reqBlock.HttpRequestLine() = %#v, want %#v", got, want)
			}

			hdr := reqBlock.HttpHeader()
			if got := hdr; got == nil {
				t.Fatalf("hdr = %v, want non-nil", got)
			}
			for key, wantVal := range tt.wantHeaders {
				if got, want := hdr.Get(key), wantVal; got != want {
					t.Errorf("hdr.Get(key) = %#v, want %#v", got, want)
				}
			}

			if got := reqBlock.ProtocolHeaderBytes(); len(got) == 0 {
				t.Errorf("reqBlock.ProtocolHeaderBytes() = %v, want nonempty", got)
			}
		})
	}
}

func Test_httpResponseBlock_Write(t *testing.T) {
	content := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 5\r\n\r\nHello"

	blockDigest, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pDigest, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := block.Cache(); err != nil {
		t.Fatalf("block.Cache() error = %v", err)
	}

	var buf bytes.Buffer
	respBlock := block.(*httpResponseBlock)
	n, err := respBlock.Write(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, bound := n, int64(0); got <= bound {
		t.Errorf("n = %v, want > %v", got, bound)
	}
	if got, want := buf.String(), "HTTP/1.1 200 OK"; !strings.Contains(got, want) {
		t.Errorf("buf.String() = %v, want to contain %v", got, want)
	}
	if got, want := buf.String(), "Hello"; !strings.Contains(got, want) {
		t.Errorf("buf.String() = %v, want to contain %v", got, want)
	}
}

func Test_httpRequestBlock_Write(t *testing.T) {
	content := "GET /index.html HTTP/1.1\r\nHost: example.com\r\n\r\n"

	blockDigest, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pDigest, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{
		&nameValue{Name: ContentType, Value: "application/http;msgtype=request"},
	}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := block.Cache(); err != nil {
		t.Fatalf("block.Cache() error = %v", err)
	}

	var buf bytes.Buffer
	reqBlock := block.(*httpRequestBlock)
	n, err := reqBlock.Write(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, bound := n, int64(0); got <= bound {
		t.Errorf("n = %v, want > %v", got, bound)
	}
	if got, want := buf.String(), "GET /index.html HTTP/1.1"; !strings.Contains(got, want) {
		t.Errorf("buf.String() = %v, want to contain %v", got, want)
	}
	if got, want := buf.String(), "Host: example.com"; !strings.Contains(got, want) {
		t.Errorf("buf.String() = %v, want to contain %v", got, want)
	}
}

func Test_newHttpBlock_EmptyContent(t *testing.T) {
	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	_, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, strings.NewReader(""), blockDigest, pDigest)
	if err == nil {
		t.Fatalf("err: expected an error")
	}
	if got, want := err.Error(), "not a http block"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %v, want to contain %v", got, want)
	}
}

func Test_newHttpBlock_MissingEndOfHeaders_ErrWarn(t *testing.T) {
	// Content with HTTP header but no blank line separator
	content := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	block, val, err := newHttpBlock(&warcRecordOptions{errSyntax: ErrWarn}, &WarcFields{
		&nameValue{Name: ContentLength, Value: "44"},
	}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
	if got := val; len(got) == 0 {
		t.Errorf("val = %v, want nonempty", got)
	}
}

func Test_newHttpBlock_MissingEndOfHeaders_ErrFail(t *testing.T) {
	content := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	_, _, err := newHttpBlock(&warcRecordOptions{errSyntax: ErrFail}, &WarcFields{
		&nameValue{Name: ContentLength, Value: "44"},
	}, strings.NewReader(content), blockDigest, pDigest)
	if err == nil {
		t.Fatalf("err: expected an error")
	}
	if got, want := err.Error(), "missing line separator"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %v, want to contain %v", got, want)
	}
}

func Test_newHttpBlock_MissingEndOfHeaders_FixSyntax(t *testing.T) {
	content := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)

	wf := &WarcFields{
		&nameValue{Name: ContentLength, Value: "44"},
	}

	block, _, err := newHttpBlock(&warcRecordOptions{fixSyntaxErrors: true}, wf, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
	// Content-Length should be updated (+2 for added CRLF)
	length, _ := wf.GetInt64(ContentLength)
	if got, want := length, int64(46); got != want {
		t.Errorf("length = %#v, want %#v", got, want)
	}
}

func Test_newHttpBlock_MalformedHTTP_ErrBlockWarn(t *testing.T) {
	content := "HTTP/INVALID RESPONSE\r\n\r\n"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	block, val, err := newHttpBlock(&warcRecordOptions{errBlock: ErrWarn}, &WarcFields{}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
	if got := val; len(got) == 0 {
		t.Errorf("val = %v, want nonempty", got)
	}
}

func Test_newHttpBlock_MalformedHTTP_ErrBlockFail(t *testing.T) {
	content := "HTTP/INVALID RESPONSE\r\n\r\n"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	_, _, err := newHttpBlock(&warcRecordOptions{errBlock: ErrFail}, &WarcFields{}, strings.NewReader(content), blockDigest, pDigest)
	if err == nil {
		t.Fatalf("err: expected an error")
	}
	if got, want := err.Error(), "error in http response block"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %v, want to contain %v", got, want)
	}
}

func Test_newHttpBlock_MalformedRequest_ErrBlockWarn(t *testing.T) {
	content := "INVALID REQUEST\r\n\r\n"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	block, val, err := newHttpBlock(&warcRecordOptions{errBlock: ErrWarn}, &WarcFields{}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
	if got := val; len(got) == 0 {
		t.Errorf("val = %v, want nonempty", got)
	}
}

func Test_newHttpBlock_MalformedRequest_ErrBlockFail(t *testing.T) {
	content := "INVALID REQUEST\r\n\r\n"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	_, _, err := newHttpBlock(&warcRecordOptions{errBlock: ErrFail}, &WarcFields{}, strings.NewReader(content), blockDigest, pDigest)
	if err == nil {
		t.Fatalf("err: expected an error")
	}
	if got, want := err.Error(), "error in http request block"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %v, want to contain %v", got, want)
	}
}

func Test_newHttpBlock_RequestMissingEndOfHeaders_NoFixSyntax(t *testing.T) {
	content := "GET / HTTP/1.1\r\nHost: example.com\r\n"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	block, _, err := newHttpBlock(&warcRecordOptions{fixSyntaxErrors: false}, &WarcFields{
		&nameValue{Name: ContentLength, Value: "35"},
	}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
	// Parser adds CRLF internally for parsing even without fixSyntaxErrors
	reqBlock := block.(*httpRequestBlock)
	if got := reqBlock.HttpRequestLine(); len(got) == 0 {
		t.Errorf("reqBlock.HttpRequestLine() = %v, want nonempty", got)
	}
}

func Test_newHttpBlock_WithDiskBuffer(t *testing.T) {
	content := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nHello"
	buf := diskbuffer.New()
	_, err := buf.WriteString(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, buf, blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
	if got := block.IsCached(); !got {
		t.Errorf("block.IsCached() = %v, want true", got)
	}
}

func Test_httpResponseBlock_PayloadBytes_NonCached(t *testing.T) {
	content := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nHello"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First call to PayloadBytes should work
	r, err := block.PayloadBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _ = io.ReadAll(r)

	// Second call on non-cached block should fail
	_, err = block.PayloadBytes()
	if err == nil {
		t.Fatalf("err: expected an error")
	}
	if got, want := err, errContentReAccessed; !errors.Is(got, want) {
		t.Errorf("err = %#v, want %#v", got, want)
	}
}

func Test_httpResponseBlock_Close_NonCloser(t *testing.T) {
	content := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nHello"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// strings.Reader payload doesn't implement io.Closer
	err = block.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func Test_httpResponseBlock_Cache_NonCached(t *testing.T) {
	content := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nHello"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := block.IsCached(); got {
		t.Errorf("block.IsCached() = %v, want false", got)
	}
	if err := block.Cache(); err != nil {
		t.Fatalf("block.Cache() error = %v", err)
	}
	if got := block.IsCached(); !got {
		t.Errorf("block.IsCached() = %v, want true", got)
	}

	// PayloadBytes should work after caching
	r, err := block.PayloadBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(data), "Hello"; got != want {
		t.Errorf("string(data) = %#v, want %#v", got, want)
	}
}

func Test_httpResponseBlock_Write_Error(t *testing.T) {
	content := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nHello"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := block.Cache(); err != nil {
		t.Fatalf("block.Cache() error = %v", err)
	}

	respBlock := block.(*httpResponseBlock)
	w := &failWriter{err: io.ErrClosedPipe}
	_, err = respBlock.Write(w)
	if err == nil {
		t.Errorf("err: expected an error")
	}
}

type failWriter struct {
	err error
}

func (w *failWriter) Write(p []byte) (int, error) {
	return 0, w.err
}

func Test_httpRequestBlock_Write_Error(t *testing.T) {
	content := "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"

	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)
	block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{
		&nameValue{Name: ContentType, Value: "application/http;msgtype=request"},
	}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := block.Cache(); err != nil {
		t.Fatalf("block.Cache() error = %v", err)
	}

	reqBlock := block.(*httpRequestBlock)
	w := &failWriter{err: io.ErrClosedPipe}
	_, err = reqBlock.Write(w)
	if err == nil {
		t.Errorf("err: expected an error")
	}
}

func Test_httpResponseBlock_Cache_PayloadBytesError(t *testing.T) {
	// Exercise the Cache() error path where PayloadBytes() returns errContentReAccessed
	content := "HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ntest"
	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)

	block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{
		&nameValue{Name: ContentType, Value: "application/http;msgtype=response"},
	}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	respBlock := block.(*httpResponseBlock)
	// First call to PayloadBytes() consumes the filter reader
	r, err := respBlock.PayloadBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _ = io.ReadAll(r)

	// Now Cache() should fail because PayloadBytes() returns errContentReAccessed
	err = respBlock.Cache()
	if err == nil {
		t.Errorf("err: expected an error")
	}
	if got, want := err, errContentReAccessed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

type failPayloadSeeker struct {
	*strings.Reader
	seekErr error
}

func (f *failPayloadSeeker) Seek(offset int64, whence int) (int64, error) {
	return 0, f.seekErr
}

func Test_httpResponseBlock_PayloadBytes_SeekError(t *testing.T) {
	// Exercise the Seek error path in PayloadBytes() for a cached block
	content := "HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ntest"
	blockDigest, _ := newDigest("sha1", Base16)
	pDigest, _ := newDigest("sha1", Base16)

	block, _, err := newHttpBlock(&warcRecordOptions{}, &WarcFields{
		&nameValue{Name: ContentType, Value: "application/http;msgtype=response"},
	}, strings.NewReader(content), blockDigest, pDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	respBlock := block.(*httpResponseBlock)
	// Replace payload with a failing seeker
	fs := &failPayloadSeeker{Reader: strings.NewReader("test"), seekErr: fmt.Errorf("seek failed")}
	respBlock.payload = fs

	// First PayloadBytes() call creates filter reader
	r, err := respBlock.PayloadBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _ = io.ReadAll(r)

	// Second call should try to Seek and fail
	_, err = respBlock.PayloadBytes()
	if err == nil {
		t.Errorf("err: expected an error")
	}
	if got, want := err.Error(), "seek failed"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %v, want to contain %v", got, want)
	}
}

func Test_httpResponseBlock_Write_RawBytesError(t *testing.T) {
	// Exercise httpResponseBlock.Write when RawBytes() fails (uncached, re-accessed block).
	// Use the Unmarshaler with a non-seekable reader to get an uncached block.
	rawWARC := "WARC/1.1\r\n" +
		"WARC-Type: response\r\n" +
		"WARC-Target-URI: http://example.com\r\n" +
		"WARC-Date: 2016-09-19T18:03:53Z\r\n" +
		"WARC-Record-ID: <urn:uuid:fff0cecc-0221-11e7-adb1-0242ac120008>\r\n" +
		"Content-Type: application/http;msgtype=response\r\n" +
		"Content-Length: 42\r\n" +
		"\r\n" +
		"HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ntest" +
		"\r\n\r\n"

	u := NewUnmarshaler(WithSpecViolationPolicy(ErrIgnore), WithSyntaxErrorPolicy(ErrIgnore))
	rec, _, _, err := u.Unmarshal(bufio.NewReader(io.NopCloser(strings.NewReader(rawWARC))))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = rec.Close() }()

	block := rec.Block().(PayloadBlock)

	// Consume the payload — since the underlying reader is not seekable, this makes it non-reaccesible
	r, err := block.PayloadBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _ = io.ReadAll(r)

	// Now RawBytes() should fail because the block is not cached
	_, err = block.RawBytes()
	if err == nil {
		t.Errorf("err: expected an error")
	}
}

func Test_httpRequestBlock_Write_RawBytesError(t *testing.T) {
	// Exercise httpRequestBlock.Write when RawBytes() fails (uncached, re-accessed block).
	rawWARC := "WARC/1.1\r\n" +
		"WARC-Type: request\r\n" +
		"WARC-Target-URI: http://example.com\r\n" +
		"WARC-Date: 2016-09-19T18:03:53Z\r\n" +
		"WARC-Record-ID: <urn:uuid:fff0cecc-0221-11e7-adb1-0242ac120008>\r\n" +
		"Content-Type: application/http;msgtype=request\r\n" +
		"Content-Length: 38\r\n" +
		"\r\n" +
		"GET / HTTP/1.0\r\nHost: example.com\r\n\r\n" +
		"\r\n\r\n"

	u := NewUnmarshaler(WithSpecViolationPolicy(ErrIgnore), WithSyntaxErrorPolicy(ErrIgnore))
	rec, _, _, err := u.Unmarshal(bufio.NewReader(io.NopCloser(strings.NewReader(rawWARC))))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := rec.Close(); err != nil {
			t.Errorf("rec.Close() error = %v", err)
		}
	}()

	block := rec.Block().(PayloadBlock)

	// Consume the payload
	r, err := block.PayloadBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _ = io.ReadAll(r)

	// RawBytes() should fail because the block is not cached
	_, err = block.RawBytes()
	if err == nil {
		t.Errorf("err: expected an error")
	}
}
