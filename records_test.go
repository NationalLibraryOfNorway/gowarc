package gowarc

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/klauspost/compress/gzip"
)

// minimalRecord returns a valid WARC record string with the given type and record ID suffix.
// The record has zero-length content.
func minimalRecord(recordType, idSuffix string) string {
	return "WARC/1.1\r\n" +
		"WARC-Type: " + recordType + "\r\n" +
		"WARC-Date: 2017-03-06T04:03:53Z\r\n" +
		"WARC-Record-ID: <urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac12" + idSuffix + ">\r\n" +
		"Content-Type: application/warc-fields\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n" +
		"\r\n\r\n"
}

func gzipString(s string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte(s))
	_ = gz.Close()
	return buf.Bytes()
}

func TestWarcFileReader_Records(t *testing.T) {
	rec1 := minimalRecord("warcinfo", "0001")
	rec2 := minimalRecord("warcinfo", "0002")
	rec3 := minimalRecord("warcinfo", "0003")

	type wantRecord struct {
		recordType    RecordType
		idSuffix      string // substring of the record ID to match
		sizeGt0       bool
		hasValidation bool // expect non-empty Validation slice
	}

	tests := []struct {
		name string
		opts []WarcRecordOption
		data []byte // raw stream data

		wantRecords []wantRecord
		wantErr     string // non-empty: the iterator should yield an error containing this
		wantErrIs   error  // non-nil: the iterator error must match errors.Is
		wantNoErr   bool   // explicitly expect NO error (distinguishes from don't-care)
	}{
		{
			name:      "empty stream yields no records",
			data:      []byte{},
			wantNoErr: true,
		},
		{
			name: "single record",
			data: []byte(rec1),
			wantRecords: []wantRecord{
				{recordType: Warcinfo, idSuffix: "0001", sizeGt0: true},
			},
		},
		{
			name: "two records with correct offsets and sizes",
			data: []byte(rec1 + rec2),
			wantRecords: []wantRecord{
				{recordType: Warcinfo, idSuffix: "0001", sizeGt0: true},
				{recordType: Warcinfo, idSuffix: "0002", sizeGt0: true},
			},
		},
		{
			name: "three records",
			data: []byte(rec1 + rec2 + rec3),
			wantRecords: []wantRecord{
				{recordType: Warcinfo, idSuffix: "0001", sizeGt0: true},
				{recordType: Warcinfo, idSuffix: "0002", sizeGt0: true},
				{recordType: Warcinfo, idSuffix: "0003", sizeGt0: true},
			},
		},
		{
			name: "gzip compressed single record",
			data: gzipString(rec1),
			wantRecords: []wantRecord{
				{recordType: Warcinfo, idSuffix: "0001", sizeGt0: true},
			},
		},
		{
			name: "gzip compressed multi-stream (two records)",
			data: append(gzipString(rec1), gzipString(rec2)...),
			wantRecords: []wantRecord{
				{recordType: Warcinfo, idSuffix: "0001", sizeGt0: true},
				{recordType: Warcinfo, idSuffix: "0002", sizeGt0: true},
			},
		},
		{
			name: "garbage before record with ErrWarn produces validation",
			opts: []WarcRecordOption{WithSyntaxErrorPolicy(ErrWarn), WithSpecViolationPolicy(ErrWarn)},
			data: append([]byte("XX"), []byte(rec1)...),
			wantRecords: []wantRecord{
				{recordType: Warcinfo, idSuffix: "0001", sizeGt0: true, hasValidation: true},
			},
		},
		{
			name:    "garbage before record with ErrFail yields error",
			opts:    []WarcRecordOption{WithSyntaxErrorPolicy(ErrFail), WithSpecViolationPolicy(ErrFail)},
			data:    append([]byte("GARBAGE"), []byte(rec1)...),
			wantErr: "expected start of record",
		},
		{
			name:      "corrupt data with ErrWarn yields ErrNoRecord",
			data:      []byte("NOT A WARC RECORD AT ALL"),
			wantErrIs: ErrNoRecord,
		},
		{
			name:    "corrupt data with ErrFail yields error",
			opts:    []WarcRecordOption{WithSyntaxErrorPolicy(ErrFail)},
			data:    []byte("NOT A WARC RECORD AT ALL"),
			wantErr: "expected start of record",
		},
		{
			name:    "truncated record yields error with strict policy",
			opts:    []WarcRecordOption{WithSpecViolationPolicy(ErrFail), WithSyntaxErrorPolicy(ErrFail)},
			data:    []byte("WARC/1.1\r\n"),
			wantErr: "missing required field",
		},
		{
			name: "truncated record yields record with validation warnings",
			data: []byte("WARC/1.1\r\n"),
			wantRecords: []wantRecord{
				{recordType: 0, idSuffix: "", sizeGt0: true, hasValidation: true},
			},
		},
		{
			name: "record with validation warnings (bad end-of-record marker)",
			opts: []WarcRecordOption{WithSpecViolationPolicy(ErrWarn), WithSyntaxErrorPolicy(ErrWarn)},
			data: []byte(
				"WARC/1.1\r\n" +
					"WARC-Type: warcinfo\r\n" +
					"WARC-Date: 2017-03-06T04:03:53Z\r\n" +
					"WARC-Record-ID: <urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac120001>\r\n" +
					"Content-Type: application/warc-fields\r\n" +
					"Content-Length: 0\r\n" +
					"\r\n" +
					"\n\n"), // bad end-of-record marker — \n\n instead of \r\n\r\n
			wantRecords: []wantRecord{
				{recordType: Warcinfo, idSuffix: "0001", sizeGt0: true, hasValidation: true},
			},
		},
		{
			name: "garbage after valid records yields records then ErrNoRecord",
			data: append([]byte(rec1+rec2), []byte("TRAILING GARBAGE")...),
			wantRecords: []wantRecord{
				{recordType: Warcinfo, idSuffix: "0001", sizeGt0: true},
				{recordType: Warcinfo, idSuffix: "0002", sizeGt0: true},
			},
			wantErrIs: ErrNoRecord,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, err := NewWarcFileReaderFromStream(bytes.NewReader(tt.data), 0, tt.opts...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer func() {
				if err := reader.Close(); err != nil {
					t.Errorf("reader.Close() error = %v", err)
				}
			}()

			var got []Record
			var iterErr error

			for rec, err := range reader.Records() {
				if err != nil {
					iterErr = err
					break
				}
				got = append(got, rec)
			}

			if tt.wantErr != "" {
				if err := iterErr; err == nil {
					t.Fatalf("iterErr: expected an error")
				}
				if got, want := iterErr.Error(), tt.wantErr; !strings.Contains(got, want) {
					t.Errorf("iterErr.Error() = %v, want to contain %v", got, want)
				}
				return
			}

			if tt.wantErrIs != nil {
				if err := iterErr; err == nil {
					t.Fatalf("iterErr: expected an error: %s", "expected an error from iterator")
				}
				if got, want := iterErr, tt.wantErrIs; !errors.Is(got, want) {
					t.Errorf("iterErr = %v, want an error matching %v", got, want)
				}
			}

			if tt.wantNoErr {
				if err := iterErr; err != nil {
					t.Errorf("iterErr error = %v: %s", err, "expected no error from iterator")
				}
			}

			if got, want := len(got), len(tt.wantRecords); got != want {
				t.Fatalf("len(got) = %v, want %v: %s", got, want, "record count")
			}

			// Validate offsets are monotonically increasing and non-overlapping
			var prevEnd int64
			for i, rec := range got {
				want := tt.wantRecords[i]

				if got, want := rec.WarcRecord.Type(), want.recordType; got != want {
					t.Errorf("rec.WarcRecord.Type() = %#v, want %#v: %s", got, want, fmt.Sprintf("record %d type", i))
				}
				if want.idSuffix != "" {
					if got, want := rec.WarcRecord.WarcHeader().Get(WarcRecordID), want.idSuffix; !strings.Contains(got, want) {
						t.Errorf("rec.WarcRecord.WarcHeader().Get(WarcRecordID) = %v, want to contain %v: %s", got, want, fmt.Sprintf("record %d ID", i))
					}
				}

				if want.sizeGt0 {
					if got, bound := rec.Size, int64(0); got <= bound {
						t.Errorf("rec.Size = %v, want > %v: %s", got, bound, fmt.Sprintf("record %d size", i))
					}
				}

				if want.hasValidation {
					if got := rec.Validation; len(got) == 0 {
						t.Errorf("rec.Validation = %v, want nonempty: %s", got, fmt.Sprintf("record %d should have validation warnings", i))
					}
				} else {
					if got := rec.Validation; len(got) != 0 {
						t.Errorf("rec.Validation = %v, want empty: %s", got, fmt.Sprintf("record %d should have no validation warnings", i))
					}
				}

				// Offset should be >= previous record's end
				if got, bound := rec.Offset, prevEnd; got < bound {
					t.Errorf("rec.Offset = %v, want >= %v: %s", got, bound, fmt.Sprintf("record %d offset >= prev end", i))
				}
				prevEnd = rec.Offset + rec.Size

				if err := rec.Close(); err != nil {
					t.Errorf("rec.Close() error = %v", err)
				}
			}

			// Consecutive records: sum of offset+size should be monotonic
			if len(got) >= 2 {
				for i := 1; i < len(got); i++ {
					prevRecEnd := got[i-1].Offset + got[i-1].Size
					if got, bound := prevRecEnd, got[i].Offset; got > bound {
						t.Errorf("prevRecEnd = %v, want <= %v: %s", got, bound, fmt.Sprintf("record %d should start at or after record %d ends", i, i-1))
					}
				}
			}
		})
	}
}

