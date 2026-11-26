// Copyright 2026 National Library of Norway.
// SPDX-License-Identifier: Apache-2.0

package gowarc

import (
	"bufio"
	"fmt"
	"strings"
	"testing"
)

// The named-field rules in WARC 1.0 and 1.1 apply to individual headers.
// Rules requiring a referenced record or a complete segment series need file context.
func TestWARCHeaderSpec(t *testing.T) {
	const (
		all     = Warcinfo | Response | Resource | Request | Metadata | Revisit | Conversion | Continuation
		target  = Response | Resource | Request | Revisit | Conversion | Continuation
		capture = Response | Resource | Request | Metadata | Revisit
		payload = Response | Resource | Request | Revisit | Conversion | Continuation
	)
	fields := []struct {
		name, value       string
		allowed, required RecordType
	}{
		{WarcType, "", all, all},
		{WarcRecordID, "<urn:example:record>", all, all},
		{WarcDate, "2026-11-26T00:00:00Z", all, all},
		{ContentLength, "0", all, all},
		{ContentType, "text/plain; charset=utf-8", all, 0},
		{WarcTargetURI, "https://example.org/", target | Metadata, target},
		{WarcConcurrentTo, "<urn:example:related>", capture, 0},
		{WarcBlockDigest, "sha1:DA39A3EE5E6B4B0D3255BFEF95601890AFD80709", all, 0},
		{WarcPayloadDigest, "sha1:DA39A3EE5E6B4B0D3255BFEF95601890AFD80709", payload, 0},
		{WarcIPAddress, "2001:db8::1", capture, 0},
		{WarcRefersTo, "<urn:example:related>", Metadata | Revisit | Conversion, 0},
		{WarcRefersToTargetURI, "https://example.org/previous", Revisit, 0},
		{WarcRefersToDate, "2026-11-25T00:00:00Z", Revisit, 0},
		{WarcTruncated, "length", all, 0},
		{WarcWarcinfoID, "<urn:example:info>", all &^ Warcinfo, 0},
		{WarcFilename, "example.warc", Warcinfo, 0},
		{WarcProfile, "https://example.org/revisit-profile", Revisit, Revisit},
		{WarcIdentifiedPayloadType, "text/plain", payload, 0},
		{WarcSegmentNumber, "1", all, Continuation},
		{WarcSegmentOriginID, "<urn:example:origin>", Continuation, Continuation},
		{WarcSegmentTotalLength, "100", Continuation, 0},
	}

	for _, version := range []*WarcVersion{V1_0, V1_1} {
		for _, rt := range []RecordType{Warcinfo, Response, Resource, Request, Metadata, Revisit, Conversion, Continuation} {
			t.Run(version.String()+"/"+rt.String(), func(t *testing.T) {
				header := specTestHeader(rt)
				checkSpecHeader(t, header, version, false)
				for _, field := range fields {
					if version == V1_0 && (field.name == WarcRefersToTargetURI || field.name == WarcRefersToDate) {
						continue // Unrecognized extension fields in WARC 1.0.
					}
					value := field.value
					switch {
					case field.name == WarcType:
						value = rt.String()
					case field.name == WarcSegmentNumber && rt == Continuation:
						value = "2"
					}
					t.Run(field.name+"/present", func(t *testing.T) {
						wf := header.clone()
						wf.Set(field.name, value)
						checkSpecHeader(t, wf, version, field.allowed&rt == 0)
					})
					t.Run(field.name+"/absent", func(t *testing.T) {
						wf := header.clone()
						wf.Delete(field.name)
						checkSpecHeader(t, wf, version, field.required&rt != 0)
					})
					if field.allowed&rt != 0 {
						t.Run(field.name+"/duplicate", func(t *testing.T) {
							wf := header.clone()
							wf.Set(field.name, value)
							wf.Add(strings.ToLower(field.name), value)
							checkSpecHeader(t, wf, version, field.name != WarcConcurrentTo)
						})
					}
				}
			})
		}
	}
}

