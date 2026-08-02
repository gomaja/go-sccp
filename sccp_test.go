// Copyright 2019-2024 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp_test

import (
	"encoding"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
	"github.com/pascaldekloe/goe/verify"
)

type serializable interface {
	encoding.BinaryMarshaler
	MarshalTo([]byte) error
	MarshalLen() int
}

var testcases = []struct {
	description string
	structured  serializable
	serialized  []byte
	parseFunc   func([]byte) (serializable, error)
}{
	{
		description: "CR/No optionals",
		structured: sccp.NewCR(
			0x040506,
			1,
			true,
			params.NewCalledPartyAddress(0x42, 0, 6, nil),
		),
		serialized: []byte{
			0x01,
			0x04, 0x05, 0x06,
			0x81,
			0x02, 0x00,
			0x02, 0x42, 0x06,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseCR(b)
		},
	},
	{
		description: "CR/with optionals",
		structured: sccp.NewCR(
			0x040506,
			1,
			true,
			params.NewCalledPartyAddress(0x42, 0, 6, nil),
			params.NewCreditOptional(5),
			params.NewCallingPartyAddressOptional(0x42, 0, 7, nil),
			params.NewDataOptional([]byte{0xaa, 0xbb}),
			params.NewHopCounterOptional(4),
			params.NewImportance(2),
		),
		serialized: []byte{
			0x01,
			0x04, 0x05, 0x06,
			0x81,
			0x02, 0x04,
			0x02, 0x42, 0x06,
			0x09, 0x01, 0x05,
			0x04, 0x02, 0x42, 0x07,
			0x0f, 0x02, 0xaa, 0xbb,
			0x11, 0x01, 0x04,
			0x12, 0x01, 0x02,
			0x00,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseCR(b)
		},
	},
	{
		description: "CC/No optionals",
		structured:  sccp.NewCC(0x010203, 0x040506, 2, false),
		serialized: []byte{
			0x02,
			0x01, 0x02, 0x03,
			0x04, 0x05, 0x06,
			0x02,
			0x00,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseCC(b)
		},
	},
	{
		description: "CC/with optionals",
		structured: sccp.NewCC(
			0x010203,
			0x040506,
			2,
			false,
			params.NewCreditOptional(5),
			params.NewCalledPartyAddressOptional(0x42, 0, 6, nil),
			params.NewDataOptional([]byte{0xaa, 0xbb}),
			params.NewImportance(2),
		),
		serialized: []byte{
			0x02,
			0x01, 0x02, 0x03,
			0x04, 0x05, 0x06,
			0x02,
			0x01,
			0x09, 0x01, 0x05,
			0x03, 0x02, 0x42, 0x06,
			0x0f, 0x02, 0xaa, 0xbb,
			0x12, 0x01, 0x02,
			0x00,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseCC(b)
		},
	},
	{
		description: "CREF/No optionals",
		structured:  sccp.NewCREF(0x010203, params.RefusalCauseSCCPUserOriginated),
		serialized:  []byte{0x03, 0x01, 0x02, 0x03, 0x03, 0x00},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseCREF(b)
		},
	},
	{
		description: "CREF/with optionals",
		structured: sccp.NewCREF(
			0x010203,
			params.RefusalCauseSCCPUserOriginated,
			params.NewCalledPartyAddressOptional(0x42, 0, 6, nil),
			params.NewDataOptional([]byte{0xaa, 0xbb}),
			params.NewImportance(2),
		),
		serialized: []byte{
			0x03,
			0x01, 0x02, 0x03,
			0x03,
			0x01,
			0x03, 0x02, 0x42, 0x06,
			0x0f, 0x02, 0xaa, 0xbb,
			0x12, 0x01, 0x02,
			0x00,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseCREF(b)
		},
	},
	{
		description: "RLSD/No optionals",
		structured:  sccp.NewRLSD(0x010203, 0x040506, params.ReleaseCauseSCCPUserOriginated),
		serialized:  []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x03, 0x00},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseRLSD(b)
		},
	},
	{
		description: "RLSD/with optionals",
		structured: sccp.NewRLSD(
			0x010203,
			0x040506,
			params.ReleaseCauseSCCPUserOriginated,
			params.NewDataOptional([]byte{0xaa, 0xbb}),
			params.NewImportance(2),
		),
		serialized: []byte{
			0x04,
			0x01, 0x02, 0x03,
			0x04, 0x05, 0x06,
			0x03,
			0x01,
			0x0f, 0x02, 0xaa, 0xbb,
			0x12, 0x01, 0x02,
			0x00,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseRLSD(b)
		},
	},
	{
		description: "RLC",
		structured:  sccp.NewRLC(0x010203, 0x040506),
		serialized:  []byte{0x05, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseRLC(b)
		},
	},
	{
		description: "DT1",
		structured:  sccp.NewDT1(0x010203, true, []byte{0xaa, 0xbb}),
		serialized:  []byte{0x06, 0x01, 0x02, 0x03, 0x01, 0x01, 0x02, 0xaa, 0xbb},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseDT1(b)
		},
	},
	{
		description: "DT2",
		structured:  sccp.NewDT2(0x010203, 0x76, 0x78, true, []byte{0xaa, 0xbb}),
		serialized:  []byte{0x07, 0x01, 0x02, 0x03, 0x76, 0x79, 0x01, 0x02, 0xaa, 0xbb},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseDT2(b)
		},
	},
	{
		description: "AK",
		structured:  sccp.NewAK(0x010203, 0x76, 5),
		serialized:  []byte{0x08, 0x01, 0x02, 0x03, 0x76, 0x05},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseAK(b)
		},
	},
	{
		description: "UDT",
		structured: sccp.NewUDT(
			1,    // Protocol Class
			true, // Message handling
			params.NewCalledPartyAddress(
				params.NewAddressIndicator(false, true, false, params.GTITTNPESNAI),
				0, 6, // SPC, SSN
				params.NewGlobalTitle(
					params.GTITTNPESNAI,
					params.TranslationType(0),
					params.NPISDNTelephony,
					params.ESBCDOdd,
					params.NAIInternationalNumber,
					[]byte{0x21, 0x43, 0x65, 0x87, 0x09, 0x21, 0x43, 0x65},
				),
			),
			params.NewCallingPartyAddress(
				params.NewAddressIndicator(false, true, false, params.GTITTNPESNAI),
				0, 7, // SPC, SSN
				params.NewGlobalTitle(
					params.GTITTNPESNAI,
					params.TranslationType(0),
					params.NPISDNTelephony,
					params.ESBCDEven,
					params.NAIInternationalNumber,
					[]byte{0x89, 0x67, 0x45, 0x23, 0x01},
				),
			),
			[]byte{0xde, 0xad, 0xbe, 0xef},
		),
		serialized: []byte{
			0x09,
			0x81,
			0x03, 0x10, 0x1a,
			0x0d, 0x12, 0x06, 0x00, 0x11, 0x04, 0x21, 0x43, 0x65, 0x87, 0x09, 0x21, 0x43, 0x65,
			0x0a, 0x12, 0x07, 0x00, 0x12, 0x04, 0x89, 0x67, 0x45, 0x23, 0x01,
			0x04, 0xde, 0xad, 0xbe, 0xef,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseUDT(b)
		},
	},
	{
		description: "UDT-2Bytes-PartyAddress",
		structured: sccp.NewUDT(
			1,    // Protocol Class
			true, // Message handling
			params.NewCalledPartyAddress(0x42, 0, 6, nil),
			params.NewCallingPartyAddress(0x42, 0, 7, nil),
			nil,
		),
		serialized: []byte{
			0x09, 0x81, 0x03, 0x05, 0x07, 0x02, 0x42, 0x06, 0x02, 0x42, 0x07, 0x00,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseUDT(b)
		},
	},
	{
		description: "UDTS",
		structured: sccp.NewUDTS(
			params.ReturnCauseSubsystemFailure,
			params.NewCalledPartyAddress(0x42, 0, 6, nil),
			params.NewCallingPartyAddress(0x42, 0, 7, nil),
			[]byte{0xaa, 0xbb},
		),
		serialized: []byte{
			0x0a, 0x03,
			0x03, 0x05, 0x07,
			0x02, 0x42, 0x06,
			0x02, 0x42, 0x07,
			0x02, 0xaa, 0xbb,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseUDTS(b)
		},
	},
	{
		description: "ED",
		structured:  sccp.NewED(0x010203, []byte{0xaa, 0xbb}),
		serialized:  []byte{0x0b, 0x01, 0x02, 0x03, 0x01, 0x02, 0xaa, 0xbb},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseED(b)
		},
	},
	{
		description: "EA",
		structured:  sccp.NewEA(0x010203),
		serialized:  []byte{0x0c, 0x01, 0x02, 0x03},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseEA(b)
		},
	},
	{
		description: "RSR",
		structured:  sccp.NewRSR(0x010203, 0x040506, params.ResetCauseMessageOutOfOrderIncorrectReceiveSequenceNumber),
		serialized:  []byte{0x0d, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x03, 0x00},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseRSR(b)
		},
	},
	{
		description: "RSC",
		structured:  sccp.NewRSC(0x010203, 0x040506),
		serialized:  []byte{0x0e, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseRSC(b)
		},
	},
	{
		description: "ERR",
		structured:  sccp.NewERR(0x010203, params.ErrorCauseServiceClassMismatch),
		serialized:  []byte{0x0f, 0x01, 0x02, 0x03, 0x03, 0x00},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseERR(b)
		},
	},
	{
		description: "IT",
		structured:  sccp.NewIT(0x010203, 0x040506, 2, false, 0x76, 0x78, true, 5),
		serialized:  []byte{0x10, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x02, 0x76, 0x79, 0x05},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseIT(b)
		},
	},
	{
		description: "XUDT/No optionals",
		structured: sccp.NewXUDT(
			1,    // Protocol Class
			true, // Message handling
			2,    // Hop Counter
			params.NewCalledPartyAddress(
				params.NewAddressIndicator(false, true, false, params.GTITTNPESNAI),
				0, 6, // SPC, SSN
				params.NewGlobalTitle(
					params.GTITTNPESNAI,
					params.TranslationType(0),
					params.NPISDNTelephony,
					params.ESBCDOdd,
					params.NAIInternationalNumber,
					[]byte{0x21, 0x43, 0x65, 0x87, 0x09, 0x21, 0x43, 0x65},
				),
			),
			params.NewCallingPartyAddress(
				params.NewAddressIndicator(false, true, false, params.GTITTNPESNAI),
				0, 7, // SPC, SSN
				params.NewGlobalTitle(
					params.GTITTNPESNAI,
					params.TranslationType(0),
					params.NPISDNTelephony,
					params.ESBCDEven,
					params.NAIInternationalNumber,
					[]byte{0x89, 0x67, 0x45, 0x23, 0x01},
				),
			),
			[]byte{0xde, 0xad, 0xbe, 0xef},
		),
		serialized: []byte{
			0x11,                   // MsgType
			0x81,                   // Protocol Class
			0x02,                   // Hop Counter
			0x04, 0x11, 0x1b, 0x00, // Pointers
			0x0d, 0x12, 0x06, 0x00, 0x11, 0x04, 0x21, 0x43, 0x65, 0x87, 0x09, 0x21, 0x43, 0x65, // CdPA
			0x0a, 0x12, 0x07, 0x00, 0x12, 0x04, 0x89, 0x67, 0x45, 0x23, 0x01, // CgPA
			0x04, 0xde, 0xad, 0xbe, 0xef, // Data
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseXUDT(b)
		},
	},
	{
		description: "XUDT/with optionals",
		structured: sccp.NewXUDT(
			1,    // Protocol Class
			true, // Message handling
			2,    // Hop Counter
			params.NewCalledPartyAddress(
				params.NewAddressIndicator(false, true, false, params.GTITTNPESNAI),
				0, 6, // SPC, SSN
				params.NewGlobalTitle(
					params.GTITTNPESNAI,
					params.TranslationType(0),
					params.NPISDNTelephony,
					params.ESBCDOdd,
					params.NAIInternationalNumber,
					[]byte{0x21, 0x43, 0x65, 0x87, 0x09, 0x21, 0x43, 0x65},
				),
			),
			params.NewCallingPartyAddress(
				params.NewAddressIndicator(false, true, false, params.GTITTNPESNAI),
				0, 7, // SPC, SSN
				params.NewGlobalTitle(
					params.GTITTNPESNAI,
					params.TranslationType(0),
					params.NPISDNTelephony,
					params.ESBCDEven,
					params.NAIInternationalNumber,
					[]byte{0x89, 0x67, 0x45, 0x23, 0x01},
				),
			),
			[]byte{0xde, 0xad, 0xbe, 0xef},
			params.NewSegmentation(true, 1, 2, 0xffffff),
			params.NewImportance(2),
		),
		serialized: []byte{
			0x11,                   // MsgType
			0x81,                   // Protocol Class
			0x02,                   // Hop Counter
			0x04, 0x11, 0x1b, 0x1f, // Pointers
			0x0d, 0x12, 0x06, 0x00, 0x11, 0x04, 0x21, 0x43, 0x65, 0x87, 0x09, 0x21, 0x43, 0x65, // CdPA
			0x0a, 0x12, 0x07, 0x00, 0x12, 0x04, 0x89, 0x67, 0x45, 0x23, 0x01, // CgPA
			0x04, 0xde, 0xad, 0xbe, 0xef, // Data
			0x10, 0x04, 0xc2, 0xff, 0xff, 0xff, // Segmentation
			0x12, 0x01, 0x02, // Importance
			0x00, // End of optional parameters
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseXUDT(b)
		},
	},
	{
		description: "XUDTS/No optionals",
		structured: sccp.NewXUDTS(
			params.ReturnCauseSubsystemFailure,
			2,
			params.NewCalledPartyAddress(0x42, 0, 6, nil),
			params.NewCallingPartyAddress(0x42, 0, 7, nil),
			[]byte{0xaa, 0xbb},
		),
		serialized: []byte{
			0x12, 0x03, 0x02,
			0x04, 0x06, 0x08, 0x00,
			0x02, 0x42, 0x06,
			0x02, 0x42, 0x07,
			0x02, 0xaa, 0xbb,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseXUDTS(b)
		},
	},
	{
		description: "XUDTS/with optionals",
		structured: sccp.NewXUDTS(
			params.ReturnCauseSubsystemFailure,
			2,
			params.NewCalledPartyAddress(0x42, 0, 6, nil),
			params.NewCallingPartyAddress(0x42, 0, 7, nil),
			[]byte{0xaa, 0xbb},
			params.NewSegmentation(true, 1, 2, 0x123456),
			params.NewImportance(2),
		),
		serialized: []byte{
			0x12, 0x03, 0x02,
			0x04, 0x06, 0x08, 0x0a,
			0x02, 0x42, 0x06,
			0x02, 0x42, 0x07,
			0x02, 0xaa, 0xbb,
			0x10, 0x04, 0xc2, 0x12, 0x34, 0x56,
			0x12, 0x01, 0x02,
			0x00,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseXUDTS(b)
		},
	},
	{
		description: "LUDT/No optionals",
		structured: sccp.NewLUDT(
			1,
			true,
			2,
			params.NewCalledPartyAddress(0x42, 0, 6, nil),
			params.NewCallingPartyAddress(0x42, 0, 7, nil),
			[]byte{0xaa, 0xbb, 0xcc},
		),
		serialized: []byte{
			0x13, 0x81, 0x02,
			0x00, 0x08, 0x00, 0x09, 0x00, 0x0a, 0x00, 0x00,
			0x02, 0x42, 0x06,
			0x02, 0x42, 0x07,
			0x00, 0x03, 0xaa, 0xbb, 0xcc,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseLUDT(b)
		},
	},
	{
		description: "LUDT/with optionals",
		structured: sccp.NewLUDT(
			1,
			true,
			2,
			params.NewCalledPartyAddress(0x42, 0, 6, nil),
			params.NewCallingPartyAddress(0x42, 0, 7, nil),
			[]byte{0xaa, 0xbb, 0xcc},
			params.NewSegmentation(true, 1, 2, 0x123456),
			params.NewImportance(2),
		),
		serialized: []byte{
			0x13, 0x81, 0x02,
			0x00, 0x08, 0x00, 0x09, 0x00, 0x0a, 0x00, 0x0d,
			0x02, 0x42, 0x06,
			0x02, 0x42, 0x07,
			0x00, 0x03, 0xaa, 0xbb, 0xcc,
			0x10, 0x04, 0xc2, 0x12, 0x34, 0x56,
			0x12, 0x01, 0x02,
			0x00,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseLUDT(b)
		},
	},
	{
		description: "LUDTS/No optionals",
		structured: sccp.NewLUDTS(
			params.ReturnCauseSubsystemFailure,
			2,
			params.NewCalledPartyAddress(0x42, 0, 6, nil),
			params.NewCallingPartyAddress(0x42, 0, 7, nil),
			[]byte{0xaa, 0xbb, 0xcc},
		),
		serialized: []byte{
			0x14, 0x03, 0x02,
			0x00, 0x08, 0x00, 0x09, 0x00, 0x0a, 0x00, 0x00,
			0x02, 0x42, 0x06,
			0x02, 0x42, 0x07,
			0x00, 0x03, 0xaa, 0xbb, 0xcc,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseLUDTS(b)
		},
	},
	{
		description: "LUDTS/with optionals",
		structured: sccp.NewLUDTS(
			params.ReturnCauseSubsystemFailure,
			2,
			params.NewCalledPartyAddress(0x42, 0, 6, nil),
			params.NewCallingPartyAddress(0x42, 0, 7, nil),
			[]byte{0xaa, 0xbb, 0xcc},
			params.NewSegmentation(true, 1, 2, 0x123456),
			params.NewImportance(2),
		),
		serialized: []byte{
			0x14, 0x03, 0x02,
			0x00, 0x08, 0x00, 0x09, 0x00, 0x0a, 0x00, 0x0d,
			0x02, 0x42, 0x06,
			0x02, 0x42, 0x07,
			0x00, 0x03, 0xaa, 0xbb, 0xcc,
			0x10, 0x04, 0xc2, 0x12, 0x34, 0x56,
			0x12, 0x01, 0x02,
			0x00,
		},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseLUDTS(b)
		},
	},
	{
		description: "SCMG SSA",
		structured:  sccp.NewSCMG(sccp.SCMGTypeSSA, 9, 405, 0, 0),
		serialized:  []byte{0x1, 0x09, 0x95, 0x01, 0x00},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseSCMG(b)
		},
	},
	{
		description: "SCMG SSC",
		structured:  sccp.NewSCMG(sccp.SCMGTypeSSC, 9, 405, 0, 4),
		serialized:  []byte{0x6, 0x09, 0x95, 0x01, 0x00, 0x04},
		parseFunc: func(b []byte) (serializable, error) {
			return sccp.ParseSCMG(b)
		},
	},
}

