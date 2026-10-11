// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

func ptr[T any](v T) *T { return &v }

func unhex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var (
	ssn6 = params.NewSSNAddress(6)
	ssn7 = params.NewSSNAddress(7)
	ssn8 = params.NewSSNAddress(8)
	seg  = params.Segmentation{FirstSegment: true, InSequenceDelivery: true, RemainingSegments: 1, LocalReference: 0x030201}
)

// vectors are hand-computed from ITU-T Q.713 (03/01): pointer values follow
// §2.3, three-octet references and two-octet pointers and lengths are sent
// least significant octet first.
var vectors = []struct {
	name string
	msg  sccp.Message
	wire string
}{
	{"CR", &sccp.CR{SourceLocalReference: 0x030201, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: ssn6},
		"01 010203 02 02 00 024206"},
	{"CR with optional parameters", &sccp.CR{
		SourceLocalReference: 0x030201, ProtocolClass: params.ProtocolClass{Class: 3}, CalledPartyAddress: ssn6,
		Credit: ptr(params.Credit(5)), CallingPartyAddress: &ssn7, Data: []byte{0xaa, 0xbb},
		HopCounter: ptr(params.HopCounter(4)), Importance: ptr(params.Importance(2)),
	}, "01 010203 03 02 04 024206 090105 04024207 0f02aabb 110104 120102 00"},
	{"CC", &sccp.CC{DestinationLocalReference: 0x030201, SourceLocalReference: 0x060504, ProtocolClass: params.ProtocolClass{Class: 2}},
		"02 010203 040506 02 00"},
	{"CC with optional parameters", &sccp.CC{
		DestinationLocalReference: 0x030201, SourceLocalReference: 0x060504, ProtocolClass: params.ProtocolClass{Class: 2},
		CalledPartyAddress: &ssn6, Data: []byte{0xcc},
	}, "02 010203 040506 02 01 03024206 0f01cc 00"},
	{"CREF", &sccp.CREF{DestinationLocalReference: 0x030201, RefusalCause: params.RefusalCauseSubsystemCongestion},
		"03 010203 0b 00"},
	{"CREF with importance", &sccp.CREF{DestinationLocalReference: 0x030201, RefusalCause: params.RefusalCauseSubsystemCongestion, Importance: ptr(params.Importance(3))},
		"03 010203 0b 01 120103 00"},
	{"RLSD", &sccp.RLSD{DestinationLocalReference: 0x030201, SourceLocalReference: 0x060504, ReleaseCause: params.ReleaseCauseSCCPUserOriginated},
		"04 010203 040506 03 00"},
	{"RLSD with data", &sccp.RLSD{DestinationLocalReference: 0x030201, SourceLocalReference: 0x060504, ReleaseCause: params.ReleaseCauseSCCPUserOriginated, Data: []byte{0xde, 0xad}},
		"04 010203 040506 03 01 0f02dead 00"},
	{"RLC", &sccp.RLC{DestinationLocalReference: 0x030201, SourceLocalReference: 0x060504},
		"05 010203 040506"},
	{"DT1", &sccp.DT1{DestinationLocalReference: 0x030201, MoreData: true, Data: []byte("hello")},
		"06 010203 01 01 0568656c6c6f"},
	{"DT1 last segment", &sccp.DT1{DestinationLocalReference: 0x030201, Data: []byte("end")},
		"06 010203 00 01 03656e64"},
	{"DT2", &sccp.DT2{DestinationLocalReference: 0x030201, SequencingSegmenting: params.SequencingSegmenting{SendSequenceNumber: 5, ReceiveSequenceNumber: 9, MoreData: true}, Data: []byte{0xaa}},
		"07 010203 0a13 01 01aa"},
	{"AK", &sccp.AK{DestinationLocalReference: 0x030201, ReceiveSequenceNumber: 127, Credit: 3},
		"08 010203 fe 03"},
	{"UDT", &sccp.UDT{ProtocolClass: params.ProtocolClass{Class: 0, ReturnOnError: true}, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte("payload")},
		"09 80 03 05 07 024206 024207 077061796c6f6164"},
	{"UDTS", &sccp.UDTS{ReturnCause: params.ReturnCauseNoTranslationForThisSpecificAddress, CalledPartyAddress: ssn7, CallingPartyAddress: ssn6, Data: []byte("payload")},
		"0a 01 03 05 07 024207 024206 077061796c6f6164"},
	{"ED", &sccp.ED{DestinationLocalReference: 0x030201, Data: []byte{0x01}},
		"0b 010203 01 0101"},
	{"EA", &sccp.EA{DestinationLocalReference: 0x030201}, "0c 010203"},
	{"RSR", &sccp.RSR{DestinationLocalReference: 0x030201, SourceLocalReference: 0x060504, ResetCause: params.ResetCauseSCCPUserOriginated},
		"0d 010203 040506 01 00"},
	{"RSC", &sccp.RSC{DestinationLocalReference: 0x030201, SourceLocalReference: 0x060504}, "0e 010203 040506"},
	{"ERR", &sccp.ERR{DestinationLocalReference: 0x030201, ErrorCause: params.ErrorCausePointCodeMismatch}, "0f 010203 02 00"},
	{"IT", &sccp.IT{DestinationLocalReference: 0x030201, SourceLocalReference: 0x060504, ProtocolClass: params.ProtocolClass{Class: 2}},
		"10 010203 040506 02 0000 00"},
	{"XUDT", &sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1, ReturnOnError: true}, HopCounter: 15, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: []byte{0xaa, 0xbb}},
		"11 81 0f 04 06 08 00 024208 024207 02aabb"},
	{"XUDT segment", &sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: []byte{0xaa, 0xbb}, Segmentation: &seg},
		"11 01 0f 04 06 08 0a 024208 024207 02aabb 1004c1010203 00"},
	{"XUDTS", &sccp.XUDTS{ReturnCause: params.ReturnCauseErrorInMessageTransport, HopCounter: 15, CalledPartyAddress: ssn7, CallingPartyAddress: ssn8, Data: []byte{0xaa, 0xbb},
		Segmentation: &seg, Importance: ptr(params.Importance(4))},
		"12 08 0f 04 06 08 0a 024207 024208 02aabb 1004c1010203 120104 00"},
	{"LUDT", &sccp.LUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: []byte{0xaa, 0xbb, 0xcc}},
		"13 01 0f 0700 0800 0900 0000 024208 024207 0300aabbcc"},
	{"LUDT segment", &sccp.LUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: []byte{0xaa, 0xbb, 0xcc},
		Segmentation: &params.Segmentation{FirstSegment: true, LocalReference: 0x030201}},
		"13 01 0f 0700 0800 0900 0c00 024208 024207 0300aabbcc 1004 80010203 00"},
	{"LUDTS", &sccp.LUDTS{ReturnCause: params.ReturnCauseSubsystemFailure, HopCounter: 15, CalledPartyAddress: ssn7, CallingPartyAddress: ssn8, Data: []byte{0xaa}},
		"14 03 0f 0700 0800 0900 0000 024207 024208 0100aa"},
	{"UDT with raw optional free XUDT", &sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 0}, HopCounter: 1, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: []byte{0x01},
		Unrecognized: []params.RawParameter{{Code: 0xf0, Value: []byte{0x12, 0x34}}}},
		"11 00 01 04 06 08 09 024208 024207 0101 f0021234 00"},
}

