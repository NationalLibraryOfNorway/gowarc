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
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nationallibraryofnorway/whatwg-url/url"
)

const (
	// WARC header field name constants
	ContentLength             = "Content-Length"
	ContentType               = "Content-Type"
	WarcBlockDigest           = "WARC-Block-Digest"
	WarcConcurrentTo          = "WARC-Concurrent-To"
	WarcDate                  = "WARC-Date"
	WarcFilename              = "WARC-Filename"
	WarcIPAddress             = "WARC-IP-Address"
	WarcIdentifiedPayloadType = "WARC-Identified-Payload-Type"
	WarcPayloadDigest         = "WARC-Payload-Digest"
	WarcProfile               = "WARC-Profile"
	WarcRecordID              = "WARC-Record-ID"
	WarcRefersTo              = "WARC-Refers-To"
	WarcRefersToDate          = "WARC-Refers-To-Date"
	WarcRefersToTargetURI     = "WARC-Refers-To-Target-URI"
	WarcSegmentNumber         = "WARC-Segment-Number"
	WarcSegmentOriginID       = "WARC-Segment-Origin-ID"
	WarcSegmentTotalLength    = "WARC-Segment-Total-Length"
	WarcTargetURI             = "WARC-Target-URI"
	WarcTruncated             = "WARC-Truncated"
	WarcType                  = "WARC-Type"
	WarcWarcinfoID            = "WARC-Warcinfo-ID"
	WarcPageID                = "WARC-Page-ID"       // Browsertrix extension field
	WarcResourceType          = "WARC-Resource-Type" // Browsertrix extension field
	WarcJSONMetadata          = "WARC-JSON-Metadata" // Browsertrix extension field
)

// validateHeader validates a WarcFields object as a WARC-record header.
func validateHeader(wf *WarcFields, version *WarcVersion, opts *warcRecordOptions) (rt RecordType, validation []error, err error) {
	rt, rtValidation, err := resolveRecordType(wf, opts)
	validation = append(validation, rtValidation...)
	if err != nil {
		return rt, validation, err
	}

	if opts.errSpec > ErrIgnore {
		for _, nv := range *wf {
			name, def := normalizeName(nv.Name)
			value, err := def.validationFunc(opts, name, nv.Value, version, rt, def)
			nv.Name = name
			if err == nil {
				nv.Value = value
			}
			if err != nil {
				switch opts.errSpec {
				case ErrWarn:
					validation = append(validation, newHeaderFieldError(name, err.Error()))
				case ErrFail:
					return rt, validation, newHeaderFieldError(name, err.Error())
				}
			}

			if !def.repeatable && len(wf.GetAll(name)) > 1 {
				switch opts.errSpec {
				case ErrWarn:
					validation = append(validation, newHeaderFieldError(name, "field occurs more than once"))
				case ErrFail:
					return rt, validation, newHeaderFieldError(name, "field occurs more than once")
				}
			}
		}

		for _, field := range requiredFields {
			if field.recordTypes&rt == 0 && field.recordTypes != 0 {
				continue
			}
			if !wf.Has(field.name) {
				err := missingHeaderField(field.name)
				if opts.errSpec == ErrFail {
					return rt, validation, err
				}
				validation = append(validation, err)
			}
		}

		contentLength, _ := wf.GetInt64(ContentLength)
		if rt != Continuation && contentLength > 0 && !wf.Has(ContentType) {
			validation = append(validation, newHeaderFieldError("", "missing recommended field: Content-Type"))
		}

		if rt == Revisit {
			profile := unbracketWARC10URI(version, wf.Get(WarcProfile))
			if profile == ProfileIdenticalPayloadDigestV1_0 || profile == ProfileIdenticalPayloadDigestV1_1 {
				var err error
				switch {
				case !wf.Has(WarcPayloadDigest):
					err = missingHeaderField(WarcPayloadDigest)
				case contentLength > 0 && wf.Get(WarcTruncated) != "length":
					err = newHeaderFieldError(WarcTruncated, "identical-payload-digest revisit with a block requires truncation reason 'length'")
				}
				if err != nil {
					if opts.errSpec == ErrFail {
						return rt, validation, err
					}
					validation = append(validation, err)
				}
			}
		}
	}
	return rt, validation, nil
}

