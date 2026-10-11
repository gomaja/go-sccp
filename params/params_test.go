// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package params_test

import (
	"bytes"
	"encoding"
	"errors"
	"io"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/gomaja/go-sccp/params"
)

type contents interface {
	encoding.BinaryMarshaler
	AppendBinary([]byte) ([]byte, error)
}

type decodable interface {
	encoding.BinaryUnmarshaler
}

func mustGT(t *testing.T) func(*params.GlobalTitle, error) *params.GlobalTitle {
	return func(gt *params.GlobalTitle, err error) *params.GlobalTitle {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return gt
	}
}

// TestVectors checks each parameter against its ITU-T Q.713 (03/01) clause 3
// encoding, in both directions.
func TestVectors(t *testing.T) {
	gt := mustGT(t)
	tests := []struct {
		name    string
		value   contents
		decoded func() decodable
		wire    []byte
	}{
		{"protocol class 0", params.ProtocolClass{Class: 0}, func() decodable { return new(params.ProtocolClass) }, []byte{0x00}},
		{"protocol class 1 with return on error", params.ProtocolClass{Class: 1, ReturnOnError: true}, func() decodable { return new(params.ProtocolClass) }, []byte{0x81}},
		{"protocol class 3", params.ProtocolClass{Class: 3}, func() decodable { return new(params.ProtocolClass) }, []byte{0x03}},
		// §3.2/§3.3: least significant octet first.
		{"local reference", params.LocalReference(0x030201), func() decodable { return new(params.LocalReference) }, []byte{0x01, 0x02, 0x03}},
		{"credit", params.Credit(127), func() decodable { return new(params.Credit) }, []byte{0x7f}},
		{"hop counter", params.HopCounter(15), func() decodable { return new(params.HopCounter) }, []byte{0x0f}},
		{"importance", params.Importance(7), func() decodable { return new(params.Importance) }, []byte{0x07}},
		// §3.9: P(S) and P(R) in bits 8-2, M in bit 1 of octet 2.
		{"sequencing/segmenting", params.SequencingSegmenting{SendSequenceNumber: 127, ReceiveSequenceNumber: 1, MoreData: true}, func() decodable { return new(params.SequencingSegmenting) }, []byte{0xfe, 0x03}},
		{"sequencing/segmenting P(R) 127", params.SequencingSegmenting{ReceiveSequenceNumber: 127}, func() decodable { return new(params.SequencingSegmenting) }, []byte{0x00, 0xfe}},
		{"credit 127", params.MaxCredit, func() decodable { return new(params.Credit) }, []byte{0x7f}},
		// §3.17: F bit 8, C bit 7, remaining bits 4-1, reference LSB first.
		{"segmentation first", params.Segmentation{FirstSegment: true, InSequenceDelivery: true, RemainingSegments: 15, LocalReference: 0x030201}, func() decodable { return new(params.Segmentation) }, []byte{0xcf, 0x01, 0x02, 0x03}},
		{"segmentation last", params.Segmentation{RemainingSegments: 0, LocalReference: 0xffffff}, func() decodable { return new(params.Segmentation) }, []byte{0x00, 0xff, 0xff, 0xff}},
		// §3.4: SSN-only, route on SSN.
		{"address SSN", params.NewSSNAddress(6), func() decodable { return new(params.PartyAddress) }, []byte{0x42, 0x06}},
		// §3.4.2.1: point code LSB first, bits 7-8 of octet 2 zero.
		{"address PC SSN", params.NewPointCodeAddress(0x3fff, 8), func() decodable { return new(params.PartyAddress) }, []byte{0x43, 0xff, 0x3f, 0x08}},
		// §3.5: calling address that is only an address indicator.
		{"address indicator only", params.PartyAddress{}, func() decodable { return new(params.PartyAddress) }, []byte{0x00}},
		{"address national use", params.PartyAddress{NationalUse: true, RouteOnSSN: true, HasSSN: true, SSN: 1}, func() decodable { return new(params.PartyAddress) }, []byte{0xc2, 0x01}},
		// §3.4.2.3.4: GTI 4, TT 0, NP E.164, ES BCD odd, NAI international,
		// nine digits with filler 0000.
		{"address GTI 4", params.NewGlobalTitleAddress(gt(params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "123456789")), 6), func() decodable { return new(params.PartyAddress) },
			[]byte{0x12, 0x06, 0x00, 0x11, 0x04, 0x21, 0x43, 0x65, 0x87, 0x09}},
		// §3.4.2.3.1: GTI 1, odd/even in bit 8 of the NAI octet.
		{"address GTI 1 odd", params.PartyAddress{GlobalTitle: gt(params.NewGlobalTitle1(params.NAIInternationalNumber, "123"))}, func() decodable { return new(params.PartyAddress) },
			[]byte{0x04, 0x84, 0x21, 0x03}},
		{"address GTI 1 even", params.PartyAddress{GlobalTitle: gt(params.NewGlobalTitle1(params.NAIInternationalNumber, "1234"))}, func() decodable { return new(params.PartyAddress) },
			[]byte{0x04, 0x04, 0x21, 0x43}},
		// A global title of its header only.
		{"address GTI 2 without address information", params.PartyAddress{HasSSN: true, SSN: 7, GlobalTitle: &params.GlobalTitle{Indicator: params.GTITTOnly, TranslationType: 9}}, func() decodable { return new(params.PartyAddress) },
			[]byte{0x0a, 0x07, 0x09}},
		// §3.4.2.3.2: GTI 2, translation type then address.
		{"address GTI 2", params.PartyAddress{HasSSN: true, SSN: 7, GlobalTitle: &params.GlobalTitle{Indicator: params.GTITTOnly, TranslationType: 9, AddressInformation: []byte{0x21}}}, func() decodable { return new(params.PartyAddress) },
			[]byte{0x0a, 0x07, 0x09, 0x21}},
		// §3.4.2.3.3: GTI 3, ES BCD even.
		{"address GTI 3 even", params.PartyAddress{GlobalTitle: gt(params.NewGlobalTitle3(0, params.NPISDNMobile, "1234"))}, func() decodable { return new(params.PartyAddress) },
			[]byte{0x0c, 0x00, 0x72, 0x21, 0x43}},
		// Spare GTI 8: no defined format, contents carried as they are.
		{"address spare GTI", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: 8, AddressInformation: []byte{0xaa, 0xbb}}}, func() decodable { return new(params.PartyAddress) },
			[]byte{0x20, 0xaa, 0xbb}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.value.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			if !bytes.Equal(got, tt.wire) {
				t.Fatalf("MarshalBinary = % x, want % x", got, tt.wire)
			}
			appended, err := tt.value.AppendBinary([]byte{0xee})
			if err != nil || !bytes.Equal(appended, append([]byte{0xee}, tt.wire...)) {
				t.Fatalf("AppendBinary = % x, %v", appended, err)
			}
			d := tt.decoded()
			if err := d.UnmarshalBinary(tt.wire); err != nil {
				t.Fatalf("UnmarshalBinary: %v", err)
			}
			want := any(tt.value)
			if p, ok := d.(*params.PartyAddress); ok {
				if diff := cmp.Diff(want, *p); diff != "" {
					t.Fatalf("decoded mismatch (-want +got):\n%s", diff)
				}
				return
			}
			if diff := cmp.Diff(want, deref(d)); diff != "" {
				t.Fatalf("decoded mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func deref(d decodable) any {
	switch v := d.(type) {
	case *params.ProtocolClass:
		return *v
	case *params.LocalReference:
		return *v
	case *params.Credit:
		return *v
	case *params.HopCounter:
		return *v
	case *params.Importance:
		return *v
	case *params.SequencingSegmenting:
		return *v
	case *params.Segmentation:
		return *v
	}
	panic("unhandled type")
}

// TestEncodeRefusesValuesThatWouldChange checks that the encoder reports every
// value it cannot carry instead of masking or truncating it.
func TestEncodeRefusesValuesThatWouldChange(t *testing.T) {
	tests := []struct {
		name  string
		value contents
	}{
		{"protocol class 4", params.ProtocolClass{Class: 4}},
		{"return on error with class 2", params.ProtocolClass{Class: 2, ReturnOnError: true}},
		{"return on error with class 3", params.ProtocolClass{Class: 3, ReturnOnError: true}},
		{"local reference over 24 bits", params.LocalReference(0x1000000)},
		{"reserved local reference", params.LocalReferenceReserved},
		{"hop counter 0", params.HopCounter(0)},
		{"hop counter 16", params.HopCounter(16)},
		{"importance 8", params.Importance(8)},
		{"send sequence number 128", params.SequencingSegmenting{SendSequenceNumber: 128}},
		{"receive sequence number 128", params.SequencingSegmenting{ReceiveSequenceNumber: 128}},
		{"remaining segments 16", params.Segmentation{RemainingSegments: 16}},
		{"segmentation local reference over 24 bits", params.Segmentation{LocalReference: 0x1000000}},
		{"point code over 14 bits", params.NewPointCodeAddress(0x4000, 1)},
		{"point code without indicator", params.PartyAddress{PointCode: 1}},
		{"SSN without indicator", params.PartyAddress{SSN: 1}},
		{"GTI 0 global title", params.PartyAddress{GlobalTitle: &params.GlobalTitle{}}},
		{"GTI 16 global title", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: 16}}},
		{"GTI 1 with translation type", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: params.GTINAIOnly, TranslationType: 1}}},
		{"GTI 1 with numbering plan", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: params.GTINAIOnly, NumberingPlan: 1}}},
		{"GTI 2 with nature of address", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: params.GTITTOnly, NatureOfAddress: 1}}},
		{"GTI 2 with encoding scheme", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: params.GTITTOnly, EncodingScheme: 1}}},
		{"GTI 3 with odd digits flag", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: params.GTITTNPES, OddDigits: true}}},
		{"GTI 3 with nature of address", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: params.GTITTNPES, NatureOfAddress: 4}}},
		{"GTI 4 with odd digits flag", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: params.GTITTNPESNAI, OddDigits: true}}},
		{"GTI 4 numbering plan over 4 bits", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: params.GTITTNPESNAI, NumberingPlan: 16}}},
		{"GTI 4 encoding scheme over 4 bits", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: params.GTITTNPESNAI, EncodingScheme: 16}}},
		{"GTI 4 nature of address over 7 bits", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: params.GTITTNPESNAI, NatureOfAddress: 0x80}}},
		{"spare GTI with translation type", params.PartyAddress{GlobalTitle: &params.GlobalTitle{Indicator: 5, TranslationType: 1}}},
		{"credit 128", params.Credit(128)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := tt.value.MarshalBinary()
			if !errors.Is(err, params.ErrInvalidValue) {
				t.Fatalf("MarshalBinary = % x, %v; want ErrInvalidValue", b, err)
			}
			prefix := []byte{0xee}
			got, err := tt.value.AppendBinary(prefix)
			if !errors.Is(err, params.ErrInvalidValue) || !bytes.Equal(got, prefix) {
				t.Fatalf("AppendBinary = % x, %v; want the input unchanged and ErrInvalidValue", got, err)
			}
		})
	}
}