var cmpOpts = cmp.Options{cmpopts.EquateEmpty()}

func TestVectors(t *testing.T) {
	bigPrefix := bytes.Repeat([]byte{0xee}, 4096)
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			want := unhex(t, v.wire)
			got, err := v.msg.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("MarshalBinary\n got % x\nwant % x", got, want)
			}
			appended, err := v.msg.AppendBinary([]byte{0xee})
			if err != nil || !bytes.Equal(appended, append([]byte{0xee}, want...)) {
				t.Fatalf("AppendBinary = % x, %v", appended, err)
			}
			// The maximum message size applies to the message, not to what
			// the buffer already holds.
			appended, err = v.msg.AppendBinary(bigPrefix[:len(bigPrefix):len(bigPrefix)])
			if err != nil || !bytes.Equal(appended[len(bigPrefix):], want) {
				t.Fatalf("AppendBinary after %d octets = %v", len(bigPrefix), err)
			}
			parsed, err := sccp.ParseMessage(want)
			if err != nil {
				t.Fatalf("ParseMessage: %v", err)
			}
			if diff := cmp.Diff(v.msg, parsed, cmpOpts); diff != "" {
				t.Fatalf("ParseMessage mismatch (-want +got):\n%s", diff)
			}
			if parsed.MessageType() != v.msg.MessageType() || parsed.String() == "" {
				t.Fatalf("MessageType %s, String %q", parsed.MessageType(), parsed.String())
			}
		})
	}
}

// optionalStart returns the offset of the optional part of an encoded
// message, or its length when it has none.
func optionalStart(wire []byte) int {
	at, size := -1, 1
	switch sccp.MsgType(wire[0]) {
	case sccp.MsgTypeCR:
		at = 6
	case sccp.MsgTypeCC, sccp.MsgTypeRLSD, sccp.MsgTypeRSR:
		at = 8
	case sccp.MsgTypeCREF, sccp.MsgTypeERR:
		at = 5
	case sccp.MsgTypeXUDT, sccp.MsgTypeXUDTS:
		at = 6
	case sccp.MsgTypeLUDT, sccp.MsgTypeLUDTS:
		at, size = 9, 2
	}
	if at < 0 {
		return len(wire)
	}
	v := int(wire[at])
	if size == 2 {
		v |= int(wire[at+1]) << 8
	}
	if v == 0 {
		return len(wire)
	}
	return at + size - 1 + v
}

// TestDecodeRejectsEveryTruncation cuts every vector at every length. A cut
// before the optional part must be reported as io.ErrUnexpectedEOF. A cut at
// an optional parameter boundary is indistinguishable from a message sent
// without the end of optional parameters octet, which is accepted (ITU-T Q.713
// (03/01) §1.6), so there decoding may succeed but must be stable.
func TestDecodeRejectsEveryTruncation(t *testing.T) {
	for _, v := range vectors {
		wire := unhex(t, v.wire)
		opt := optionalStart(wire)
		for n := range len(wire) {
			m, err := sccp.ParseMessage(wire[:n])
			if n < opt {
				if err == nil {
					t.Errorf("%s cut to %d octets decoded as %v", v.name, n, m)
					continue
				}
				if n > 0 && !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Errorf("%s cut to %d octets: %v, want io.ErrUnexpectedEOF", v.name, n, err)
				}
				if !errors.Is(err, sccp.ErrMalformedMessage) {
					t.Errorf("%s cut to %d octets: %v, want ErrMalformedMessage", v.name, n, err)
				}
				continue
			}
			if err != nil {
				if !errors.Is(err, sccp.ErrMalformedMessage) {
					t.Errorf("%s cut to %d octets: %v, want ErrMalformedMessage", v.name, n, err)
				}
				continue
			}
			b, err := m.MarshalBinary()
			if err != nil {
				t.Errorf("%s cut to %d octets decoded as %v, which does not encode: %v", v.name, n, m, err)
				continue
			}
			again, err := sccp.ParseMessage(b)
			if err != nil {
				t.Errorf("%s cut to %d octets: re-encoding does not decode: %v", v.name, n, err)
				continue
			}
			if diff := cmp.Diff(m, again, cmpOpts); diff != "" {
				t.Errorf("%s cut to %d octets is not stable:\n%s", v.name, n, diff)
			}
		}
	}
}

