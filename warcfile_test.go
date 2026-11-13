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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	uncompressedRecordSize               int64 = 529
	uncompressedRecordWithWarcInfoIdSize int64 = 596
	uncompressedWarcinfoSize             int64 = 250
)

func TestWarcFileWriter_write_cases(t *testing.T) {
	freezeClockAndHost(t)

	type wantWrite struct {
		nRecords      int
		wantBytes     []int64 // per record (uncompressed bytes returned by marshaler)
		wantOffsets   []int64 // only for uncompressed; for compressed we derive dynamically
		wantFileRegex string  // filename returned in WriteResponse.FileName
	}

	type tc struct {
		name         string
		compress     bool
		withWarcinfo bool
		crossRef     bool

		pattern string
		prefix  string

		steps []wantWrite

		// optional: validate crossref mutations on the records for the single step
		checkCrossRef bool
	}

	tests := []tc{
		{
			name:     "uncompressed sequential offsets",
			compress: false,
			pattern:  defaultPattern,
			prefix:   "foo-",
			steps: []wantWrite{
				{
					nRecords:      1,
					wantBytes:     []int64{uncompressedRecordSize},
					wantOffsets:   []int64{0},
					wantFileRegex: `^foo-20010912053020-0001-example\.warc$`,
				},
				{
					nRecords:      1,
					wantBytes:     []int64{uncompressedRecordSize},
					wantOffsets:   []int64{uncompressedRecordSize},
					wantFileRegex: `^foo-20010912053020-0001-example\.warc$`,
				},
			},
		},
		{
			name:     "compressed offsets derived from open file size",
			compress: true,
			pattern:  defaultPattern,
			prefix:   "foo-",
			steps: []wantWrite{
				{
					nRecords:      1,
					wantBytes:     []int64{uncompressedRecordSize},
					wantFileRegex: `^foo-20010912053020-0001-example\.warc\.gz$`,
				},
				{
					nRecords:      1,
					wantBytes:     []int64{uncompressedRecordSize},
					wantFileRegex: `^foo-20010912053020-0001-example\.warc\.gz$`,
				},
			},
		},
		{
			name:         "uncompressed + warcinfo offsets start after warcinfo",
			compress:     false,
			withWarcinfo: true,
			// Use explicit pattern to avoid hostOrIp field entirely, matching your old test style.
			pattern: "%{prefix}s%{ts}s-%04{serial}d-10.10.10.10.warc",
			prefix:  "foo-",
			steps: []wantWrite{
				{
					nRecords:      1,
					wantBytes:     []int64{uncompressedRecordWithWarcInfoIdSize},
					wantOffsets:   []int64{uncompressedWarcinfoSize},
					wantFileRegex: `^foo-20010912053020-0001-10\.10\.10\.10\.warc$`,
				},
				{
					nRecords:      1,
					wantBytes:     []int64{uncompressedRecordWithWarcInfoIdSize},
					wantOffsets:   []int64{uncompressedWarcinfoSize + uncompressedRecordWithWarcInfoIdSize},
					wantFileRegex: `^foo-20010912053020-0001-10\.10\.10\.10\.warc$`,
				},
			},
		},
		{
			name:     "multi-write in one call keeps same file and increments offsets",
			compress: false,
			pattern:  "%{prefix}s%{ts}s-%04{serial}d-10.10.10.10.warc",
			prefix:   "foo-",
			steps: []wantWrite{
				{
					nRecords:      2,
					wantBytes:     []int64{uncompressedRecordSize, uncompressedRecordSize},
					wantOffsets:   []int64{0, uncompressedRecordSize},
					wantFileRegex: `^foo-20010912053020-0001-10\.10\.10\.10\.warc$`,
				},
			},
		},
		{
			name:          "multi-write crossref mutates headers and affects size+offsets",
			compress:      false,
			crossRef:      true,
			checkCrossRef: true,
			pattern:       "%{prefix}s%{ts}s-%04{serial}d-10.10.10.10.warc",
			prefix:        "foo-",
			steps: []wantWrite{
				func() wantWrite {
					overhead := int64(2 * len("WARC-Concurrent-To: <urn:uuid:cccccccc-0221-11e7-adb1-0242ac120008>\r\n"))
					recSize := uncompressedRecordSize + overhead
					return wantWrite{
						nRecords:      3,
						wantBytes:     []int64{recSize, recSize, recSize},
						wantOffsets:   []int64{0, recSize, 2 * recSize},
						wantFileRegex: `^foo-20010912053020-0001-10\.10\.10\.10\.warc$`,
					}
				}(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			ng := &PatternNameGenerator{
				Directory: dir,
				Prefix:    tt.prefix,
				Pattern:   tt.pattern,
				Extension: "warc",
			}

			opts := []WarcFileWriterOption{
				WithCompression(tt.compress),
				WithFileNameGenerator(ng),
				WithMaxFileSize(0),
				WithMaxConcurrentWriters(1),
			}

			if tt.withWarcinfo {
				opts = append(opts,
					WithWarcInfoFunc(func(rb WarcRecordBuilder) error {
						rb.AddWarcHeader(WarcRecordID, "<urn:uuid:4f271dba-fdfa-4915-ab7e-3e4e1fc0791b>")
						return nil
					}),
					WithRecordOptions(WithDefaultDigestEncoding(Base16)),
				)
			}
			if tt.crossRef {
				opts = append(opts, WithAddWarcConcurrentToHeader(true))
			}

			w := NewWarcFileWriter(opts...)
			t.Cleanup(func() { _ = w.Close() })

			var lastOpenSize int64 // only used for compressed offset expectations

			for _, st := range tt.steps {
				recs := make([]WarcRecord, 0, st.nRecords)
				if tt.checkCrossRef {
					// fixed IDs to assert header mutation
					rec1 := createTestRecord()
					rec1.WarcHeader().Set(WarcRecordID, "<urn:uuid:aaaaaaaa-0221-11e7-adb1-0242ac120008>")
					rec2 := createTestRecord()
					rec2.WarcHeader().Set(WarcRecordID, "<urn:uuid:bbbbbbbb-0221-11e7-adb1-0242ac120008>")
					rec3 := createTestRecord()
					rec3.WarcHeader().Set(WarcRecordID, "<urn:uuid:cccccccc-0221-11e7-adb1-0242ac120008>")
					recs = append(recs, rec1, rec2, rec3)
				} else {
					for i := 0; i < st.nRecords; i++ {
						recs = append(recs, createTestRecord())
					}
				}

				// For compressed tests, capture current open size before writing.
				if tt.compress {
					lastOpenSize = openFileSize(t, dir) // 0 if no file yet
				}

				res := w.Write(recs...)
				if got, want := len(res), st.nRecords; got != want {
					t.Fatalf("len(res) = %v, want %v", got, want)
				}

				// If crossref case, validate headers were mutated.
				if tt.checkCrossRef {
					if got, want := len(recs), 3; got != want {
						t.Fatalf("len(recs) = %v, want %v", got, want)
					}
					rec1, rec2, rec3 := recs[0], recs[1], recs[2]
					if got, want := rec1.WarcHeader().GetAll(WarcConcurrentTo), rec1.WarcHeader().Get(WarcRecordID); slices.Contains(got, want) {
						t.Fatalf("rec1.WarcHeader().GetAll(WarcConcurrentTo) = %v, must not contain %v", got, want)
					}
					if got, want := rec1.WarcHeader().GetAll(WarcConcurrentTo), rec2.WarcHeader().Get(WarcRecordID); !slices.Contains(got, want) {
						t.Fatalf("rec1.WarcHeader().GetAll(WarcConcurrentTo) = %v, want to contain %v", got, want)
					}
					if got, want := rec1.WarcHeader().GetAll(WarcConcurrentTo), rec3.WarcHeader().Get(WarcRecordID); !slices.Contains(got, want) {
						t.Fatalf("rec1.WarcHeader().GetAll(WarcConcurrentTo) = %v, want to contain %v", got, want)
					}
				}

				for i := range res {
					if err := res[i].Err; err != nil {
						t.Fatalf("res[i].Err error = %v", err)
					}

					if got, pattern := res[i].FileName, st.wantFileRegex; !regexp.MustCompile(pattern).MatchString(got) {
						t.Fatalf("value %q does not match %q", got, pattern)
					}
					if got, want := res[i].BytesWritten, st.wantBytes[i]; got != want {
						t.Fatalf("res[i].BytesWritten = %#v, want %#v", got, want)
					}

					if !tt.compress {
						if got, want := res[i].FileOffset, st.wantOffsets[i]; got != want {
							t.Fatalf("res[i].FileOffset = %#v, want %#v", got, want)
						}
					} else {
						// For compressed, offsets must equal file size before the record is appended.
						if got, want := res[i].FileOffset, lastOpenSize; got != want {
							t.Fatalf("res[i].FileOffset = %#v, want %#v", got, want)
						}
						// After each record, update lastOpenSize to current .open file size for next record.
						lastOpenSize = openFileSize(t, dir)
					}
				}
			}

			// Before rotate: should have .open file
			if got := listFiles(t, dir, `\.open$`); len(got) == 0 {
				t.Fatalf("listFiles(t, dir, `\\.open$`) = %v, want nonempty", got)
			}

			if err := w.Rotate(); err != nil {
				t.Fatalf("w.Rotate() error = %v", err)
			}

			// After rotate: no .open files
			if got := listFiles(t, dir, `\.open$`); len(got) != 0 {
				t.Fatalf("listFiles(t, dir, `\\.open$`) = %v, want empty", got)
			}
		})
	}
}

func TestWarcFileWriter_files_parallel_and_rotation(t *testing.T) {
	freezeClockAndHost(t)

	type wantFile struct {
		pattern string
		// for compressed we avoid exact sizes (not stable); uncompressed stays exact
		size *int64
	}

	int64p := func(v int64) *int64 { return &v }

	tests := []struct {
		name          string
		compress      bool
		maxFileSize   int64
		expectedRatio float64
		maxWriters    int
		numRecords    int
		parallel      bool
		nameGen       *PatternNameGenerator
		wantClosedMin int
		wantClosedMax int
		wantFiles     []wantFile
	}{
		{
			name:          "parallel 1 writer uncompressed: single file",
			compress:      false,
			maxWriters:    1,
			numRecords:    3,
			parallel:      true,
			nameGen:       &PatternNameGenerator{Prefix: "foo-", Pattern: "%{prefix}s%{ts}s-%04{serial}d-10.10.10.10.warc"},
			wantClosedMin: 1,
			wantClosedMax: 1,
			wantFiles: []wantFile{
				{pattern: `^foo-20010912053020-0001-10\.10\.10\.10\.warc$`, size: int64p(uncompressedRecordSize * 3)},
			},
		},
		{
			name:          "parallel 2 writers uncompressed: 1-2 files, total size sums",
			compress:      false,
			maxWriters:    2,
			numRecords:    3,
			parallel:      true,
			nameGen:       &PatternNameGenerator{Prefix: "foo-", Pattern: "%{prefix}s%{ts}s-%04{serial}d-10.10.10.10.warc"},
			wantClosedMin: 1,
			wantClosedMax: 2,
			wantFiles: []wantFile{
				{pattern: `^foo-20010912053020-000\d-10\.10\.10\.10\.warc$`, size: int64p(uncompressedRecordSize * 3)},
			},
		},
		{
			name:          "limited file size compressed: force split via expectedRatio=1.0",
			compress:      true,
			maxFileSize:   800,
			expectedRatio: 1.0,
			maxWriters:    1,
			numRecords:    3,
			parallel:      false,
			nameGen:       &PatternNameGenerator{Prefix: "foo-", Pattern: "%{prefix}s%{ts}s-%04{serial}d-10.10.10.10.warc"},
			wantClosedMin: 2,
			wantClosedMax: 2,
			wantFiles: []wantFile{
				{pattern: `^foo-20010912053020-0001-10\.10\.10\.10\.warc\.gz$`, size: nil},
				{pattern: `^foo-20010912053020-0002-10\.10\.10\.10\.warc\.gz$`, size: nil},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			ng := *tt.nameGen
			ng.Directory = dir
			ng.Extension = "warc"

			opts := []WarcFileWriterOption{
				WithCompression(tt.compress),
				WithFileNameGenerator(&ng),
				WithMaxFileSize(tt.maxFileSize),
				WithMaxConcurrentWriters(tt.maxWriters),
			}
			if tt.expectedRatio != 0 {
				opts = append(opts, WithExpectedCompressionRatio(tt.expectedRatio))
			}

			w := NewWarcFileWriter(opts...)
			t.Cleanup(func() { _ = w.Close() })

			writeOne := func() {
				res := w.Write(createTestRecord())
				if got, want := len(res), 1; got != want {
					t.Fatalf("len(res) = %v, want %v", got, want)
				}
				if err := res[0].Err; err != nil {
					t.Fatalf("res[0].Err error = %v", err)
				}
				if got, want := res[0].BytesWritten, uncompressedRecordSize; got != want {
					t.Fatalf("res[0].BytesWritten = %#v, want %#v", got, want)
				}
			}

			if tt.parallel {
				var wg sync.WaitGroup
				wg.Add(tt.numRecords)
				for i := 0; i < tt.numRecords; i++ {
					go func() { defer wg.Done(); writeOne() }()
				}
				wg.Wait()
			} else {
				for i := 0; i < tt.numRecords; i++ {
					writeOne()
				}
			}

			if got := listFiles(t, dir, `\.open$`); len(got) == 0 {
				t.Fatalf("listFiles(t, dir, `\\.open$`) = %v, want nonempty", got)
			}
			if err := w.Rotate(); err != nil {
				t.Fatalf("w.Rotate() error = %v", err)
			}
			if got := listFiles(t, dir, `\.open$`); len(got) != 0 {
				t.Fatalf("listFiles(t, dir, `\\.open$`) = %v, want empty", got)
			}

			closed := listFiles(t, dir, `\.warc(\.gz)?$`)
			if got, bound := len(closed), tt.wantClosedMin; got < bound {
				t.Fatalf("len(closed) = %v, want >= %v", got, bound)
			}
			if got, bound := len(closed), tt.wantClosedMax; got > bound {
				t.Fatalf("len(closed) = %v, want <= %v", got, bound)
			}

			for _, wf := range tt.wantFiles {
				total := sumSizesMatching(t, dir, wf.pattern)
				if wf.size != nil {
					if got, want := total, *wf.size; got != want {
						t.Fatalf("total = %#v, want %#v", got, want)
					}
				} else {
					if got, bound := total, int64(0); got <= bound {
						t.Fatalf("total = %v, want > %v", got, bound)
					}
				}
			}
		})
	}
}

func TestDefaultNameGenerator_NewWarcfileName(t *testing.T) {
	freezeClockAndHost(t)

	tests := []struct {
		name        string
		generator   PatternNameGenerator
		invocations int
		wantDir     string
		wantMatch   string
	}{
		{"default", PatternNameGenerator{}, 5, "", `^20010912053020-000\d-example\.warc$`},
		{"prefix", PatternNameGenerator{Prefix: "foo-"}, 5, "", `^foo-20010912053020-000\d-example\.warc$`},
		{"dir", PatternNameGenerator{Directory: "mydir"}, 5, `^mydir$`, `^20010912053020-000\d-example\.warc$`},
		{"dir+prefix", PatternNameGenerator{Prefix: "foo-", Directory: "mydir"}, 5, `^mydir$`, `^foo-20010912053020-000\d-example\.warc$`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < tt.invocations; i++ {
				gotDir, gotName := tt.generator.NewWarcfileName()
				if got, pattern := gotDir, tt.wantDir; !regexp.MustCompile(pattern).MatchString(got) {
					t.Fatalf("value %q does not match %q", got, pattern)
				}
				if got, pattern := gotName, tt.wantMatch; !regexp.MustCompile(pattern).MatchString(got) {
					t.Fatalf("value %q does not match %q", got, pattern)
				}
			}
		})
	}
}