// TestDecodeIgnoresSpareBits checks ITU-T Q.714 (05/01) §1.1.4.4: spare fields
// are not examined at the destination node.
func TestDecodeIgnoresSpareBits(t *testing.T) {
	// For classes 0 and 1, bits 5-8 are a message handling code (ITU-T Q.713
	// (03/01) §3.6): 1000 is return message on error, and the spare codes
	// 0001 to 0111 and 1001 to 1111 are reset to the default, no special
	// options (ITU-T Q.714 (05/01) §1.1.4.3).
	var pc params.ProtocolClass
	for octet, want := range map[byte]params.ProtocolClass{
		0x01: {Class: 1}, 0x71: {Class: 1}, 0x81: {Class: 1, ReturnOnError: true},
		0x91: {Class: 1}, 0xf1: {Class: 1}, 0x80: {Class: 0, ReturnOnError: true}, 0xf0: {Class: 0},
	} {
		if err := pc.UnmarshalBinary([]byte{octet}); err != nil || pc != want {
			t.Errorf("protocol class %#02x = %+v, %v; want %+v", octet, pc, err, want)
		}
	}
	// Bits 5-8 are spare for classes 2 and 3, so bit 8 is not return on error.
	if err := pc.UnmarshalBinary([]byte{0x82}); err != nil || pc != (params.ProtocolClass{Class: 2}) {
		t.Errorf("protocol class 0x82 = %+v, %v", pc, err)
	}
	// Undefined classes are returned for the procedures to reject.
	if err := pc.UnmarshalBinary([]byte{0x0e}); err != nil || pc.Class != 14 {
		t.Errorf("protocol class 0x0e = %+v, %v", pc, err)
	}

	var imp params.Importance
	if err := imp.UnmarshalBinary([]byte{0xfd}); err != nil || imp != 5 {
		t.Errorf("importance 0xfd = %d, %v; bits 4-8 are spare", imp, err)
	}
	var seq params.SequencingSegmenting
	if err := seq.UnmarshalBinary([]byte{0x05, 0x04}); err != nil || seq != (params.SequencingSegmenting{SendSequenceNumber: 2, ReceiveSequenceNumber: 2}) {
		t.Errorf("sequencing/segmenting 05 04 = %+v, %v; bit 1 of octet 1 is spare", seq, err)
	}
	var seg params.Segmentation
	if err := seg.UnmarshalBinary([]byte{0x31, 0x00, 0x00, 0x00}); err != nil || seg != (params.Segmentation{RemainingSegments: 1}) {
		t.Errorf("segmentation 31 = %+v, %v; bits 6-5 are spare", seg, err)
	}
	var addr params.PartyAddress
	if err := addr.UnmarshalBinary([]byte{0x41, 0xff, 0xff}); err != nil || addr.PointCode != 0x3fff {
		t.Errorf("point code ff ff = %+v, %v; bits 7-8 of octet 2 are spare", addr, err)
	}
	// GTI 4: bit 8 of the nature of address octet is spare.
	if err := addr.UnmarshalBinary([]byte{0x10, 0x00, 0x12, 0x84, 0x21}); err != nil || addr.GlobalTitle.NatureOfAddress != params.NAIInternationalNumber {
		t.Errorf("GTI 4 NAI 0x84 = %+v, %v", addr.GlobalTitle, err)
	}
}