// TestDecodeIsLiberal checks the receiving rules of ITU-T Q.713 (03/01) §1.4,
// §1.6 and Q.714 (05/01) §1.1.4.
func TestDecodeIsLiberal(t *testing.T) {
	udt := &sccp.UDT{ProtocolClass: params.ProtocolClass{Class: 0, ReturnOnError: true}, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte("payload")}
	xudt := &sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: []byte{0xaa, 0xbb}, Segmentation: &seg}
	tests := []struct {
		name string
		wire string
		want sccp.Message
	}{
		// §1.4/Q.714 §1.1.4.5: gaps are processed as though not present.
		{"gaps between parameters", "09 80 04 07 0a ff 024206 ee 024207 dd 077061796c6f6164", udt},
		// §1.4: parameters may be sent in an order different from the pointers.
		{"parameters in reverse order", "09 80 0e 0a 01 077061796c6f6164 024207 024206", udt},
		// §1.4: extraneous octets after the last parameter.
		{"octets after the last parameter", "09 80 03 05 07 024206 024207 077061796c6f6164 0102030405", udt},
		// §1.6: the end of optional parameters octet is not used to find the
		// end of the message.
		{"missing end of optional parameters", "11 01 0f 04 06 08 0a 024208 024207 02aabb 1004c1010203", xudt},
		{"octets after end of optional parameters", "11 01 0f 04 06 08 0a 024208 024207 02aabb 1004c1010203 00 ffff", xudt},
		// Q.714 §1.1.4.4: spare bits not examined (protocol class bits 5-7,
		// segmentation bits 5-6).
		{"spare bits", "11 71 0f 04 06 08 0a 024208 024207 02aabb 1004f1010203 00", xudt},
		// An optional pointer to the end of the message: no optional part.
		{"empty optional part", "11 01 0f 04 06 08 0a 024208 024207 02aabb",
			&sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: []byte{0xaa, 0xbb}}},
		// Q.714 §1.1.4.2: unrecognised optional parameters, including a
		// recognised name the message type does not carry, are kept raw.
		{"unrecognised optional parameters", "11 01 0f 04 06 08 0a 024208 024207 02aabb f00101 1004c1010203 0f0199 00",
			&sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: []byte{0xaa, 0xbb}, Segmentation: &seg,
				Unrecognized: []params.RawParameter{{Code: 0xf0, Value: []byte{0x01}}, {Code: params.PCodeData, Value: []byte{0x99}}}}},
		// No size limit on decoding: a UDT of 271 octets (255 octets of data,
		// point code and SSN addresses) is accepted.
		{"message above the maximum size", "09 00 03 07 0b 0443020108 0443020107 ff" + strings.Repeat("ab", 255),
			&sccp.UDT{CalledPartyAddress: params.NewPointCodeAddress(0x0102, 8), CallingPartyAddress: params.NewPointCodeAddress(0x0102, 7), Data: bytes.Repeat([]byte{0xab}, 255)}},
		// §3.5: a calling party address of the address indicator alone.
		{"calling party address indicator only", "09 00 03 05 06 024206 0100 0101",
			&sccp.UDT{CalledPartyAddress: ssn6, Data: []byte{0x01}}},
		// Empty data is decoded, though it is never encoded.
		{"empty data", "09 00 03 05 07 024206 024207 00",
			&sccp.UDT{CalledPartyAddress: ssn6, CallingPartyAddress: ssn7}},
		{"LUDT empty long data", "13 01 0f 0700 0800 0900 0000 024208 024207 0000",
			&sccp.LUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7}},
		{"empty raw parameter at the end without end of optional parameters", "11 01 0f 04 06 08 0a 024208 024207 02aabb f000",
			&sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: []byte{0xaa, 0xbb},
				Unrecognized: []params.RawParameter{{Code: 0xf0}}}},
		// Q.713 §3.7: bits 8-2 of segmenting/reassembling are spare.
		{"DT1 spare bits set, more data", "06 010203 ff 01 01aa", &sccp.DT1{DestinationLocalReference: 0x030201, MoreData: true, Data: []byte{0xaa}}},
		{"DT1 spare bits set, no more data", "06 010203 fe 01 01aa", &sccp.DT1{DestinationLocalReference: 0x030201, Data: []byte{0xaa}}},
		// Values the encoder refuses are returned for the procedures.
		{"hop counter zero", "11 01 00 04 06 08 00 024208 024207 02aabb",
			&sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1}, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: []byte{0xaa, 0xbb}}},
		{"CR data above the table maximum", "01 010203 02 02 04 024206 0f81" + strings.Repeat("aa", 129) + "00",
			&sccp.CR{SourceLocalReference: 0x030201, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: ssn6, Data: bytes.Repeat([]byte{0xaa}, 129)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sccp.ParseMessage(unhex(t, tt.wire))
			if err != nil {
				t.Fatalf("ParseMessage: %v", err)
			}
			if diff := cmp.Diff(tt.want, got, cmpOpts); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		wire string
	}{
		{"mandatory pointer zero", "09 80 00 05 07 024206 024207 0101"},
		{"pointer into the pointers", "09 80 01 05 07 024206 024207 0101"},
		{"pointer past the end", "09 80 03 05 30 024206 024207 0101"},
		{"parameter past the end", "09 80 03 05 07 024206 024207 0501"},
		{"optional pointer past the end", "11 01 0f 04 06 08 30 024208 024207 02aabb"},
		{"data pointer into the pointers", "11 01 0f 04 06 01 0a 024208 024207 02aabb"},
		{"optional parameter past the end", "11 01 0f 04 06 08 0a 024208 024207 02aabb 1005c1010203"},
		{"optional parameter without length", "11 01 0f 04 06 08 0a 024208 024207 02aabb 10"},
		{"duplicate segmentation", "11 01 0f 04 06 08 0a 024208 024207 02aabb 1004c1010203 1004c1010203 00"},
		{"segmentation of the wrong length", "11 01 0f 04 06 08 0a 024208 024207 02aabb 1003c10102 00"},
		{"importance of the wrong length", "11 01 0f 04 06 08 0a 024208 024207 02aabb 12020101 00"},
		{"truncated called party address", "09 80 03 05 07 024306 024207 0101"},
		{"empty called party address", "09 80 03 03 05 00 024207 0101"},
		{"LUDT long data past the end", "13 01 0f 0700 0800 0900 0000 024208 024207 0400aabbcc"},
		{"LUDT pointer into the pointers", "13 01 0f 0100 0800 0900 0000 024208 024207 0300aabbcc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := sccp.ParseMessage(unhex(t, tt.wire))
			if !errors.Is(err, sccp.ErrMalformedMessage) {
				t.Fatalf("ParseMessage = %v, %v; want ErrMalformedMessage", m, err)
			}
		})
	}
	if _, err := sccp.ParseMessage(nil); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("ParseMessage(nil) = %v", err)
	}
	var unsupported sccp.UnsupportedTypeError
	if _, err := sccp.ParseMessage([]byte{0x15}); !errors.As(err, &unsupported) || unsupported != 0x15 || err.Error() == "" {
		t.Errorf("ParseMessage(0x15) = %v, want UnsupportedTypeError", err)
	}
	var udt sccp.UDT
	if err := udt.UnmarshalBinary(unhex(t, "11 01 0f 04 06 08 00 024208 024207 02aabb")); !errors.Is(err, sccp.ErrMalformedMessage) {
		t.Errorf("UDT.UnmarshalBinary(XUDT) = %v, want ErrMalformedMessage", err)
	}
}

