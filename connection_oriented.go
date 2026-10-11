// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"fmt"

	"github.com/gomaja/go-sccp/params"
)

const (
	// maxConnectionData is the largest data parameter of CR, CC, CREF and
	// RLSD: "Data 3.16 O 3-130", ITU-T Q.713 (03/01) Tables 3 to 6.
	maxConnectionData = 128

	// maxExpeditedData is the largest data parameter of ED: "Data 3.16 V
	// 2-33", ITU-T Q.713 (03/01) Table 13.
	maxExpeditedData = 32
)

// CR is the connection request message, ITU-T Q.713 (03/01) §4.2, Table 3.
type CR struct {
	SourceLocalReference params.LocalReference
	// ProtocolClass is class 2 or 3.
	ProtocolClass      params.ProtocolClass
	CalledPartyAddress params.PartyAddress

	Credit              *params.Credit
	CallingPartyAddress *params.PartyAddress
	// Data is optional, 1 to 128 octets; nil or empty means absent.
	Data         []byte
	HopCounter   *params.HopCounter
	Importance   *params.Importance
	Unrecognized []params.RawParameter
}

// MessageType returns MsgTypeCR.
func (*CR) MessageType() MsgType { return MsgTypeCR }

// MarshalBinary encodes the message; see Message.
func (m *CR) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *CR) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

var crCodes = []params.ParameterNameCode{params.PCodeCredit, params.PCodeCallingPartyAddress, params.PCodeData, params.PCodeHopCounter, params.PCodeImportance}

func (m *CR) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeCR)
	e.fixed("source local reference", m.SourceLocalReference)
	e.connectionOrientedClass(m.ProtocolClass)
	e.pointers(2, 1, true)
	e.address(0, "called party address", m.CalledPartyAddress, false)
	if m.Credit != nil {
		e.optional(params.PCodeCredit, "credit", *m.Credit)
	}
	if m.CallingPartyAddress != nil {
		e.optionalAddress(params.PCodeCallingPartyAddress, "calling party address", *m.CallingPartyAddress)
	}
	e.optionalData("data", m.Data, maxConnectionData)
	if m.HopCounter != nil {
		e.optional(params.PCodeHopCounter, "hop counter", *m.HopCounter)
	}
	if m.Importance != nil {
		e.optional(params.PCodeImportance, "importance", *m.Importance)
	}
	e.raws(m.Unrecognized, crCodes)
	return e.finish()
}

// UnmarshalBinary decodes a CR; see Message.
func (m *CR) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeCR)
	if err != nil {
		return err
	}
	var v CR
	if err := d.fixedParam("source local reference", 3, &v.SourceLocalReference); err != nil {
		return err
	}
	if err := d.fixedParam("protocol class", 1, &v.ProtocolClass); err != nil {
		return err
	}
	if err := d.pointers(2, 1); err != nil {
		return err
	}
	if err := d.variableParam(0, "called party address", &v.CalledPartyAddress); err != nil {
		return err
	}
	v.Unrecognized, err = d.optional(1, []optSpec{
		{params.PCodeCredit, "credit", optValue(&v.Credit)},
		{params.PCodeCallingPartyAddress, "calling party address", optValue(&v.CallingPartyAddress)},
		{params.PCodeData, "data", optBytes(&v.Data)},
		{params.PCodeHopCounter, "hop counter", optValue(&v.HopCounter)},
		{params.PCodeImportance, "importance", optValue(&v.Importance)},
	})
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *CR) String() string {
	return fmt.Sprintf("CR{SourceLocalReference: %#06x, ProtocolClass: %s, CalledPartyAddress: %s%s}",
		uint32(m.SourceLocalReference), m.ProtocolClass, m.CalledPartyAddress,
		connectionOptionalString(m.Credit, m.CallingPartyAddress, nil, m.Data, m.HopCounter, m.Importance, m.Unrecognized))
}