// TestFailedDecodeKeepsReceiver checks that a decode error leaves the value as
// it was, and that truncation matches both ErrMalformed and io.ErrUnexpectedEOF.
func TestFailedDecodeKeepsReceiver(t *testing.T) {
	a := params.NewPointCodeAddress(0x1234, 6)
	before := a
	err := a.UnmarshalBinary([]byte{0x43, 0x01})
	if !errors.Is(err, params.ErrMalformed) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated address: %v", err)
	}
	if diff := cmp.Diff(before, a); diff != "" {
		t.Fatalf("failed decode changed the address:\n%s", diff)
	}
	var c params.Credit
	if err := c.UnmarshalBinary(nil); !errors.Is(err, params.ErrMalformed) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("empty credit: %v", err)
	}
	if err := (params.RawParameter{Code: params.PCodeReserved}).Validate(); !errors.Is(err, params.ErrInvalidValue) {
		t.Fatalf("reserved parameter name: %v", err)
	}
}

func TestDecodeRejectsTruncatedContents(t *testing.T) {
	tests := []struct {
		name string
		dec  decodable
		wire []byte
	}{
		{"protocol class", new(params.ProtocolClass), nil},
		{"local reference", new(params.LocalReference), []byte{1, 2}},
		{"sequencing/segmenting", new(params.SequencingSegmenting), []byte{1}},
		{"segmentation", new(params.Segmentation), []byte{0x80, 1, 2}},
		{"empty address", new(params.PartyAddress), nil},
		{"address short point code", new(params.PartyAddress), []byte{0x01, 0x01}},
		{"address missing SSN", new(params.PartyAddress), []byte{0x03, 0x01, 0x00}},
		{"address GTI 4 short header", new(params.PartyAddress), []byte{0x10, 0x00, 0x11}},
		{"address GTI 1 missing header", new(params.PartyAddress), []byte{0x04}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.dec.UnmarshalBinary(tt.wire); !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("UnmarshalBinary(% x) = %v, want io.ErrUnexpectedEOF", tt.wire, err)
			}
		})
	}
}