// TestUnmarshalReplacesState checks that decoding into a used value keeps
// nothing of the previous message, and that a failed decode keeps it whole.
func TestUnmarshalReplacesState(t *testing.T) {
	var m sccp.XUDT
	if err := m.UnmarshalBinary(unhex(t, "12 08 0f 04 06 08 0a 024207 024208 02aabb 1004c1010203 120104 00")[0:0]); err == nil {
		t.Fatal("empty input decoded")
	}
	withOptional := unhex(t, "11 01 0f 04 06 08 0a 024208 024207 02aabb 1004c1010203 f00101 00")
	without := unhex(t, "11 81 0f 04 06 08 00 024208 024207 02aabb")
	if err := m.UnmarshalBinary(withOptional); err != nil {
		t.Fatal(err)
	}
	if err := m.UnmarshalBinary(without); err != nil {
		t.Fatal(err)
	}
	if m.Segmentation != nil || m.Unrecognized != nil {
		t.Fatalf("optional parameters of the previous message kept: %v", &m)
	}
	before := m
	if err := m.UnmarshalBinary(withOptional[:12]); err == nil {
		t.Fatal("truncated message decoded")
	}
	if diff := cmp.Diff(before, m); diff != "" {
		t.Fatalf("failed decode changed the receiver:\n%s", diff)
	}
}

func TestDecodedDataDoesNotAliasInputInEveryForm(t *testing.T) {
	for _, wire := range []string{
		"13 01 0f 0700 0800 0900 0000 024208 024207 0300aabbcc",               // long data
		"01 010203 03 02 04 024206 090105 04024207 0f02aabb 110104 120102 00", // optional data
		"06 010203 01 01 02aabb", // DT1 data
	} {
		b := unhex(t, wire)
		m, err := sccp.ParseMessage(b)
		if err != nil {
			t.Fatal(err)
		}
		before := m.String()
		for i := range b {
			b[i] = 0
		}
		if after := m.String(); after != before {
			t.Fatalf("decoded message changed with its input:\n%s\n%s", before, after)
		}
	}
}