func TestMessages(t *testing.T) {
	t.Helper()

	for _, c := range testcases {
		t.Run(c.description, func(t *testing.T) {
			t.Run("Decode", func(t *testing.T) {
				msg, err := c.parseFunc(c.serialized)
				if err != nil {
					t.Fatal(err)
				}

				if got, want := msg, c.structured; !verify.Values(t, "", got, want) {
					t.Fail()
				}
			})

			t.Run("Serialize", func(t *testing.T) {
				b, err := c.structured.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}

				if got, want := b, c.serialized; !verify.Values(t, "", got, want) {
					t.Fail()
				}
			})

			t.Run("Len", func(t *testing.T) {
				if got, want := c.structured.MarshalLen(), len(c.serialized); got != want {
					t.Fatalf("got %v want %v", got, want)
				}
			})

			t.Run("Interface", func(t *testing.T) {
				if _, ok := c.structured.(*sccp.SCMG); ok {
					return
				}

				decoded, err := sccp.ParseMessage(c.serialized)
				if err != nil {
					t.Fatal(err)
				}

				if got, want := decoded.MessageType(), c.structured.(sccp.Message).MessageType(); got != want {
					t.Fatalf("got %v want %v", got, want)
				}
				if got, want := decoded.MessageTypeName(), c.structured.(sccp.Message).MessageTypeName(); got != want {
					t.Fatalf("got %v want %v", got, want)
				}
			})
		})
	}
}

