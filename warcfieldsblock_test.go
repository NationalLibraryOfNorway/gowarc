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
	"bytes"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/nationallibraryofnorway/gowarc/v4/internal/diskbuffer"
)

func Test_warcfieldsBlock_BlockDigest(t *testing.T) {
	content := "foo: bar\r\ncontent-type:bb\r\n"
	digest := "sha1:a1d43d400c5985bee035c4e5a2e08f3d57989596"

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

			o := defaultWarcRecordOptions()
			block, validation, err := newWarcFieldsBlock(&o, &WarcFields{}, tt.data, d)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := validation; len(got) != 0 {
				t.Fatalf("validation = %v, want empty", got)
			}

			validateBlockDigestTest(t, block, digest)
		})
	}
}

func Test_warcfieldsBlock_Cache(t *testing.T) {
	content := "foo: bar\r\ncontent-type:bb\r\n"
	digest := "sha1:a1d43d400c5985bee035c4e5a2e08f3d57989596"

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

			o := defaultWarcRecordOptions()
			block, validation, err := newWarcFieldsBlock(&o, &WarcFields{}, tt.data, d)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantCacheErr {
				if got := validation; len(got) == 0 {
					t.Fatalf("validation = %v, want nonempty", got)
				}
			} else {
				if got := validation; len(got) != 0 {
					t.Fatalf("validation = %v, want empty", got)
				}
			}

			validateCacheTest(t, block, content, digest, false)
		})
	}
}

func Test_warcfieldsBlock_IsCached(t *testing.T) {
	content := "foo: bar\r\ncontent-type:bb\r\n"

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
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			o := defaultWarcRecordOptions()
			block, validation, err := newWarcFieldsBlock(&o, &WarcFields{}, tt.data, d)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := validation; len(got) != 0 {
				t.Fatalf("validation = %v, want empty", got)
			}

			got := block.IsCached()
			if got, want := got, tt.want; got != want {
				t.Errorf("got = %#v, want %#v", got, want)
			}
		})
	}
}

func Test_warcfieldsBlock_RawBytes(t *testing.T) {
	content := "foo: bar\r\ncontent-type:bb\r\n"
	digest := "sha1:a1d43d400c5985bee035c4e5a2e08f3d57989596"

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

			o := defaultWarcRecordOptions()
			block, validation, err := newWarcFieldsBlock(&o, &WarcFields{}, tt.data, d)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := validation; len(got) != 0 {
				t.Fatalf("validation = %v, want empty", got)
			}

			validateRawBytesTest(t, tt, block, content, digest)
		})
	}
}

func Test_warcfieldsBlock_Write(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantContain []string
	}{
		{
			name:        "single field",
			content:     "software: Heritrix\r\n",
			wantContain: []string{"Software: Heritrix"},
		},
		{
			name:        "multiple fields",
			content:     "software: Heritrix\r\nformat: WARC File Format 1.1\r\n",
			wantContain: []string{"Software: Heritrix", "Format: WARC File Format 1.1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := newDigest("sha1", Base16)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			o := defaultWarcRecordOptions()
			block, _, err := newWarcFieldsBlock(&o, &WarcFields{}, strings.NewReader(tt.content), d)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			wfblock := block.(*warcFieldsBlock)
			var buf bytes.Buffer
			n, err := wfblock.Write(&buf)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got, bound := n, int64(0); got <= bound {
				t.Errorf("n = %v, want > %v", got, bound)
			}

			output := buf.String()
			for _, want := range tt.wantContain {
				if got, want := output, want; !strings.Contains(got, want) {
					t.Errorf("output = %v, want to contain %v", got, want)
				}
			}
			// Write should end with CRLF
			if got := strings.HasSuffix(output, "\r\n"); !got {
				t.Errorf("strings.HasSuffix(output, \"\\r\\n\") = %v, want true", got)
			}
		})
	}
}

func Test_newWarcFieldsBlock_ReadError_ErrFail(t *testing.T) {
	d, _ := newDigest("sha1", Base16)

	opts := &warcRecordOptions{errSyntax: ErrFail}

	_, _, err := newWarcFieldsBlock(opts, &WarcFields{}, iotest.ErrReader(io.ErrUnexpectedEOF), d)
	if err == nil {
		t.Fatalf("err: expected an error")
	}
}

func Test_newWarcFieldsBlock_ReadError_ErrWarn(t *testing.T) {
	d, _ := newDigest("sha1", Base16)

	opts := &warcRecordOptions{errSyntax: ErrWarn}

	block, validation, err := newWarcFieldsBlock(opts, &WarcFields{}, iotest.ErrReader(io.ErrUnexpectedEOF), d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
	if got := validation; len(got) == 0 {
		t.Errorf("validation = %v, want nonempty", got)
	}
}

func Test_newWarcFieldsBlock_BlockValidationError_ErrBlockFail(t *testing.T) {
	d, _ := newDigest("sha1", Base16)

	opts := &warcRecordOptions{errBlock: ErrFail, errSyntax: ErrWarn}

	// Malformed warc fields content - errSyntax=ErrWarn puts error in blockValidation, errBlock=ErrFail returns it
	_, _, err := newWarcFieldsBlock(opts, &WarcFields{}, strings.NewReader("invalid-no-colon-line\r\n"), d)
	if err == nil {
		t.Fatalf("err: expected an error")
	}
	if got, want := err.Error(), "warc fields block"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %v, want to contain %v", got, want)
	}
}

func Test_newWarcFieldsBlock_BlockValidationError_ErrBlockWarn(t *testing.T) {
	d, _ := newDigest("sha1", Base16)

	opts := &warcRecordOptions{errBlock: ErrWarn, errSyntax: ErrWarn}

	block, validation, err := newWarcFieldsBlock(opts, &WarcFields{}, strings.NewReader("invalid-no-colon-line\r\n"), d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
	if got := validation; len(got) == 0 {
		t.Errorf("validation = %v, want nonempty", got)
	}
}

func Test_newWarcFieldsBlock_FixWarcFieldsBlockErrors(t *testing.T) {
	d, _ := newDigest("sha1", Base16)

	opts := &warcRecordOptions{fixWarcFieldsBlockErrors: true}

	// Even with invalid content, fix should not error out
	block, _, err := newWarcFieldsBlock(opts, &WarcFields{}, strings.NewReader("software: test\r\n"), d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := block; got == nil {
		t.Errorf("block = %v, want non-nil", got)
	}
}

func Test_warcfieldsBlock_Write_Error(t *testing.T) {
	d, _ := newDigest("sha1", Base16)
	o := defaultWarcRecordOptions()

	block, _, err := newWarcFieldsBlock(&o, &WarcFields{}, strings.NewReader("name: value\r\n"), d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wfblock := block.(*warcFieldsBlock)
	w := &failWriterAt{failAfter: 0, err: io.ErrClosedPipe}
	_, err = wfblock.Write(w)
	if err == nil {
		t.Errorf("err: expected an error")
	}
}