func TestWarcFileReader_Records_BreakEarly(t *testing.T) {
	// Verify that breaking out of the iterator mid-stream works correctly
	rec1 := minimalRecord("warcinfo", "0001")
	rec2 := minimalRecord("warcinfo", "0002")
	rec3 := minimalRecord("warcinfo", "0003")

	reader, err := NewWarcFileReaderFromStream(
		bytes.NewReader([]byte(rec1+rec2+rec3)), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Errorf("reader.Close() error = %v", err)
		}
	}()

	var count int
	for rec, err := range reader.Records() {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		func() {
			if err := rec.Close(); err != nil {
				t.Errorf("rec.Close() error = %v", err)
			}
		}()
		count++
		if count == 2 {
			break
		}
	}
	if got, want := count, 2; got != want {
		t.Errorf("count = %#v, want %#v: %s", got, want, "should have seen exactly 2 records before break")
	}
}

func TestWarcFileReader_Records_ValidationWarnings(t *testing.T) {
	// Feed a stream with garbage prefix (ErrWarn) — Records() should yield
	// the record with validation populated.
	input := "XX" +
		"WARC/1.1\r\n" +
		"WARC-Type: warcinfo\r\n" +
		"WARC-Date: 2017-03-06T04:03:53Z\r\n" +
		"WARC-Record-ID: <urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac120001>\r\n" +
		"Content-Type: application/warc-fields\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n" +
		"\r\n\r\n"

	reader, err := NewWarcFileReaderFromStream(
		strings.NewReader(input), 0,
		WithSyntaxErrorPolicy(ErrWarn), WithSpecViolationPolicy(ErrWarn))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Errorf("reader.Close() error = %v", err)
		}
	}()

	var records []Record
	for rec, err := range reader.Records() {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		records = append(records, rec)
	}

	if got, want := len(records), 1; got != want {
		t.Fatalf("len(records) = %v, want %v", got, want)
	}
	if got := records[0].Validation; len(got) == 0 {
		t.Errorf("records[0].Validation = %v, want nonempty: %s", got, "should have validation warnings")
	}
	// Offset should account for the skipped garbage
	if got, want := records[0].Offset, int64(2); got != want {
		t.Errorf("records[0].Offset = %#v, want %#v", got, want)
	}
	func() {
		if err := records[0].Close(); err != nil {
			t.Errorf("records[0].Close() error = %v", err)
		}
	}()
}

