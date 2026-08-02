# go-sccp

Package sccp provides simple and painless handling of SCCP (Signaling Connection Control Part) in SS7/SIGTRAN stack, implemented in the Go Programming Language.

[![CI status](https://github.com/gomaja/go-sccp/actions/workflows/ci.yml/badge.svg)](https://github.com/gomaja/go-sccp/actions/workflows/ci.yml)
[![Security](https://github.com/gomaja/go-sccp/actions/workflows/security.yml/badge.svg)](https://github.com/gomaja/go-sccp/actions/workflows/security.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/gomaja/go-sccp.svg)](https://pkg.go.dev/github.com/gomaja/go-sccp)
[![GitHub](https://img.shields.io/github/license/mashape/apistatus.svg)](https://github.com/gomaja/go-sccp/blob/main/LICENSE)

## Disclaimer

This is still an experimental project, and currently in its very early stage of development. Any part of implementations(including exported APIs) may be changed before released as v1.0.0.

## Getting started

Run `go mod tidy` to download the dependency, and you're ready to start developing.

## Supported Features

See [docs/standards.md](docs/standards.md) for the current standards baseline,
errata policy, open-source implementation survey, and remaining compliance
gaps.

### Message Types

| Message type                   | Abbreviation | Reference                  | Supported? |
| ------------------------------ | ------------ | -------------------------- | ---------- |
| Connection request             | CR           | ITU-T Q.713 (03/01) 4.2   | Yes        |
| Connection confirm             | CC           | ITU-T Q.713 (03/01) 4.3   | Yes        |
| Connection refused             | CREF         | ITU-T Q.713 (03/01) 4.4   | Yes        |
| Released                       | RLSD         | ITU-T Q.713 (03/01) 4.5   | Yes        |
| Release complete               | RLC          | ITU-T Q.713 (03/01) 4.6   | Yes        |
| Data form 1                    | DT1          | ITU-T Q.713 (03/01) 4.7   | Yes        |
| Data form 2                    | DT2          | ITU-T Q.713 (03/01) 4.8   | Yes        |
| Data acknowledgement           | AK           | ITU-T Q.713 (03/01) 4.9   | Yes        |
| Unitdata                       | UDT          | ITU-T Q.713 (03/01) 4.10  | Yes        |
| Unitdata service               | UDTS         | ITU-T Q.713 (03/01) 4.11  | Yes        |
| Expedited data                 | ED           | ITU-T Q.713 (03/01) 4.12  | Yes        |
| Expedited data acknowledgement | EA           | ITU-T Q.713 (03/01) 4.13  | Yes        |
| Reset request                  | RSR          | ITU-T Q.713 (03/01) 4.14  | Yes        |
| Reset confirm                  | RSC          | ITU-T Q.713 (03/01) 4.15  | Yes        |
| Protocol data unit error       | ERR          | ITU-T Q.713 (03/01) 4.16  | Yes        |
| Inactivity test                | IT           | ITU-T Q.713 (03/01) 4.17  | Yes        |
| Extended unitdata              | XUDT         | ITU-T Q.713 (03/01) 4.18  | Yes        |
| Extended unitdata service      | XUDTS        | ITU-T Q.713 (03/01) 4.19  | Yes        |
| Long unitdata                  | LUDT         | ITU-T Q.713 (03/01) 4.20  | Yes        |
| Long unitdata service          | LUDTS        | ITU-T Q.713 (03/01) 4.21  | Yes        |

### Parameters

| Parameter name              | Reference                  | Supported? |
| --------------------------- | -------------------------- | ---------- |
| End of optional parameters  | ITU-T Q.713 (03/01) 3.1   | Yes        |
| Destination local reference | ITU-T Q.713 (03/01) 3.2   | Yes        |
| Source local reference      | ITU-T Q.713 (03/01) 3.3   | Yes        |
| Called party address        | ITU-T Q.713 (03/01) 3.4   | Yes        |
| Calling party address       | ITU-T Q.713 (03/01) 3.5   | Yes        |
| Protocol class              | ITU-T Q.713 (03/01) 3.6   | Yes        |
| Segmenting/reassembling     | ITU-T Q.713 (03/01) 3.7   | Yes        |
| Receive sequence number     | ITU-T Q.713 (03/01) 3.8   | Yes        |
| Sequencing/segmenting       | ITU-T Q.713 (03/01) 3.9   | Yes        |
| Credit                      | ITU-T Q.713 (03/01) 3.10  | Yes        |
| Release cause               | ITU-T Q.713 (03/01) 3.11  | Yes        |
| Return cause                | ITU-T Q.713 (03/01) 3.12  | Yes        |
| Reset cause                 | ITU-T Q.713 (03/01) 3.13  | Yes        |
| Error cause                 | ITU-T Q.713 (03/01) 3.14  | Yes        |
| Refusal cause               | ITU-T Q.713 (03/01) 3.15  | Yes        |
| Data                        | ITU-T Q.713 (03/01) 3.16  | Yes        |
| Segmentation                | ITU-T Q.713 (03/01) 3.17  | Yes        |
| Hop counter                 | ITU-T Q.713 (03/01) 3.18  | Yes        |
| Importance                  | ITU-T Q.713 (03/01) 3.19  | Yes        |
| Long data                   | ITU-T Q.713 (03/01) 3.20  | Yes        |

## Author(s)

go-sccp authors and [contributors](https://github.com/gomaja/go-sccp/graphs/contributors).

## LICENSE

[MIT](https://github.com/gomaja/go-sccp/blob/main/LICENSE)