// CC is the connection confirm message, ITU-T Q.713 (03/01) §4.3, Table 4.
type CC struct {
	DestinationLocalReference params.LocalReference
	SourceLocalReference      params.LocalReference
	// ProtocolClass is class 2 or 3.
	ProtocolClass params.ProtocolClass

	Credit             *params.Credit
	CalledPartyAddress *params.PartyAddress
	// Data is optional, 1 to 128 octets; nil or empty means absent.
	Data         []byte
	Importance   *params.Importance
	Unrecognized []params.RawParameter
}

// MessageType returns MsgTypeCC.
func (*CC) MessageType() MsgType { return MsgTypeCC }

// MarshalBinary encodes the message; see Message.
func (m *CC) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *CC) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

var ccCodes = []params.ParameterNameCode{params.PCodeCredit, params.PCodeCalledPartyAddress, params.PCodeData, params.PCodeImportance}

func (m *CC) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeCC)
	e.fixed("destination local reference", m.DestinationLocalReference)
	e.fixed("source local reference", m.SourceLocalReference)
	e.connectionOrientedClass(m.ProtocolClass)
	e.pointers(1, 1, true)
	if m.Credit != nil {
		e.optional(params.PCodeCredit, "credit", *m.Credit)
	}
	if m.CalledPartyAddress != nil {
		e.optionalAddress(params.PCodeCalledPartyAddress, "called party address", *m.CalledPartyAddress)
	}
	e.optionalData("data", m.Data, maxConnectionData)
	if m.Importance != nil {
		e.optional(params.PCodeImportance, "importance", *m.Importance)
	}
	e.raws(m.Unrecognized, ccCodes)
	return e.finish()
}

// UnmarshalBinary decodes a CC; see Message.
func (m *CC) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeCC)
	if err != nil {
		return err
	}
	var v CC
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	if err := d.fixedParam("source local reference", 3, &v.SourceLocalReference); err != nil {
		return err
	}
	if err := d.fixedParam("protocol class", 1, &v.ProtocolClass); err != nil {
		return err
	}
	if err := d.pointers(1, 1); err != nil {
		return err
	}
	v.Unrecognized, err = d.optional(0, []optSpec{
		{params.PCodeCredit, "credit", optValue(&v.Credit)},
		{params.PCodeCalledPartyAddress, "called party address", optValue(&v.CalledPartyAddress)},
		{params.PCodeData, "data", optBytes(&v.Data)},
		{params.PCodeImportance, "importance", optValue(&v.Importance)},
	})
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *CC) String() string {
	return fmt.Sprintf("CC{DestinationLocalReference: %#06x, SourceLocalReference: %#06x, ProtocolClass: %s%s}",
		uint32(m.DestinationLocalReference), uint32(m.SourceLocalReference), m.ProtocolClass,
		connectionOptionalString(m.Credit, nil, m.CalledPartyAddress, m.Data, nil, m.Importance, m.Unrecognized))
}

// CREF is the connection refused message, ITU-T Q.713 (03/01) §4.4, Table 5.
type CREF struct {
	DestinationLocalReference params.LocalReference
	RefusalCause              params.RefusalCause

	CalledPartyAddress *params.PartyAddress
	// Data is optional, 1 to 128 octets; nil or empty means absent.
	Data         []byte
	Importance   *params.Importance
	Unrecognized []params.RawParameter
}

// MessageType returns MsgTypeCREF.
func (*CREF) MessageType() MsgType { return MsgTypeCREF }

// MarshalBinary encodes the message; see Message.
func (m *CREF) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *CREF) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

var crefCodes = []params.ParameterNameCode{params.PCodeCalledPartyAddress, params.PCodeData, params.PCodeImportance}

