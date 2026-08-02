# go-sccp

[![CI status](https://github.com/gomaja/go-sccp/actions/workflows/ci.yml/badge.svg)](https://github.com/gomaja/go-sccp/actions/workflows/ci.yml)
[![Security](https://github.com/gomaja/go-sccp/actions/workflows/security.yml/badge.svg)](https://github.com/gomaja/go-sccp/actions/workflows/security.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gomaja/go-sccp.svg)](https://pkg.go.dev/github.com/gomaja/go-sccp)
[![License](https://img.shields.io/github/license/gomaja/go-sccp.svg)](https://github.com/gomaja/go-sccp/blob/main/LICENSE)

`go-sccp` is a Go implementation of SCCP, the Signalling Connection Control
Part used in SS7 and SIGTRAN stacks.

The package currently provides complete ITU-T Q.713 message and parameter
encoding/decoding, plus the first Q.714 connection-section procedure helpers.
It is suitable for building, parsing, validating, and testing SCCP wire
messages while the full procedure stack is being completed.

## Status

This module is pre-v1. Public APIs may still change while the Q.714 procedure
layer is completed.

Implemented:

- ITU-T Q.713 (03/01) message codecs for all SCCP message types.
- ITU-T Q.713 (03/01) parameter codecs for the full standard parameter set.
- Strict parser coverage for short buffers and malformed optional parameter
  lengths.
- Fuzz targets for top-level message parsing and parameter parsing.
- Initial ITU-T Q.714 (05/01) connection-section helpers for CC/CREF setup
  outcomes, RLSD/RLC release handling, reference mismatch detection, and frozen
  reference state.
- CI for build, tests, race tests, vet, static analysis, vulnerability checks,
  portability, and security scanning.

Remaining stack work:

- Q.714 connectionless routing, return-on-error handling, and management
  procedure integration.
- Q.714 local-reference allocation, freeze/guard timers, and relay coupling.
- Q.714 class 2/class 3 connection-oriented data-transfer procedures.
- Q.714 class 3 reset, sequencing, flow control, expedited data, inactivity
  testing, and timer behavior.

See [docs/standards.md](docs/standards.md) for the standards baseline, errata
policy, open-source survey, and detailed compliance ledger.

## Install

```sh
go get github.com/gomaja/go-sccp
```

## Message Encoding

```go
package main

import (
	"log"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

func main() {
	msg := sccp.NewUDT(
		0,
		true,
		params.NewCalledPartyAddress(0x42, 0, 6, nil),
		params.NewCallingPartyAddress(0x42, 0, 7, nil),
		[]byte("payload"),
	)

	wire, err := msg.MarshalBinary()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("%x", wire)
}
```

## Message Parsing

```go
package main

import (
	"log"

	"github.com/gomaja/go-sccp"
)

func main() {
	msg, err := sccp.ParseMessage([]byte{
		0x09, 0x80, 0x03, 0x06, 0x09,
		0x02, 0x42, 0x06,
		0x02, 0x42, 0x07,
		0x07, 'p', 'a', 'y', 'l', 'o', 'a', 'd',
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("%s", msg.MessageTypeName())
}
```

## Connection-Section Procedures

```go
package main

import (
	"log"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

func main() {
	section := sccp.NewPendingConnectionSection(0x010203, 3, 8)

	confirm := sccp.NewCC(0x010203, 0x040506, 2, false)
	if err := section.HandleConnectionConfirm(confirm); err != nil {
		log.Fatal(err)
	}

	released, err := section.Release(params.ReleaseCauseSCCPUserOriginated)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("%s to %06x", released.MessageTypeName(), released.DestinationLocalReference.Uint32())
}
```

## Supported Messages

| Message type | Abbreviation | Reference | Codec |
| --- | --- | --- | --- |
| Connection request | CR | ITU-T Q.713 (03/01) 4.2 | Yes |
| Connection confirm | CC | ITU-T Q.713 (03/01) 4.3 | Yes |
| Connection refused | CREF | ITU-T Q.713 (03/01) 4.4 | Yes |
| Released | RLSD | ITU-T Q.713 (03/01) 4.5 | Yes |
| Release complete | RLC | ITU-T Q.713 (03/01) 4.6 | Yes |
| Data form 1 | DT1 | ITU-T Q.713 (03/01) 4.7 | Yes |
| Data form 2 | DT2 | ITU-T Q.713 (03/01) 4.8 | Yes |
| Data acknowledgement | AK | ITU-T Q.713 (03/01) 4.9 | Yes |
| Unitdata | UDT | ITU-T Q.713 (03/01) 4.10 | Yes |
| Unitdata service | UDTS | ITU-T Q.713 (03/01) 4.11 | Yes |
| Expedited data | ED | ITU-T Q.713 (03/01) 4.12 | Yes |
| Expedited data acknowledgement | EA | ITU-T Q.713 (03/01) 4.13 | Yes |
| Reset request | RSR | ITU-T Q.713 (03/01) 4.14 | Yes |
| Reset confirm | RSC | ITU-T Q.713 (03/01) 4.15 | Yes |
| Protocol data unit error | ERR | ITU-T Q.713 (03/01) 4.16 | Yes |
| Inactivity test | IT | ITU-T Q.713 (03/01) 4.17 | Yes |
| Extended unitdata | XUDT | ITU-T Q.713 (03/01) 4.18 | Yes |
| Extended unitdata service | XUDTS | ITU-T Q.713 (03/01) 4.19 | Yes |
| Long unitdata | LUDT | ITU-T Q.713 (03/01) 4.20 | Yes |
| Long unitdata service | LUDTS | ITU-T Q.713 (03/01) 4.21 | Yes |

## Supported Parameters

| Parameter name | Reference | Codec |
| --- | --- | --- |
| End of optional parameters | ITU-T Q.713 (03/01) 3.1 | Yes |
| Destination local reference | ITU-T Q.713 (03/01) 3.2 | Yes |
| Source local reference | ITU-T Q.713 (03/01) 3.3 | Yes |
| Called party address | ITU-T Q.713 (03/01) 3.4 | Yes |
| Calling party address | ITU-T Q.713 (03/01) 3.5 | Yes |
| Protocol class | ITU-T Q.713 (03/01) 3.6 | Yes |
| Segmenting/reassembling | ITU-T Q.713 (03/01) 3.7 | Yes |
| Receive sequence number | ITU-T Q.713 (03/01) 3.8 | Yes |
| Sequencing/segmenting | ITU-T Q.713 (03/01) 3.9 | Yes |
| Credit | ITU-T Q.713 (03/01) 3.10 | Yes |
| Release cause | ITU-T Q.713 (03/01) 3.11 | Yes |
| Return cause | ITU-T Q.713 (03/01) 3.12 | Yes |
| Reset cause | ITU-T Q.713 (03/01) 3.13 | Yes |
| Error cause | ITU-T Q.713 (03/01) 3.14 | Yes |
| Refusal cause | ITU-T Q.713 (03/01) 3.15 | Yes |
| Data | ITU-T Q.713 (03/01) 3.16 | Yes |
| Segmentation | ITU-T Q.713 (03/01) 3.17 | Yes |
| Hop counter | ITU-T Q.713 (03/01) 3.18 | Yes |
| Importance | ITU-T Q.713 (03/01) 3.19 | Yes |
| Long data | ITU-T Q.713 (03/01) 3.20 | Yes |

## Validation

The local validation target for Go changes is:

```sh
go build ./...
go test ./... -count=1
go vet ./...
~/go/bin/staticcheck ./...
~/go/bin/golangci-lint run ./...
```

Protocol changes should also run race tests and the relevant fuzz targets.

## License

MIT. See [LICENSE](LICENSE).