func TestDecodedDataDoesNotAliasInput(t *testing.T) {
	wire := unhex(t, "11 01 0f 04 06 08 0a 024208 024207 02aabb f00101 00")
	m, err := sccp.ParseMessage(wire)
	if err != nil {
		t.Fatal(err)
	}
	for i := range wire {
		wire[i] = 0
	}
	x := m.(*sccp.XUDT)
	if !bytes.Equal(x.Data, []byte{0xaa, 0xbb}) || !bytes.Equal(x.Unrecognized[0].Value, []byte{0x01}) {
		t.Fatalf("decoded slices alias the input: %v", x)
	}
}

func TestEncodeRefusesInvalidMessages(t *testing.T) {
	long := bytes.Repeat([]byte{0xab}, 256)
	gt := &params.GlobalTitle{Indicator: params.GTITTNPESNAI, NumberingPlan: params.NPISDNTelephony, EncodingScheme: params.ESBCDEven, AddressInformation: bytes.Repeat([]byte{0x11}, 248)}
	bigAddress := params.NewGlobalTitleAddress(gt, 6)
	tooBigAddress := params.NewGlobalTitleAddress(&params.GlobalTitle{Indicator: params.GTITTNPESNAI, AddressInformation: bytes.Repeat([]byte{0x11}, 249)}, 6)
	tests := []struct {
		name string
		msg  sccp.Message
	}{
		{"UDT empty data", &sccp.UDT{CalledPartyAddress: ssn6, CallingPartyAddress: ssn7}},
		// The defect this package used to have: 256 octets went out as a
		// length indicator of 0.
		{"UDT 256 octets of data", &sccp.UDT{CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: long}},
		{"UDTS 256 octets of data", &sccp.UDTS{CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: long}},
		{"XUDT 300 octets of data", &sccp.XUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: bytes.Repeat([]byte{1}, 300)}},
		{"XUDTS 256 octets of data", &sccp.XUDTS{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: long}},
		{"UDT connection-oriented class", &sccp.UDT{ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}}},
		{"XUDT hop counter zero", &sccp.XUDT{CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}}},
		{"XUDT invalid segmentation", &sccp.XUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}, Segmentation: &params.Segmentation{RemainingSegments: 16}}},
		{"XUDT importance 8", &sccp.XUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}, Importance: ptr(params.Importance(8))}},
		{"XUDT raw parameter with a recognised code", &sccp.XUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1},
			Unrecognized: []params.RawParameter{{Code: params.PCodeImportance, Value: []byte{1}}}}},
		{"XUDT raw end of optional parameters", &sccp.XUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1},
			Unrecognized: []params.RawParameter{{Code: params.PCodeEndOfOptionalParameters}}}},
		{"XUDT raw parameter over 255 octets", &sccp.XUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1},
			Unrecognized: []params.RawParameter{{Code: 0xf0, Value: long}}}},
		{"LUDT empty long data", &sccp.LUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7}},
		{"LUDT 3953 octets", &sccp.LUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: make([]byte, 3953)}},
		{"LUDTS 3953 octets", &sccp.LUDTS{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: make([]byte, 3953)}},
		{"party address over 255 octets", &sccp.UDT{CalledPartyAddress: tooBigAddress, CallingPartyAddress: ssn7, Data: []byte{1}}},
		// Two 256-octet addresses push the third pointer past 255.
		{"pointer out of reach", &sccp.UDT{CalledPartyAddress: bigAddress, CallingPartyAddress: bigAddress, Data: []byte{1}}},
		{"invalid address", &sccp.UDT{CalledPartyAddress: params.NewPointCodeAddress(0x4000, 1), CallingPartyAddress: ssn7, Data: []byte{1}}},
		{"CR connectionless class", &sccp.CR{CalledPartyAddress: ssn6, ProtocolClass: params.ProtocolClass{Class: 1}}},
		{"CR data over 128 octets", &sccp.CR{CalledPartyAddress: ssn6, ProtocolClass: params.ProtocolClass{Class: 2}, Data: make([]byte, 129)}},
		{"CR reserved reference", &sccp.CR{SourceLocalReference: params.LocalReferenceReserved, CalledPartyAddress: ssn6, ProtocolClass: params.ProtocolClass{Class: 2}}},
		{"CC data over 128 octets", &sccp.CC{ProtocolClass: params.ProtocolClass{Class: 2}, Data: make([]byte, 129)}},
		{"CC connectionless class", &sccp.CC{ProtocolClass: params.ProtocolClass{Class: 0}}},
		{"CREF data over 128 octets", &sccp.CREF{Data: make([]byte, 129)}},
		{"RLSD data over 128 octets", &sccp.RLSD{Data: make([]byte, 129)}},
		{"DT1 empty data", &sccp.DT1{}},
		{"DT1 256 octets", &sccp.DT1{Data: long}},
		{"DT2 256 octets", &sccp.DT2{Data: long}},
		{"DT2 sequence number 128", &sccp.DT2{SequencingSegmenting: params.SequencingSegmenting{SendSequenceNumber: 128}, Data: []byte{1}}},
		{"AK sequence number 128", &sccp.AK{ReceiveSequenceNumber: 128}},
		{"ED 33 octets", &sccp.ED{Data: make([]byte, 33)}},
		{"IT connectionless class", &sccp.IT{}},
		{"RLC reference over 24 bits", &sccp.RLC{DestinationLocalReference: 0x1000000}},
		// Q.713 Tables 11, 12, 19-22: an address indicator and at least one
		// element, except the indicator-only calling address of UDT and XUDT
		// with bits 1-7 zero (Table 11 Note 2, Table 19 Note 3).
		{"UDT indicator-only called address", &sccp.UDT{CallingPartyAddress: ssn7, Data: []byte{1}}},
		{"UDT calling address of the routing indicator alone", &sccp.UDT{CalledPartyAddress: ssn6, CallingPartyAddress: params.PartyAddress{RouteOnSSN: true}, Data: []byte{1}}},
		{"UDTS indicator-only calling address", &sccp.UDTS{CalledPartyAddress: ssn6, Data: []byte{1}}},
		{"XUDTS indicator-only called address", &sccp.XUDTS{HopCounter: 15, CallingPartyAddress: ssn6, Data: []byte{1}}},
		{"LUDT indicator-only calling address", &sccp.LUDT{HopCounter: 15, CalledPartyAddress: ssn6, Data: []byte{1}}},
		{"CR indicator-only called address", &sccp.CR{ProtocolClass: params.ProtocolClass{Class: 2}}},
		{"CR indicator-only calling address", &sccp.CR{ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: ssn6, CallingPartyAddress: &params.PartyAddress{}}},
		{"CC indicator-only called address", &sccp.CC{ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: &params.PartyAddress{NationalUse: true}}},
		{"CREF indicator-only called address", &sccp.CREF{CalledPartyAddress: &params.PartyAddress{}}},
		// Q.714 §4.1.1.1.2: segments are protocol class 1.
		{"XUDT first segment in class 0", &sccp.XUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}, Segmentation: &params.Segmentation{FirstSegment: true, RemainingSegments: 1}}},
		{"XUDT later segment in class 0", &sccp.XUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}, Segmentation: &params.Segmentation{}}},
		{"LUDT segment in class 0", &sccp.LUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}, Segmentation: &params.Segmentation{RemainingSegments: 3}}},
		// Q.714 §3.5.2.3: the window "shall not exceed 127".
		{"CR credit 128", &sccp.CR{ProtocolClass: params.ProtocolClass{Class: 3}, CalledPartyAddress: ssn6, Credit: ptr(params.Credit(128))}},
		{"AK credit 128", &sccp.AK{Credit: 128}},
		// Q.713 §1.9 and §3.11-§3.15: code 11111111 is reserved.
		{"raw parameter name 0xff", &sccp.ERR{Unrecognized: []params.RawParameter{{Code: 0xff, Value: []byte{1}}}}},
		{"UDTS return cause 0xff", &sccp.UDTS{ReturnCause: 0xff, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}}},
		{"XUDTS return cause 0xff", &sccp.XUDTS{ReturnCause: 0xff, HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}}},
		{"LUDTS return cause 0xff", &sccp.LUDTS{ReturnCause: 0xff, HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}}},
		{"CREF refusal cause 0xff", &sccp.CREF{RefusalCause: 0xff}},
		{"RLSD release cause 0xff", &sccp.RLSD{ReleaseCause: 0xff}},
		{"RSR reset cause 0xff", &sccp.RSR{ResetCause: 0xff}},
		{"ERR error cause 0xff", &sccp.ERR{ErrorCause: 0xff}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := tt.msg.MarshalBinary()
			if !errors.Is(err, sccp.ErrInvalidMessage) {
				t.Fatalf("MarshalBinary = % x, %v; want ErrInvalidMessage", b, err)
			}
			prefix := []byte{0xee}
			if got, err := tt.msg.AppendBinary(prefix); err == nil || !bytes.Equal(got, prefix) {
				t.Fatalf("AppendBinary = % x, %v; want the input unchanged and an error", got, err)
			}
			if got, err := (sccp.MarshalOptions{MaxMessageSize: sccp.MaxBroadbandMessageSize}).Marshal(tt.msg); !errors.Is(err, sccp.ErrInvalidMessage) {
				t.Fatalf("Marshal with a large maximum = % x, %v; want ErrInvalidMessage", got, err)
			}
		})
	}
}