func TestDecodeRejectsWrongFixedLength(t *testing.T) {
	for _, d := range []decodable{new(params.ProtocolClass), new(params.Importance), new(params.HopCounter), new(params.Credit)} {
		if err := d.UnmarshalBinary([]byte{1, 2}); !errors.Is(err, params.ErrMalformed) {
			t.Errorf("%T.UnmarshalBinary(2 octets) = %v, want ErrMalformed", d, err)
		}
	}
	for _, d := range []decodable{new(params.LocalReference), new(params.Segmentation), new(params.SequencingSegmenting)} {
		if err := d.UnmarshalBinary(make([]byte, 5)); !errors.Is(err, params.ErrMalformed) {
			t.Errorf("%T.UnmarshalBinary(5 octets) = %v, want ErrMalformed", d, err)
		}
	}
}

// TestDecodedValuesDoNotAliasInput checks that decoded byte slices survive the
// caller reusing its receive buffer.
func TestDecodedValuesDoNotAliasInput(t *testing.T) {
	wire := []byte{0x12, 0x06, 0x00, 0x12, 0x04, 0x21, 0x43}
	var addr params.PartyAddress
	if err := addr.UnmarshalBinary(wire); err != nil {
		t.Fatal(err)
	}
	for i := range wire {
		wire[i] = 0xff
	}
	if !bytes.Equal(addr.GlobalTitle.AddressInformation, []byte{0x21, 0x43}) {
		t.Fatalf("address information changed with the input buffer: % x", addr.GlobalTitle.AddressInformation)
	}
}