func (m *CREF) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeCREF)
	e.fixed("destination local reference", m.DestinationLocalReference)
	e.cause("refusal cause", byte(m.RefusalCause))
	e.pointers(1, 1, true)
	if m.CalledPartyAddress != nil {
		e.optionalAddress(params.PCodeCalledPartyAddress, "called party address", *m.CalledPartyAddress)
	}
	e.optionalData("data", m.Data, maxConnectionData)
	if m.Importance != nil {
		e.optional(params.PCodeImportance, "importance", *m.Importance)
	}
	e.raws(m.Unrecognized, crefCodes)
	return e.finish()
}

// UnmarshalBinary decodes a CREF; see Message.
func (m *CREF) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeCREF)
	if err != nil {
		return err
	}
	var v CREF
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	cause, err := d.fixed("refusal cause", 1)
	if err != nil {
		return err
	}
	v.RefusalCause = params.RefusalCause(cause[0])
	if err := d.pointers(1, 1); err != nil {
		return err
	}
	v.Unrecognized, err = d.optional(0, []optSpec{
		{params.PCodeCalledPartyAddress, "called party address", optValue(&v.CalledPartyAddress)},
		{params.PCodeData, "data", optBytes(&v.Data)},
		{params.PCodeImportance, "importance", optValue(&v.Importance)},
	})
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *CREF) String() string {
	return fmt.Sprintf("CREF{DestinationLocalReference: %#06x, RefusalCause: %s%s}",
		uint32(m.DestinationLocalReference), m.RefusalCause,
		connectionOptionalString(nil, nil, m.CalledPartyAddress, m.Data, nil, m.Importance, m.Unrecognized))
}

// RLSD is the released message, ITU-T Q.713 (03/01) §4.5, Table 6.
type RLSD struct {
	DestinationLocalReference params.LocalReference
	SourceLocalReference      params.LocalReference
	ReleaseCause              params.ReleaseCause

	// Data is optional, 1 to 128 octets; nil or empty means absent.
	Data         []byte
	Importance   *params.Importance
	Unrecognized []params.RawParameter
}

// MessageType returns MsgTypeRLSD.
func (*RLSD) MessageType() MsgType { return MsgTypeRLSD }

// MarshalBinary encodes the message; see Message.
func (m *RLSD) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *RLSD) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

var rlsdCodes = []params.ParameterNameCode{params.PCodeData, params.PCodeImportance}

func (m *RLSD) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeRLSD)
	e.fixed("destination local reference", m.DestinationLocalReference)
	e.fixed("source local reference", m.SourceLocalReference)
	e.cause("release cause", byte(m.ReleaseCause))
	e.pointers(1, 1, true)
	e.optionalData("data", m.Data, maxConnectionData)
	if m.Importance != nil {
		e.optional(params.PCodeImportance, "importance", *m.Importance)
	}
	e.raws(m.Unrecognized, rlsdCodes)
	return e.finish()
}

// UnmarshalBinary decodes an RLSD; see Message.
func (m *RLSD) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeRLSD)
	if err != nil {
		return err
	}
	var v RLSD
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	if err := d.fixedParam("source local reference", 3, &v.SourceLocalReference); err != nil {
		return err
	}
	cause, err := d.fixed("release cause", 1)
	if err != nil {
		return err
	}
	v.ReleaseCause = params.ReleaseCause(cause[0])
	if err := d.pointers(1, 1); err != nil {
		return err
	}
	v.Unrecognized, err = d.optional(0, []optSpec{
		{params.PCodeData, "data", optBytes(&v.Data)},
		{params.PCodeImportance, "importance", optValue(&v.Importance)},
	})
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *RLSD) String() string {
	return fmt.Sprintf("RLSD{DestinationLocalReference: %#06x, SourceLocalReference: %#06x, ReleaseCause: %s%s}",
		uint32(m.DestinationLocalReference), uint32(m.SourceLocalReference), m.ReleaseCause,
		connectionOptionalString(nil, nil, nil, m.Data, nil, m.Importance, m.Unrecognized))
}