func TestEncodeAcceptsEdgeValues(t *testing.T) {
	for _, m := range []sccp.Message{
		// The indicator-only calling address of UDT and XUDT, with the
		// national use bit (AI = X0000000).
		&sccp.UDT{CalledPartyAddress: ssn6, CallingPartyAddress: params.PartyAddress{NationalUse: true}, Data: []byte{1}},
		&sccp.XUDT{HopCounter: 15, CalledPartyAddress: ssn6, Data: []byte{1}},
		// A complete message carrying the segmentation parameter is not a
		// segment, so class 0 is allowed.
		&sccp.XUDT{HopCounter: 15, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}, Segmentation: &params.Segmentation{FirstSegment: true}},
		&sccp.CR{ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: ssn6, Data: make([]byte, 128)},
		&sccp.CC{ProtocolClass: params.ProtocolClass{Class: 3}, Credit: ptr(params.MaxCredit), Data: make([]byte, 128)},
		&sccp.ED{Data: make([]byte, 32)},
		&sccp.UDTS{ReturnCause: 0xfe, CalledPartyAddress: ssn6, CallingPartyAddress: ssn7, Data: []byte{1}},
		&sccp.ERR{Unrecognized: []params.RawParameter{{Code: 0xfe}}},
	} {
		b, err := m.MarshalBinary()
		if err != nil {
			t.Fatalf("%v: %v", m, err)
		}
		back, err := sccp.ParseMessage(b)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(m, back, cmpOpts); diff != "" {
			t.Fatalf("round trip:\n%s", diff)
		}
	}
}

