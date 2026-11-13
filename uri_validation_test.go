package gowarc

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nationallibraryofnorway/whatwg-url/url"
)

func uriTestHeader() *WarcFields {
	wf := &WarcFields{}
	wf.Set(WarcType, "resource")
	wf.Set(WarcRecordID, "<urn:example:record>")
	wf.Set(WarcDate, "2026-11-13T12:00:00Z")
	wf.Set(ContentLength, "0")
	return wf
}

func TestWithUrlParserOptions(t *testing.T) {
	fields := []struct {
		name      string
		bracketed bool
	}{
		{WarcTargetURI, false}, {WarcRefersToTargetURI, false}, {WarcProfile, false},
		{WarcRecordID, true}, {WarcConcurrentTo, true}, {WarcRefersTo, true},
		{WarcSegmentOriginID, true}, {WarcWarcinfoID, true},
	}
	cases := []struct {
		name    string
		input   string
		opts    []WarcRecordOption
		invalid bool
	}{
		{name: "HTTP text preserved", input: "HTTP://Example.COM:80/a/../b?x=%2f#frag"},
		{name: "Unicode host", input: "https://blåbær.no/"},
		{name: "non UUID URN", input: "urn:example:record"},
		{name: "recoverable percent", input: "https://example.org/%zz"},
		{name: "strict percent", input: "https://example.org/%zz", opts: []WarcRecordOption{WithUrlParserOptions(url.WithFailOnValidationError())}, invalid: true},
		{name: "invalid host", input: "foo://example^example/", invalid: true},
		{name: "lax host", input: "foo://example^example/", opts: []WarcRecordOption{WithUrlParserOptions(url.WithLaxHostParsing())}},
		{name: "strict overrides lax", input: "foo://example^example/", opts: []WarcRecordOption{WithUrlParserOptions(url.WithLaxHostParsing()), WithUrlParserOptions(url.WithFailOnValidationError())}, invalid: true},
		{name: "fatal relative URI", input: "relative/path", invalid: true},
		{name: "custom numeric port", input: "custom://example.org:8080/", opts: []WarcRecordOption{WithUrlParserOptions(url.WithSpecialSchemes(map[string]uint16{"custom": 8080}))}},
		{name: "strict before lax", input: "foo://example^example/", opts: []WarcRecordOption{WithUrlParserOptions(url.WithFailOnValidationError()), WithUrlParserOptions(url.WithLaxHostParsing())}, invalid: true},
	}
	for _, field := range fields {
		for _, tc := range cases {
			for _, policy := range []ErrorPolicy{ErrIgnore, ErrWarn, ErrFail} {
				t.Run(fmt.Sprintf("%s/%s/policy=%d", field.name, tc.name, policy), func(t *testing.T) {
					wf := uriTestHeader()
					// These fields are legal on different record types.
					switch field.name {
					case WarcProfile, WarcRefersTo, WarcRefersToTargetURI:
						wf.Set(WarcType, "revisit")
					case WarcSegmentOriginID:
						wf.Set(WarcType, "continuation")
					}
					value := tc.input
					if field.bracketed {
						value = "<" + value + ">"
					}
					wf.Set(field.name, value)
					opts := append([]WarcRecordOption{WithSpecViolationPolicy(policy)}, tc.opts...)
					_, warnings, err := validateHeader(wf, V1_1, newOptions(opts...))
					if tc.invalid && policy == ErrFail {
						if err == nil {
							t.Fatalf("err: expected an error")
						}
					} else {
						if err != nil {
							t.Fatalf("unexpected error: %v", err)
						}
					}
					if tc.invalid && policy == ErrWarn {
						if got, want := len(warnings), 1; got != want {
							t.Fatalf("len(warnings) = %v, want %v", got, want)
						}
						var fieldErr *HeaderFieldError
						if err := warnings[0]; !errors.As(err, &fieldErr) {
							t.Fatalf("warnings[0] = %v, want an error assignable to %T", err, &fieldErr)
						}
						if got, want := fieldErr.FieldName, field.name; got != want {
							t.Errorf("fieldErr.FieldName = %#v, want %#v", got, want)
						}
					} else {
						if got := warnings; len(got) != 0 {
							t.Errorf("warnings = %v, want empty", got)
						}
					}
					if got, want := wf.Get(field.name), value; got != want {
						t.Errorf("wf.Get(field.name) = %#v, want %#v", got, want)
					}
				})
			}
		}
	}
}