// RLC is the release complete message, ITU-T Q.713 (03/01) §4.6, Table 7.
type RLC struct {
	DestinationLocalReference params.LocalReference
	SourceLocalReference      params.LocalReference
}

// MessageType returns MsgTypeRLC.
func (*RLC) MessageType() MsgType { return MsgTypeRLC }

// MarshalBinary encodes the message; see Message.
func (m *RLC) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *RLC) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *RLC) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeRLC)
	e.fixed("destination local reference", m.DestinationLocalReference)
	e.fixed("source local reference", m.SourceLocalReference)
	return e.finish()
}

// UnmarshalBinary decodes an RLC; see Message.
func (m *RLC) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeRLC)
	if err != nil {
		return err
	}
	var v RLC
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	if err := d.fixedParam("source local reference", 3, &v.SourceLocalReference); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *RLC) String() string {
	return fmt.Sprintf("RLC{DestinationLocalReference: %#06x, SourceLocalReference: %#06x}",
		uint32(m.DestinationLocalReference), uint32(m.SourceLocalReference))
}

// DT1 is the data form 1 message, ITU-T Q.713 (03/01) §4.7, Table 8.
type DT1 struct {
	DestinationLocalReference params.LocalReference
	// MoreData is the M bit of the segmenting/reassembling parameter (§3.7).
	MoreData bool
	// Data is 1 to 255 octets (§3.16).
	Data []byte
}

// MessageType returns MsgTypeDT1.
func (*DT1) MessageType() MsgType { return MsgTypeDT1 }

// MarshalBinary encodes the message; see Message.
func (m *DT1) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *DT1) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *DT1) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeDT1)
	e.fixed("destination local reference", m.DestinationLocalReference)
	e.octet(moreDataOctet(m.MoreData))
	e.pointers(1, 1, false)
	e.data(0, "data", m.Data, maxData)
	return e.finish()
}

// UnmarshalBinary decodes a DT1; see Message. Bits 8-2 of the
// segmenting/reassembling parameter are spare.
func (m *DT1) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeDT1)
	if err != nil {
		return err
	}
	var v DT1
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	sr, err := d.fixed("segmenting/reassembling", 1)
	if err != nil {
		return err
	}
	v.MoreData = sr[0]&1 != 0
	if err := d.pointers(1, 1); err != nil {
		return err
	}
	if v.Data, err = d.variableData(0, "data"); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *DT1) String() string {
	return fmt.Sprintf("DT1{DestinationLocalReference: %#06x, MoreData: %t, Data: %x}",
		uint32(m.DestinationLocalReference), m.MoreData, m.Data)
}

// DT2 is the data form 2 message, ITU-T Q.713 (03/01) §4.8, Table 9.
type DT2 struct {
	DestinationLocalReference params.LocalReference
	SequencingSegmenting      params.SequencingSegmenting
	// Data is 1 to 255 octets (§3.16).
	Data []byte
}

// MessageType returns MsgTypeDT2.
func (*DT2) MessageType() MsgType { return MsgTypeDT2 }

// MarshalBinary encodes the message; see Message.
func (m *DT2) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *DT2) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *DT2) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeDT2)
	e.fixed("destination local reference", m.DestinationLocalReference)
	e.fixed("sequencing/segmenting", m.SequencingSegmenting)
	e.pointers(1, 1, false)
	e.data(0, "data", m.Data, maxData)
	return e.finish()
}

// UnmarshalBinary decodes a DT2; see Message.
func (m *DT2) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeDT2)
	if err != nil {
		return err
	}
	var v DT2
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	if err := d.fixedParam("sequencing/segmenting", 2, &v.SequencingSegmenting); err != nil {
		return err
	}
	if err := d.pointers(1, 1); err != nil {
		return err
	}
	if v.Data, err = d.variableData(0, "data"); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *DT2) String() string {
	return fmt.Sprintf("DT2{DestinationLocalReference: %#06x, SequencingSegmenting: %s, Data: %x}",
		uint32(m.DestinationLocalReference), m.SequencingSegmenting, m.Data)
}