func TestBCD(t *testing.T) {
	tests := []struct {
		digits string
		wire   []byte
		odd    bool
	}{
		{"", []byte{}, false},
		{"1", []byte{0x01}, true},
		{"12", []byte{0x21}, false},
		// §3.4.2.3.1: an odd count ends with the filler code 0000.
		{"123", []byte{0x21, 0x03}, true},
		{"4479000000", []byte{0x44, 0x97, 0x00, 0x00, 0x00}, false},
		// Figure 8/Q.713: code 11, code 12 and ST.
		{"9BCF", []byte{0xb9, 0xfc}, false},
	}
	for _, tt := range tests {
		got, odd, err := params.EncodeBCD(tt.digits)
		if err != nil || !bytes.Equal(got, tt.wire) || odd != tt.odd {
			t.Errorf("EncodeBCD(%q) = % x, %t, %v; want % x, %t", tt.digits, got, odd, err, tt.wire, tt.odd)
		}
		back, err := params.DecodeBCD(tt.wire, tt.odd)
		if err != nil || back != tt.digits {
			t.Errorf("DecodeBCD(% x, %t) = %q, %v; want %q", tt.wire, tt.odd, back, err, tt.digits)
		}
	}
	if _, _, err := params.EncodeBCD("12a"); !errors.Is(err, params.ErrInvalidValue) {
		t.Errorf("EncodeBCD with the spare code A = %v, want ErrInvalidValue", err)
	}
	if _, _, err := params.EncodeBCD("1+"); !errors.Is(err, params.ErrInvalidValue) {
		t.Errorf("EncodeBCD with + = %v, want ErrInvalidValue", err)
	}
	// Liberal decoding: spare codes decode, the filler is not examined.
	if s, err := params.DecodeBCD([]byte{0xda, 0xfe}, true); err != nil || s != "ADE" {
		t.Errorf("DecodeBCD(da fe, odd) = %q, %v; want ADE", s, err)
	}
	if _, err := params.DecodeBCD(nil, true); !errors.Is(err, params.ErrMalformed) {
		t.Errorf("DecodeBCD(empty, odd) = %v, want ErrMalformed", err)
	}
	if s, _, err := params.EncodeBCD("b"); err != nil {
		t.Errorf("lower case code 11: %v", err)
	} else if s[0] != 0x0b {
		t.Errorf("lower case code 11 = % x", s)
	}
}

func TestGlobalTitleDigits(t *testing.T) {
	gt, err := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "4479000000")
	if err != nil {
		t.Fatal(err)
	}
	if gt.EncodingScheme != params.ESBCDEven {
		t.Errorf("even digit count set encoding scheme %s", gt.EncodingScheme)
	}
	if d, err := gt.Digits(); err != nil || d != "4479000000" {
		t.Errorf("Digits = %q, %v", d, err)
	}
	gt1, err := params.NewGlobalTitle1(params.NAINationalSignificantNumber, "12345")
	if err != nil || !gt1.OddDigits {
		t.Fatalf("NewGlobalTitle1 = %+v, %v", gt1, err)
	}
	if d, err := gt1.Digits(); err != nil || d != "12345" {
		t.Errorf("GTI 1 Digits = %q, %v", d, err)
	}
	for _, gt := range []*params.GlobalTitle{
		{Indicator: params.GTITTOnly, AddressInformation: []byte{0x21}},
		{Indicator: params.GTITTNPESNAI, EncodingScheme: params.ESNationalSpecific, AddressInformation: []byte{0x21}},
		{Indicator: 9, AddressInformation: []byte{0x21}},
	} {
		if d, err := gt.Digits(); !errors.Is(err, params.ErrMalformed) {
			t.Errorf("Digits of %v = %q, %v; want ErrMalformed", gt, d, err)
		}
	}
	if _, err := params.NewGlobalTitle4(0, params.NPISDNTelephony, 0x80, "1"); !errors.Is(err, params.ErrInvalidValue) {
		t.Errorf("NewGlobalTitle4 with an 8-bit NAI = %v, want ErrInvalidValue", err)
	}
}

