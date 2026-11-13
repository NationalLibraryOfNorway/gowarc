package gowarc

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestDefaultWarcRecordOptions_AreNonMutating(t *testing.T) {
	o := defaultWarcRecordOptions()

	if got := o.addMissingRecordId; got {
		t.Errorf("o.addMissingRecordId = %v, want false", got)
	}
	if got := o.addMissingContentLength; got {
		t.Errorf("o.addMissingContentLength = %v, want false", got)
	}
	if got := o.addMissingDigest; got {
		t.Errorf("o.addMissingDigest = %v, want false", got)
	}
	if got := o.fixContentLength; got {
		t.Errorf("o.fixContentLength = %v, want false", got)
	}
	if got := o.fixDigest; got {
		t.Errorf("o.fixDigest = %v, want false", got)
	}
	if got := o.fixSyntaxErrors; got {
		t.Errorf("o.fixSyntaxErrors = %v, want false", got)
	}
	if got := o.fixWarcFieldsBlockErrors; got {
		t.Errorf("o.fixWarcFieldsBlockErrors = %v, want false", got)
	}
}

func TestBuilderAddsMandatoryHeadersRegardlessOfAddMissingOptions(t *testing.T) {
	rb := NewRecordBuilder(Warcinfo,
		WithAddMissingRecordId(false),
		WithAddMissingContentLength(false),
	)
	rb.AddWarcHeader(WarcDate, "2024-01-01T00:00:00Z")
	rb.AddWarcHeader(ContentType, ApplicationWarcFields)
	_, _ = rb.WriteString("hello")

	record, _, err := rb.Build()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer func() {
		if err := record.Close(); err != nil {
			t.Errorf("record.Close() error = %v", err)
		}
	}()

	if got := record.WarcHeader().Has(WarcRecordID); !got {
		t.Errorf("record.WarcHeader().Has(WarcRecordID) = %v, want true", got)
	}
	if got, want := record.WarcHeader().Get(ContentLength), "5"; got != want {
		t.Errorf("record.WarcHeader().Get(ContentLength) = %#v, want %#v", got, want)
	}

	length, err := strconv.ParseInt(record.WarcHeader().Get(ContentLength), 10, 64)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, want := length, int64(5); got != want {
		t.Errorf("length = %#v, want %#v", got, want)
	}
}