// AK is the data acknowledgement message, ITU-T Q.713 (03/01) §4.9, Table 10.
type AK struct {
	DestinationLocalReference params.LocalReference
	// ReceiveSequenceNumber is P(R), 0 to 127, in bits 8-2 of the receive
	// sequence number parameter (§3.8).
	ReceiveSequenceNumber uint8
	Credit                params.Credit
}

// MessageType returns MsgTypeAK.
func (*AK) MessageType() MsgType { return MsgTypeAK }

// MarshalBinary encodes the message; see Message.
func (m *AK) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *AK) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *AK) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeAK)
	e.fixed("destination local reference", m.DestinationLocalReference)
	if m.ReceiveSequenceNumber > params.MaxSequenceNumber {
		e.invalid("receive sequence number %d exceeds %d", m.ReceiveSequenceNumber, params.MaxSequenceNumber)
	}
	e.octet(m.ReceiveSequenceNumber << 1)
	e.fixed("credit", m.Credit)
	return e.finish()
}

// UnmarshalBinary decodes an AK; see Message. Bit 1 of the receive sequence
// number parameter is spare.
func (m *AK) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeAK)
	if err != nil {
		return err
	}
	var v AK
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	pr, err := d.fixed("receive sequence number", 1)
	if err != nil {
		return err
	}
	v.ReceiveSequenceNumber = pr[0] >> 1
	if err := d.fixedParam("credit", 1, &v.Credit); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *AK) String() string {
	return fmt.Sprintf("AK{DestinationLocalReference: %#06x, P(R): %d, Credit: %d}",
		uint32(m.DestinationLocalReference), m.ReceiveSequenceNumber, m.Credit)
}

// ED is the expedited data message, ITU-T Q.713 (03/01) §4.12, Table 13.
type ED struct {
	DestinationLocalReference params.LocalReference
	// Data is 1 to 32 octets.
	Data []byte
}

// MessageType returns MsgTypeED.
func (*ED) MessageType() MsgType { return MsgTypeED }

// MarshalBinary encodes the message; see Message.
func (m *ED) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *ED) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *ED) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeED)
	e.fixed("destination local reference", m.DestinationLocalReference)
	e.pointers(1, 1, false)
	e.data(0, "data", m.Data, maxExpeditedData)
	return e.finish()
}

// UnmarshalBinary decodes an ED; see Message.
func (m *ED) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeED)
	if err != nil {
		return err
	}
	var v ED
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	if err := d.pointers(1, 1); err != nil {
		return err
	}
	if v.Data, err = d.variableData(0, "data"); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *ED) String() string {
	return fmt.Sprintf("ED{DestinationLocalReference: %#06x, Data: %x}", uint32(m.DestinationLocalReference), m.Data)
}

// EA is the expedited data acknowledgement message, ITU-T Q.713 (03/01) §4.13,
// Table 14.
type EA struct {
	DestinationLocalReference params.LocalReference
}

// MessageType returns MsgTypeEA.
func (*EA) MessageType() MsgType { return MsgTypeEA }

// MarshalBinary encodes the message; see Message.
func (m *EA) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *EA) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *EA) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeEA)
	e.fixed("destination local reference", m.DestinationLocalReference)
	return e.finish()
}

// UnmarshalBinary decodes an EA; see Message.
func (m *EA) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeEA)
	if err != nil {
		return err
	}
	var v EA
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *EA) String() string {
	return fmt.Sprintf("EA{DestinationLocalReference: %#06x}", uint32(m.DestinationLocalReference))
}

// RSR is the reset request message, ITU-T Q.713 (03/01) §4.14, Table 15. Its
// pointer to the optional part allows for optional parameters in the future;
// none is defined, so any present is Unrecognized.
type RSR struct {
	DestinationLocalReference params.LocalReference
	SourceLocalReference      params.LocalReference
	ResetCause                params.ResetCause
	Unrecognized              []params.RawParameter
}

