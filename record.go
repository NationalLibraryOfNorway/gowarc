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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http/httputil"
	"strconv"
	"strings"
	"time"

	"github.com/nationallibraryofnorway/gowarc/v4/internal/diskbuffer"
)

const (
	sphtcrlf = " \t\r\n" // Space, Tab, Carriage return, Newline
	cr       = '\r'      // Carriage return
	lf       = '\n'      // Newline
	sp       = ' '       // Space
	ht       = '\t'      // Tab
	// crlf     = "\r\n"     // Carriage return, Newline
	// crlfcrlf = "\r\n\r\n" // Carriage return, Newline, Carriage return, Newline
	// fieldSep = ": " // Field separator in WARC header fields
)

var (
	crlf      = []byte("\r\n")
	crlfcrlf  = []byte("\r\n\r\n")
	fieldSep  = []byte(": ")
	warcMagic = []byte("WARC/")
	v1_0      = []byte(V1_0.txt)
	v1_1      = []byte(V1_1.txt)
)

// WarcRecord is the interface implemented by types that can represent a WARC record.
// A new instance of WarcRecord is created by a [WarcRecordBuilder].
type WarcRecord interface {
	// Version returns the WARC version of the record.
	Version() *WarcVersion

	// Type returns the WARC record type.
	Type() RecordType

	// WarcHeader returns the WARC header fields.
	WarcHeader() *WarcFields

	// Block returns the content block of the record.
	Block() Block

	// RecordId returns the WARC-Record-ID header field.
	RecordId() string

	// ContentLength returns the Content-Length header field.
	ContentLength() (int64, error)

	// Date returns the WARC-Date header field.
	Date() (time.Time, error)

	// String returns a string representation of the record.
	String() string

	// Closer closes the record and releases any resources associated with it.
	io.Closer

	// ToRevisitRecord takes RevisitRef referencing the record we want to make a revisit of and returns a revisit record.
	ToRevisitRecord(ref *RevisitRef) (WarcRecord, error)

	// RevisitRef extracts a RevisitRef from the current record if it is a revisit record.
	RevisitRef() (*RevisitRef, error)

	// CreateRevisitRef creates a RevisitRef which references the current record.
	//
	// The RevisitRef might be used by another record's ToRevisitRecord to create a revisit record referencing this record.
	CreateRevisitRef(profile string) (*RevisitRef, error)

	// Merge reconstructs an independently owned record from this revisit and its donor.
	// Close the returned record to release its storage.
	//
	// It is implemented only for revisit records, but this function will be enhanced to also support segmented records.
	Merge(record ...WarcRecord) (WarcRecord, error)

	// ValidateDigest validates block and payload digests if present.
	//
	// If option FixDigest is set, an invalid or missing digest will be corrected in the header.
	// Digest validation requires the whole content block to be read. As a side effect the
	// Content-Length field is also validated, and if option FixContentLength is set, a wrong
	// content length will be corrected in the header.
	//
	// If the record is not cached, it might not be possible to read any content from this
	// record after validation.
	//
	// The returned values depend on the [ErrorPolicy] options:
	//   - [ErrIgnore]: only fatal errors are returned via err.
	//   - [ErrWarn]: non-fatal findings are collected in validation; err is nil.
	//   - [ErrFail]: the first validation failure is returned via err.
	ValidateDigest() (validation []error, err error)
}

// WarcVersion represents a WARC specification version.
//
// For record creation, only WARC 1.0 and 1.1 are supported which are represented by the constants [V1_0] and [V1_1].
// During parsing of a record, the WarcVersion will take on the version value found in the record itself.
type WarcVersion struct {
	id    uint8
	txt   string
	major uint8
	minor uint8
}

// String returns a string representation of the WARC version in the format used by WARC files i.e. 'WARC/1.0' or 'WARC/1.1'.
func (v *WarcVersion) String() string {
	return "WARC/" + v.txt
}

func (v *WarcVersion) Major() uint8 {
	return v.major
}

func (v *WarcVersion) Minor() uint8 {
	return v.minor
}

var (
	// WARC versions
	V1_0 = &WarcVersion{id: 1, txt: "1.0", major: 1, minor: 0} // WARC 1.0
	V1_1 = &WarcVersion{id: 2, txt: "1.1", major: 1, minor: 1} // WARC 1.1
)