func TestRawParameterValidate(t *testing.T) {
	if err := (params.RawParameter{Code: 0x7f, Value: make([]byte, 255)}).Validate(); err != nil {
		t.Errorf("255-octet raw parameter: %v", err)
	}
	if err := (params.RawParameter{Code: 0x7f, Value: make([]byte, 256)}).Validate(); !errors.Is(err, params.ErrInvalidValue) {
		t.Errorf("256-octet raw parameter = %v, want ErrInvalidValue", err)
	}
	if err := (params.RawParameter{Code: params.PCodeEndOfOptionalParameters}).Validate(); !errors.Is(err, params.ErrInvalidValue) {
		t.Errorf("raw end of optional parameters = %v, want ErrInvalidValue", err)
	}
}

func TestStrings(t *testing.T) {
	gt, _ := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "123")
	for _, s := range []string{
		params.NewGlobalTitleAddress(gt, 6).String(),
		params.NewPointCodeAddress(1, 2).String(),
		params.PartyAddress{NationalUse: true}.String(),
		(&params.GlobalTitle{Indicator: params.GTITTOnly, TranslationType: 3}).String(),
		(&params.GlobalTitle{Indicator: params.GTITTNPES}).String(),
		(&params.GlobalTitle{Indicator: params.GTINAIOnly}).String(),
		params.Segmentation{}.String(),
		params.SequencingSegmenting{}.String(),
		params.ProtocolClass{}.String(),
		params.RawParameter{Code: 0x55}.String(),
		params.ReturnCauseSegmentationFailure.String(),
		params.ReturnCause(0xff).String(),
		params.PCodeLongData.String(),
	} {
		if s == "" {
			t.Error("empty String()")
		}
	}
}

// FuzzPartyAddress checks that decoding never panics and that any decoded
// address re-encodes to contents that decode to the same address.
func FuzzPartyAddress(f *testing.F) {
	for _, seed := range [][]byte{
		{0x42, 0x06},
		{0x43, 0xff, 0x3f, 0x08},
		{0x12, 0x06, 0x00, 0x11, 0x04, 0x21, 0x43, 0x65, 0x87, 0x09},
		{0x04, 0x84, 0x21, 0x03},
		{0x0a, 0x07, 0x09, 0x21},
		{0x20, 0xaa},
		{0x00},
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, wire []byte) {
		var a params.PartyAddress
		if err := a.UnmarshalBinary(wire); err != nil {
			return
		}
		_ = a.String()
		enc, err := a.MarshalBinary()
		if err != nil {
			t.Fatalf("decoded address %v does not re-encode: %v", a, err)
		}
		var b params.PartyAddress
		if err := b.UnmarshalBinary(enc); err != nil {
			t.Fatalf("re-encoded address % x does not decode: %v", enc, err)
		}
		if diff := cmp.Diff(a, b); diff != "" {
			t.Fatalf("re-encoding changed the address (-first +second):\n%s", diff)
		}
	})
}

// FuzzBCD checks that BCD encoding round-trips every encodable signal string.
func FuzzBCD(f *testing.F) {
	for _, seed := range []string{"", "1", "4479000000", "9BCF", "123"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, digits string) {
		addr, odd, err := params.EncodeBCD(digits)
		if err != nil {
			return
		}
		back, err := params.DecodeBCD(addr, odd)
		if err != nil {
			t.Fatalf("DecodeBCD(% x, %t): %v", addr, odd, err)
		}
		if !bytes.EqualFold([]byte(back), []byte(digits)) {
			t.Fatalf("BCD round trip %q -> % x -> %q", digits, addr, back)
		}
	})
}