func TestPartialStructuredMessages(t *testing.T) {
	for _, c := range testcases {
		if strings.Contains(c.description, "SCMG") {
			continue
		}
		for i := range c.serialized {
			partial := c.serialized[:i]
			_, err := c.parseFunc(partial)
			if err != io.ErrUnexpectedEOF {
				t.Errorf("parse %v / %#x: got error %v, want unexpected EOF", c.description, partial, err)
			}
		}

		for i := range c.serialized {
			if i == len(c.serialized) {
				continue
			}
			b := make([]byte, i)
			if err := c.structured.MarshalTo(b); err != io.ErrUnexpectedEOF {
				t.Errorf("marshal %v / %#x: got error %v, want unexpected EOF", c.description, b, err)
			}
		}
	}
}

func TestMalformedXUDTShortPointerTableReturnsUnexpectedEOF(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("parse panicked: %v", r)
		}
	}()

	_, err := sccp.ParseMessage([]byte{0x11, 0x30, 0x30, 0x00, 0x00, 0x00})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got error %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

func TestLongUnitdataUsesTwoOctetPointers(t *testing.T) {
	data := make([]byte, 260)
	for i := range data {
		data[i] = byte(i)
	}

	msg := sccp.NewLUDT(
		1,
		true,
		2,
		params.NewCalledPartyAddress(0x42, 0, 6, nil),
		params.NewCallingPartyAddress(0x42, 0, 7, nil),
		data,
		params.NewImportance(7),
	)

	b, err := msg.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	if got, want := b[3:11], []byte{0x00, 0x08, 0x00, 0x09, 0x00, 0x0a, 0x01, 0x0e}; !verify.Values(t, "", got, want) {
		t.Fail()
	}

	decoded, err := sccp.ParseMessage(b)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := decoded.(*sccp.LUDT).LongData.Value(), data; !verify.Values(t, "", got, want) {
		t.Fail()
	}
}

func FuzzParseMessageNoPanic(f *testing.F) {
	f.Add([]byte{})
	for _, c := range testcases {
		if strings.Contains(c.description, "SCMG") {
			continue
		}
		f.Add(c.serialized)
	}

	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = sccp.ParseMessage(b)
	})
}