// MessageType returns MsgTypeRSR.
func (*RSR) MessageType() MsgType { return MsgTypeRSR }

// MarshalBinary encodes the message; see Message.
func (m *RSR) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *RSR) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *RSR) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeRSR)
	e.fixed("destination local reference", m.DestinationLocalReference)
	e.fixed("source local reference", m.SourceLocalReference)
	e.cause("reset cause", byte(m.ResetCause))
	e.pointers(1, 1, true)
	e.raws(m.Unrecognized, nil)
	return e.finish()
}

// UnmarshalBinary decodes an RSR; see Message.
func (m *RSR) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeRSR)
	if err != nil {
		return err
	}
	var v RSR
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	if err := d.fixedParam("source local reference", 3, &v.SourceLocalReference); err != nil {
		return err
	}
	cause, err := d.fixed("reset cause", 1)
	if err != nil {
		return err
	}
	v.ResetCause = params.ResetCause(cause[0])
	if err := d.pointers(1, 1); err != nil {
		return err
	}
	if v.Unrecognized, err = d.optional(0, nil); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *RSR) String() string {
	return fmt.Sprintf("RSR{DestinationLocalReference: %#06x, SourceLocalReference: %#06x, ResetCause: %s%s}",
		uint32(m.DestinationLocalReference), uint32(m.SourceLocalReference), m.ResetCause,
		connectionOptionalString(nil, nil, nil, nil, nil, nil, m.Unrecognized))
}

// RSC is the reset confirmation message, ITU-T Q.713 (03/01) §4.15, Table 16.
type RSC struct {
	DestinationLocalReference params.LocalReference
	SourceLocalReference      params.LocalReference
}

// MessageType returns MsgTypeRSC.
func (*RSC) MessageType() MsgType { return MsgTypeRSC }

// MarshalBinary encodes the message; see Message.
func (m *RSC) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *RSC) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *RSC) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeRSC)
	e.fixed("destination local reference", m.DestinationLocalReference)
	e.fixed("source local reference", m.SourceLocalReference)
	return e.finish()
}

// UnmarshalBinary decodes an RSC; see Message.
func (m *RSC) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeRSC)
	if err != nil {
		return err
	}
	var v RSC
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	if err := d.fixedParam("source local reference", 3, &v.SourceLocalReference); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *RSC) String() string {
	return fmt.Sprintf("RSC{DestinationLocalReference: %#06x, SourceLocalReference: %#06x}",
		uint32(m.DestinationLocalReference), uint32(m.SourceLocalReference))
}

// ERR is the protocol data unit error message, ITU-T Q.713 (03/01) §4.16,
// Table 17. Like RSR it has a pointer to an optional part with no defined
// parameters.
type ERR struct {
	DestinationLocalReference params.LocalReference
	ErrorCause                params.ErrorCause
	Unrecognized              []params.RawParameter
}

// MessageType returns MsgTypeERR.
func (*ERR) MessageType() MsgType { return MsgTypeERR }

// MarshalBinary encodes the message; see Message.
func (m *ERR) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *ERR) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *ERR) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeERR)
	e.fixed("destination local reference", m.DestinationLocalReference)
	e.cause("error cause", byte(m.ErrorCause))
	e.pointers(1, 1, true)
	e.raws(m.Unrecognized, nil)
	return e.finish()
}

// UnmarshalBinary decodes an ERR; see Message.
func (m *ERR) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeERR)
	if err != nil {
		return err
	}
	var v ERR
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	cause, err := d.fixed("error cause", 1)
	if err != nil {
		return err
	}
	v.ErrorCause = params.ErrorCause(cause[0])
	if err := d.pointers(1, 1); err != nil {
		return err
	}
	if v.Unrecognized, err = d.optional(0, nil); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *ERR) String() string {
	return fmt.Sprintf("ERR{DestinationLocalReference: %#06x, ErrorCause: %s%s}",
		uint32(m.DestinationLocalReference), m.ErrorCause,
		connectionOptionalString(nil, nil, nil, nil, nil, nil, m.Unrecognized))
}