// TestTwoOctetPointerAbove255 checks a two-octet pointer whose more
// significant octet is not zero, in both directions (ITU-T Q.713 (03/01) §1.4,
// §2.3).
func TestTwoOctetPointerAbove255(t *testing.T) {
	m := &sccp.LUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7,
		Data: payloadOf(300), Segmentation: &params.Segmentation{FirstSegment: true, LocalReference: 7}}
	b, err := (sccp.MarshalOptions{MaxMessageSize: sccp.MaxBroadbandMessageSize}).Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	// The optional part starts at 19 + 300 = 319; its pointer occupies
	// octets 9 and 10 and counts from octet 10: 309 = 0x0135.
	if b[9] != 0x35 || b[10] != 0x01 || b[17] != 0x2c || b[18] != 0x01 {
		t.Fatalf("pointer % x, long data length % x", b[9:11], b[17:19])
	}
	back, err := sccp.ParseMessage(b)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(sccp.Message(m), back, cmpOpts); diff != "" {
		t.Fatalf("round trip:\n%s", diff)
	}
}

func payloadOf(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// TestBoundaries checks the lengths named by ITU-T Q.713 (03/01) §3.16, Table
// 11 Note 1 and Table 19 Note 1, Q.715 (04/02) §8.3.2 and the maximum message
// size of Q.711 (03/01) §7.1.
func TestBoundaries(t *testing.T) {
	pcSSN8 := params.NewPointCodeAddress(0x0102, 8)
	pcSSN7 := params.NewPointCodeAddress(0x0102, 7)
	indicatorOnly := params.PartyAddress{}
	segment := &params.Segmentation{FirstSegment: true, RemainingSegments: 1, LocalReference: 1}
	udt := func(cd, cg params.PartyAddress, n int) sccp.Message {
		return &sccp.UDT{CalledPartyAddress: cd, CallingPartyAddress: cg, Data: make([]byte, n)}
	}
	xudt := func(cd, cg params.PartyAddress, n int, s *params.Segmentation) sccp.Message {
		return &sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: cd, CallingPartyAddress: cg, Data: make([]byte, n), Segmentation: s}
	}
	tests := []struct {
		name    string
		msg     sccp.Message
		size    int   // encoded size when it encodes
		wantErr error // nil when it encodes at the default maximum
	}{
		// UDT with SSN-only addresses: 12 octets of overhead.
		{"UDT 254 octets", udt(ssn8, ssn7, 254), 266, nil},
		{"UDT 255 octets", udt(ssn8, ssn7, 255), 267, nil},
		{"UDT 256 octets", udt(ssn8, ssn7, 256), 0, sccp.ErrInvalidMessage},
		// UDT with point code and SSN addresses: 16 octets of overhead, so 255
		// octets (271) exceed 268.
		{"UDT PC+SSN 252 octets", udt(pcSSN8, pcSSN7, 252), 268, nil},
		{"UDT PC+SSN 253 octets", udt(pcSSN8, pcSSN7, 253), 269, sccp.ErrMessageTooLong},
		{"UDT PC+SSN 255 octets", udt(pcSSN8, pcSSN7, 255), 271, sccp.ErrMessageTooLong},
		// Single XUDT: Y = 254 with SSN-only addresses and no optional part.
		{"XUDT 254 octets", xudt(ssn8, ssn7, 254, nil), 268, nil},
		{"XUDT 255 octets", xudt(ssn8, ssn7, 255, nil), 269, sccp.ErrMessageTooLong},
		{"XUDT 256 octets", xudt(ssn8, ssn7, 256, nil), 0, sccp.ErrInvalidMessage},
		// Segmented XUDT: Y is at most 247 with the segmentation parameter.
		// One octet more and the one-octet pointer to the optional part
		// would have to be 256, so the message cannot be encoded at any
		// maximum size.
		{"XUDT segment 247 octets", xudt(ssn8, ssn7, 247, segment), 268, nil},
		{"XUDT segment 248 octets", xudt(ssn8, ssn7, 248, segment), 0, sccp.ErrInvalidMessage},
		// Q.715 §8.3.2: 248 with the minimal one-octet calling party address.
		{"XUDT segment 248 octets, minimal calling address", xudt(ssn8, indicatorOnly, 248, segment), 268, nil},
		{"XUDT segment 249 octets, minimal calling address", xudt(ssn8, indicatorOnly, 249, segment), 0, sccp.ErrInvalidMessage},
		// Without the optional part the same 248 octets are only too long.
		{"XUDT 248 octets, PC+SSN called address", xudt(pcSSN8, ssn7, 248, nil), 264, nil},
		{"XUDT 253 octets, PC+SSN called address", xudt(pcSSN8, ssn7, 253, nil), 269, sccp.ErrMessageTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := tt.msg.MarshalBinary()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("MarshalBinary: %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && len(b) != tt.size {
				t.Fatalf("encoded %d octets, want %d", len(b), tt.size)
			}
			if !errors.Is(tt.wantErr, sccp.ErrMessageTooLong) {
				return
			}
			// The configured maximum moves the boundary: exactly the size
			// encodes, one octet less refuses it.
			if b, err := (sccp.MarshalOptions{MaxMessageSize: tt.size}).Marshal(tt.msg); err != nil || len(b) != tt.size {
				t.Fatalf("Marshal at maximum %d: %d octets, %v", tt.size, len(b), err)
			}
			if _, err := (sccp.MarshalOptions{MaxMessageSize: tt.size - 1}).Marshal(tt.msg); !errors.Is(err, sccp.ErrMessageTooLong) {
				t.Fatalf("Marshal at maximum %d: %v, want ErrMessageTooLong", tt.size-1, err)
			}
		})
	}

	ludt := &sccp.LUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: make([]byte, sccp.MaxLongData)}
	if _, err := ludt.MarshalBinary(); !errors.Is(err, sccp.ErrMessageTooLong) {
		t.Errorf("LUDT of %d octets at the default maximum: %v, want ErrMessageTooLong", sccp.MaxLongData, err)
	}
	b, err := (sccp.MarshalOptions{MaxMessageSize: sccp.MaxBroadbandMessageSize}).Marshal(ludt)
	if err != nil {
		t.Fatalf("LUDT of %d octets at the broadband maximum: %v", sccp.MaxLongData, err)
	}
	back, err := sccp.ParseMessage(b)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(sccp.Message(ludt), back, cmpOpts); diff != "" {
		t.Fatalf("LUDT round trip:\n%s", diff)
	}
	// No MTP-SAP carries more than the broadband maximum (ITU-T Q.711
	// (03/01) §7.1 b)).
	for _, limit := range []int{-1, sccp.MaxBroadbandMessageSize + 1} {
		if _, err := (sccp.MarshalOptions{MaxMessageSize: limit}).Marshal(ludt); !errors.Is(err, sccp.ErrInvalidMessage) {
			t.Errorf("maximum %d: %v", limit, err)
		}
	}
}

