# Standards and Compliance Ledger

go-sccp implements SCCP, the Signalling Connection Control Part of SS7: the
message and parameter codecs of ITU-T Q.713 and the end-node procedures of
ITU-T Q.714. This ledger records the documents the code follows, how each rule
is applied, and what is deliberately out of scope.

Last verified: 2026-10-11.

## Normative SCCP Baseline

| Area | Current document | Status checked | Used for |
| ---- | ---------------- | -------------- | -------- |
| Functional description | ITU-T Q.711 (03/01) | In force | MTP-SAP sizes (§7.1), service primitives (Tables 12 and 13) |
| Message definitions | ITU-T Q.712 (07/96) | In force | Message set |
| Formats and codes | ITU-T Q.713 (03/01) | In force | Every message and parameter codec |
| Procedures | ITU-T Q.714 (05/01) | In force | Routing, connectionless and connection-oriented procedures |
| User guide | ITU-T Q.715 (04/02) | In force | Length guidance (§8.3.2), return of segments (§8.3.1.6) |
| Performance | ITU-T Q.716 (03/93) | In force | Not used by the code |

Status was read from the ITU-T recommendation pages (https://www.itu.int/rec/T-REC-Q.711
to Q.716): each lists the edition above as the only component in force and the
earlier editions as superseded. Older editions are not valid baselines.

## Wire conventions

| Rule | Source |
| ---- | ------ |
| Pointer value counts from the pointer's most significant octet, included, to the first octet of its parameter | Q.713 §2.3 and footnote 2 |
| LUDT and LUDTS pointers are two octets, less significant octet first | Q.713 §1.4 |
| The long data length indicator is two octets, less significant octet first | Q.713 §1.2 |
| Point codes in addresses and SCMG: two octets, LSB first, bits 7-8 of the second octet zero (14 bits) | Q.713 §3.4.2.1, §5.2.2 |
| Three-octet local references (destination, source, segmentation) are sent least significant octet first | SS7 multi-octet order; Wireshark dissects `sccp.dlr`, `sccp.slr` and `sccp.segmentation.slr` this way |
| BCD global title digits: first digit in bits 1-4, filler 0000 after an odd last digit | Q.713 §3.4.2.3.1, Figure 8 |
| Odd/even: bit 8 of the NAI octet for GTI 1, the encoding scheme for GTI 3 and 4 | Q.713 §3.4.2.3 |
| Protocol class bits 5-8 are the message handling code of classes 0 and 1: 1000 is return message on error; 0000 and the spare codes decode as no special options, the default (Q.714 §1.1.4.3). They are spare for classes 2 and 3 | Q.713 §3.6 |
| Segmentation: F bit 8, in-sequence option bit 7, remaining segments bits 4-1 | Q.713 §3.17 |
| End of optional parameters only when optional parameters are present | Q.713 §1.6 |

## Decoding (liberal)

The decoder accepts what a receiver is required to accept:

- gaps between parameters, parameters in an order different from their
  pointers, overlapping parameters and octets after the last parameter
  (Q.713 §1.4, Q.714 §1.1.4.5);
- optional parameters it does not recognise for the message type, kept as raw
  parameters and encoded again unchanged (Q.714 §1.1.4.2), except one named
  with the reserved code 11111111, which the encoder refuses (Q.713 §1.9);
- a missing end of optional parameters octet, and octets after it (Q.713 §1.6:
  it "should not be used to detect the end of messages");
- spare bits with any value (Q.714 §1.1.4.4);
- the calling party address consisting of the address indicator alone
  (Q.713 §3.5);
- messages of any size, and parameter contents outside the length limits of
  the message tables: a UDT of 271 octets, an empty data parameter, 129 octets
  of CR data.

It rejects: truncated messages, a mandatory pointer that is zero or points into
the pointers, a parameter running past the end of the message, a malformed
recognised parameter, a recognised optional parameter that appears twice and a
message type code that does not match. Decoded values never alias the input.