func resolveRecordType(wf *WarcFields, opts *warcRecordOptions) (rt RecordType, validation []error, err error) {
	typeFieldNameLc := "warc-type"
	var typeField string
	for _, f := range *wf {
		if lowerASCII(f.Name) == typeFieldNameLc {
			typeField = f.Value
			break
		}
	}

	if typeField == "" {
		rt = 0
		switch opts.errSpec {
		case ErrIgnore:
		case ErrWarn:
			validation = append(validation, errors.New("missing required field WARC-Type"))
		case ErrFail:
			return rt, validation, errors.New("missing required field WARC-Type")
		}
	}
	typeFieldValLc := lowerASCII(typeField)
	rt = stringToRecordType(typeFieldValLc)
	if rt == 0 {
		switch opts.errUnknownRecordType {
		case ErrIgnore:
		case ErrWarn:
			validation = append(validation, fmt.Errorf("unrecognized value '%s' in field WARC-Type", typeField))
		case ErrFail:
			return rt, validation, fmt.Errorf("unrecognized value '%s' in field WARC-Type", typeField)
		}
	}

	return rt, validation, nil
}

// A zero mask applies to every record type, including extensions.
var requiredFields = []struct {
	name        string
	recordTypes RecordType
}{
	{WarcRecordID, 0},
	{ContentLength, 0},
	{WarcDate, 0},
	{WarcType, 0},
	{WarcTargetURI, Response | Resource | Request | Revisit | Conversion | Continuation},
	{WarcProfile, Revisit},
	{WarcSegmentNumber, Continuation},
	{WarcSegmentOriginID, Continuation},
}

func missingHeaderField(name string) error {
	return newHeaderFieldErrorf("", "missing required field: %s", name)
}

type fieldDef struct {
	name           string
	validationFunc func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (validatedValue string, err error)
	repeatable     bool
	supportedRec   RecordType
	supportedSpec  uint8
}