func TestWarcFileReader(t *testing.T) {
	freezeClockAndHost(t)

	dir := t.TempDir()
	ng := &PatternNameGenerator{
		Directory: dir,
		Prefix:    "test-",
		Pattern:   "%{prefix}s%{ts}s.warc",
		Extension: "warc",
	}
	w := NewWarcFileWriter(
		WithCompression(false),
		WithFileNameGenerator(ng),
		WithMaxFileSize(0),
		WithMaxConcurrentWriters(1),
	)
	res := w.Write(createTestRecord())
	if got, want := len(res), 1; got != want {
		t.Fatalf("len(res) = %v, want %v", got, want)
	}
	if err := res[0].Err; err != nil {
		t.Fatalf("res[0].Err error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("w.Close() error = %v", err)
	}

	// Find the created file
	files := listFiles(t, dir, `\.warc$`)
	if got, want := len(files), 1; got != want {
		t.Fatalf("len(files) = %v, want %v", got, want)
	}
	filePath := filepath.Join(dir, files[0])

	t.Run("read back records", func(t *testing.T) {
		reader, err := NewWarcFileReader(filePath, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer func() {
			if err := reader.Close(); err != nil {
				t.Errorf("reader.Close() error = %v", err)
			}
		}()

		rec, err := reader.Next()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := rec.WarcRecord; got == nil {
			t.Fatalf("rec.WarcRecord = %v, want non-nil", got)
		}
		if got, want := rec.Offset, int64(0); got != want {
			t.Errorf("rec.Offset = %#v, want %#v", got, want)
		}
		if got, bound := rec.Size, int64(0); got <= bound {
			t.Errorf("rec.Size = %v, want > %v", got, bound)
		}
		if got := rec.Validation; len(got) != 0 {
			t.Errorf("rec.Validation = %v, want empty", got)
		}
		if got, want := rec.WarcRecord.Type(), Response; got != want {
			t.Errorf("rec.WarcRecord.Type() = %#v, want %#v", got, want)
		}

		// EOF
		_, err = reader.Next()
		if got, want := err, io.EOF; !errors.Is(got, want) {
			t.Errorf("err = %v, want an error matching %v", got, want)
		}
	})

	t.Run("read with offset", func(t *testing.T) {
		reader, err := NewWarcFileReader(filePath, 999999)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer func() {
			if err := reader.Close(); err != nil {
				t.Errorf("reader.Close() error = %v", err)
			}
		}()

		_, err = reader.Next()
		if err == nil {
			t.Errorf("err: expected an error")
		}
	})

	t.Run("directory error", func(t *testing.T) {
		_, err := NewWarcFileReader(dir, 0)
		if err == nil {
			t.Errorf("err: expected an error")
		}
		if got, want := err.Error(), "is directory"; !strings.Contains(got, want) {
			t.Errorf("err.Error() = %v, want to contain %v", got, want)
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := NewWarcFileReader(filepath.Join(dir, "no-such-file.warc"), 0)
		if err == nil {
			t.Errorf("err: expected an error")
		}
	})
}

func TestNewWarcFileReaderFromStream(t *testing.T) {
	freezeClockAndHost(t)

	dir := t.TempDir()
	ng := &PatternNameGenerator{
		Directory: dir,
		Prefix:    "stream-",
		Pattern:   "%{prefix}s%{ts}s.warc",
		Extension: "warc",
	}
	w := NewWarcFileWriter(
		WithCompression(false),
		WithFileNameGenerator(ng),
		WithMaxFileSize(0),
		WithMaxConcurrentWriters(1),
	)
	res := w.Write(createTestRecord())
	if got, want := len(res), 1; got != want {
		t.Fatalf("len(res) = %v, want %v", got, want)
	}
	if err := res[0].Err; err != nil {
		t.Fatalf("res[0].Err error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("w.Close() error = %v", err)
	}

	files := listFiles(t, dir, `\.warc$`)
	if got, want := len(files), 1; got != want {
		t.Fatalf("len(files) = %v, want %v", got, want)
	}
	filePath := filepath.Join(dir, files[0])

	t.Run("from opened file", func(t *testing.T) {
		f, err := os.Open(filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// already closed by reader.Close(), so expect error
		defer func() {
			if err := f.Close(); err == nil {
				t.Errorf("f.Close(): expected an error")
			}
		}()

		reader, err := NewWarcFileReaderFromStream(f, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer func() {
			if err := reader.Close(); err != nil {
				t.Errorf("reader.Close() error = %v", err)
			}
		}()

		rec, err := reader.Next()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := rec.WarcRecord; got == nil {
			t.Fatalf("rec.WarcRecord = %v, want non-nil", got)
		}
		if got, want := rec.WarcRecord.Type(), Response; got != want {
			t.Errorf("rec.WarcRecord.Type() = %#v, want %#v", got, want)
		}
	})

	t.Run("from stream non-closer", func(t *testing.T) {
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		reader, err := NewWarcFileReaderFromStream(bytes.NewReader(data), 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		rec, err := reader.Next()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := rec.WarcRecord; got == nil {
			t.Fatalf("rec.WarcRecord = %v, want non-nil", got)
		}

		// Close when underlying is not an io.Closer
		err = reader.Close()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestWarcFileWriter_String(t *testing.T) {
	freezeClockAndHost(t)

	w := NewWarcFileWriter(
		WithCompression(true),
		WithMaxFileSize(1024),
		WithMaxConcurrentWriters(2),
	)
	defer func() {
		if err := w.Close(); err != nil {
			t.Errorf("w.Close() error = %v", err)
		}
	}()

	s := w.String()
	if got, want := s, "WarcFileWriter"; !strings.Contains(got, want) {
		t.Errorf("s = %v, want to contain %v", got, want)
	}
	if got, want := s, "1024"; !strings.Contains(got, want) {
		t.Errorf("s = %v, want to contain %v", got, want)
	}
	if got, want := s, "true"; !strings.Contains(got, want) {
		t.Errorf("s = %v, want to contain %v", got, want)
	}
	if got, want := s, "2"; !strings.Contains(got, want) {
		t.Errorf("s = %v, want to contain %v", got, want)
	}
}

func TestWarcFileWriter_WriteAfterClose(t *testing.T) {
	freezeClockAndHost(t)

	dir := t.TempDir()
	ng := &PatternNameGenerator{Directory: dir, Prefix: "closed-", Pattern: "%{prefix}s%{ts}s.warc", Extension: "warc"}
	w := NewWarcFileWriter(
		WithCompression(false),
		WithFileNameGenerator(ng),
		WithMaxFileSize(0),
		WithMaxConcurrentWriters(1),
	)
	if err := w.Close(); err != nil {
		t.Fatalf("w.Close() error = %v", err)
	}

	// Write after close should return nil
	res := w.Write(createTestRecord())
	if got := res; got != nil {
		t.Errorf("res = %v, want nil", got)
	}
}

func TestWarcFileWriter_RotateAfterClose(t *testing.T) {
	freezeClockAndHost(t)

	dir := t.TempDir()
	ng := &PatternNameGenerator{Directory: dir, Prefix: "rot-", Pattern: "%{prefix}s%{ts}s.warc", Extension: "warc"}
	w := NewWarcFileWriter(
		WithCompression(false),
		WithFileNameGenerator(ng),
		WithMaxFileSize(0),
		WithMaxConcurrentWriters(1),
	)
	if err := w.Close(); err != nil {
		t.Fatalf("w.Close() error = %v", err)
	}

	err := w.Rotate()
	if err == nil {
		t.Errorf("err: expected an error")
	}
	if got, want := err.Error(), "closed"; !strings.Contains(got, want) {
		t.Errorf("err.Error() = %v, want to contain %v", got, want)
	}
}

func TestWarcFileWriterOptions(t *testing.T) {
	// Exercise each option function
	t.Run("WithCompressionLevel", func(t *testing.T) {
		o := defaultwarcFileWriterOptions()
		WithCompressionLevel(9).apply(&o)
		if got, want := o.gzipLevel, 9; got != want {
			t.Errorf("o.gzipLevel = %#v, want %#v", got, want)
		}
	})
	t.Run("WithFlush", func(t *testing.T) {
		o := defaultwarcFileWriterOptions()
		WithFlush(true).apply(&o)
		if got := o.flush; !got {
			t.Errorf("o.flush = %v, want true", got)
		}
	})
	t.Run("WithSegmentation", func(t *testing.T) {
		o := defaultwarcFileWriterOptions()
		WithSegmentation().apply(&o)
		if got := o.useSegmentation; !got {
			t.Errorf("o.useSegmentation = %v, want true", got)
		}
	})
	t.Run("WithCompressedFileSuffix", func(t *testing.T) {
		o := defaultwarcFileWriterOptions()
		WithCompressedFileSuffix(".zst").apply(&o)
		if got, want := o.compressSuffix, ".zst"; got != want {
			t.Errorf("o.compressSuffix = %#v, want %#v", got, want)
		}
	})
	t.Run("WithOpenFileSuffix", func(t *testing.T) {
		o := defaultwarcFileWriterOptions()
		WithOpenFileSuffix(".tmp").apply(&o)
		if got, want := o.openFileSuffix, ".tmp"; got != want {
			t.Errorf("o.openFileSuffix = %#v, want %#v", got, want)
		}
	})
	t.Run("WithMarshaler", func(t *testing.T) {
		o := defaultwarcFileWriterOptions()
		m := &defaultMarshaler{}
		WithMarshaler(m).apply(&o)
		if got, want := o.marshaler, m; got != want {
			t.Errorf("pointer identity: got %p, compared with %p", got, want)
		}
	})
	t.Run("WithBeforeFileCreationHook", func(t *testing.T) {
		o := defaultwarcFileWriterOptions()
		called := false
		WithBeforeFileCreationHook(func(string) error { called = true; return nil }).apply(&o)
		if got := o.beforeFileCreationHook; got == nil {
			t.Fatal("o.beforeFileCreationHook is nil")
		}
		_ = o.beforeFileCreationHook("test")
		if got := called; !got {
			t.Errorf("called = %v, want true", got)
		}
	})
	t.Run("WithAfterFileCreationHook", func(t *testing.T) {
		o := defaultwarcFileWriterOptions()
		called := false
		WithAfterFileCreationHook(func(string, int64, string) error { called = true; return nil }).apply(&o)
		if got := o.afterFileCreationHook; got == nil {
			t.Fatal("o.afterFileCreationHook is nil")
		}
		_ = o.afterFileCreationHook("test", 0, "")
		if got := called; !got {
			t.Errorf("called = %v, want true", got)
		}
	})
}

func TestWarcFileWriter_WithSegmentation(t *testing.T) {
	freezeClockAndHost(t)

	dir := t.TempDir()
	ng := &PatternNameGenerator{Directory: dir, Prefix: "seg-", Pattern: "%{prefix}s%{ts}s-%04{serial}d.warc", Extension: "warc"}
	w := NewWarcFileWriter(
		WithCompression(false),
		WithFileNameGenerator(ng),
		WithMaxFileSize(600),
		WithSegmentation(),
		WithMaxConcurrentWriters(1),
	)
	defer func() {
		if err := w.Close(); err != nil {
			t.Errorf("w.Close() error = %v", err)
		}
	}()

	res := w.Write(createTestRecord())
	if got, want := len(res), 1; got != want {
		t.Fatalf("len(res) = %v, want %v", got, want)
	}
	// Exercised maxRecordSize with segmentation=true, compress=false
}

func TestWarcFileWriter_WithFlushEnabled(t *testing.T) {
	freezeClockAndHost(t)

	dir := t.TempDir()
	ng := &PatternNameGenerator{Directory: dir, Prefix: "flush-", Pattern: "%{prefix}s%{ts}s.warc", Extension: "warc"}
	w := NewWarcFileWriter(
		WithCompression(false),
		WithFileNameGenerator(ng),
		WithMaxFileSize(0),
		WithFlush(true),
		WithMaxConcurrentWriters(1),
	)
	defer func() {
		if err := w.Close(); err != nil {
			t.Errorf("w.Close() error = %v", err)
		}
	}()

	res := w.Write(createTestRecord())
	if got, want := len(res), 1; got != want {
		t.Fatalf("len(res) = %v, want %v", got, want)
	}
	if err := res[0].Err; err != nil {
		t.Fatalf("res[0].Err error = %v", err)
	}
}

func TestWarcFileWriter_HooksAreCalled(t *testing.T) {
	freezeClockAndHost(t)

	dir := t.TempDir()
	ng := &PatternNameGenerator{Directory: dir, Prefix: "hook-", Pattern: "%{prefix}s%{ts}s.warc", Extension: "warc"}

	var beforeFile string
	var afterFile string
	var afterSize int64

	w := NewWarcFileWriter(
		WithCompression(false),
		WithFileNameGenerator(ng),
		WithMaxFileSize(0),
		WithMaxConcurrentWriters(1),
		WithBeforeFileCreationHook(func(f string) error { beforeFile = f; return nil }),
		WithAfterFileCreationHook(func(f string, s int64, _ string) error { afterFile = f; afterSize = s; return nil }),
	)

	res := w.Write(createTestRecord())
	if got, want := len(res), 1; got != want {
		t.Fatalf("len(res) = %v, want %v", got, want)
	}
	if err := res[0].Err; err != nil {
		t.Fatalf("res[0].Err error = %v", err)
	}

	if got := beforeFile; len(got) == 0 {
		t.Errorf("beforeFile = %v, want nonempty", got)
	}
	if got, want := beforeFile, ".open"; !strings.Contains(got, want) {
		t.Errorf("beforeFile = %v, want to contain %v", got, want)
	}

	if err := w.Rotate(); err != nil {
		t.Fatalf("w.Rotate() error = %v", err)
	}
	if got := afterFile; len(got) == 0 {
		t.Errorf("afterFile = %v, want nonempty", got)
	}
	if got, bound := afterSize, int64(0); got <= bound {
		t.Errorf("afterSize = %v, want > %v", got, bound)
	}
}

func TestPatternNameGenerator_CustomParams(t *testing.T) {
	freezeClockAndHost(t)

	ng := &PatternNameGenerator{
		Directory: "mydir",
		Prefix:    "pre-",
		Pattern:   "%{prefix}s%{ts}s-%{custom}s.%{ext}s",
		Extension: "warc",
		Params:    map[string]any{"custom": "myvalue"},
	}
	dir, name := ng.NewWarcfileName()
	if got, want := dir, "mydir"; got != want {
		t.Errorf("dir = %#v, want %#v", got, want)
	}
	if got, want := name, "myvalue"; !strings.Contains(got, want) {
		t.Errorf("name = %v, want to contain %v", got, want)
	}
	if got, want := name, "pre-"; !strings.Contains(got, want) {
		t.Errorf("name = %v, want to contain %v", got, want)
	}
}

func TestNewWarcFileWriter_PanicOnInvalidGzipLevel(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("expected panic")
			}
		}()
		(func() {
			NewWarcFileWriter(WithCompressionLevel(42))
		})()
	}()
}

func TestNewWarcFileWriter_InvalidCompressionRatio(t *testing.T) {
	freezeClockAndHost(t)
	// Should not panic, ratio is clamped to 0.5
	w := NewWarcFileWriter(
		WithCompression(true),
		WithExpectedCompressionRatio(-1),
		WithMaxConcurrentWriters(1),
	)
	defer func() {
		if err := w.Close(); err != nil {
			t.Errorf("w.Close() error = %v", err)
		}
	}()
}

var warcFileWriterBenchmarkResult []WriteResponse

func BenchmarkWarcFileWriter_Write_compressed(b *testing.B) {
	dir := b.TempDir()
	now = func() time.Time {
		return time.Date(2001, 9, 12, 5, 30, 20, 0, time.UTC)
	}
	hostOrIp = func() string {
		return "example"
	}

	nameGenerator := &PatternNameGenerator{
		Prefix:    "bench-",
		Directory: dir,
	}
	record := createTestRecord()
	b.Cleanup(func() {
		_ = record.Close()
	})

	w := NewWarcFileWriter(
		WithCompression(true),
		WithFileNameGenerator(nameGenerator),
		WithMaxFileSize(1<<30),
		WithMaxConcurrentWriters(1))
	defer func() {
		if err := w.Close(); err != nil {
			b.Errorf("w.Close() error = %v", err)
		}
	}()

	b.ResetTimer()
	for b.Loop() {
		warcFileWriterBenchmarkResult = w.Write(record)
	}
}

// ---- helpers ----

func freezeClockAndHost(t *testing.T) {
	oldNow := now
	oldHostOrIp := hostOrIp

	now = func() time.Time {
		return time.Date(2001, 9, 12, 5, 30, 20, 0, time.UTC)
	}
	hostOrIp = func() string { return "example" }

	t.Cleanup(func() {
		now = oldNow
		hostOrIp = oldHostOrIp
	})
}

func listFiles(t *testing.T, dir string, pattern string) []string {
	t.Helper()
	re := regexp.MustCompile(pattern)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if re.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func openFileSize(t *testing.T, dir string) int64 {
	t.Helper()
	opens := listFiles(t, dir, `\.open$`)
	if len(opens) == 0 {
		return 0
	}
	fi, err := os.Stat(filepath.Join(dir, opens[0]))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return fi.Size()
}

func sumSizesMatching(t *testing.T, dir, fileNameRegex string) int64 {
	t.Helper()
	re := regexp.MustCompile(fileNameRegex)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var total int64
	var matched bool
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !re.MatchString(e.Name()) {
			continue
		}
		matched = true
		fi, err := e.Info()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		total += fi.Size()
	}
	if got := matched; !got {
		t.Fatalf("matched = %v, want true: %s", got, fmt.Sprintf("no file matching %q in %q", fileNameRegex, dir))
	}
	return total
}

func createTestRecord() WarcRecord {
	builder := NewRecordBuilder(Response, WithFixDigest(false), WithStrictValidation(), WithAddMissingDigest(false))
	_, err := builder.WriteString("HTTP/1.1 200 OK\nDate: Tue, 19 Sep 2016 17:18:40 GMT\nServer: Apache/2.0.54 (Ubuntu)\n" +
		"Last-Modified: Mon, 16 Jun 2013 22:28:51 GMT\nETag: \"3e45-67e-2ed02ec0\"\nAccept-Ranges: bytes\n" +
		"Content-Length: 19\nConnection: close\nContent-Type: text/plain\n\nThis is the content\n")
	if err != nil {
		panic(err)
	}
	builder.AddWarcHeader(WarcRecordID, "<urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac120008>")
	builder.AddWarcHeader(WarcDate, "2006-01-02T15:04:05Z")
	builder.AddWarcHeader(ContentLength, "258")
	builder.AddWarcHeader(ContentType, "application/http;msgtype=response")
	builder.AddWarcHeader(WarcBlockDigest, "sha1:7CBE117BFA2B22C3A02DEFF3BC04D5F912964A45")

	wr, _, err := builder.Build()
	if err != nil {
		panic(err)
	}
	return wr
}

func TestWarcFileWriter_WarcInfoFunc_Error(t *testing.T) {
	dir := t.TempDir()
	w := NewWarcFileWriter(
		WithFileNameGenerator(&PatternNameGenerator{Prefix: "test-", Directory: dir}),
		WithWarcInfoFunc(func(rb WarcRecordBuilder) error {
			return fmt.Errorf("warcinfo callback failed")
		}),
	)
	defer func() {
		if err := w.Close(); err != nil {
			t.Errorf("w.Close() error = %v", err)
		}
	}()

	rec := createTestRecord()
	defer func() {
		if err := rec.Close(); err != nil {
			t.Errorf("rec.Close() error = %v", err)
		}
	}()

	// Write should fail because warcInfoFunc fails during file creation
	responses := w.Write(rec)
	if got, want := len(responses), 1; got != want {
		t.Fatalf("len(responses) = %v, want %v", got, want)
	}
	if err := responses[0].Err; err == nil {
		t.Errorf("responses[0].Err: expected an error")
	}
	if got, want := responses[0].Err.Error(), "warcinfo callback failed"; !strings.Contains(got, want) {
		t.Errorf("responses[0].Err.Error() = %v, want to contain %v", got, want)
	}
}

func TestWarcFileWriter_MaxFileSize_ContentLengthMissing(t *testing.T) {
	// Test contentLength() when record has no Content-Length header (returns 0, false).
	// Use WithMaxFileSize to trigger wouldExceedMax which calls contentLength.
	dir := t.TempDir()
	w := NewWarcFileWriter(
		WithMaxFileSize(100000), // large enough that first write succeeds
		WithFileNameGenerator(&PatternNameGenerator{Prefix: "test-", Directory: dir}),
	)
	defer func() {
		if err := w.Close(); err != nil {
			t.Errorf("w.Close() error = %v", err)
		}
	}()

	// Build a record without Content-Length header
	rb := NewRecordBuilder(Warcinfo,
		WithSpecViolationPolicy(ErrIgnore), WithSyntaxErrorPolicy(ErrIgnore),
		WithAddMissingContentLength(false), WithFixDigest(false), WithAddMissingDigest(false))
	rb.AddWarcHeader(WarcRecordID, "<urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac120008>")
	rb.AddWarcHeader(WarcDate, "2024-01-01T00:00:00Z")
	rb.AddWarcHeader(ContentType, "application/warc-fields")
	// Deliberately NOT setting Content-Length
	rec, _, err := rb.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := rec.Close(); err != nil {
			t.Errorf("rec.Close() error = %v", err)
		}
	}()

	responses := w.Write(rec)
	if got, want := len(responses), 1; got != want {
		t.Fatalf("len(responses) = %v, want %v", got, want)
	}
	if err := responses[0].Err; err != nil {
		t.Errorf("responses[0].Err error = %v", err)
	}
}

func TestWarcFileWriter_MaxFileSize_Rotate(t *testing.T) {
	// Test file rotation when maxFileSize is exceeded.
	dir := t.TempDir()
	w := NewWarcFileWriter(
		WithMaxFileSize(1), // very small — forces rotation on each write
		WithFileNameGenerator(&PatternNameGenerator{Prefix: "test-", Directory: dir}),
	)

	rec1 := createTestRecord()
	defer func() {
		if err := rec1.Close(); err != nil {
			t.Fatalf("rec1.Close() error = %v: %s", err, "failed to close rec1")
		}
	}()
	responses := w.Write(rec1)
	if got, want := len(responses), 1; got != want {
		t.Fatalf("len(responses) = %v, want %v", got, want)
	}
	if err := responses[0].Err; err != nil {
		t.Fatalf("responses[0].Err error = %v", err)
	}

	rec2 := createTestRecord()
	defer func() {
		if err := rec2.Close(); err != nil {
			t.Fatalf("rec2.Close() error = %v: %s", err, "failed to close rec2")
		}
	}()
	responses = w.Write(rec2)
	if got, want := len(responses), 1; got != want {
		t.Fatalf("len(responses) = %v, want %v", got, want)
	}
	if err := responses[0].Err; err != nil {
		t.Fatalf("responses[0].Err error = %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("w.Close() error = %v", err)
	}
}
