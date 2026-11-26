[![Tests](https://github.com/nationallibraryofnorway/gowarc/actions/workflows/test.yml/badge.svg?event=push)](https://github.com/nationallibraryofnorway/gowarc/actions/workflows/test.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/nationallibraryofnorway/gowarc/v4.svg)](https://pkg.go.dev/github.com/nationallibraryofnorway/gowarc/v4)

[Releases](https://github.com/nationallibraryofnorway/gowarc/releases) · [Apache-2.0 license](LICENSE)

# gowarc

A library for creating, parsing and evaluating WARC-files, written in go.

## Migrating to v4.0.0

Use Go 1.27 or later. Change gowarc imports in source, tests, and examples to
`github.com/nationallibraryofnorway/gowarc/v4`, then run:

```sh
go get github.com/nationallibraryofnorway/gowarc/v4@v4.0.0
go mod tidy
go test ./...
```

Use `RawBytes()` to copy an archived HTTP message and `PayloadBytes()` to read
or verify its entity. This applies to requests and responses.

| Reader | Bytes returned | Matching WARC header |
| --- | --- | --- |
| `RawBytes()` | Complete stored HTTP message, including headers, chunk framing and trailers | `WARC-Block-Digest` |
| `PayloadBytes()` | Entity body after removing chunk framing and trailers, with content encodings such as gzip preserved | `WARC-Payload-Digest` |

`PayloadLength()` reports the decoded byte count. `HasTrailers()` reports
actual trailer fields, including undeclared fields. Malformed chunk framing
causes a payload read error and prevents digest generation. An explicitly
truncated capture ending mid-chunk yields the recoverable entity bytes.

This follows the [community annotation to WARC 1.1](https://iipc.github.io/warc-specifications/specifications/warc-format/warc-1.1-annotated/#warc-payload-digest)
and matches [Zeno’s response recorder](https://github.com/internetarchive/gowarc/blob/v0.8.103/dialer.go#L657-L704).
For archives retaining HTTP chunk framing, warcio 1.8.2 can report a mismatch
because it hashes the stored body including framing. Older gowarc records with
that digest also fail v4 payload validation. The default warning policy
preserves the stored header. To repair it, enable `WithFixDigest(true)` with
`WithSpecViolationPolicy(ErrWarn)`, then serialize the record. Repair changes
the digest used to look up identical-payload revisits.

Keep the full capture when `errors.Is(err, gowarc.ErrRevisitHasTrailers)`
holds after `ToRevisitRecord`. An empty trailer section remains eligible.
`Merge` returns a separately owned record. Close it when finished. It uses
the revisit's HTTP headers and the donor entity, regenerates framing and
recomputes the block digest and length.

Callers migrating from `github.com/nlnwa/gowarc/v3` should also update any
direct whatwg-url imports to `github.com/nationallibraryofnorway/whatwg-url`.
Options passed to `WithUrlParserOptions` must use types from the new module.
See [URI validation](#uri-validation) for parsing changes.

## What is WARC?

The WARC format offers a standard way to structure, manage and store billions of resources collected from the web and
elsewhere. It is used to build applications for harvesting, managing, accessing, mining and exchanging content.

To learn more about the WARC standard, read the specification
at https://iipc.github.io/warc-specifications/specifications/warc-format/warc-1.1/

## Library documentation

### Requirements

Go 1.27 or later is required. URL validation uses whatwg-url v0.9.1.

### URI validation

`WithUrlParserOptions` now applies to URI headers and the URIs inside WARC
record IDs and identifier references. Repeated calls append options in order.
Identifiers must still have exactly one correctly positioned outer `<...>`
pair; valid non-UUID identifiers such as `<urn:example:record>` are accepted.

Default parsing accepts recoverable WHATWG issues. To reject them, pass
`url.WithFailOnValidationError()` to `WithUrlParserOptions`; failures follow
`WithSpecViolationPolicy` (ignore, warn, or fail). The updated parser's
conformance fixes can change which inputs are accepted, but validation does
not normalize stored URI text. Failed header validation also preserves the
original value, including when a warning is returned.

### Installation

```
$ go get github.com/nationallibraryofnorway/gowarc/v4
```

### Create a new WARC record

To get you started, here is a simple example of how to create a new WARC record.

```go
package main

import (
	"fmt"
	"github.com/nationallibraryofnorway/gowarc/v4"
)

func main() {
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

	if wr, v, err := builder.Build(); err == nil {
		fmt.Println(wr, v)
	}
}
```

### godoc

For complete documentation and examples consult the godoc online at: https://pkg.go.dev/github.com/nationallibraryofnorway/gowarc/v4


## Command line tools

[warchaeology](https://github.com/nationallibraryofnorway/warchaeology) is a command line tool based on gowarc.
