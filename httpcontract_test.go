// Copyright 2026 National Library of Norway.
// SPDX-License-Identifier: Apache-2.0

package gowarc

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

const helloDigest = "sha1:VL2MMHO4YXUKFWV63YHTWSBM3GXKSQ2N"
const chunkHead = "Transfer-Encoding: chunked\r\nTrailer: X-Result\r\n\r\n"
const fixedHead = "Content-Length: 5\r\n\r\n"

func captureHTTP(t *testing.T, kind RecordType, raw string, fields ...string) WarcRecord {
	t.Helper()
	b := NewRecordBuilder(kind, WithAddMissingDigest(true), WithDefaultDigestAlgorithm("sha1"), WithBufferMaxMemBytes(1))
	b.AddWarcHeader(ContentType, "application/http;msgtype="+kind.String())
	b.AddWarcHeader(WarcDate, "2020-01-02T03:04:05Z")
	b.AddWarcHeader(WarcTargetURI, "https://example.org/")
	for i := 0; i < len(fields); i += 2 {
		b.AddWarcHeader(fields[i], fields[i+1])
	}
	if _, err := b.WriteString(raw); err != nil {
		t.Fatal(err)
	}
	rec, warnings, err := b.Build()
	if err != nil || len(warnings) != 0 {
		t.Fatalf("build: %v, %v", warnings, err)
	}
	t.Cleanup(func() { checkClose(t, rec) })
	return rec
}

func readHTTP(t *testing.T, open func() (io.Reader, error)) string {
	t.Helper()
	r, err := open()
	if err != nil {
		t.Fatal(err)
	}
	p, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(p)
}

func TestHTTPInterop(t *testing.T) {
	const head = "HTTP/1.1 200 OK\r\n"
	digests := map[string]bool{}
	for _, value := range []string{"ok", "changed"} {
		raw := head + chunkHead + "5\r\nhello\r\n0\r\nX-Result: " + value + "\r\n\r\n"
		rec := captureHTTP(t, Response, raw)
		block := rec.Block().(HttpResponseBlock)
		digests[block.BlockDigest()] = true
		if rec.WarcHeader().Get(WarcPayloadDigest) != helloDigest || !block.HasTrailers() || block.PayloadLength() != 5 {
			t.Fatal("wrong digest or trailer metadata")
		}
		ref, err := rec.CreateRevisitRef(ProfileIdenticalPayloadDigestV1_1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rec.ToRevisitRecord(ref); !errors.Is(err, ErrRevisitHasTrailers) {
			t.Fatalf("conversion: %v", err)
		}
		first, err := block.PayloadBytes()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.CopyN(io.Discard, first, 2); err != nil {
			t.Fatal(err)
		}
		if readHTTP(t, block.RawBytes) != raw || readHTTP(t, block.PayloadBytes) != "hello" {
			t.Fatal("reopened reader differs")
		}
		rest, err := io.ReadAll(first)
		if string(rest) != "llo" || err != nil {
			t.Fatalf("remaining entity: %q %v", rest, err)
		}
	}
	if len(digests) != 2 {
		t.Fatal("trailer change must affect block digest")
	}
}

func TestHTTPGzipEntity(t *testing.T) {
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	if _, err := io.WriteString(w, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf("POST / HTTP/1.1\r\nHost: example.org\r\nContent-Encoding: gzip\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n0\r\nX-Undeclared: yes\r\n\r\n", compressed.Len(), compressed.Bytes())
	b := captureHTTP(t, Request, raw).Block().(HttpRequestBlock)
	hash := sha1.Sum(compressed.Bytes())
	if !b.HasTrailers() || b.PayloadDigest() != "sha1:"+base32.StdEncoding.EncodeToString(hash[:]) || readHTTP(t, b.PayloadBytes) != compressed.String() {
		t.Fatal("gzip entity or undeclared trailer lost")
	}
}

func TestRevisitReplay(t *testing.T) {
	for _, kind := range []RecordType{Request, Response} {
		start := "POST / HTTP/1.1\r\nHost: example.org\r\n"
		if kind == Response {
			start = "HTTP/1.1 200 OK\r\n"
		}
		for _, chunked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/chunked=%t", kind, chunked), func(t *testing.T) {
				head, body := fixedHead, "hello"
				donorRaw := start + chunkHead + "1\r\nh\r\n4\r\nello\r\n0\r\nX-Result: old\r\n\r\n"
				if chunked {
					head, body = chunkHead, "5\r\nhello\r\n0\r\n\r\n"
					donorRaw = start + fixedHead + "hello"
				}
				donor := captureHTTP(t, kind, donorRaw)
				capture := captureHTTP(t, kind, start+"X-Capture: new\r\n"+head+body)
				ref, err := donor.CreateRevisitRef(ProfileIdenticalPayloadDigestV1_1)
				if err != nil {
					t.Fatal(err)
				}
				revisit, err := capture.ToRevisitRecord(ref)
				if err != nil {
					t.Fatal(err)
				}
				merged, err := revisit.Merge(donor)
				if err != nil {
					t.Fatal(err)
				}
				defer checkClose(t, merged)
				if revisit.Type() != Revisit || readHTTP(t, donor.Block().RawBytes) != donorRaw {
					t.Fatal("merge changed an input")
				}
				checkClose(t, revisit)
				checkClose(t, donor)
				checkClose(t, capture)
				verifyReplay(t, merged, kind)
			})
		}
	}
}