// RecordType represents the type of a WARC record.
type RecordType uint16

// String returns a string representation of the record type.
func (rt RecordType) String() string {
	switch rt {
	case 1:
		return "warcinfo"
	case 2:
		return "response"
	case 4:
		return "resource"
	case 8:
		return "request"
	case 16:
		return "metadata"
	case 32:
		return "revisit"
	case 64:
		return "conversion"
	case 128:
		return "continuation"
	default:
		return "unknown"
	}
}

func stringToRecordType(rt string) RecordType {
	switch rt {
	case "warcinfo":
		return 1
	case "response":
		return 2
	case "resource":
		return 4
	case "request":
		return 8
	case "metadata":
		return 16
	case "revisit":
		return 32
	case "conversion":
		return 64
	case "continuation":
		return 128
	default:
		return 0
	}
}

type RevisitRef struct {
	Profile        string
	TargetRecordId string
	TargetUri      string
	TargetDate     string
}

const (
	// WARC record types
	Warcinfo     RecordType = 1
	Response     RecordType = 2
	Resource     RecordType = 4
	Request      RecordType = 8
	Metadata     RecordType = 16
	Revisit      RecordType = 32
	Conversion   RecordType = 64
	Continuation RecordType = 128
)

const (
	// Well known content types
	ApplicationWarcFields = "application/warc-fields"
	ApplicationHttp       = "application/http"
)

const (
	// Well known revisit profiles
	ProfileIdenticalPayloadDigestV1_1 = "http://netpreserve.org/warc/1.1/revisit/identical-payload-digest"
	ProfileServerNotModifiedV1_1      = "http://netpreserve.org/warc/1.1/revisit/server-not-modified"
	ProfileIdenticalPayloadDigestV1_0 = "http://netpreserve.org/warc/1.0/revisit/identical-payload-digest"
	ProfileServerNotModifiedV1_0      = "http://netpreserve.org/warc/1.0/revisit/server-not-modified"
)

type warcRecord struct {
	opts       *warcRecordOptions
	version    *WarcVersion
	headers    *WarcFields
	recordType RecordType
	block      Block
	closer     func() error
}

func (wr *warcRecord) Version() *WarcVersion { return wr.version }

func (wr *warcRecord) Type() RecordType { return wr.recordType }

func (wr *warcRecord) WarcHeader() *WarcFields { return wr.headers }

func (wr *warcRecord) Block() Block {
	return wr.block
}

func (wr *warcRecord) RecordId() string {
	return wr.headers.GetId(WarcRecordID)
}

func (wr *warcRecord) ContentLength() (int64, error) {
	return wr.headers.GetInt64(ContentLength)
}

func (wr *warcRecord) Date() (time.Time, error) {
	return wr.headers.GetTime(WarcDate)
}

func (wr *warcRecord) String() string {
	return fmt.Sprintf("WARC record: version: %s, type: %s, id: %s", wr.version, wr.Type(), wr.WarcHeader().GetId(WarcRecordID))
}

func (wr *warcRecord) Close() error {
	if wr.closer != nil {
		err := wr.closer()
		wr.closer = nil
		return err
	}
	return nil
}

