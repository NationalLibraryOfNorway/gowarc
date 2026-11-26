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
	"math"
	"net/http"
	"net/http/httputil"

	"github.com/nationallibraryofnorway/gowarc/v4/internal/diskbuffer"
	"golang.org/x/net/http/httpguts"
)

// HttpBlock is an archived HTTP request or response.
type HttpBlock interface {
	PayloadBlock
	ProtocolHeaderBlock
	HttpHeader() *http.Header
	// PayloadLength returns the number of entity bytes, or -1 for malformed framing.
	// Like PayloadDigest, it can consume an uncached body.
	PayloadLength() int64
	// HasTrailers reports whether the stored message carried actual trailer fields.
	HasTrailers() bool
}

type HttpRequestBlock interface {
	HttpBlock
	HttpRequestLine() string
}

type HttpResponseBlock interface {
	HttpBlock
	HttpStatusLine() string
	HttpStatusCode() int
}

var errMissingEndOfHeaders = errors.New("missing line separator at end of http headers")

// baseHttpBlock contains common fields and methods for HTTP request and response blocks
type baseHttpBlock struct {
	*genericBlock
	httpHeaderBytes     []byte
	payload             io.Reader
	payloadDigestString string
	httpHeader          *http.Header
	payloadDigest       *digest
	payloadDigestErr    error
	chunked             bool
	truncated           bool
	hasTrailers         bool
	decodedLength       int64
}

type httpRequestBlock struct {
	*baseHttpBlock
	httpRequestLine string
}

type httpResponseBlock struct {
	*baseHttpBlock
	httpStatusLine string
	httpStatusCode int
}

// Common methods for baseHttpBlock

func (block *baseHttpBlock) IsCached() bool {
	_, ok := block.payload.(io.Seeker)
	return ok
}

func (block *baseHttpBlock) Cache() error {
	if block.IsCached() {
		return nil
	}

	r, err := block.rawBody()
	if err != nil {
		return err
	}

	buf := diskbuffer.New(block.opts.bufferOptions...)
	_, err = buf.ReadFrom(r)
	if c, ok := block.payload.(io.Closer); ok {
		_ = c.Close()
	}
	block.blockDigestString = block.blockDigest.format()
	block.payloadDigestString = block.payloadDigest.format()
	block.payload = buf
	return err
}

