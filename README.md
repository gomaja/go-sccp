# go-sccp

[![CI status](https://github.com/gomaja/go-sccp/actions/workflows/ci.yml/badge.svg)](https://github.com/gomaja/go-sccp/actions/workflows/ci.yml)
[![Security](https://github.com/gomaja/go-sccp/actions/workflows/security.yml/badge.svg)](https://github.com/gomaja/go-sccp/actions/workflows/security.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gomaja/go-sccp.svg)](https://pkg.go.dev/github.com/gomaja/go-sccp)
[![License](https://img.shields.io/github/license/gomaja/go-sccp.svg)](https://github.com/gomaja/go-sccp/blob/main/LICENSE)

`go-sccp` is a Go implementation of SCCP, the Signalling Connection Control
Part of SS7 and SIGTRAN: the ITU-T Q.713 message codecs and an end-node ITU-T
Q.714 procedure stack with connectionless segmentation and reassembly.

The library performs no I/O and depends on no transport. The examples run it
over M3UA with [go-m3ua](https://github.com/gomaja/go-m3ua) and
[go-sctp](https://github.com/gomaja/go-sctp).

## Installation

```sh
go get github.com/gomaja/go-sccp@main
```

Track the `main` branch. Every version tagged so far is retracted, so ask for
`@main` explicitly. The public API follows `main` and may change.

It requires Go 1.26 or later.

## What it does

- **Codecs for every SCCP message** of ITU-T Q.713 (03/01) Table 1 and every
  parameter of Table 2, plus the SCMG messages of clause 5.
- **Conservative encoding**: canonical layout, every length, pointer and field
  range-checked — a value is never masked, wrapped or truncated — and the whole
  message checked against a maximum size, 268 octets by default (the 272-octet
  MTP-SDU of ITU-T Q.711 §7.1 minus the 4-octet routing label).
- **Liberal decoding**: messages of any size, gaps, parameters out of pointer
  order, unrecognised optional parameters (kept and re-encoded), a missing end of
  optional parameters octet, any spare bits.
- **Connectionless stack** (Q.714 §4): UDT or XUDT for short user data, up to 16
  XUDT segments for user data up to 3952 octets, reassembly with T(reassembly),
  duplicate and out-of-sequence detection and memory bounds, and the message
  return procedure with the Q.713 §3.12 causes, once per failed message.
- **Connection-oriented stack** (Q.714 §3), protocol class 2: establishment,
  refusal, release with T(rel)/T(repeat rel)/T(int), frozen references,
  inactivity control, M-bit segmentation and reassembly within memory bounds,
  the Annex B action tables with the abnormal cases of the Annex C diagrams,
  and a `MaintenanceIndication` when a release never completes.

[docs/standards.md](docs/standards.md) lists every rule with its clause, and
what is out of scope (global title translation beyond the final one at this
node, relaying, SCCP management procedures, protocol class 3).

## Messages

Messages are structs with exported fields. Optional parameters are pointers,
nil when absent.

```go
package main

import (
	"log"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

func main() {
	gt, err := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "447700900001")
	if err != nil {
		log.Fatal(err)
	}
	udt := &sccp.UDT{
		ProtocolClass:       params.ProtocolClass{Class: 0, ReturnOnError: true},
		CalledPartyAddress:  params.NewGlobalTitleAddress(gt, 6),
		CallingPartyAddress: params.NewSSNAddress(147),
		Data:                []byte{0x62, 0x03, 0x48, 0x01, 0x01},
	}
	wire, err := udt.MarshalBinary()
	if err != nil {
		log.Fatal(err) // e.g. sccp.ErrMessageTooLong above 268 octets
	}

	m, err := sccp.ParseMessage(wire)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%s", m)
}
```

`MarshalOptions{MaxMessageSize: n}.Marshal(m)` encodes for another maximum, up
to `sccp.MaxBroadbandMessageSize` (4091 octets) where every MTP-SAP is
broadband, which long unitdata (LUDT) needs.

## Stack

A `Stack` takes the SCCP messages the MTP delivered, with their routing label,
and returns what to send and what to deliver:

```go
stack, err := sccp.NewStack(sccp.StackConfig{PointCode: 0x0101})
if err != nil {
	log.Fatal(err)
}
stack.RegisterSubsystem(147)

// N-UNITDATA request: one UDT, or XUDT segments for long user data.
res, err := stack.Unitdata(sccp.UnitdataRequest{
	CalledPartyAddress:  params.NewGlobalTitleAddress(gt, 6),
	CallingPartyAddress: params.NewSSNAddress(147),
	ProtocolClass:       1,
	ReturnOnError:       true,
	Data:                tcapBegin,
	DPC:                 0x0202,
})
if err != nil {
	log.Fatal(err) // sccp.ErrInvalidRequest or sccp.ErrSegmentation
}
for _, tr := range res.Transfers {
	send(tr.OPC, tr.DPC, tr.SLS, tr.Data) // MTP-TRANSFER request, e.g. M3UA DATA
}

// Every message from the MTP, with its routing label.
res = stack.HandleTransfer(sccp.TransferIndication{
	RoutingLabel: sccp.RoutingLabel{OPC: opc, DPC: dpc, SLS: sls},
	Data:         payload,
})
for _, ind := range res.Indications {
	switch ind := ind.(type) {
	case *sccp.UnitdataIndication: // reassembled when it was segmented
		deliver(ind.CalledPartyAddress.SSN, ind.Data)
	case *sccp.NoticeIndication: // returned by the message return procedure
		log.Printf("returned: %s", ind.ReasonForReturn)
	case *sccp.MaintenanceIndication: // for the maintenance function
		log.Printf("%s: %s", ind.MessageType, ind.Reason)
	}
}
```

Timers run when the caller asks: call `HandleTimeout` when `NextTimeout` says
one is due, or let `RunTimers` do it in a goroutine. The clock is injectable
(`StackConfig.Now`), so tests drive every timer without sleeping.

## Examples

The [examples](examples) are a separate module that tracks the `main` branches
of go-m3ua and go-sctp:

- `examples/sgp`: an M3UA SGP that passes every DATA message to a stack and
  prints the N-UNITDATA indications, reassembled.
- `examples/asp`: an M3UA ASP that sends one N-UNITDATA request through the
  stack; its default user data is a TCAP Begin carrying a MAP
  unstructuredSS-Request (network-initiated USSD).

```sh
cd examples
go run ./sgp -addr 127.0.0.1:2905
go run ./asp -addr 127.0.0.1:2905 -segmentation-threshold 160
```

SCTP needs Linux.

## Validation

```sh
go build ./...
go test ./... -count=1
go test ./... -race -count=1
go vet ./...
~/go/bin/staticcheck ./...
~/go/bin/golangci-lint run ./...
(cd examples && go build ./... && go vet ./...)
```

The fuzz targets are `FuzzParseMessage`, `FuzzMarshalRoundTrip`, `FuzzSCMG`,
`FuzzUnitdataTransfer`, `FuzzConnectionTransfer`, `params.FuzzPartyAddress` and
`params.FuzzBCD`.

## License

MIT. See [LICENSE](LICENSE).