func TestWARCHeaderSpecValues(t *testing.T) {
	cases := []struct {
		name    string
		version *WarcVersion // nil applies to both versions
		rt      RecordType
		values  map[string]string
		invalid bool
	}{
		{"decimal with leading zero", nil, Resource, map[string]string{ContentLength: "08", ContentType: "text/plain"}, false},
		{"negative content length", nil, Resource, map[string]string{ContentLength: "-1", ContentType: "text/plain"}, true},
		{"signed content length", nil, Resource, map[string]string{ContentLength: "+1", ContentType: "text/plain"}, true},
		{"hexadecimal content length", nil, Resource, map[string]string{ContentLength: "0x10", ContentType: "text/plain"}, true},
		{"negative segment length", nil, Continuation, map[string]string{WarcSegmentTotalLength: "-1"}, true},
		{"hexadecimal segment length", nil, Continuation, map[string]string{WarcSegmentTotalLength: "0x10"}, true},
		{"continuation cannot be the first segment", nil, Continuation, map[string]string{WarcSegmentNumber: "1"}, true},
		{"initial segment must be first", nil, Resource, map[string]string{WarcSegmentNumber: "2"}, true},
		{"signed segment number", nil, Continuation, map[string]string{WarcSegmentNumber: "+2"}, true},
		{"content type is recommended", nil, Resource, map[string]string{ContentLength: "1"}, false},
		{"malformed content type", nil, Resource, map[string]string{ContentType: "text"}, true},
		{"malformed identified payload type", nil, Resource, map[string]string{WarcIdentifiedPayloadType: "text/"}, true},
		{"missing digest value", nil, Resource, map[string]string{WarcBlockDigest: "sha1:"}, true},
		{"missing digest separator", nil, Resource, map[string]string{WarcPayloadDigest: "sha1"}, true},
		{"digest containing whitespace", nil, Resource, map[string]string{WarcBlockDigest: "sha1:abc def"}, true},
		{"invalid IP address", nil, Resource, map[string]string{WarcIPAddress: "127.0.0.999"}, true},
		{"invalid record ID", nil, Resource, map[string]string{WarcRecordID: "urn:example:record"}, true},
		{"relative target URI", nil, Resource, map[string]string{WarcTargetURI: "relative/path"}, true},
		{"bracketed 1.0 target URI", V1_0, Resource, map[string]string{WarcTargetURI: "<https://example.org/>"}, false},
		{"bracketed 1.0 profile URI", V1_0, Revisit, map[string]string{WarcProfile: "<https://example.org/profile>"}, false},
		{"bracketed 1.1 target URI", V1_1, Resource, map[string]string{WarcTargetURI: "<https://example.org/>"}, true},
		{"invalid calendar date", nil, Resource, map[string]string{WarcDate: "2026-02-30T00:00:00Z"}, true},
		{"1.0 fractional seconds", V1_0, Resource, map[string]string{WarcDate: "2026-11-26T00:00:00.1Z"}, true},
		{"1.1 year", V1_1, Resource, map[string]string{WarcDate: "2026"}, false},
		{"1.1 year and month", V1_1, Resource, map[string]string{WarcDate: "2026-11"}, false},
		{"1.1 date", V1_1, Resource, map[string]string{WarcDate: "2026-11-26"}, false},
		{"1.1 minute precision", V1_1, Resource, map[string]string{WarcDate: "2026-11-26T00:00Z"}, false},
		{"1.1 nine fractional digits", V1_1, Resource, map[string]string{WarcDate: "2026-11-26T00:00:00.123456789Z"}, false},
		{"1.1 ten fractional digits", V1_1, Resource, map[string]string{WarcDate: "2026-11-26T00:00:00.1234567890Z"}, true},
		{"non-UTC time", nil, Resource, map[string]string{WarcDate: "2026-11-26T01:00:00+01:00"}, true},
		{"extension truncation reason", nil, Resource, map[string]string{WarcTruncated: "storage-limit"}, false},
		{"truncation reason with whitespace", nil, Resource, map[string]string{WarcTruncated: "storage limit"}, true},
		{"identical-payload revisit requires digest", V1_0, Revisit, map[string]string{WarcProfile: ProfileIdenticalPayloadDigestV1_0}, true},
		{"identical-payload revisit requires digest", V1_1, Revisit, map[string]string{WarcProfile: ProfileIdenticalPayloadDigestV1_1}, true},
		{"identical-payload revisit truncation reason", V1_1, Revisit, map[string]string{
			WarcProfile: ProfileIdenticalPayloadDigestV1_1, ContentLength: "1",
			ContentType: "text/plain", WarcPayloadDigest: "sha1:DA39A3EE5E6B4B0D3255BFEF95601890AFD80709",
			WarcTruncated: "time",
		}, true},
		{"unrecognized extension header", nil, Resource, map[string]string{"WARC-Example-Extension": "opaque data"}, false},
	}
	for _, version := range []*WarcVersion{V1_0, V1_1} {
		for _, tc := range cases {
			if tc.version != nil && tc.version != version {
				continue
			}
			t.Run(version.String()+"/"+tc.name, func(t *testing.T) {
				header := specTestHeader(tc.rt)
				for name, value := range tc.values {
					header.Set(name, value)
				}
				checkSpecHeader(t, header, version, tc.invalid)
			})
		}
	}
}