// IT is the inactivity test message, ITU-T Q.713 (03/01) §4.17, Table 18.
type IT struct {
	DestinationLocalReference params.LocalReference
	SourceLocalReference      params.LocalReference
	// ProtocolClass is class 2 or 3.
	ProtocolClass params.ProtocolClass
	// SequencingSegmenting and Credit reflect the last DT2 or AK sent; they
	// are ignored for protocol class 2 (Table 18 Note).
	SequencingSegmenting params.SequencingSegmenting
	Credit               params.Credit
}

// MessageType returns MsgTypeIT.
func (*IT) MessageType() MsgType { return MsgTypeIT }

// MarshalBinary encodes the message; see Message.
func (m *IT) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *IT) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *IT) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeIT)
	e.fixed("destination local reference", m.DestinationLocalReference)
	e.fixed("source local reference", m.SourceLocalReference)
	e.connectionOrientedClass(m.ProtocolClass)
	e.fixed("sequencing/segmenting", m.SequencingSegmenting)
	e.fixed("credit", m.Credit)
	return e.finish()
}

// UnmarshalBinary decodes an IT; see Message.
func (m *IT) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeIT)
	if err != nil {
		return err
	}
	var v IT
	if err := d.fixedParam("destination local reference", 3, &v.DestinationLocalReference); err != nil {
		return err
	}
	if err := d.fixedParam("source local reference", 3, &v.SourceLocalReference); err != nil {
		return err
	}
	if err := d.fixedParam("protocol class", 1, &v.ProtocolClass); err != nil {
		return err
	}
	if err := d.fixedParam("sequencing/segmenting", 2, &v.SequencingSegmenting); err != nil {
		return err
	}
	if err := d.fixedParam("credit", 1, &v.Credit); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *IT) String() string {
	return fmt.Sprintf("IT{DestinationLocalReference: %#06x, SourceLocalReference: %#06x, ProtocolClass: %s, SequencingSegmenting: %s, Credit: %d}",
		uint32(m.DestinationLocalReference), uint32(m.SourceLocalReference), m.ProtocolClass, m.SequencingSegmenting, m.Credit)
}

// connectionOrientedClass appends a protocol class that must be
// connection-oriented: CR, CC and IT exist for classes 2 and 3 only (ITU-T
// Q.713 (03/01) Table 1).
func (e *encoder) connectionOrientedClass(p params.ProtocolClass) {
	if p.Class < 2 {
		e.invalid("protocol class %d is not a connection-oriented class", p.Class)
		return
	}
	e.fixed("protocol class", p)
}

func moreDataOctet(more bool) byte {
	if more {
		return 1
	}
	return 0
}

func connectionOptionalString(credit *params.Credit, calling, called *params.PartyAddress, data []byte, hop *params.HopCounter, imp *params.Importance, raw []params.RawParameter) string {
	s := ""
	if credit != nil {
		s += fmt.Sprintf(", Credit: %d", *credit)
	}
	if calling != nil {
		s += fmt.Sprintf(", CallingPartyAddress: %s", *calling)
	}
	if called != nil {
		s += fmt.Sprintf(", CalledPartyAddress: %s", *called)
	}
	if len(data) > 0 {
		s += fmt.Sprintf(", Data: %x", data)
	}
	if hop != nil {
		s += fmt.Sprintf(", HopCounter: %d", *hop)
	}
	if imp != nil {
		s += fmt.Sprintf(", Importance: %d", *imp)
	}
	if len(raw) > 0 {
		s += fmt.Sprintf(", Unrecognized: %v", raw)
	}
	return s
}