func TestWarcFileReader_Records_SizeCoversFullRecord(t *testing.T) {
	// Write two records via the writer, read them back via Records(),
	// verify that Offset + Size of record i == Offset of record i+1.
	freezeClockAndHost(t)

	dir := t.TempDir()
	ng := &PatternNameGenerator{
		Directory: dir,
		Prefix:    "iter-",
		Pattern:   "%{prefix}s%{ts}s.warc",
		Extension: "warc",
	}
	w := NewWarcFileWriter(
		WithCompression(false),
		WithFileNameGenerator(ng),
		WithMaxFileSize(0),
		WithMaxConcurrentWriters(1),
	)
	res := w.Write(createTestRecord(), createTestRecord())
	if got, want := len(res), 2; got != want {
		t.Fatalf("len(res) = %v, want %v", got, want)
	}
	if err := res[0].Err; err != nil {
		t.Fatalf("res[0].Err error = %v", err)
	}
	if err := res[1].Err; err != nil {
		t.Fatalf("res[1].Err error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("w.Close() error = %v", err)
	}

	files := listFiles(t, dir, `\.warc$`)
	if got, want := len(files), 1; got != want {
		t.Fatalf("len(files) = %v, want %v", got, want)
	}

	reader, err := NewWarcFileReader(dir+"/"+files[0], 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Errorf("reader.Close() error = %v", err)
		}
	}()

	var records []Record
	for rec, err := range reader.Records() {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		records = append(records, rec)
		defer func() {
			if err := rec.Close(); err != nil {
				t.Errorf("rec.Close() error = %v", err)
			}
		}()
	}

	if got, want := len(records), 2; got != want {
		t.Fatalf("len(records) = %v, want %v", got, want)
	}
	if got, want := records[0].Offset, int64(0); got != want {
		t.Errorf("records[0].Offset = %#v, want %#v", got, want)
	}
	if got, bound := records[0].Size, int64(0); got <= bound {
		t.Errorf("records[0].Size = %v, want > %v", got, bound)
	}
	// Second record starts exactly where the first ends
	if got, want := records[1].Offset, records[0].Offset+records[0].Size; got != want {
		t.Errorf("records[1].Offset = %#v, want %#v: %s", got, want, "second record offset should equal first record offset + size")
	}
}

func TestRecord_Close_NilWarcRecord(t *testing.T) {
	// Record.Close() should be safe to call when WarcRecord is nil
	r := Record{}
	if err := r.Close(); err != nil {
		t.Errorf("r.Close() error = %v", err)
	}
}