func TestWARCHeaderSpecVersionLine(t *testing.T) {
	for _, version := range []string{"1.0", "1.1", "1.0invalid", "1.10", "2.0"} {
		t.Run(version, func(t *testing.T) {
			raw := fmt.Sprintf("WARC/%s\r\n%s\r\n\r\n\r\n", version, specTestHeader(Resource))
			record, _, _, err := NewUnmarshaler(WithStrictValidation()).Unmarshal(bufio.NewReader(strings.NewReader(raw)))
			if record != nil {
				t.Cleanup(func() { _ = record.Close() })
			}
			wantErr := version != "1.0" && version != "1.1"
			if (err != nil) != wantErr {
				t.Fatalf("Unmarshal error = %v, want error %t", err, wantErr)
			}
		})
	}
}

func TestWARCHeaderSpecFieldSyntax(t *testing.T) {
	cases := []struct {
		name, line string
		invalid    bool
	}{
		{"UTF-8", "WARC-Extension: blåbær", false},
		{"folding", "WARC-Extension: first\r\n\tsecond", false},
		{"RFC 2047", "WARC-Extension: =?UTF-8?Q?bl=C3=A5b=C3=A6r?=", false},
		{"empty field name", ": value", true},
		{"space before colon", "WARC-Extension : value", true},
		{"space within field name", "WARC Extension: value", true},
		{"invalid UTF-8", "WARC-Extension: \xff", true},
		{"unescaped control character", "WARC-Extension: one\x00two", true},
	}
	for _, version := range []*WarcVersion{V1_0, V1_1} {
		for _, tc := range cases {
			t.Run(version.String()+"/"+tc.name, func(t *testing.T) {
				raw := fmt.Sprintf("%s\r\n%s%s\r\n\r\n\r\n\r\n", version, specTestHeader(Resource), tc.line)
				record, _, _, err := NewUnmarshaler(WithStrictValidation()).Unmarshal(bufio.NewReader(strings.NewReader(raw)))
				if record != nil {
					t.Cleanup(func() { _ = record.Close() })
				}
				if (err != nil) != tc.invalid {
					t.Fatalf("Unmarshal error = %v, want error %t", err, tc.invalid)
				}
			})
		}
	}
}

func specTestHeader(rt RecordType) *WarcFields {
	wf := &WarcFields{
		{Name: WarcType, Value: rt.String()},
		{Name: WarcRecordID, Value: "<urn:example:record>"},
		{Name: WarcDate, Value: "2026-11-26T00:00:00Z"},
		{Name: ContentLength, Value: "0"},
	}
	switch rt {
	case Response, Resource, Request, Revisit, Conversion, Continuation:
		wf.Set(WarcTargetURI, "https://example.org/")
	}
	switch rt {
	case Revisit:
		wf.Set(WarcProfile, "https://example.org/revisit-profile")
	case Continuation:
		wf.Set(WarcSegmentNumber, "2")
		wf.Set(WarcSegmentOriginID, "<urn:example:origin>")
	}
	return wf
}

func checkSpecHeader(t *testing.T, header *WarcFields, version *WarcVersion, wantErr bool) {
	t.Helper()
	_, _, err := validateHeader(header, version, newOptions(WithStrictValidation()))
	if (err != nil) != wantErr {
		t.Fatalf("validateHeader error = %v, want error %t", err, wantErr)
	}
}