func (wr *warcRecord) ToRevisitRecord(ref *RevisitRef) (WarcRecord, error) {
	h := wr.headers.clone()

	switch ref.Profile {
	case ProfileIdenticalPayloadDigestV1_0, ProfileIdenticalPayloadDigestV1_1:
		b, parsedHTTP := wr.block.(HttpBlock)
		if parsedHTTP && b.HasTrailers() {
			return nil, ErrRevisitHasTrailers
		}
		if !h.Has(WarcPayloadDigest) && wr.recordType == Resource && h.Has(WarcBlockDigest) {
			h.Set(WarcPayloadDigest, h.Get(WarcBlockDigest))
		}
		if !h.Has(WarcPayloadDigest) || (parsedHTTP && b.PayloadDigest() == "") {
			return nil, ErrMissingPayloadDigest
		}
	case ProfileServerNotModifiedV1_0:
	case ProfileServerNotModifiedV1_1:
	default:
		return nil, ErrUnknownRevisitProfile
	}

	h.Set(WarcType, Revisit.String())
	h.Set(WarcProfile, ref.Profile)
	if ref.TargetRecordId != "" {
		h.SetId(WarcRefersTo, ref.TargetRecordId)
	}
	if ref.TargetUri != "" {
		h.Set(WarcRefersToTargetURI, ref.TargetUri)
	}
	if ref.TargetDate != "" {
		h.Set(WarcRefersToDate, ref.TargetDate)
	}
	h.Set(WarcTruncated, "length")

	block, err := newRevisitBlock(wr.opts, wr.block)
	if err != nil {
		return nil, err
	}
	h.Set(WarcBlockDigest, block.BlockDigest())
	h.SetInt(ContentLength, len(block.headerBytes))

	revisit := &warcRecord{
		opts:       wr.opts,
		version:    wr.version,
		recordType: Revisit,
		headers:    h,
		block:      block,
	}
	return revisit, nil
}

func (wr *warcRecord) RevisitRef() (*RevisitRef, error) {
	if wr.recordType != Revisit {
		return nil, ErrNotRevisitRecord
	}

	return &RevisitRef{
		Profile:        unbracketWARC10URI(wr.version, wr.headers.Get(WarcProfile)),
		TargetRecordId: wr.headers.GetId(WarcRefersTo),
		TargetUri:      wr.headers.Get(WarcRefersToTargetURI),
		TargetDate:     wr.headers.Get(WarcRefersToDate),
	}, nil
}

func (wr *warcRecord) CreateRevisitRef(profile string) (*RevisitRef, error) {
	if wr.recordType == Revisit {
		return nil, ErrIsRevisitRecord
	}

	return &RevisitRef{
		Profile:        profile,
		TargetRecordId: wr.headers.GetId(WarcRecordID),
		TargetUri:      unbracketWARC10URI(wr.version, wr.headers.Get(WarcTargetURI)),
		TargetDate:     wr.headers.Get(WarcDate),
	}, nil
}

func (wr *warcRecord) Merge(record ...WarcRecord) (WarcRecord, error) {
	if number, _ := wr.headers.GetInt(WarcSegmentNumber); number == 1 {
		return nil, ErrMergeSegmentedNotImplemented
	}
	if wr.recordType != Revisit {
		return nil, ErrMergeNotSupported
	}
	if len(record) != 1 {
		return nil, ErrMergeRequiresOneRecord
	}

	revisit, ok := wr.block.(*revisitBlock)
	if !ok {
		return nil, ErrMergeWrongBlockType
	}
	donor := record[0]
	h := wr.headers.clone()
	merged := &warcRecord{opts: wr.opts, version: wr.version, headers: h, recordType: donor.Type()}
	ref, _ := wr.RevisitRef()
	h.Set(WarcType, donor.Type().String())
	for _, field := range []string{WarcRefersTo, WarcRefersToTargetURI, WarcRefersToDate, WarcProfile, WarcTruncated} {
		h.Delete(field)
	}
	if donor.WarcHeader().Has(WarcTruncated) {
		h.Set(WarcTruncated, donor.WarcHeader().Get(WarcTruncated))
	}

	bd, err := newDigestFromField(merged, WarcBlockDigest)
	if err != nil {
		return nil, err
	}
	pd, err := newDigestFromField(merged, WarcPayloadDigest)
	if err != nil {
		return nil, err
	}
	base := &baseHttpBlock{genericBlock: &genericBlock{opts: wr.opts, blockDigest: bd}, payloadDigest: pd}
	var parse func([]byte) error

	switch donor.Block().(type) {
	case HttpRequestBlock:
		b := &httpRequestBlock{baseHttpBlock: base}
		merged.block, parse = b, b.parseHeaders
	case HttpResponseBlock:
		b := &httpResponseBlock{baseHttpBlock: base}
		merged.block, parse = b, b.parseHeaders
	default:
		return nil, ErrMergeUnsupportedBlock
	}
	head, err := headerBytes(bufio.NewReader(bytes.NewReader(revisit.headerBytes)))
	if err != nil {
		if wr.opts.errSyntax == ErrFail {
			return nil, err
		}
		head = append(head, cr, lf)
	}
	if err := parse(head); err != nil {
		return nil, err
	}
	checkLength := true
	if resp, ok := merged.block.(*httpResponseBlock); ok {
		checkLength = resp.httpStatusCode >= 200 && resp.httpStatusCode != 204 && resp.httpStatusCode != 304
	}
	base.httpHeaderBytes = head
	_, _ = bd.Write(head) // hash.Hash.Write always succeeds.
	entity, err := donor.Block().(PayloadBlock).PayloadBytes()
	if err != nil {
		return nil, err
	}
	buf := diskbuffer.New(wr.opts.bufferOptions...)
	if err := writeRevisitEntity(io.MultiWriter(buf, bd), base, io.TeeReader(entity, pd), checkLength); err != nil {
		return nil, errors.Join(err, buf.Close())
	}
	base.payload = buf
	merged.closer = buf.Close
	base.decodedLength = pd.count
	base.blockDigestString = bd.format()
	base.payloadDigestString = pd.format()
	h.Set(WarcBlockDigest, base.blockDigestString)
	bd.updateDigest()
	h.SetInt64(ContentLength, base.Size())
	if ref.Profile == ProfileIdenticalPayloadDigestV1_0 || ref.Profile == ProfileIdenticalPayloadDigestV1_1 {
		if pd.hash != "" {
			if err := pd.validate(); err != nil {
				return nil, errors.Join(fmt.Errorf("revisit payload: %w", err), merged.Close())
			}
		} else {
			h.Set(WarcPayloadDigest, base.PayloadDigest())
		}
	}
	return merged, nil
}