func (block *baseHttpBlock) Close() error {
	if c, ok := block.payload.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (block *baseHttpBlock) RawBytes() (io.Reader, error) {
	r, err := block.rawBody()
	if err != nil {
		return nil, err
	}
	return io.MultiReader(bytes.NewReader(block.httpHeaderBytes), r), nil
}

func (block *baseHttpBlock) BlockDigest() string {
	if block.blockDigestString == "" {
		if block.filterReader == nil {
			block.filterReader = newDigestFilterReader(block.payload, block.blockDigest, block.payloadDigest)
		}
		_, _ = io.Copy(io.Discard, block.filterReader)
		block.blockDigestString = block.blockDigest.format()
		block.payloadDigestString = block.payloadDigest.format()
	}
	return block.blockDigestString
}

func (block *baseHttpBlock) PayloadLength() int64 {
	if block.chunked {
		return block.decodedLength
	}
	block.BlockDigest()
	return block.payloadDigest.count
}

func (block *baseHttpBlock) HasTrailers() bool { return block.hasTrailers }

func (block *baseHttpBlock) Size() int64 {
	block.BlockDigest()
	return block.blockDigest.count
}

func (block *baseHttpBlock) PayloadBytes() (io.Reader, error) {
	r, err := block.rawBody()
	if err != nil || !block.chunked {
		return r, err
	}
	return newChunkedEntity(r, block.truncated), nil
}

func (block *baseHttpBlock) rawBody() (io.Reader, error) {
	if r, ok := block.payload.(io.ReaderAt); ok {
		return io.NewSectionReader(r, 0, math.MaxInt64), nil
	}
	if block.blockDigestString == "" && block.filterReader == nil {
		block.filterReader = newDigestFilterReader(block.payload, block.blockDigest, block.payloadDigest)
		return block.filterReader, nil
	}

	if block.blockDigestString == "" {
		block.BlockDigest()
	}

	if !block.IsCached() {
		return nil, errContentReAccessed
	}

	if _, err := block.payload.(io.Seeker).Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return newDigestFilterReader(block.payload), nil
}

func (block *baseHttpBlock) PayloadDigest() string {
	block.BlockDigest()
	return block.payloadDigestString
}

// ProtocolHeaderBytes implements ProtocolHeaderBlock
func (block *baseHttpBlock) ProtocolHeaderBytes() []byte {
	return block.httpHeaderBytes
}

func (block *baseHttpBlock) HttpHeader() *http.Header {
	return block.httpHeader
}

// Request-specific methods

func (block *httpRequestBlock) HttpRequestLine() string {
	return block.httpRequestLine
}

func (block *httpRequestBlock) parseHeaders(headerBytes []byte) (err error) {
	request, e := http.ReadRequest(bufio.NewReader(bytes.NewReader(headerBytes)))
	if e != nil {
		err = e
		return
	}
	block.httpRequestLine = request.RequestURI
	block.chunked = len(request.TransferEncoding) > 0 && request.TransferEncoding[0] == "chunked" && request.Body != http.NoBody
	block.httpHeader = &request.Header
	return
}

func (block *httpRequestBlock) Write(w io.Writer) (int64, error) {
	p, err := block.RawBytes()
	if err != nil {
		return 0, err
	}
	bytesWritten, err := io.Copy(w, p)
	if err != nil {
		return bytesWritten, err
	}
	_, err = w.Write(crlf)
	bytesWritten += 2
	return bytesWritten, err
}

// Response-specific methods

func (block *httpResponseBlock) HttpStatusLine() string {
	return block.httpStatusLine
}

func (block *httpResponseBlock) HttpStatusCode() int {
	return block.httpStatusCode
}

func (block *httpResponseBlock) parseHeaders(headerBytes []byte) (err error) {
	response, e := http.ReadResponse(bufio.NewReader(bytes.NewReader(headerBytes)), nil)
	if e != nil {
		err = e
		return
	}
	block.httpStatusLine = response.Status
	block.httpStatusCode = response.StatusCode
	block.chunked = len(response.TransferEncoding) > 0 && response.TransferEncoding[0] == "chunked" && response.Body != http.NoBody
	block.httpHeader = &response.Header
	return
}

func (block *httpResponseBlock) Write(w io.Writer) (int64, error) {
	p, err := block.RawBytes()
	if err != nil {
		return 0, err
	}
	bytesWritten, err := io.Copy(w, p)
	if err != nil {
		return bytesWritten, err
	}
	_, err = w.Write(crlf)
	bytesWritten += 2
	return bytesWritten, err
}

// headerBytes reads the http-headers into a byte array.
func headerBytes(r buffer) ([]byte, error) {
	sepFound := false
	result := bytes.Buffer{}
	for {
		line, err := r.ReadBytes('\n')
		result.Write(line)
		if err != nil {
			break
		}
		if len(line) < 3 {
			sepFound = true
			break
		}
	}
	var err error
	if !sepFound {
		err = errMissingEndOfHeaders
	}
	return result.Bytes(), err
}

type buffer interface {
	Read(p []byte) (n int, err error)
	ReadBytes(delim byte) ([]byte, error)
	Peek(n int) ([]byte, error)
}

func newHttpBlock(opts *warcRecordOptions, wf *WarcFields, r io.Reader, blockDigest, payloadDigest *digest) (block PayloadBlock, validation []error, err error) {
	var rb buffer
	if v, ok := r.(diskbuffer.Buffer); ok {
		rb = v
	} else {
		rb = bufio.NewReader(r)
	}

	_, err = rb.Peek(4)
	if err != nil {
		return nil, nil, fmt.Errorf("not a http block: %w", err)
	}

	hb, herr := headerBytes(rb)
	if herr != nil {
		switch opts.errSyntax {
		case ErrWarn:
			validation = append(validation, herr)
		case ErrFail:
			return nil, validation, herr
		}
	}

	if herr == errMissingEndOfHeaders && opts.fixSyntaxErrors {
		// Fix header and update content-length field
		hb = append(hb, '\r', '\n')
		l, _ := wf.GetInt64(ContentLength)
		wf.SetInt64(ContentLength, l+2)
	}

	if _, err := blockDigest.Write(hb); err != nil {
		return nil, validation, err
	}

	var payload io.Reader
	if buf, ok := rb.(diskbuffer.Buffer); ok {
		payload = io.NewSectionReader(buf, int64(len(hb)), math.MaxInt64)
	} else {
		payload = rb
	}

	base := &baseHttpBlock{
		genericBlock:    &genericBlock{opts: opts, blockDigest: blockDigest},
		httpHeaderBytes: hb,
		payload:         payload,
		truncated:       wf.Has(WarcTruncated),
		payloadDigest:   payloadDigest,
	}
	if herr == errMissingEndOfHeaders && !opts.fixSyntaxErrors {
		// Fix the header for parsing while preserving the archived bytes.
		hb = append(hb, '\r', '\n')
	}
	var kind string
	if bytes.HasPrefix(hb, []byte("HTTP")) {
		resp := &httpResponseBlock{baseHttpBlock: base}
		block, kind = resp, "response"
		err = resp.parseHeaders(hb)
	} else {
		req := &httpRequestBlock{baseHttpBlock: base}
		block, kind = req, "request"
		err = req.parseHeaders(hb)
	}
	if err != nil && opts.errBlock > ErrIgnore {
		err = fmt.Errorf("error in http %s block: %w", kind, err)
		if opts.errBlock == ErrFail {
			return block, validation, err
		}
		validation = append(validation, err)
	}
	if base.chunked {
		if err := base.digestChunkedBody(wf); err != nil {
			return block, validation, err
		}
	}
	return block, validation, nil
}

// digestChunkedBody hashes the stored message for the block digest and the
// transfer-decoded bytes for the payload digest. Raw access retains the complete stored body.
func (block *baseHttpBlock) digestChunkedBody(wf *WarcFields) error {
	if !block.IsCached() {
		buf := diskbuffer.New(block.opts.bufferOptions...)
		if _, err := buf.ReadFrom(block.payload); err != nil {
			_ = buf.Close()
			return err
		}
		block.payload = buf
	}

	raw := io.TeeReader(block.payload, block.blockDigest)
	decoder := newChunkedEntity(raw, block.truncated)
	n, framingErr := io.Copy(block.payloadDigest, decoder)
	block.decodedLength = n
	block.hasTrailers = decoder.hasTrailers
	// The decoder can stop before the end of a malformed message.
	// Include all remaining stored bytes in the block digest.
	if _, err := io.Copy(io.Discard, raw); err != nil {
		return err
	}
	block.blockDigestString = block.blockDigest.format()
	if wf.Has(WarcSegmentNumber) {
		// Only the reassembled record has a meaningful payload digest.
		block.payloadDigest = nil
		return nil
	}
	if framingErr != nil {
		block.payloadDigest = nil
		block.decodedLength = -1
		block.payloadDigestErr = fmt.Errorf("cannot determine chunked payload digest: %w", framingErr)
		return nil
	}
	block.payloadDigestString = block.payloadDigest.format()
	return nil
}

// chunkedEntity shares the buffered input with the chunk reader so that
// trailers already fetched while decoding the last chunk remain available.
type chunkedEntity struct {
	input       *bufio.Reader
	chunks      io.Reader
	truncated   bool
	hasTrailers bool
	err         error
}

func newChunkedEntity(raw io.Reader, truncated bool) *chunkedEntity {
	input := bufio.NewReader(raw)
	return &chunkedEntity{input: input, chunks: httputil.NewChunkedReader(input), truncated: truncated}
}

func (r *chunkedEntity) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	n, err := r.chunks.Read(p)
	switch {
	case err == io.EOF:
		err = r.readTrailers()
	case r.truncated && errors.Is(err, io.ErrUnexpectedEOF):
		err = io.EOF
	}
	r.err = err
	return n, err
}

