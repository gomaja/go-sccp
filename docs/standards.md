# Standards and Compliance Ledger

This project implements SCCP message and parameter encoding/decoding for the
SS7/SIGTRAN stack. SCCP wire formats are governed primarily by ITU-T and ETSI
specifications; IETF RFCs are relevant for adjacent SIGTRAN adaptation and SCTP
transport layers.

Last verified: 2026-08-02.

## Normative SCCP Baseline

| Area | Current document | Status checked | Project scope |
| ---- | ---------------- | -------------- | ------------- |
| Functional description | ITU-T Q.711 (03/01) | In force | SCCP service model and class behavior |
| Message definitions | ITU-T Q.712 (07/96) | In force | SCCP message set |
| Formats and codes | ITU-T Q.713 (03/01) | In force | Wire format, message type codes, parameter codes |
| Procedures | ITU-T Q.714 (05/01) | In force | Connectionless, connection-oriented, and management procedures |
| User guidance | ITU-T Q.715 (04/02) | In force | Interoperability guidance |
| Performance | ITU-T Q.716 (03/93) | In force | Performance guidance and measurement context |

Older Q.711, Q.712, Q.713, Q.714, and Q.716 components shown by ITU as
superseded are not valid baselines for new code.

## ETSI Profile

ETSI EN 300 009-1 V1.4.3 (2000-12) applies ITU-T Q.711 through Q.716 with
modifications for international interconnection. It covers connectionless
classes 0 and 1 and connection-oriented class 2, excluding embedded connection
set-up. It also excludes class 3, permanent signalling connections, flow
control, expedited data, and reset functions from that ETSI profile. LUDT and
LUDTS need not be provided in that profile; if they are provided, they follow
the endorsed ITU-T recommendations unless ETSI modifies them.

The default package target is ITU-T international SCCP. ETSI restrictions must
be represented as a profile, not silently applied to the default codec.

## Adjacent IETF Specifications

| Area | Current document | RFC Editor state | Datatracker relationship check |
| ---- | ---------------- | ---------------- | ------------------------------ |
| SUA | RFC 3868 | Proposed Standard, no obsoletes/updates/updated_by/obsoleted_by | No RFC found pointing at it with `obs` or `updates` |
| M3UA | RFC 4666 | Proposed Standard; obsoletes RFC 3332; no updated_by/obsoleted_by | No RFC found pointing at it with `obs` or `updates` |
| SCTP | RFC 9260 | Proposed Standard; obsoletes RFC 4460, RFC 4960, RFC 6096, RFC 7053, RFC 8540; no updated_by/obsoleted_by | No RFC found pointing at it with `obs` or `updates` |

RFC 3332 and RFC 4960 are obsolete and must not be cited as current. RFC 3868
and RFC 4666 still contain older normative references to RFC 2960 through their
publication history; code and documentation in this repository should cite RFC
9260 for current SCTP behavior.

Errata policy:

- RFC 3868: RFC Editor JSON currently reports no errata URL.
- RFC 4666: current errata page shows Held for Document Update (2) and
  Rejected (1), with no verified entries in the default current page.
- RFC 9260: current errata page shows Verified (5), Held for Document Update
  (1), and Rejected (3). Verified errata are normative for SCTP-adjacent work;
  held and rejected errata must be reported before acting on them.

## Open-Source Go SCCP Survey

| Repository | Role | Findings |
| ---------- | ---- | -------- |
| User-provided dedicated Go SCCP codec | Dedicated Go SCCP codec | Current local code is still close to this implementation. Recent merged PRs fixed UDT/XUDT pointer offsets, little-endian SCCP point-code encoding, XUDT support, SCMG support, and robustness checks. Closed unmerged CR/CC/CREF work means connection-oriented support still needs a fresh spec-backed design. |
| github.com/fkgi/gsmap | Broader MAP/TCAP/SCCP/xUA stack | Useful for stack-level comparison, but SCCP message-codec coverage appears narrower than this repository's UDT/XUDT/SCMG surface and has little test coverage. No current GitHub issues or PRs were found. |