func writeRevisitEntity(buf io.Writer, base *baseHttpBlock, entity io.Reader, checkLength bool) error {
	if base.chunked {
		chunks := httputil.NewChunkedWriter(buf)
		if _, err := io.Copy(chunks, entity); err != nil {
			return err
		}
		if err := chunks.Close(); err != nil {
			return err
		}
		_, err := buf.Write(crlf)
		return err
	}
	n, err := io.Copy(buf, entity)
	if err != nil {
		return err
	}
	if field := base.httpHeader.Get("Content-Length"); checkLength && field != "" {
		expected, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return err
		}
		if expected != n {
			return fmt.Errorf("HTTP Content-Length: %w", &ContentLengthError{Expected: expected, Actual: n})
		}
	}
	return nil
}

func (wr *warcRecord) parseBlock(reader io.Reader) (validation []error, err error) {
	blockDigest, err := newDigestFromField(wr, WarcBlockDigest)
	if err != nil {
		return nil, err
	}
	payloadDigest, err := newDigestFromField(wr, WarcPayloadDigest)
	if err != nil {
		return nil, err
	}

	if !wr.opts.skipParseBlock {
		contentType := strings.ToLower(wr.headers.Get(ContentType))
		if wr.recordType&(Response|Resource|Request|Conversion|Continuation) != 0 {
			if strings.HasPrefix(contentType, ApplicationHttp) {
				wr.block, validation, err = newHttpBlock(wr.opts, wr.headers, reader, blockDigest, payloadDigest)
				return
			}
		}
		if wr.recordType == Revisit {
			wr.block, err = parseRevisitBlock(wr.opts, reader, blockDigest, wr.headers.Get(WarcPayloadDigest))
			return
		}
		if strings.HasPrefix(contentType, ApplicationWarcFields) {
			wr.block, validation, err = newWarcFieldsBlock(wr.opts, wr.headers, reader, blockDigest)
			return
		}
	}

	wr.block = newGenericBlock(wr.opts, reader, blockDigest)
	return
}