func TestWarcIdentifierBrackets(t *testing.T) {
	for _, value := range []string{
		"urn:example:id", "<urn:example:id", "urn:example:id>",
		">urn:example:id<", "<<urn:example:id", "urn:example:id>>",
		"<urn:example:id<", ">urn:example:id>", "<<urn:example:id>>",
		"<urn:example:id>>", "<<urn:example:id>", "", "<>",
	} {
		t.Run(value, func(t *testing.T) {
			wf := uriTestHeader()
			wf.Set(WarcRecordID, value)
			_, _, err := validateHeader(wf, V1_1, newOptions(WithSpecViolationPolicy(ErrFail)))
			if err == nil {
				t.Fatalf("err: expected an error")
			}
			if got, want := wf.Get(WarcRecordID), value; got != want {
				t.Errorf("wf.Get(WarcRecordID) = %#v, want %#v", got, want)
			}
		})
	}
}

func TestHeaderValidationPreservesInvalidNonURIValues(t *testing.T) {
	for _, policy := range []ErrorPolicy{ErrIgnore, ErrWarn, ErrFail} {
		t.Run(fmt.Sprint(policy), func(t *testing.T) {
			wf := uriTestHeader()
			wf.Set(WarcDate, "not-a-date")
			_, warnings, err := validateHeader(wf, V1_1, newOptions(WithSpecViolationPolicy(policy)))
			if policy == ErrFail {
				if err == nil {
					t.Fatalf("err: expected an error")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if policy == ErrWarn {
				if got, want := len(warnings), 1; got != want {
					t.Fatalf("len(warnings) = %v, want %v", got, want)
				}
			} else {
				if got := warnings; len(got) != 0 {
					t.Errorf("warnings = %v, want empty", got)
				}
			}
			if got, want := wf.Get(WarcDate), "not-a-date"; got != want {
				t.Errorf("wf.Get(WarcDate) = %#v, want %#v", got, want)
			}
		})
	}
}

func TestURIValidationRecordPaths(t *testing.T) {
	for _, field := range []string{WarcTargetURI, WarcRecordID} {
		for _, policy := range []ErrorPolicy{ErrIgnore, ErrWarn, ErrFail} {
			for _, path := range []string{"builder", "unmarshaler"} {
				t.Run(fmt.Sprintf("%s/%d/%s", field, policy, path), func(t *testing.T) {
					wf := uriTestHeader()
					value := "https://example.org/%zz"
					if field == WarcRecordID {
						value = "<" + value + ">"
					}
					wf.Set(field, value)
					opts := []WarcRecordOption{WithSpecViolationPolicy(policy), WithUrlParserOptions(url.WithFailOnValidationError())}
					var record WarcRecord
					var warnings []error
					var err error
					if path == "builder" {
						builder := NewRecordBuilder(Resource, opts...)
						for _, nv := range *wf {
							if nv.Name != WarcType {
								builder.AddWarcHeader(nv.Name, nv.Value)
							}
						}
						record, warnings, err = builder.Build()
					} else {
						var raw strings.Builder
						raw.WriteString("WARC/1.1\r\n")
						for _, nv := range *wf {
							fmt.Fprintf(&raw, "%s: %s\r\n", nv.Name, nv.Value)
						}
						raw.WriteString("\r\n\r\n\r\n")
						record, _, warnings, err = NewUnmarshaler(opts...).Unmarshal(bufio.NewReader(strings.NewReader(raw.String())))
					}
					if record != nil {
						defer func() {
							if err := record.Close(); err != nil {
								t.Fatalf("record.Close() error = %v", err)
							}
						}()
					}
					if policy == ErrFail {
						if err == nil {
							t.Fatalf("err: expected an error")
						}
						var fieldErr *HeaderFieldError
						if !errors.As(err, &fieldErr) {
							t.Fatalf("err = %v, want an error assignable to %T", err, &fieldErr)
						}
						if got, want := fieldErr.FieldName, field; got != want {
							t.Errorf("fieldErr.FieldName = %#v, want %#v", got, want)
						}
						return
					}
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					if got := record; got == nil {
						t.Fatalf("record = %v, want non-nil", got)
					}
					if policy == ErrWarn {
						if got, want := len(warnings), 1; got != want {
							t.Fatalf("len(warnings) = %v, want %v", got, want)
						}
					} else {
						if got := warnings; len(got) != 0 {
							t.Errorf("warnings = %v, want empty", got)
						}
					}
					if got, want := record.WarcHeader().Get(field), value; got != want {
						t.Errorf("record.WarcHeader().Get(field) = %#v, want %#v", got, want)
					}
				})
			}
		}
	}
}