Searches for other Go `package sccp` implementations found Cisco Skinny Client
Control Protocol code and unrelated repositories; those are not SS7 SCCP
implementations.

## Current Implementation Gaps

- `ParseMessage` currently dispatches every SCCP message type defined in
  ITU-T Q.713 (03/01), Table 1. The implemented scope is still message
  encoding/decoding, not the complete Q.714 connection-oriented or
  connectionless procedure state machines.
- Connection-oriented message wire formats are implemented from ITU-T Q.713
  (03/01), sections 4.2 through 4.9 and 4.12 through 4.17. A first
  connection-section procedure helper now covers the originating-node CC/CREF
  outcomes from ITU-T Q.714 (05/01), sections 3.1.4.2 and 3.2.3, and end-node
  RLSD/RLC release handling and frozen references from sections 3.3.2, 3.3.3,
  3.3.5, and 3.8.2.2. The in-memory stack core now allocates local references,
  registers local subsystems, orchestrates local connection-section lifecycle
  messages, delivers locally addressed UDT/XUDT/LUDT messages, generates
  UDTS/XUDTS/LUDTS responses when return-on-error is requested, and produces
  notice indications for received service messages. Connection-oriented
  data-transfer procedures now cover class 2 DT1 indications and class 3
  modulo-128 DT2/AK sequence and flow-control windows from ITU-T Q.714
  (05/01), sections 3.5.1 and 3.5.2. The remaining procedure work includes
  guard/freeze timers, relay-node coupling, global title translation,
  compatibility tests, message type changes, segmentation and reassembly across
  multiple DT messages, class 3 reset handling, expedited data, inactivity
  testing, and timer behavior from ITU-T Q.714 (05/01).
- Connectionless service messages are implemented from ITU-T Q.713 (03/01),
  sections 4.11, 4.19, 4.20, and 4.21. Long data enforces the Q.713 section
  3.20 SCCP-user-data range of 1 to 3952 octets.
- Existing parameter support needs strict malformed-input behavior: parsers must
  return errors for short buffers and invalid fixed optional lengths, never
  panic or only log invalid wire data.
- Existing comments that cite Q.713 must be upgraded over time to exact
  document and section references, for example `ITU-T Q.713 (03/01)`.
- The test suite needs fuzz targets and negative wire-format tests for every
  parser before missing message types are added.

## Verification Sources

- https://www.itu.int/rec/T-REC-Q.711
- https://www.itu.int/rec/T-REC-Q.712
- https://www.itu.int/rec/T-REC-Q.713
- https://www.itu.int/rec/T-REC-Q.714
- https://www.itu.int/rec/T-REC-Q.715-200204-I
- https://www.itu.int/rec/T-REC-Q.716
- https://www.etsi.org/deliver/etsi_en/300001_300099/30000901/01.04.03_30/en_30000901v010403v.pdf
- https://www.rfc-editor.org/rfc/rfc3868.json
- https://datatracker.ietf.org/doc/rfc3868/
- https://datatracker.ietf.org/api/v1/doc/relateddocument/?target__name=rfc3868&limit=200&format=json
- https://www.rfc-editor.org/rfc/rfc4666.json
- https://datatracker.ietf.org/doc/rfc4666/
- https://www.rfc-editor.org/errata/rfc4666
- https://datatracker.ietf.org/api/v1/doc/relateddocument/?target__name=rfc4666&limit=200&format=json
- https://www.rfc-editor.org/rfc/rfc9260.json
- https://datatracker.ietf.org/doc/rfc9260/
- https://www.rfc-editor.org/errata/rfc9260
- https://datatracker.ietf.org/api/v1/doc/relateddocument/?target__name=rfc9260&limit=200&format=json