## Encoding (conservative)

Messages are encoded in the canonical layout and every value is checked against
its field. Nothing is masked, wrapped or truncated. The encoder refuses:

- a parameter longer than its one-octet length indicator (Q.713 §2.2), user
  data outside 1 to 255 octets (§3.16), long data outside 1 to 3952 octets
  (§3.20), and data above the message tables: 128 octets in CR, CC, CREF and
  RLSD (Tables 3 to 6), 32 octets in ED (Table 13);
- a pointer that cannot reach its parameter. With three-octet addresses and the
  segmentation parameter this caps an XUDT at 247 octets of data, the Y of
  Table 19 Note 1;
- an encoded message above the maximum message size: 268 octets by default,
  the 272-octet MTP-SDU of the narrowband MTP-SAP minus the 4-octet ITU routing
  label (Q.711 §7.1 a)); configurable up to 4091 octets for the broadband
  MTP-SAP (Q.711 §7.1 b)), the most any MTP-SAP carries, so a larger maximum
  is refused too;
- a party address with no address element, except the indicator-only calling
  address of UDT and XUDT (Tables 3 to 5, 11, 12, 19 to 22);
- a protocol class that does not belong to the message (Table 1), return on
  error with class 2 or 3 (§3.6), a segment that is not class 1 (Q.714
  §4.1.1.1.2);
- out-of-range fields: hop counter outside 1 to 15 (§3.18), importance above 7
  (§3.19), sequence numbers above 127 (§3.8, §3.9), credit above 127 (Q.714
  §3.5.2.3), remaining segments above 15 (§3.17), point codes above 14 bits,
  local references above 24 bits or all ones (§3.2, §3.3), and fields a global
  title format does not carry (§3.4.2.3);
- the reserved code 11111111 as a parameter name (§1.9) or a cause value
  (§3.11 to §3.15). The other cause codes not defined, spare or reserved, are
  carried, so that a cause received can be passed on.

Global title address information is carried as transmitted, so that addresses
received from peers can be returned unchanged; the constructors
`params.NewGlobalTitle1`, `NewGlobalTitle3` and `NewGlobalTitle4` produce
Figure 8 BCD. A message decoded from the output of a successful encoding is
equal to the encoded message, with nil and empty slices equivalent.

## Procedures

The `Stack` type is an end node. It performs no I/O: messages arrive with their
MTP routing label (Q.711 §7.2.1) and leave as MTP-TRANSFER requests. Its
maximum message size is at least 11 octets, an IT (Q.713 Table 18), so that
every message without user data the connection-oriented procedures must send
fits.

### Originating requests

- N-UNITDATA and N-CONNECT requests are refused when the called party
  address breaks §2.2.2.1: routed on SSN, it must carry a subsystem number
  other than 0 (item 2; §3.8.3.3 b6); with SSN 0 or none it must carry a
  global title and be routed on GT ("the routing indicator shall be set to
  "Route on GT"", item 3), SSN 0 without a global title being incomplete
  (item 5); routed on GT, it must carry a global title (§3.8.3.3 b7). So is
  a calling party address without an address element, other than the address
  indicator alone (Q.713 Table 11 Note 2).
- The importance of a message is the requested one, at most the maximum of
  Q.714 Table 2 for its type: 6 for XUDT and RLSD, 4 for CR, CC and CREF
  (§2.6.2 a)). Without a request, and in what the SCCP sends of its own
  accord, the parameter is absent and the default of Table 2 applies.
- The messages of a request share no memory with the request or with one
  another, and the address passed to `LocalGlobalTitle` is a copy.

### Routing at the end node (Q.714 §2.3.1)

- Route on SSN with a subsystem number other than 0: local delivery. SSN 0 with
  a global title goes to translation (§2.2.2.1 item 2); SSN 0 without one is
  discarded (§2.2.2.1 item 4).