func (r *chunkedEntity) readTrailers() error {
	for {
		field, err := r.readTrailerField()
		if err != nil {
			return err
		}
		if !field {
			if _, err := r.input.Peek(1); err != nil {
				return err
			}
			return errors.New("unexpected bytes after HTTP trailers")
		}
		r.hasTrailers = true
	}
}

// readTrailerField validates one field without retaining its name or value.
// ReadSlice may split a long field anywhere, including just before the colon.
func (r *chunkedEntity) readTrailerField() (bool, error) {
	var haveName, inValue bool
	for {
		part, err := r.input.ReadSlice('\r')
		switch err {
		case nil:
			part = part[:len(part)-1]
		case bufio.ErrBufferFull:
		case io.EOF:
			return false, io.ErrUnexpectedEOF
		default:
			return false, err
		}
		if !inValue {
			var name []byte
			name, part, inValue = bytes.Cut(part, []byte(":"))
			if len(name) > 0 {
				if !httpguts.ValidHeaderFieldName(string(name)) {
					return false, errors.New("invalid HTTP trailer field name")
				}
				haveName = true
			}
		}
		if !httpguts.ValidHeaderFieldValue(string(part)) {
			return false, errors.New("invalid HTTP trailer field value")
		}
		if err == nil {
			break
		}
	}
	end, err := r.input.ReadByte()
	if err == io.EOF {
		return false, io.ErrUnexpectedEOF
	}
	if err != nil {
		return false, err
	}
	if end != '\n' {
		return false, errors.New("invalid HTTP trailer line ending")
	}
	if haveName != inValue {
		return false, errors.New("invalid HTTP trailer field")
	}
	return haveName, nil
}
