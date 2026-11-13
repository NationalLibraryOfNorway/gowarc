[![Tests](https://github.com/nationallibraryofnorway/gowarc/actions/workflows/test.yml/badge.svg?event=push)](https://github.com/nationallibraryofnorway/gowarc/actions/workflows/test.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/nationallibraryofnorway/gowarc/v3.svg)](https://pkg.go.dev/github.com/nationallibraryofnorway/gowarc/v3)

[Releases](https://github.com/nationallibraryofnorway/gowarc/releases) · [Apache-2.0 license](LICENSE)

# gowarc

A library for creating, parsing and evaluating WARC-files, written in go.

## Organization change: migrating from nlnwa

gowarc is moving from **nlnwa** to **nationallibraryofnorway**, the same
organization as whatwg-url. Releases under the new organization use the module
path below; the package name `gowarc` and major-version suffix `/v3` stay the same.

| Module | Previous path | New path |
| --- | --- | --- |
| gowarc | `github.com/nlnwa/gowarc/v3` | `github.com/nationallibraryofnorway/gowarc/v3` |
| whatwg-url | `github.com/nlnwa/whatwg-url` | `github.com/nationallibraryofnorway/whatwg-url` |

To upgrade to a release under the new organization:

1. Use **Go 1.27 or later**, including in CI.
2. Replace the old gowarc import prefix in your source, tests, and examples.
   If you import whatwg-url directly, replace that prefix too, preserving
   subpackage names such as `/url`.
3. Update dependencies and run your tests:

   ```bash
   go get github.com/nationallibraryofnorway/gowarc/v3@latest
   go mod tidy
   go test ./...
   ```

This migration upgrades whatwg-url from v0.6.2 to v0.8.0. Options passed to
`WithUrlParserOptions` must come from the new `whatwg-url/url` package; options
from the old import path are different Go types. Review the
[whatwg-url migration guide](https://github.com/nationallibraryofnorway/whatwg-url/blob/v0.8.0/README.md#upgrading-from-v062)
for API changes, including `WithReportValidationErrors(bool)` and numeric ports
in `WithSpecialSchemes(map[string]uint16)`.

Parser options now also apply to WARC IDs and references. See
[URI validation](#uri-validation) below for behavior changes to check when upgrading.

## What is WARC?

The WARC format offers a standard way to structure, manage and store billions of resources collected from the web and
elsewhere. It is used to build applications for harvesting, managing, accessing, mining and exchanging content.

To learn more about the WARC standard, read the specification
at https://iipc.github.io/warc-specifications/specifications/warc-format/warc-1.1/

## Library documentation

### Requirements

Go 1.27 or later is required. URL validation uses whatwg-url v0.8.0.

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
$ go get github.com/nationallibraryofnorway/gowarc/v3
```

### Create a new WARC record

To get you started, here is a simple example of how to create a new WARC record.

```go
package main

import (
	"fmt"
	"github.com/nationallibraryofnorway/gowarc/v3"
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

For complete documentation and examples consult the godoc online at: https://pkg.go.dev/github.com/nationallibraryofnorway/gowarc/v3


## Command line tools

[warchaeology](https://github.com/nationallibraryofnorway/warchaeology) is a command line tool based on gowarc.