- Route on GT: the hop counter is decremented and a value of zero is a hop
  counter violation (§2.3.1 item 3, §2.8.6), which besides the return or
  refusal gives a `MaintenanceIndication`: "In addition, maintenance functions
  are alerted" (§2.3.1 item 3). The final translation at this node
  delivers to the subsystem number of the address; `LocalGlobalTitle` can
  refuse global titles (return cause no translation for this specific address,
  §2.8.2) and is called without the stack locked. An address without a
  subsystem number, or with SSN 0, gets the same cause.
- A called address routed on SSN without a subsystem number, or on GT without a
  global title, is a construction error (§3.8.3.3 b6 and b7): the message is
  discarded and never returned (§3.8.4, §4.3).
- A subsystem that is not registered gives unequipped user (§2.8.5); one that
  is registered but prohibited gives subsystem failure (§2.8.3). Every segment
  of a segmented message is checked.

### Connectionless (Q.714 §4)

- User data that fits one message goes in a UDT, or an XUDT when an importance
  is requested or the stack is configured for XUDT.
- Longer user data is segmented into at most 16 XUDT messages (§4.1.1.1.2):
  class 1, identical addresses and routing label, the segmentation parameter on
  every segment with the F bit on the first, the remaining segment count, the
  in-sequence option of the request and one local reference. The reference
  stays frozen for 20 seconds by default, beyond T(reassembly) at the far end
  (footnote 7: the mechanism is implementation dependent). Segments are as
  equal as possible with the first the largest, so that the total does not
  exceed the first segment's length times the number of segments. Return on
  error is set in the first segment only (§4.1.1.1.3; Q.715 §8.3.1.6).
- The segmentation threshold Z (§4.1.1.1.1, footnote 6: 160 <= Z <= Y) is
  configurable: user data longer than Z octets is segmented into segments of
  at most Z octets. User data of Z octets makes a single segment, so it goes
  unsegmented with the requested class, since the segmentation parameter
  "Should not be present in case of a single XUDT message" (Q.713 Table 19
  Note 2).
- Reassembly (§4.1.1.2) is keyed on the calling party address, the whole
  routing label and the segmentation local reference (§4.1.1.2.2). The SLS is
  part of the key: the segments of a message travel with "identical MTP routing
  information (DPC, SLS)" (§4.1.1.1.2). Segments of any length are accepted
  while the total stays within the first segment's length times the number of
  segments (§4.1.1.2.1, §4.1.1.2.2). A failure ends the process and returns the
  first segment, its segmentation parameter unchanged (§4.1.1.2.3), when any
  segment of the process asked for return:
  - error in message transport (§4.1.1.2.3.2): an out-of-sequence or
    duplicated segment, a segment with no process, a new first segment for a
    busy reference (the new message is returned and the old segments are
    discarded, §4.1.1.2.1), a total above the first segment's length times the
    number of segments, and the expiry of T(reassembly) (15 seconds by
    default; Annex C.4: 10 to 20 seconds);
  - destination cannot perform reassembly (§4.1.1.2.3.1): a total above 3952
    octets, which LUDT segments can reach;
  - network congestion (§4.1.1.2.3.4): no room within the bounds on processes
    and buffered octets;
  - subsystem failure or unequipped user (§2.3.1 item 2 b)): a segment for a
    prohibited or unknown subsystem, whichever segment it is.

  "Only one XUDTS/LUDTS message will be sent" for a reassembly error (§4.2): a
  failed message, including one whose T(reassembly) expired, is recorded for
  another T(reassembly), and its other segments, duplicates included, are
  discarded. A first segment identical to the failed message's, a duplicate
  (§4.1.1.2.3.2), is part of it; another first segment starts a new message
  with the reference. When the failed message's first segment was never
  seen, a first segment for the reference is taken as its late arrival.
  Records hold no data and are never ended early, which would let a message
  be returned twice. A failed process becomes the record of its
  message in its own place, so `MaxReassemblies` bounds processes and their
  records together, and every message admitted to reassembly is recorded. The
  records of messages that failed with no process (a first segment refused or
  never received) are bounded separately by the same number; when none is
  left, such a message is discarded without a return. Buffers are allocated
  to the exact length of the user data received, so the octet bound covers
  the memory reassembly holds; the length a first segment announces allocates
  nothing.
