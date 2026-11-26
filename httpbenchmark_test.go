// Copyright 2026 National Library of Norway.
// SPDX-License-Identifier: Apache-2.0

package gowarc

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nationallibraryofnorway/gowarc/v4/internal/diskbuffer"
)

func BenchmarkChunkedDigest(b *testing.B) {
	const size = 8 << 20
	for _, trailer := range []string{"", "X-Result: ok\r\n"} {
		b.Run(fmt.Sprintf("trailers=%t", trailer != ""), func(b *testing.B) {
			raw := diskbuffer.New()
			defer checkClose(b, raw)
			if _, err := fmt.Fprint(raw, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n"); err != nil {
				b.Fatal(err)
			}
			chunk := strings.Repeat("x", 32<<10)
			for i := 0; i < size; i += len(chunk) {
				if _, err := fmt.Fprintf(raw, "%x\r\n%s\r\n", len(chunk), chunk); err != nil {
					b.Fatal(err)
				}
			}
			if _, err := fmt.Fprintf(raw, "0\r\n%s\r\n", trailer); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(size)
			for b.Loop() {
				if _, err := raw.Seek(0, 0); err != nil {
					b.Fatal(err)
				}
				bd, _ := newDigest("sha1", Base32)
				pd, _ := newDigest("sha1", Base32)
				block, _, err := newHttpBlock(newOptions(), &WarcFields{}, raw, bd, pd)
				if err != nil || block.PayloadDigest() == "" {
					b.Fatalf("digest: %v", err)
				}
			}
		})
	}
}