var fieldDefs = []fieldDef{
	{"", pUnknown, true,
		Warcinfo | Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{ContentLength, pLong, false,
		Warcinfo | Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{ContentType, pMediaType, false,
		Warcinfo | Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{WarcBlockDigest, pDigest, false,
		Warcinfo | Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{WarcConcurrentTo, pWarcId, true,
		Response | Resource | Request | Metadata | Revisit,
		V1_0.id | V1_1.id},
	{WarcDate, pTime, false,
		Warcinfo | Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{WarcFilename, pString, false,
		Warcinfo,
		V1_0.id | V1_1.id},
	{WarcIPAddress, pIp, false,
		Response | Resource | Request | Metadata | Revisit,
		V1_0.id | V1_1.id},
	{WarcIdentifiedPayloadType, pMediaType, false,
		Response | Resource | Request | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{WarcPayloadDigest, pDigest, false,
		Response | Resource | Request | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{WarcProfile, pURI, false,
		Revisit,
		V1_0.id | V1_1.id},
	{WarcRecordID, pWarcId, false,
		Warcinfo | Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{WarcRefersTo, pWarcId, false,
		Metadata | Revisit | Conversion,
		V1_0.id | V1_1.id},
	{WarcRefersToDate, pTime, false,
		Revisit,
		V1_1.id},
	{WarcRefersToTargetURI, pURI, false,
		Revisit,
		V1_1.id},
	{WarcSegmentNumber, pSegmentNumber, false,
		Warcinfo | Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{WarcSegmentOriginID, pWarcId, false,
		Continuation,
		V1_0.id | V1_1.id},
	{WarcSegmentTotalLength, pLong, false,
		Continuation,
		V1_0.id | V1_1.id},
	{WarcTargetURI, pURI, false,
		Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{WarcTruncated, pTruncReason, false,
		Warcinfo | Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{WarcType, pWarcType, false,
		Warcinfo | Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{WarcWarcinfoID, pWarcId, false,
		Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		V1_0.id | V1_1.id},
	{WarcPageID, pString, false,
		Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		0}, // Browsertrix extension field
	{WarcResourceType, pString, false,
		Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		0}, // Browsertrix extension field
	{WarcJSONMetadata, pString, false,
		Response | Resource | Request | Metadata | Revisit | Conversion | Continuation,
		0}, // Browsertrix extension field
}

// Map lower case header name to field definition
var lcHdrNameToDef = make(map[string]fieldDef)

func init() {
	for _, fd := range fieldDefs {
		lcHdrNameToDef[strings.ToLower(fd.name)] = fd
	}
}

func lowerASCII(s string) string {
	hasUpper := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			hasUpper = true
			break
		}
	}

	if !hasUpper {
		return s
	}

	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		} else {
			b[i] = c
		}
	}
	return string(b)
}

func normalizeName(name string) (string, fieldDef) {
	lcName := lowerASCII(name)
	if f, ok := lcHdrNameToDef[lcName]; ok {
		return f.name, f
	}
	return http.CanonicalHeaderKey(name), lcHdrNameToDef[""]
}

var (
	pUnknown = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		return value, nil
	}
	pString = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		if _, err := checkLegal(opts, name, version, recordType, def); err != nil {
			return "", err
		}
		return value, nil
	}
	pURI = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		if shouldValidate, err := checkLegal(opts, name, version, recordType, def); err != nil {
			return "", err
		} else if shouldValidate {
			if err := validateURI(opts, unbracketWARC10URI(version, value)); err != nil {
				return "", err
			}
		}
		return value, nil
	}
	pIp = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		if shouldValidate, err := checkLegal(opts, name, version, recordType, def); err != nil {
			return "", err
		} else if shouldValidate {
			if ip := net.ParseIP(value); ip == nil {
				return "", fmt.Errorf("illegal ip address: %s", value)
			}
		}
		return value, nil
	}
	pTime = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		if shouldValidate, err := checkLegal(opts, name, version, recordType, def); err != nil {
			return "", err
		} else if shouldValidate {
			if _, err := parseWARCDate(version, value); err != nil {
				return "", err
			}
		}
		return value, nil
	}
	pWarcType = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		if _, err := checkLegal(opts, name, version, recordType, def); err != nil {
			return "", err
		}
		return value, nil
	}
	pWarcId = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		if shouldValidate, err := checkLegal(opts, name, version, recordType, def); err != nil {
			return "", err
		} else if shouldValidate {
			v := strings.Trim(value, "<>")
			if value != "<"+v+">" {
				return "", fmt.Errorf("WARC id should be encapsulated by <>")
			}
			if err := validateURI(opts, v); err != nil {
				return "", err
			}
		}
		return value, nil
	}
	pSegmentNumber = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		shouldValidate, err := checkLegal(opts, name, version, recordType, def)
		if err != nil || !shouldValidate {
			return value, err
		}
		number, err := parseHeaderDecimal(value)
		if err != nil {
			return "", err
		}
		if recordType == Continuation && number < 2 {
			return "", fmt.Errorf("continuation segment number must be at least 2")
		}
		if recordType != Continuation && number != 1 {
			return "", fmt.Errorf("initial segment number must be 1")
		}
		return value, nil
	}
	pLong = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		if shouldValidate, err := checkLegal(opts, name, version, recordType, def); err != nil {
			return "", err
		} else if shouldValidate {
			if _, err := parseHeaderDecimal(value); err != nil {
				return "", err
			}
		}
		return value, nil
	}
	pMediaType = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		if shouldValidate, err := checkLegal(opts, name, version, recordType, def); err != nil {
			return "", err
		} else if shouldValidate {
			mediaType, _, err := mime.ParseMediaType(value)
			if err != nil || !strings.Contains(mediaType, "/") {
				return "", fmt.Errorf("invalid media type %q", value)
			}
		}
		return value, nil
	}
	pDigest = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		if shouldValidate, err := checkLegal(opts, name, version, recordType, def); err != nil {
			return "", err
		} else if shouldValidate {
			algorithm, digest, ok := strings.Cut(value, ":")
			// Retain support for the library's padded Base32 and Base64 encodings.
			digest = strings.ReplaceAll(strings.TrimRight(digest, "="), "/", "A")
			if !ok || !isHeaderToken(algorithm) || !isHeaderToken(digest) {
				return "", fmt.Errorf("invalid labelled digest %q", value)
			}
		}
		return value, nil
	}
	pTruncReason = func(opts *warcRecordOptions, name, value string, version *WarcVersion, recordType RecordType, def fieldDef) (string, error) {
		if shouldValidate, err := checkLegal(opts, name, version, recordType, def); err != nil {
			return "", err
		} else if shouldValidate && !isHeaderToken(value) {
			return "", fmt.Errorf("invalid truncation reason %q", value)
		}
		return value, nil
	}
)