func TestSCMG(t *testing.T) {
	tests := []struct {
		msg  sccp.SCMG
		wire string
	}{
		{sccp.SCMG{Type: sccp.SCMGTypeSST, AffectedSSN: 8, AffectedPointCode: 0x1234}, "03 08 3412 00"},
		{sccp.SCMG{Type: sccp.SCMGTypeSSA, AffectedSSN: 6, AffectedPointCode: 0x3fff, SubsystemMultiplicityIndicator: 3}, "01 06 ff3f 03"},
		{sccp.SCMG{Type: sccp.SCMGTypeSSC, AffectedSSN: 1, AffectedPointCode: 0x0102, CongestionLevel: 8}, "06 01 0201 00 08"},
	}
	for _, tt := range tests {
		want := unhex(t, tt.wire)
		got, err := tt.msg.MarshalBinary()
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s MarshalBinary = % x, %v; want % x", tt.msg, got, err, want)
		}
		var back sccp.SCMG
		if err := back.UnmarshalBinary(want); err != nil || back != tt.msg {
			t.Errorf("UnmarshalBinary(% x) = %v, %v", want, back, err)
		}
	}
	// Q.714 §1.1.4.4: spare bits are not examined. Q.713 §5.2.2 to §5.2.4:
	// point code bits 15-16, SMI bits 3-8 and congestion bits 5-8 are spare.
	var s sccp.SCMG
	if err := s.UnmarshalBinary(unhex(t, "06 06 34d2 fc f4 ff")); err != nil {
		t.Fatal(err)
	}
	if want := (sccp.SCMG{Type: sccp.SCMGTypeSSC, AffectedSSN: 6, AffectedPointCode: 0x1234, CongestionLevel: 4}); s != want {
		t.Errorf("spare bits examined: %v, want %v", s, want)
	}
	for _, invalid := range []sccp.SCMG{
		{Type: 0}, {Type: 7},
		{Type: sccp.SCMGTypeSSA, AffectedPointCode: 0x4000},
		{Type: sccp.SCMGTypeSSA, SubsystemMultiplicityIndicator: 4},
		{Type: sccp.SCMGTypeSSA, CongestionLevel: 1},
		{Type: sccp.SCMGTypeSSC, CongestionLevel: 0},
		{Type: sccp.SCMGTypeSSC, CongestionLevel: 9},
	} {
		if b, err := invalid.MarshalBinary(); !errors.Is(err, sccp.ErrInvalidMessage) {
			t.Errorf("%v MarshalBinary = % x, %v; want ErrInvalidMessage", invalid, b, err)
		}
	}
	for _, wire := range []string{"03 08 3412", "06 01 0201 00", "07 01 0201 00", ""} {
		if err := s.UnmarshalBinary(unhex(t, wire)); !errors.Is(err, sccp.ErrMalformedMessage) {
			t.Errorf("UnmarshalBinary(%s) = %v, want ErrMalformedMessage", wire, err)
		}
	}
}

func TestZeroValueStrings(t *testing.T) {
	for _, m := range []sccp.Message{
		&sccp.CR{}, &sccp.CC{}, &sccp.CREF{}, &sccp.RLSD{}, &sccp.RLC{}, &sccp.DT1{}, &sccp.DT2{}, &sccp.AK{},
		&sccp.UDT{}, &sccp.UDTS{}, &sccp.ED{}, &sccp.EA{}, &sccp.RSR{}, &sccp.RSC{}, &sccp.ERR{}, &sccp.IT{},
		&sccp.XUDT{}, &sccp.XUDTS{}, &sccp.LUDT{}, &sccp.LUDTS{},
	} {
		if m.String() == "" || m.MessageType().String() == "" {
			t.Errorf("%T has an empty String", m)
		}
	}
	if sccp.MsgType(0xff).String() == "" || sccp.SCMGType(0).String() == "" {
		t.Error("unknown type has an empty String")
	}
}
