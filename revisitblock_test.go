package gowarc

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func Test_revisitBlock_Accessors(t *testing.T) {
	tests := []struct {
		name              string
		content           string
		payloadDigest     string
		wantHeaderBytes   string
		wantPayloadDigest string
	}{
		{
			name:              "response revisit with headers",
			content:           "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\n",
			payloadDigest:     "sha1:da39a3ee5e6b4b0d3255bfef95601890afd80709",
			wantHeaderBytes:   "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\n",
			wantPayloadDigest: "sha1:da39a3ee5e6b4b0d3255bfef95601890afd80709",
		},
		{
			name:              "empty revisit",
			content:           "",
			payloadDigest:     "",
			wantHeaderBytes:   "",
			wantPayloadDigest: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, err := parseRevisitBlock(&warcRecordOptions{defaultDigestAlgorithm: "sha1", defaultDigestEncoding: Base16},
				strings.NewReader(tt.content), d, tt.payloadDigest)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// ProtocolHeaderBytes
			if got, want := string(block.ProtocolHeaderBytes()), tt.wantHeaderBytes; got != want {
				t.Errorf("string(block.ProtocolHeaderBytes()) = %#v, want %#v", got, want)
			}

			// PayloadBytes returns empty reader
			pr, err := block.PayloadBytes()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			payload, err := io.ReadAll(pr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := payload; len(got) != 0 {
				t.Errorf("payload = %v, want empty", got)
			}

			// PayloadDigest
			if got, want := block.PayloadDigest(), tt.wantPayloadDigest; got != want {
				t.Errorf("block.PayloadDigest() = %#v, want %#v", got, want)
			}

			// BlockDigest should be non-empty for non-empty content
			if tt.content != "" {
				if got := block.BlockDigest(); len(got) == 0 {
					t.Errorf("block.BlockDigest() = %v, want nonempty", got)
				}
			}

			// IsCached always true for revisit blocks
			if got := block.IsCached(); !got {
				t.Errorf("block.IsCached() = %v, want true", got)
			}
		})
	}
}

func Test_revisitBlock_Write(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"with http headers", "HTTP/1.1 200 OK\r\nServer: Apache\r\n\r\n"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			block, err := parseRevisitBlock(&warcRecordOptions{defaultDigestAlgorithm: "sha1", defaultDigestEncoding: Base16},
				strings.NewReader(tt.content), d, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var buf bytes.Buffer
			n, err := block.Write(&buf)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got, want := n, int64(len(tt.content)); got != want {
				t.Errorf("n = %#v, want %#v", got, want)
			}
			if got, want := buf.String(), tt.content; got != want {
				t.Errorf("buf.String() = %#v, want %#v", got, want)
			}
		})
	}
}

func Test_revisitBlock_Write_Error(t *testing.T) {
	d, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	block, err := parseRevisitBlock(&warcRecordOptions{defaultDigestAlgorithm: "sha1", defaultDigestEncoding: Base16},
		strings.NewReader("HTTP/1.1 200 OK\r\n\r\n"), d, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w := &failWriter{err: io.ErrClosedPipe}
	n, err := block.Write(w)
	if err == nil {
		t.Errorf("err: expected an error")
	}
	if got, want := n, int64(0); got != want {
		t.Errorf("n = %#v, want %#v", got, want)
	}
}

func Test_newRevisitBlock_FromResponseBlock(t *testing.T) {
	builder := NewRecordBuilder(Response, WithSpecViolationPolicy(ErrIgnore), WithSyntaxErrorPolicy(ErrIgnore))
	_, err := builder.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\ntest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	builder.AddWarcHeader(WarcRecordID, "<urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac120008>")
	builder.AddWarcHeader(WarcDate, "2006-01-02T15:04:05Z")
	builder.AddWarcHeader(ContentType, "application/http;msgtype=response")

	rec, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := rec.Close(); err != nil {
			t.Errorf("rec.Close() error = %v", err)
		}
	}()
	if err := rec.Block().Cache(); err != nil {
		t.Fatalf("rec.Block().Cache() error = %v", err)
	}

	opts := defaultWarcRecordOptions()
	block, err := newRevisitBlock(&opts, rec.Block())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
	if got := block.BlockDigest(); len(got) == 0 {
		t.Errorf("block.BlockDigest() = %v, want nonempty", got)
	}
	if got := block.PayloadDigest(); len(got) == 0 {
		t.Errorf("block.PayloadDigest() = %v, want nonempty", got)
	}
}

func Test_newRevisitBlock_FromRequestBlock(t *testing.T) {
	builder := NewRecordBuilder(Request, WithSpecViolationPolicy(ErrIgnore), WithSyntaxErrorPolicy(ErrIgnore))
	_, err := builder.WriteString("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	builder.AddWarcHeader(WarcRecordID, "<urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac120008>")
	builder.AddWarcHeader(WarcDate, "2006-01-02T15:04:05Z")
	builder.AddWarcHeader(ContentType, "application/http;msgtype=request")

	rec, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := rec.Close(); err != nil {
			t.Errorf("rec.Close() error = %v", err)
		}
	}()
	if err := rec.Block().Cache(); err != nil {
		t.Fatalf("rec.Block().Cache() error = %v", err)
	}

	opts := defaultWarcRecordOptions()
	block, err := newRevisitBlock(&opts, rec.Block())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
	if got := block.ProtocolHeaderBytes(); len(got) == 0 {
		t.Errorf("block.ProtocolHeaderBytes() = %v, want nonempty", got)
	}
}

func Test_newRevisitBlock_FromGenericBlock(t *testing.T) {
	opts := defaultWarcRecordOptions()
	gb := &genericBlock{
		rawBytes: strings.NewReader("some content"),
		opts:     &opts,
	}

	block, err := newRevisitBlock(&opts, gb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
	if got := block.headerBytes; len(got) != 0 {
		t.Errorf("block.headerBytes = %v, want empty", got)
	}
}

func Test_newRevisitBlock_UnsupportedType(t *testing.T) {
	opts := defaultWarcRecordOptions()
	wfb := &warcFieldsBlock{}

	_, err := newRevisitBlock(&opts, wfb)
	if err == nil {
		t.Fatalf("err: expected an error")
	}
	if got, want := err.Error(), "not supported"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %v, want to contain %v", got, want)
	}
}

func Test_parseRevisitBlock_ReadError(t *testing.T) {
	opts := defaultWarcRecordOptions()
	d, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	errReader := ReplaceErrReader(strings.NewReader("header bytes"), io.ErrUnexpectedEOF)
	_, err = parseRevisitBlock(&opts, errReader, d, "sha1:abc")
	if err == nil {
		t.Errorf("err: expected an error")
	}
}
