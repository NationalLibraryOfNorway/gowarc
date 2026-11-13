package gowarc

import (
	"io"
	"testing"
)

type cacheTest struct {
	name         string
	data         io.Reader
	wantCacheErr bool
}

func validateCacheTest(t *testing.T, block Block, expectedContent string, expectedDigest string, wantCacheErr bool) {
	t.Helper()

	err := block.Cache()
	if wantCacheErr {
		if err == nil {
			t.Errorf("err: expected an error")
		}
	} else {
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if got := block.IsCached(); !got {
		t.Errorf("block.IsCached() = %v, want true", got)
	}

	// Reading content twice should be ok
	got, err := block.RawBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err := io.ReadAll(got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(content), expectedContent; got != want {
		t.Errorf("string(content) = %#v, want %#v", got, want)
	}
	got, err = block.RawBytes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, err = io.ReadAll(got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(content), expectedContent; got != want {
		t.Errorf("string(content) = %#v, want %#v", got, want)
	}

	// BlockDigest should be ok
	gotDigest := block.BlockDigest()
	if got, want := gotDigest, expectedDigest; got != want {
		t.Errorf("gotDigest = %#v, want %#v", got, want)
	}
}

type blockDigestTest struct {
	name string
	data io.Reader
}

func validateBlockDigestTest(t *testing.T, block Block, expectedDigest string) {
	t.Helper()

	got := block.BlockDigest()
	if got, want := got, expectedDigest; got != want {
		t.Errorf("got = %#v, want %#v", got, want)
	}

	// Repeated call to BlockDigest should be ok
	got = block.BlockDigest()
	if got, want := got, expectedDigest; got != want {
		t.Errorf("got = %#v, want %#v", got, want)
	}

	if block.IsCached() {
		// Call to RawBytes after call to BlockDigest should be ok
		_, err := block.RawBytes()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	} else {
		// Call to RawBytes after call to BlockDigest should fail
		_, err := block.RawBytes()
		if err == nil {
			t.Fatalf("err: expected an error")
		}
	}
}

func validatePayloadDigestTest(t *testing.T, block Block, expectedDigest string) {
	t.Helper()

	payloadBlock, ok := block.(PayloadBlock)
	if !ok {
		t.Fatalf("block is not a PayloadBlock")
	}

	got := payloadBlock.PayloadDigest()
	if got, want := got, expectedDigest; got != want {
		t.Errorf("got = %#v, want %#v", got, want)
	}

	// Repeated call to PayloadDigest should be ok
	got = payloadBlock.PayloadDigest()
	if got, want := got, expectedDigest; got != want {
		t.Errorf("got = %#v, want %#v", got, want)
	}

	if payloadBlock.IsCached() {
		// Call to RawBytes after call to PayloadDigest should be ok
		_, err := payloadBlock.RawBytes()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	} else {
		// Call to RawBytes after call to PayloadDigest should fail
		_, err := payloadBlock.RawBytes()
		if err == nil {
			t.Fatalf("err: expected an error")
		}
	}
}

type isCachedTest struct {
	name string
	data io.Reader
	want bool
}

type rawBytesTest struct {
	name    string
	data    io.Reader
	wantErr bool
}

func validateRawBytesTest(t *testing.T, tt rawBytesTest, block Block, expectedContent string, expectedDigest string) {
	t.Helper()

	got, err := block.RawBytes()
	if tt.wantErr {
		if err == nil {
			t.Fatalf("err: expected an error")
		}
	} else {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	content, err := io.ReadAll(got)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := string(content), expectedContent; got != want {
		t.Errorf("string(content) = %#v, want %#v", got, want)
	}

	if block.IsCached() {
		// Repeated call to RawBytes should be ok
		got, err := block.RawBytes()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, err := io.ReadAll(got)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got, want := string(content), expectedContent; got != want {
			t.Errorf("string(content) = %#v, want %#v", got, want)
		}
	} else {
		// Repeated call to RawBytes should fail
		_, err := block.RawBytes()
		if err == nil {
			t.Fatalf("err: expected an error")
		}
	}

	// Call to BlockDigest after call to RawBytes should be ok
	gotDigest := block.BlockDigest()
	if got, want := gotDigest, expectedDigest; got != want {
		t.Errorf("gotDigest = %#v, want %#v", got, want)
	}
}

// ReplaceErrReader returns an io.Reader that returns err instead of io.EOF.
func ReplaceErrReader(r io.Reader, err error) io.Reader {
	return &replaceErrReader{r: r, err: err}
}

type replaceErrReader struct {
	r   io.Reader
	err error
}

func (r *replaceErrReader) Read(p []byte) (int, error) {
	i, e := r.r.Read(p)
	if e == io.EOF {
		e = r.err
	}
	return i, e
}

// equalHeaderFields compares header values without ordering them or changing
// either input. Repeated fields must occur the same number of times.
func equalHeaderFields(got, want WarcFields) bool {
	if len(got) != len(want) {
		return false
	}
	counts := make(map[nameValue]int, len(want))
	nils := 0
	for _, field := range want {
		if field == nil {
			nils++
		} else {
			counts[*field]++
		}
	}
	for _, field := range got {
		if field == nil {
			nils--
			if nils < 0 {
				return false
			}
		} else {
			if counts[*field] == 0 {
				return false
			}
			counts[*field]--
		}
	}
	return nils == 0
}

func TestEqualHeaderFields(t *testing.T) {
	a := &nameValue{Name: "A", Value: "one"}
	b := &nameValue{Name: "B", Value: "two"}
	for _, tt := range []struct {
		name      string
		got, want WarcFields
		equal     bool
	}{
		{"empty", nil, WarcFields{}, true},
		{"order", WarcFields{a, b}, WarcFields{b, a}, true},
		{"values", WarcFields{a}, WarcFields{&nameValue{Name: "A", Value: "one"}}, true},
		{"different values", WarcFields{a}, WarcFields{b}, false},
		{"duplicate counts", WarcFields{a, a, b}, WarcFields{a, b, b}, false},
		{"length", WarcFields{a}, WarcFields{a, a}, false},
		{"nil fields", WarcFields{nil, a}, WarcFields{a, nil}, true},
		{"nil mismatch", WarcFields{nil}, WarcFields{a}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalHeaderFields(tt.got, tt.want); got != tt.equal {
				t.Errorf("equalHeaderFields(%v, %v) = %v, want %v", tt.got, tt.want, got, tt.equal)
			}
		})
	}
}
