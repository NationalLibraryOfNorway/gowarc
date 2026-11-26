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

package gowarc_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"

	"github.com/nationallibraryofnorway/gowarc/v4"
	"github.com/nationallibraryofnorway/whatwg-url/url"
)

func ExampleWithUrlParserOptions() {
	builder := gowarc.NewRecordBuilder(gowarc.Resource,
		gowarc.WithSpecViolationPolicy(gowarc.ErrFail),
		gowarc.WithUrlParserOptions(url.WithFailOnValidationError()),
	)
	builder.AddWarcHeader(gowarc.WarcRecordID, "<urn:example:record>")
	builder.AddWarcHeader(gowarc.WarcDate, "2026-11-13T12:00:00Z")
	builder.AddWarcHeader(gowarc.WarcTargetURI, "https://example.org/%zz")
	_, _, err := builder.Build()
	fmt.Println(err != nil)
	// Output: true
}

func ExampleNewRecordBuilder() {
	builder := gowarc.NewRecordBuilder(gowarc.Response)
	_, err := builder.WriteString("HTTP/1.1 200 OK\nDate: Tue, 19 Sep 2016 17:18:40 GMT\nServer: Apache/2.0.54 (Ubuntu)\n" +
		"Last-Modified: Mon, 16 Jun 2013 22:28:51 GMT\nETag: \"3e45-67e-2ed02ec0\"\nAccept-Ranges: bytes\n" +
		"Content-Length: 19\nConnection: close\nContent-Type: text/plain\n\nThis is the content")
	if err != nil {
		panic(err)
	}
	builder.AddWarcHeader(gowarc.WarcRecordID, "<urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac120008>")
	builder.AddWarcHeader(gowarc.WarcDate, "2006-01-02T15:04:05Z")
	builder.AddWarcHeader(gowarc.ContentLength, "257")
	builder.AddWarcHeader(gowarc.ContentType, "application/http;msgtype=response")
	builder.AddWarcHeader(gowarc.WarcBlockDigest, "sha1:B285747AD7CC57AA74BCE2E30B453C8D1CB71BA4")

	if wr, _, err := builder.Build(); err == nil {
		fmt.Println(wr)
	}
	// Output: WARC record: version: WARC/1.1, type: response, id: urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac120008
}

func ExampleUnmarshaler() {
	data := bytes.NewBufferString("  WARC/1.1\r\n" +
		"WARC-Date: 2017-03-06T04:03:53Z\r\n" +
		"WARC-Record-ID: <urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac120008>\r\n" +
		"WARC-Filename: temp-20170306040353.warc.gz\r\n" +
		"WARC-Type: warcinfo\r\n" +
		"Content-Type: application/warc-fields\r\n" +
		"Warc-Block-Digest: sha1:af4d582b4ffc017d07a947d841e392a821f754f3\r\n" +
		"Content-Length: 34\r\n" +
		"\r\n" +
		"format: WARC File Format 1.1\r\n" +
		"\r\n\r\n")
	input := bufio.NewReader(data)

	// Create a new unmarshaler
	unmarshaler := gowarc.NewUnmarshaler(gowarc.WithSpecViolationPolicy(gowarc.ErrWarn), gowarc.WithSyntaxErrorPolicy(gowarc.ErrWarn))
	wr, off, validation, err := unmarshaler.Unmarshal(input)
	if err == nil {
		fmt.Printf("Offset: %d, %s\n", off, wr)
		if len(validation) > 0 {
			fmt.Println("Validation errors:")
			for i, e := range validation {
				fmt.Printf("  %d: %s\n", i+1, e)
			}
		}
	}

	// Output: Offset: 2, WARC record: version: WARC/1.1, type: warcinfo, id: urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac120008
	// Validation errors:
	//   1: gowarc: record was found 2 bytes after expected offset
	//   2: block: wrong digest: expected sha1:af4d582b4ffc017d07a947d841e392a821f754f3, computed: sha1:8a936f9fd60d664cf95b1ffb40f1c4093e65bb40
	//   3: too few bytes in end of record marker. Expected "\r\n\r\n", was ""
}

func ExampleNewWarcFileWriter() {
	nameGenerator := &gowarc.PatternNameGenerator{Directory: "directory-name"}

	w := gowarc.NewWarcFileWriter(gowarc.WithFileNameGenerator(nameGenerator))
	defer func() {
		_ = w.Close()
	}()

	builder := gowarc.NewRecordBuilder(gowarc.Response, gowarc.WithStrictValidation())
	_, err := builder.WriteString("HTTP/1.1 200 OK\r\nDate: Tue, 19 Sep 2016 17:18:40 GMT\r\nContent-Length: 19 ....")
	if err != nil {
		panic(err)
	}
	builder.AddWarcHeader(gowarc.WarcRecordID, "<urn:uuid:e9a0cecc-0221-11e7-adb1-0242ac120008>")
	builder.AddWarcHeader(gowarc.WarcDate, "2006-01-02T15:04:05Z")
	builder.AddWarcHeader(gowarc.ContentType, "application/http;msgtype=response")

	if wr, _, err := builder.Build(); err == nil {
		w.Write(wr)
	}
}

func ExampleNewWarcFileReader() {
	reader, err := gowarc.NewWarcFileReader("test.warc.gz", 0, gowarc.WithStrictValidation())
	if err != nil {
		fmt.Println("Error creating warc reader:", err)
		return
	}

	for {
		rec, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("Error reading record:", err)
			return
		}
		fmt.Println("Record type:", rec.WarcRecord.Type().String())
		fmt.Println("Record version:", rec.WarcRecord.Version())
		// Do more with record as per needs
	}

}

func ExampleHttpBlock() {
	builder := gowarc.NewRecordBuilder(gowarc.Response, gowarc.WithDefaultDigestAlgorithm("sha1"))
	builder.AddWarcHeader(gowarc.ContentType, "application/http;msgtype=response")
	builder.AddWarcHeader(gowarc.WarcDate, "2020-01-02T03:04:05Z")
	builder.AddWarcHeader(gowarc.WarcTargetURI, "https://example.org/")
	raw := "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\nX-Result: ok\r\n\r\n"
	if _, err := builder.WriteString(raw); err != nil {
		panic(err)
	}
	record, _, err := builder.Build()
	if err != nil {
		panic(err)
	}
	defer record.Close()
	block := record.Block().(gowarc.HttpBlock)
	entity, err := block.PayloadBytes()
	if err != nil {
		panic(err)
	}
	body, err := io.ReadAll(entity)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(body), block.PayloadDigest())
	fmt.Println(block.PayloadLength(), block.HasTrailers())
	// Output:
	// hello sha1:VL2MMHO4YXUKFWV63YHTWSBM3GXKSQ2N
	// 5 true
}