- The message return procedure (§4.2) answers a UDT with a UDTS, an XUDT with
  an XUDTS and an LUDT with an LUDTS, truncated to a short LUDTS when needed
  (Q.715 §8.3.1.6), addresses swapped, a fresh hop counter. It goes to the
  signalling point code of the calling party address when that address is
  routed on SSN and carries one, which then "identifies the originating SCCP
  node" (§2.7.5.1 c)), otherwise to the OPC of the received message. User data
  that does not fit the maximum message size is truncated (§4.2, last
  paragraph; §4.1.2). Service messages are never returned: received ones give
  N-NOTICE indications, and one that cannot be delivered is discarded (§4.2
  b)). Returned segments are not reassembled (§4.2 leaves it to the
  implementation).
- The return causes 13 (segmentation not supported) and 14 (segmentation
  failure) belong to segmentation at an interworking node that receives an LUDT
  (§4.1.1.1.3.1 and §4.1.1.1.3.2). Reassembly failures use 8, 9, 10 and 6
  (§4.1.1.2.3). The originating node reports segmentation it cannot perform to
  its user as `ErrSegmentation` (§4.1.1.1.1: "the error treatment is
  initiated").

### Connection-oriented (Q.714 §3)

The stack runs protocol class 2. Class 3 is never proposed, and a CR proposing
class 3 is confirmed as class 2, which §3.1.3.1 permits; a CC that answers a
class 2 proposal with class 3 releases the connection. DT2, AK, ED, EA, RSR and
RSC are unknown message types for the implemented class (Table B.1 Note):
whatever their references, they are discarded before any other check, as
syntax errors ("not supported by the implementation, are discarded with a
report to OMAP ("syntax error")", §1.1.4.1). Relay coupling (§3.1.5) and the
restart procedure (§3.8.1 to §3.8.3) are not implemented. State c5 of Table
B.3 is never entered: this end node answers an RLSD at once (§3.3.5). An
incoming connection request waits for its user's answer with no timer (Figure
C.3 sheet 2).

Received messages are checked as Annex B lays out:

- An unassigned destination reference, an OPC other than the peer's and a
  source reference other than the peer's get the Table B.2 responses, built
  from the received message (Note 2). While the CR is pending (c3) no peer is
  associated yet (§3.1.4.2 items 1 and 3), so only the destination reference
  is checked.
- An IT whose source reference differs from the stored one is answered with an
  RLSD built from its own references, and the section is released with an RLSD
  built from the stored ones (Table B.2 Note 3; §3.4 Table 3: "Release
  connection").
- A CC or IT with a protocol class other than 2 or 3, a reserved (all ones)
  local reference and a DT1 without data are syntax errors, discarded without
  touching the section (§3.8.3.3 a2 and b1, §3.8.4). Data longer than the 128
  octets of Q.713 Tables 3 to 6 is accepted: §3.8.4 makes checks beyond routing
  optional.

Table B.3 uses the action ERROR 3 (states c3, c5 and c6) without defining it:
the legend defines only NORMAL, DISCARD and ERROR 1, in Q.714 (05/01) and in
the superseded Q.714 (07/96) alike. The stack takes the actions from the Annex C
diagrams, which "are intended to show precisely the behaviour of the signalling
system under normal and abnormal conditions" (Annex C.1):

- c3, "Connection pending outgoing" (Figure C.2 sheet 2): an RLSD is answered
  with RLC. It, any other message ("Other NPDU type"), a CREF and the expiry
  of T(conn est) stop T(conn est), release the section, freeze the reference
  and give an N-DISCONNECT indication.
- c3 after the user's N-DISCONNECT request, "Wait connection confirm" (sheet
  3; §3.1.4.2): the same, without the N-DISCONNECT indication, which Q.711
  Figure 8 does not allow after a request. A CC is answered with an RLSD
  carrying the user's cause and data.
- c6, "Disconnect pending" (Figure C.2 sheet 6, Figure C.3 sheet 5): an RLC,
  an RLSD or an ERR ("Released, error") releases the section and freezes the
  reference, without an N-DISCONNECT indication. So does the expiry of T(int),
  which also alerts maintenance with a `MaintenanceIndication` ("Inform
  maintenance"; Annex C.4).

Data transfer (§3.5): an NSDU of up to `MaxDataLength` octets, 65535 by
default, travels in DT1 messages of at most 255 octets with the M bit set on
all but the last (§3.5.3). A peer that sends a longer one is released with the
cause remote procedure error. The reassembly buffers of all connections hold at
most `MaxReassemblyOctets`, 4 MiB by default; a DT1 that needs more releases
its connection with the cause network congestion, the cause §4.1.1.2.3.4 gives
connectionless reassembly when buffer space runs out.

Frozen references are kept for two minutes by default, longer than the peer's
T(int) of up to one minute (Annex C.4), so that a repeated RLSD still meets a
frozen reference and is answered with RLC (Table B.2).

### Not implemented

Global title translation beyond the final translation at this node, relaying,
the compatibility test and message change (§2.5, §4.1.2), segmentation of LUDT
at an interworking node, SCCP management procedures (§5) — the SCMG messages
have a codec — and protocol class 3.

## ETSI Profile

ETSI EN 300 009-1 V1.4.3 (2001-02), the latest published version, applies
ITU-T Q.711 to Q.716 with modifications for international interconnection:
connectionless classes 0 and 1 and connection-oriented class 2. "Class 3 is
not in the scope of the present document", nor are permanent signalling
connections, sequence control and flow control, N-EXPEDITED DATA and N-RESET.
The stack's scope matches that profile. The default codec target is ITU-T
international SCCP.

## Adjacent IETF Specifications

The library does not implement these; the examples run SCCP over M3UA through
github.com/gomaja/go-m3ua and github.com/gomaja/go-sctp.

| Area | Current document | RFC Editor (rfcNNNN.json) | Datatracker `obs`/`updates` relations | Errata |
| ---- | ---------------- | ------------------------- | ------------------------------------- | ------ |
| SUA | RFC 3868 | Proposed Standard; no obsoletes, updates, updated_by or obsoleted_by | none | No matching errata found |
| M3UA | RFC 4666 | Proposed Standard; obsoletes RFC 3332; no updated_by or obsoleted_by | none | Held for Document Update (2), Rejected (1) |
| SCTP | RFC 9260 | Proposed Standard; obsoletes RFC 4460, 4960, 6096, 7053, 8540; no updated_by or obsoleted_by | none | Verified (5), Held for Document Update (1), Rejected (3) |

RFC 3332 and RFC 4960 are obsolete and must not be cited as current. The
verified RFC 9260 errata concern SCTP, which go-sctp implements.

## Verification Sources

- https://www.itu.int/rec/T-REC-Q.711, -Q.712, -Q.713, -Q.714, -Q.715, -Q.716
- https://www.etsi.org/deliver/etsi_en/300001_300099/30000901/01.04.03_60/en_30000901v010403p.pdf
  (the published version; the index lists 01.04.02_20, 01.04.03_30 and
  01.04.03_60)
- https://www.rfc-editor.org/rfc/rfc3868.json, rfc4666.json, rfc9260.json
- https://datatracker.ietf.org/api/v1/doc/relateddocument/?target__name=rfcNNNN&relationship__slug__in=obs,updates&format=json
  for 3868, 4666 and 9260
- https://errata.rfc-editor.org/search/?rfc_number=NNNN&presentation=records
  for 3868, 4666 and 9260