func isHeaderToken(value string) bool {
	return value != "" && strings.IndexFunc(value, func(r rune) bool {
		return r <= ' ' || r >= 127 || strings.ContainsRune(`()<>@,;:\"/[]?={}`, r)
	}) < 0
}

func parseHeaderDecimal(value string) (int64, error) {
	if value == "" || strings.ContainsFunc(value, func(r rune) bool { return r < '0' || r > '9' }) {
		return 0, fmt.Errorf("expected decimal digits, got %q", value)
	}
	return strconv.ParseInt(value, 10, 64)
}

func unbracketWARC10URI(version *WarcVersion, value string) string {
	// The WARC 1.0 grammar uses brackets, while its examples often omit them.
	if version == V1_0 && strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
		return value[1 : len(value)-1]
	}
	return value
}

func parseWARCDate(version *WarcVersion, value string) (time.Time, error) {
	if version == V1_0 {
		if len(value) == len("2006-01-02T15:04:05Z") && strings.HasSuffix(value, "Z") {
			return time.Parse(time.RFC3339, value)
		}
		return time.Time{}, fmt.Errorf("expected WARC 1.0 UTC date in YYYY-MM-DDThh:mm:ssZ form, got %q", value)
	}

	switch len(value) {
	case len("2006"):
		return time.Parse("2006", value)
	case len("2006-01"):
		return time.Parse("2006-01", value)
	case len(time.DateOnly):
		return time.Parse(time.DateOnly, value)
	}

	clock, utc := strings.CutSuffix(value, "Z")
	if !utc {
		clock, utc = strings.CutSuffix(value, "+00:00")
	}
	if utc && len(clock) >= 16 && clock[13] == ':' {
		switch {
		case len(clock) == 16:
			return time.Parse("2006-01-02T15:04Z07:00", value)
		case len(clock) == 19, len(clock) >= 21 && len(clock) <= 29 && clock[19] == '.':
			parsed, err := time.Parse(time.RFC3339, value)
			return parsed.UTC(), err
		}
	}
	return time.Time{}, fmt.Errorf("invalid WARC UTC date %q", value)
}

func validateURI(opts *warcRecordOptions, value string) error {
	_, err := url.NewParser(opts.urlParserOptions...).Parse(value)
	return err
}

func checkLegal(opts *warcRecordOptions, name string, version *WarcVersion, recordType RecordType, def fieldDef) (shouldValidate bool, err error) {
	// All fields are allowed for unknown record types
	if recordType == 0 {
		return
	}

	// If field is not defined in spec version, skip validation
	if opts.errSpec > ErrIgnore && version.id&def.supportedSpec == 0 {
		return
	}

	if opts.errSpec > ErrIgnore && recordType&def.supportedRec == 0 {
		err = fmt.Errorf("illegal field '%v' in record type '%v'", name, recordType.String())
		return
	}
	shouldValidate = true
	return
}
