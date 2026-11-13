/*
 * Copyright 2021 National Library of Norway.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *       http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package gowarc

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/nationallibraryofnorway/gowarc/v3/internal/diskbuffer"
)

func Test_genericBlock_BlockDigest(t *testing.T) {
	content := "foo"
	digest := "sha1:0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33"

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
			d, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			block := newGenericBlock(&warcRecordOptions{}, tt.data, d)

			validateBlockDigestTest(t, block, digest)
		})
	}
}

func Test_genericBlock_Cache(t *testing.T) {
	content := "foo"
	digest := "sha1:0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33"

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
			d, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			block := newGenericBlock(&warcRecordOptions{}, tt.data, d)

			validateCacheTest(t, block, content, digest, tt.wantCacheErr)
		})
	}
}

func Test_genericBlock_IsCached(t *testing.T) {
	content := "foo"

	tests := []isCachedTest{
		{
			"strings.Reader",
			strings.NewReader(content),
			true,
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
			d, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			block := newGenericBlock(&warcRecordOptions{}, tt.data, d)

			got := block.IsCached()
			if got, want := got, tt.want; got != want {
				t.Errorf("got = %#v, want %#v", got, want)
			}
		})
	}
}

func Test_genericBlock_RawBytes(t *testing.T) {
	content := "foo"
	digest := "sha1:0beec7b5ea3f0fdbc95d0dd47f3c5bc275da8a33"

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
			d, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			block := newGenericBlock(&warcRecordOptions{}, tt.data, d)

			validateRawBytesTest(t, tt, block, content, digest)
		})
	}
}

func Test_genericBlock_RawBytes_NonCached_ReAccess(t *testing.T) {
	d, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Use strings.Reader which is NOT a Seeker (wrapped)
	block := newGenericBlock(&warcRecordOptions{}, iotest.HalfReader(strings.NewReader("foo")), d)

	r, err := block.RawBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _ = io.ReadAll(r)

	// Second access on non-cached block should fail
	_, err = block.RawBytes()
	if err == nil {
		t.Fatalf("err: expected an error")
	}
	if got, want := err, errContentReAccessed; !errors.Is(got, want) {
		t.Errorf("err = %#v, want %#v", got, want)
	}
}

func Test_genericBlock_RawBytes_Cached_ReAccess(t *testing.T) {
	d, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	block := newGenericBlock(&warcRecordOptions{}, strings.NewReader("foo"), d)

	if err := block.Cache(); err != nil {
		t.Fatalf("block.Cache() error = %v", err)
	}
	if got := block.IsCached(); !got {
		t.Errorf("block.IsCached() = %v, want true", got)
	}

	r1, err := block.RawBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data1, _ := io.ReadAll(r1)

	// Re-access on cached block should succeed
	r2, err := block.RawBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data2, _ := io.ReadAll(r2)
	if got, want := string(data2), string(data1); got != want {
		t.Errorf("string(data2) = %#v, want %#v", got, want)
	}
}

func Test_genericBlock_Close_NonCloser(t *testing.T) {
	d, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// strings.Reader does not implement io.Closer
	block := newGenericBlock(&warcRecordOptions{}, strings.NewReader("foo"), d)

	err = block.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func Test_genericBlock_Close_Closer(t *testing.T) {
	d, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	buf := diskbuffer.New()
	_, _ = buf.WriteString("foo")
	block := newGenericBlock(&warcRecordOptions{}, buf, d)

	err = block.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func Test_genericBlock_Cache_AlreadyCached(t *testing.T) {
	d, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	buf := diskbuffer.New()
	_, _ = buf.WriteString("foo")
	block := newGenericBlock(&warcRecordOptions{}, buf, d)

	if got := block.IsCached(); !got {
		t.Errorf("block.IsCached() = %v, want true", got)
	}
	if err := block.Cache(); err != nil {
		t.Fatalf("block.Cache() error = %v", err)
	}
}

func Test_genericBlock_Size(t *testing.T) {
	d, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	block := newGenericBlock(&warcRecordOptions{}, strings.NewReader("hello"), d)

	if got, want := block.Size(), int64(5); got != want {
		t.Errorf("block.Size() = %#v, want %#v", got, want)
	}
}

// pipeReader is a simple reader that does NOT implement io.Seeker.
type pipeReader struct {
	data []byte
	pos  int
}

func (p *pipeReader) Read(b []byte) (int, error) {
	if p.pos >= len(p.data) {
		return 0, io.EOF
	}
	n := copy(b, p.data[p.pos:])
	p.pos += n
	return n, nil
}

func Test_genericBlock_Cache_RawBytesError(t *testing.T) {
	// Exercise the Cache() error path where RawBytes() fails.
	// Use a non-seekable reader so IsCached() returns false.
	d, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	block := newGenericBlock(&warcRecordOptions{}, &pipeReader{data: []byte("hello")}, d)

	if got := block.IsCached(); got {
		t.Errorf("block.IsCached() = %v, want false", got)
	}

	// First RawBytes() succeeds (creates and returns filter reader)
	r, err := block.RawBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _ = io.ReadAll(r)

	// Now calling Cache() should fail because RawBytes() returns errContentReAccessed
	err = block.Cache()
	if err == nil {
		t.Errorf("err: expected an error")
	}
	if got, want := err, errContentReAccessed; !errors.Is(got, want) {
		t.Errorf("err = %v, want an error matching %v", got, want)
	}
}

type failSeeker struct {
	*strings.Reader
	seekErr error
}

func (f *failSeeker) Seek(offset int64, whence int) (int64, error) {
	return 0, f.seekErr
}

func Test_genericBlock_RawBytes_SeekError(t *testing.T) {
	// Exercise the Seek error path in RawBytes() for a cached (seekable) block
	d, err := newDigest("sha1", Base16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fs := &failSeeker{Reader: strings.NewReader("hello"), seekErr: fmt.Errorf("seek failed")}
	block := &genericBlock{
		rawBytes:    fs,
		blockDigest: d,
		opts:        &warcRecordOptions{},
	}

	// First call succeeds (creates filterReader)
	r, err := block.RawBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _ = io.ReadAll(r)

	// Second call attempts Seek, which fails
	_, err = block.RawBytes()
	if err == nil {
		t.Errorf("err: expected an error")
	}
	if got, want := err.Error(), "seek failed"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %v, want to contain %v", got, want)
	}
}