// ValidateDigest validates block and payload digests if present.
//
// If option FixDigest is set, an invalid or missing digest will be corrected in the header.
// Digest validation requires the whole content block to be read. As a side effect the
// Content-Length field is also validated, and if option FixContentLength is set, a wrong
// content length will be corrected in the header.
//
// If the record is not cached, it might not be possible to read any content from this
// record after validation.
//
// The returned values depend on the [ErrorPolicy] options:
//
//	[ErrIgnore]: only fatal errors are returned via err.
//	[ErrWarn]: non-fatal findings are collected in validation; err is nil.
//	[ErrFail]: the first validation failure is returned via err.
func (wr *warcRecord) ValidateDigest() (validation []error, err error) {
	if wr.opts.errSpec > ErrIgnore || wr.opts.addMissingDigest {
		if err = wr.Block().Cache(); err != nil {
			return nil, err
		}
		wr.Block().BlockDigest()

		size := strconv.FormatInt(wr.block.Size(), 10)
		if wr.WarcHeader().Has(ContentLength) && size != wr.headers.Get(ContentLength) {
			headerLen, _ := wr.headers.GetInt64(ContentLength)
			clErr := &ContentLengthError{Expected: headerLen, Actual: wr.block.Size()}
			switch wr.opts.errSpec {
			case ErrWarn:
				validation = append(validation, clErr)
				if wr.opts.fixContentLength {
					wr.WarcHeader().Set(ContentLength, size)
				}
			case ErrFail:
				return nil, clErr
			}
		}
	}

	var blockDigest, payloadDigest *digest
	var payloadDigestErr error
	switch v := wr.Block().(type) {
	case *genericBlock:
		blockDigest = v.blockDigest
		if wr.recordType == Resource {
			payloadDigest = blockDigest
		}
	case *httpRequestBlock:
		blockDigest = v.blockDigest
		payloadDigest = v.payloadDigest
		payloadDigestErr = v.payloadDigestErr
	case *httpResponseBlock:
		blockDigest = v.blockDigest
		payloadDigest = v.payloadDigest
		payloadDigestErr = v.payloadDigestErr
	case *revisitBlock:
		blockDigest = v.blockDigest
	case *warcFieldsBlock:
		blockDigest = v.blockDigest
	}

	if blockDigest != nil {
		if blockDigest.hash == "" {
			// Missing digest header is allowed, so skip validation. But if addMissingDigest option is set, a header will be added.
			if wr.opts.addMissingDigest {
				wr.WarcHeader().Set(WarcBlockDigest, blockDigest.format())
			}
		} else if wr.opts.errSpec > ErrIgnore {
			if err := blockDigest.validate(); err != nil {
				switch wr.opts.errSpec {
				case ErrIgnore:
				case ErrWarn:
					validation = append(validation, fmt.Errorf("block: %w", err))
					if wr.opts.fixDigest {
						// Digest validation failed. But if fixDigest option is set, the calculated digest will be set.
						blockDigest.updateDigest()
						wr.WarcHeader().Set(WarcBlockDigest, blockDigest.format())
					}
				case ErrFail:
					return nil, fmt.Errorf("block: %w", err)
				}
			}
		}
	}

	if wr.Type() == Revisit || wr.WarcHeader().Has(WarcSegmentNumber) {
		// Can't check payload digest for revisit records or segmented records since the payload digest is a digest of
		// the original record
		return validation, nil
	}

	if payloadDigestErr != nil {
		policy := wr.opts.errBlock
		if wr.headers.Has(WarcPayloadDigest) || wr.opts.addMissingDigest {
			policy = max(policy, wr.opts.errSpec)
		}
		switch policy {
		case ErrFail:
			return nil, payloadDigestErr
		case ErrWarn:
			validation = append(validation, payloadDigestErr)
		}
		return validation, nil
	}
	if payloadDigest != nil {
		if payloadDigest.hash == "" {
			// Missing digest header is allowed, so skip validation. But if addMissingDigest option is set, a header will be added.
			if wr.opts.addMissingDigest {
				wr.WarcHeader().Set(WarcPayloadDigest, payloadDigest.format())
			}
		} else if wr.opts.errSpec > ErrIgnore {
			if err := payloadDigest.validate(); err != nil {
				switch wr.opts.errSpec {
				case ErrIgnore:
				case ErrWarn:
					validation = append(validation, fmt.Errorf("payload: %w", err))
					// Digest validation failed. But if fixDigest option is set, the calculated digest will be set.
					if wr.opts.fixDigest {
						payloadDigest.updateDigest()
						wr.WarcHeader().Set(WarcPayloadDigest, payloadDigest.format())
					}
				case ErrFail:
					return nil, fmt.Errorf("payload: %w", err)
				}
			}
		}
	}
	return validation, nil
}