func verifyReplay(t *testing.T, merged WarcRecord, kind RecordType) {
	t.Helper()
	raw := readHTTP(t, merged.Block().RawBytes)
	reader := bufio.NewReader(strings.NewReader(raw))
	var body io.ReadCloser
	var headers, trailers http.Header
	if kind == Response {
		resp, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatal(err)
		}
		body, headers, trailers = resp.Body, resp.Header, resp.Trailer
	} else {
		req, err := http.ReadRequest(reader)
		if err != nil {
			t.Fatal(err)
		}
		body, headers, trailers = req.Body, req.Header, req.Trailer
	}
	defer checkClose(t, body)
	data, err := io.ReadAll(body)
	if err != nil || string(data) != "hello" || headers.Get("X-Capture") != "new" || trailers.Get("X-Result") != "" {
		t.Fatalf("replay: %q %v %v %v", data, err, headers, trailers)
	}
	h := sha1.Sum([]byte(raw))
	n, err := merged.ContentLength()
	if err != nil || n != int64(len(raw)) || merged.WarcHeader().Get(WarcBlockDigest) != "sha1:"+base32.StdEncoding.EncodeToString(h[:]) || merged.WarcHeader().Get(WarcPayloadDigest) != helloDigest {
		t.Fatal("wrong reconstructed headers")
	}
	if warnings, err := merged.ValidateDigest(); len(warnings) != 0 || err != nil {
		t.Fatalf("validation: %v, %v", warnings, err)
	}
}

func TestHistoricalRevisit(t *testing.T) {
	const start = "HTTP/1.1 200 OK\r\n"
	donor := captureHTTP(t, Response, start+chunkHead+"5\r\nhello\r\n0\r\nX-Result: donor\r\n\r\n")
	for _, head := range []string{chunkHead, "Content-Length: 4\r\n\r\n"} {
		revisit := captureHTTP(t, Revisit, start+"X-Capture: new\r\n"+head, WarcProfile, ProfileIdenticalPayloadDigestV1_1, WarcPayloadDigest, helloDigest, WarcTruncated, "length")
		merged, err := revisit.Merge(donor)
		if head != chunkHead {
			var mismatch *ContentLengthError
			if !errors.As(err, &mismatch) || mismatch.Actual != 5 {
				t.Fatalf("length error: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		defer checkClose(t, merged)
		verifyReplay(t, merged, Response)
	}
}

func TestLegacyPayloadRepair(t *testing.T) {
	const body = "5\r\nhello\r\n0\r\n\r\n"
	hash := sha1.Sum([]byte(body))
	legacy := "sha1:" + base32.StdEncoding.EncodeToString(hash[:])
	for _, repair := range []bool{false, true} {
		b := NewRecordBuilder(Response, WithFixDigest(repair))
		b.AddWarcHeader(WarcTargetURI, "https://example.org/")
		b.AddWarcHeader(WarcDate, "2020-01-02T03:04:05Z")
		b.AddWarcHeader(ContentType, "application/http;msgtype=response")
		b.AddWarcHeader(WarcPayloadDigest, legacy)
		if _, err := b.WriteString("HTTP/1.1 200 OK\r\n" + chunkHead + body); err != nil {
			t.Fatal(err)
		}
		rec, warnings, err := b.Build()
		if err != nil || len(warnings) != 1 {
			t.Fatalf("legacy validation: %v, %v", warnings, err)
		}
		defer checkClose(t, rec)
		want := legacy
		if repair {
			want = helloDigest
		}
		if rec.WarcHeader().Get(WarcPayloadDigest) != want {
			t.Fatal("unexpected digest repair")
		}
	}
}