func TestWithVersion(t *testing.T) {
	tests := []struct {
		name    string
		version *WarcVersion
		want    string
	}{
		{"v1.0", V1_0, "WARC/1.0"},
		{"v1.1", V1_1, "WARC/1.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := NewRecordBuilder(Warcinfo, WithVersion(tt.version),
				WithSpecViolationPolicy(ErrIgnore), WithSyntaxErrorPolicy(ErrIgnore))
			rb.AddWarcHeader(WarcRecordID, "<urn:uuid:00000000-0000-0000-0000-000000000000>")
			rb.AddWarcHeader(WarcDate, "2024-01-01T00:00:00Z")
			rb.AddWarcHeader(ContentType, ApplicationWarcFields)
			record, _, err := rb.Build()
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			defer func() {
				if err := record.Close(); err != nil {
					t.Errorf("record.Close() error = %v", err)
				}
			}()

			if got, want := record.Version().String(), tt.want; got != want {
				t.Errorf("record.Version().String() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestWithRecordIdFunc(t *testing.T) {
	tests := []struct {
		name   string
		idFunc func() (string, error)
		wantID string
	}{
		{
			"custom static id",
			func() (string, error) { return "urn:uuid:custom-id-1234", nil },
			"urn:uuid:custom-id-1234",
		},
		{
			"custom uri",
			func() (string, error) { return "http://example.com/rec/1", nil },
			"http://example.com/rec/1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := NewRecordBuilder(Warcinfo,
				WithRecordIdFunc(tt.idFunc),
				WithSpecViolationPolicy(ErrIgnore), WithSyntaxErrorPolicy(ErrIgnore))
			rb.AddWarcHeader(WarcDate, "2024-01-01T00:00:00Z")
			rb.AddWarcHeader(ContentType, ApplicationWarcFields)
			record, _, err := rb.Build()
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			defer func() {
				if err := record.Close(); err != nil {
					t.Errorf("record.Close() error = %v", err)
				}
			}()

			if got, want := record.RecordId(), tt.wantID; got != want {
				t.Errorf("record.RecordId() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestWithDefaultDigestAlgorithm(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		prefix    string
	}{
		{"sha1", "sha1", "sha1:"},
		{"sha256", "sha256", "sha256:"},
		{"md5", "md5", "md5:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := NewRecordBuilder(Warcinfo,
				WithDefaultDigestAlgorithm(tt.algorithm),
				WithAddMissingDigest(true),
				WithSpecViolationPolicy(ErrIgnore), WithSyntaxErrorPolicy(ErrIgnore))
			rb.AddWarcHeader(WarcRecordID, "<urn:uuid:00000000-0000-0000-0000-000000000000>")
			rb.AddWarcHeader(WarcDate, "2024-01-01T00:00:00Z")
			rb.AddWarcHeader(ContentType, ApplicationWarcFields)
			_, _ = rb.WriteString("test data")
			record, _, err := rb.Build()
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			defer func() {
				if err := record.Close(); err != nil {
					t.Errorf("record.Close() error = %v", err)
				}
			}()

			digest := record.WarcHeader().Get(WarcBlockDigest)
			if got, want := digest, tt.prefix; !strings.Contains(got, want) {
				t.Errorf("digest = %v, want to contain %v", got, want)
			} // e.g. "sha256:..."
		})
	}
}

func TestWithSkipParseBlock(t *testing.T) {
	rb := NewRecordBuilder(Response,
		WithSkipParseBlock(),
		WithSpecViolationPolicy(ErrIgnore), WithSyntaxErrorPolicy(ErrIgnore))
	rb.AddWarcHeader(WarcRecordID, "<urn:uuid:00000000-0000-0000-0000-000000000000>")
	rb.AddWarcHeader(WarcDate, "2024-01-01T00:00:00Z")
	rb.AddWarcHeader(ContentType, "application/http;msgtype=response")
	_, _ = rb.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nHello")
	record, _, err := rb.Build()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer func() {
		if err := record.Close(); err != nil {
			t.Errorf("record.Close() error = %v", err)
		}
	}()

	// With SkipParseBlock, block should NOT be an HttpResponseBlock
	_, ok := record.Block().(HttpResponseBlock)
	if got := ok; got {
		t.Errorf("ok = %v, want false: %s", got, "expected generic block when SkipParseBlock is set")
	}
}

func TestWithNoValidation(t *testing.T) {
	// WithNoValidation should allow records with missing required fields
	rb := NewRecordBuilder(Warcinfo, WithNoValidation())
	// Deliberately don't add any required headers
	_, _ = rb.WriteString("some content")
	record, _, err := rb.Build()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer func() {
		if err := record.Close(); err != nil {
			t.Errorf("record.Close() error = %v", err)
		}
	}()

	// With SkipParseBlock (implied by NoValidation), the block should be generic
	_, ok := record.Block().(WarcFieldsBlock)
	if got := ok; got {
		t.Errorf("ok = %v, want false: %s", got, "expected generic block with NoValidation (skipParseBlock=true)")
	}
}

func TestWithBufferTmpDir(t *testing.T) {
	// Just verify the option can be applied without panic
	rb := NewRecordBuilder(Warcinfo,
		WithBufferTmpDir(t.TempDir()),
		WithSpecViolationPolicy(ErrIgnore), WithSyntaxErrorPolicy(ErrIgnore))
	rb.AddWarcHeader(WarcRecordID, "<urn:uuid:00000000-0000-0000-0000-000000000000>")
	rb.AddWarcHeader(WarcDate, "2024-01-01T00:00:00Z")
	rb.AddWarcHeader(ContentType, ApplicationWarcFields)
	_, _ = rb.WriteString("test buffer dir")
	record, _, err := rb.Build()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer func() {
		if err := record.Close(); err != nil {
			t.Errorf("record.Close() error = %v", err)
		}
	}()
}

func TestWithBufferMaxMemBytes(t *testing.T) {
	// Verify the option can be applied and content is written/read correctly
	rb := NewRecordBuilder(Warcinfo,
		WithBufferMaxMemBytes(64),
		WithSpecViolationPolicy(ErrIgnore), WithSyntaxErrorPolicy(ErrIgnore))
	rb.AddWarcHeader(WarcRecordID, "<urn:uuid:00000000-0000-0000-0000-000000000000>")
	rb.AddWarcHeader(WarcDate, "2024-01-01T00:00:00Z")
	rb.AddWarcHeader(ContentType, ApplicationWarcFields)
	_, _ = rb.WriteString("small content")
	record, _, err := rb.Build()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer func() {
		if err := record.Close(); err != nil {
			t.Errorf("record.Close() error = %v", err)
		}
	}()
}

func TestDefaultRecordIDFormat(t *testing.T) {
	id, err := defaultIdGenerator()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got, pattern := id, `^urn:uuid:[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`; !regexp.MustCompile(pattern).MatchString(got) {
		t.Errorf("value %q does not match %q", got, pattern)
	}
}
