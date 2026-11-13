[![Test](https://github.com/nationallibraryofnorway/gowarc/actions/workflows/test.yml/badge.svg)](https://github.com/nationallibraryofnorway/gowarc/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/release/nationallibraryofnorway/gowarc.svg)](https://github.com/nationallibraryofnorway/gowarc/releases/latest)
[![License](https://img.shields.io/github/license/nationallibraryofnorway/gowarc)](/LICENSE)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/nationallibraryofnorway/gowarc/v3.svg)](https://pkg.go.dev/github.com/nationallibraryofnorway/gowarc/v3)

# gowarc

A library for creating, parsing and evaluating WARC-files, written in go.

### What is WARC?

The WARC format offers a standard way to structure, manage and store billions of resources collected from the web and
elsewhere. It is used to build applications for harvesting, managing, accessing, mining and exchanging content.

To learn more about the WARC standard, read the specification
at https://iipc.github.io/warc-specifications/specifications/warc-format/warc-1.1/

## Library documentation

### Requirements and migration

Go 1.27 or later is required. The module is now
`github.com/nationallibraryofnorway/gowarc/v3`, and URL validation uses
`github.com/nationallibraryofnorway/whatwg-url v0.8.0`.

To migrate, replace imports of `github.com/nlnwa/gowarc/v3` with the new
module path. If you use `WithUrlParserOptions`, also replace imports of
`github.com/nlnwa/whatwg-url/url` with
`github.com/nationallibraryofnorway/whatwg-url/url`: parser options from the
old package are different Go types and cannot be passed to the new API.
Then update the dependency and run `go mod tidy`.

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

See the [whatwg-url migration guide](https://github.com/nationallibraryofnorway/whatwg-url/blob/v0.8.0/README.md#upgrading-from-v062)
for changed parser options, including `WithReportValidationErrors(bool)` and
numeric ports in `WithSpecialSchemes(map[string]uint16)`.

#### Installation

```
$ go get github.com/nationallibraryofnorway/gowarc/v3
```

#### Create a new WARC record

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
