// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"fmt"

	"github.com/gomaja/go-sccp/params"
)

const (
	// maxData is the largest data parameter: ITU-T Q.713 (03/01) §3.16.
	maxData = 255

	// MaxLongData is the largest long data parameter, and the largest user
	// data the connectionless service carries: ITU-T Q.713 (03/01) §3.20 and
	// Q.714 (05/01) §4.1.1.1.1.
	MaxLongData = 3952
)

// UDT is the unitdata message, ITU-T Q.713 (03/01) §4.10, Table 11.
type UDT struct {
	// ProtocolClass is class 0 or 1.
	ProtocolClass       params.ProtocolClass
	CalledPartyAddress  params.PartyAddress
	CallingPartyAddress params.PartyAddress
	// Data is the SCCP-user data, 1 to 255 octets (§3.16).
	Data []byte
}

// MessageType returns MsgTypeUDT.
func (*UDT) MessageType() MsgType { return MsgTypeUDT }

// MarshalBinary encodes the message; see Message.
func (m *UDT) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *UDT) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *UDT) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeUDT)
	e.connectionlessClass(m.ProtocolClass)
	e.pointers(3, 1, false)
	e.address(0, "called party address", m.CalledPartyAddress, false)
	e.address(1, "calling party address", m.CallingPartyAddress, true)
	e.data(2, "data", m.Data, maxData)
	return e.finish()
}

// UnmarshalBinary decodes a UDT; see Message.
func (m *UDT) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeUDT)
	if err != nil {
		return err
	}
	var v UDT
	if err := d.fixedParam("protocol class", 1, &v.ProtocolClass); err != nil {
		return err
	}
	if err := d.pointers(3, 1); err != nil {
		return err
	}
	if err := d.variableParam(0, "called party address", &v.CalledPartyAddress); err != nil {
		return err
	}
	if err := d.variableParam(1, "calling party address", &v.CallingPartyAddress); err != nil {
		return err
	}
	if v.Data, err = d.variableData(2, "data"); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *UDT) String() string {
	return fmt.Sprintf("UDT{ProtocolClass: %s, CalledPartyAddress: %s, CallingPartyAddress: %s, Data: %x}",
		m.ProtocolClass, m.CalledPartyAddress, m.CallingPartyAddress, m.Data)
}

// UDTS is the unitdata service message, ITU-T Q.713 (03/01) §4.11, Table 12.
type UDTS struct {
	ReturnCause         params.ReturnCause
	CalledPartyAddress  params.PartyAddress
	CallingPartyAddress params.PartyAddress
	// Data is the returned SCCP-user data, 1 to 255 octets (§3.16).
	Data []byte
}

// MessageType returns MsgTypeUDTS.
func (*UDTS) MessageType() MsgType { return MsgTypeUDTS }

// MarshalBinary encodes the message; see Message.
func (m *UDTS) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *UDTS) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *UDTS) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeUDTS)
	e.cause("return cause", byte(m.ReturnCause))
	e.pointers(3, 1, false)
	e.address(0, "called party address", m.CalledPartyAddress, false)
	e.address(1, "calling party address", m.CallingPartyAddress, false)
	e.data(2, "data", m.Data, maxData)
	return e.finish()
}

// UnmarshalBinary decodes a UDTS; see Message.
func (m *UDTS) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeUDTS)
	if err != nil {
		return err
	}
	var v UDTS
	cause, err := d.fixed("return cause", 1)
	if err != nil {
		return err
	}
	v.ReturnCause = params.ReturnCause(cause[0])
	if err := d.pointers(3, 1); err != nil {
		return err
	}
	if err := d.variableParam(0, "called party address", &v.CalledPartyAddress); err != nil {
		return err
	}
	if err := d.variableParam(1, "calling party address", &v.CallingPartyAddress); err != nil {
		return err
	}
	if v.Data, err = d.variableData(2, "data"); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *UDTS) String() string {
	return fmt.Sprintf("UDTS{ReturnCause: %s, CalledPartyAddress: %s, CallingPartyAddress: %s, Data: %x}",
		m.ReturnCause, m.CalledPartyAddress, m.CallingPartyAddress, m.Data)
}

// XUDT is the extended unitdata message, ITU-T Q.713 (03/01) §4.18, Table 19.
type XUDT struct {
	// ProtocolClass is class 0 or 1; every segment of a segmented message is
	// class 1 (ITU-T Q.714 (05/01) §4.1.1.1.2).
	ProtocolClass       params.ProtocolClass
	HopCounter          params.HopCounter
	CalledPartyAddress  params.PartyAddress
	CallingPartyAddress params.PartyAddress
	// Data is the SCCP-user data, 1 to 255 octets (§3.16). How much fits
	// depends on the addresses and the optional parameters: Table 19 Note 1.
	Data []byte

	// Segmentation is present in the segments of a segmented message only
	// (Table 19 Note 2).
	Segmentation *params.Segmentation
	Importance   *params.Importance
	Unrecognized []params.RawParameter
}

// MessageType returns MsgTypeXUDT.
func (*XUDT) MessageType() MsgType { return MsgTypeXUDT }

// MarshalBinary encodes the message; see Message.
func (m *XUDT) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *XUDT) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *XUDT) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeXUDT)
	e.connectionlessClass(m.ProtocolClass)
	e.segmentClass(m.ProtocolClass, m.Segmentation)
	e.fixed("hop counter", m.HopCounter)
	e.pointers(4, 1, true)
	e.address(0, "called party address", m.CalledPartyAddress, false)
	e.address(1, "calling party address", m.CallingPartyAddress, true)
	e.data(2, "data", m.Data, maxData)
	e.segmentationImportance(m.Segmentation, m.Importance, m.Unrecognized)
	return e.finish()
}

// UnmarshalBinary decodes an XUDT; see Message.
func (m *XUDT) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeXUDT)
	if err != nil {
		return err
	}
	var v XUDT
	if err := d.fixedParam("protocol class", 1, &v.ProtocolClass); err != nil {
		return err
	}
	if err := d.fixedParam("hop counter", 1, &v.HopCounter); err != nil {
		return err
	}
	if err := d.pointers(4, 1); err != nil {
		return err
	}
	if err := d.variableParam(0, "called party address", &v.CalledPartyAddress); err != nil {
		return err
	}
	if err := d.variableParam(1, "calling party address", &v.CallingPartyAddress); err != nil {
		return err
	}
	if v.Data, err = d.variableData(2, "data"); err != nil {
		return err
	}
	if v.Unrecognized, err = d.optional(3, segmentationImportanceSpecs(&v.Segmentation, &v.Importance)); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *XUDT) String() string {
	return fmt.Sprintf("XUDT{ProtocolClass: %s, HopCounter: %d, CalledPartyAddress: %s, CallingPartyAddress: %s, Data: %x%s}",
		m.ProtocolClass, m.HopCounter, m.CalledPartyAddress, m.CallingPartyAddress, m.Data,
		optionalString(m.Segmentation, m.Importance, m.Unrecognized))
}

// XUDTS is the extended unitdata service message, ITU-T Q.713 (03/01) §4.19,
// Table 20.
type XUDTS struct {
	ReturnCause         params.ReturnCause
	HopCounter          params.HopCounter
	CalledPartyAddress  params.PartyAddress
	CallingPartyAddress params.PartyAddress
	// Data is the returned SCCP-user data, 1 to 255 octets (§3.16).
	Data []byte

	Segmentation *params.Segmentation
	Importance   *params.Importance
	Unrecognized []params.RawParameter
}

// MessageType returns MsgTypeXUDTS.
func (*XUDTS) MessageType() MsgType { return MsgTypeXUDTS }

// MarshalBinary encodes the message; see Message.
func (m *XUDTS) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *XUDTS) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *XUDTS) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeXUDTS)
	e.cause("return cause", byte(m.ReturnCause))
	e.fixed("hop counter", m.HopCounter)
	e.pointers(4, 1, true)
	e.address(0, "called party address", m.CalledPartyAddress, false)
	e.address(1, "calling party address", m.CallingPartyAddress, false)
	e.data(2, "data", m.Data, maxData)
	e.segmentationImportance(m.Segmentation, m.Importance, m.Unrecognized)
	return e.finish()
}

// UnmarshalBinary decodes an XUDTS; see Message.
func (m *XUDTS) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeXUDTS)
	if err != nil {
		return err
	}
	var v XUDTS
	cause, err := d.fixed("return cause", 1)
	if err != nil {
		return err
	}
	v.ReturnCause = params.ReturnCause(cause[0])
	if err := d.fixedParam("hop counter", 1, &v.HopCounter); err != nil {
		return err
	}
	if err := d.pointers(4, 1); err != nil {
		return err
	}
	if err := d.variableParam(0, "called party address", &v.CalledPartyAddress); err != nil {
		return err
	}
	if err := d.variableParam(1, "calling party address", &v.CallingPartyAddress); err != nil {
		return err
	}
	if v.Data, err = d.variableData(2, "data"); err != nil {
		return err
	}
	if v.Unrecognized, err = d.optional(3, segmentationImportanceSpecs(&v.Segmentation, &v.Importance)); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *XUDTS) String() string {
	return fmt.Sprintf("XUDTS{ReturnCause: %s, HopCounter: %d, CalledPartyAddress: %s, CallingPartyAddress: %s, Data: %x%s}",
		m.ReturnCause, m.HopCounter, m.CalledPartyAddress, m.CallingPartyAddress, m.Data,
		optionalString(m.Segmentation, m.Importance, m.Unrecognized))
}

// LUDT is the long unitdata message, ITU-T Q.713 (03/01) §4.20, Table 21. Its
// four pointers and its long data length indicator are two octets each, sent
// less significant octet first (§1.2, §1.4). It needs a maximum message size
// above the default; see MaxBroadbandMessageSize and MarshalOptions.
type LUDT struct {
	// ProtocolClass is class 0 or 1.
	ProtocolClass       params.ProtocolClass
	HopCounter          params.HopCounter
	CalledPartyAddress  params.PartyAddress
	CallingPartyAddress params.PartyAddress
	// Data is the long data parameter, 1 to 3952 octets (§3.20).
	Data []byte

	Segmentation *params.Segmentation
	Importance   *params.Importance
	Unrecognized []params.RawParameter
}

// MessageType returns MsgTypeLUDT.
func (*LUDT) MessageType() MsgType { return MsgTypeLUDT }

// MarshalBinary encodes the message; see Message.
func (m *LUDT) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *LUDT) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *LUDT) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeLUDT)
	e.connectionlessClass(m.ProtocolClass)
	e.segmentClass(m.ProtocolClass, m.Segmentation)
	e.fixed("hop counter", m.HopCounter)
	e.pointers(4, 2, true)
	e.address(0, "called party address", m.CalledPartyAddress, false)
	e.address(1, "calling party address", m.CallingPartyAddress, false)
	e.longData(2, "long data", m.Data, MaxLongData)
	e.segmentationImportance(m.Segmentation, m.Importance, m.Unrecognized)
	return e.finish()
}

// UnmarshalBinary decodes an LUDT; see Message.
func (m *LUDT) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeLUDT)
	if err != nil {
		return err
	}
	var v LUDT
	if err := d.fixedParam("protocol class", 1, &v.ProtocolClass); err != nil {
		return err
	}
	if err := d.fixedParam("hop counter", 1, &v.HopCounter); err != nil {
		return err
	}
	if err := d.pointers(4, 2); err != nil {
		return err
	}
	if err := d.variableParam(0, "called party address", &v.CalledPartyAddress); err != nil {
		return err
	}
	if err := d.variableParam(1, "calling party address", &v.CallingPartyAddress); err != nil {
		return err
	}
	if v.Data, err = d.longData(2, "long data"); err != nil {
		return err
	}
	if v.Unrecognized, err = d.optional(3, segmentationImportanceSpecs(&v.Segmentation, &v.Importance)); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *LUDT) String() string {
	return fmt.Sprintf("LUDT{ProtocolClass: %s, HopCounter: %d, CalledPartyAddress: %s, CallingPartyAddress: %s, Data: %x%s}",
		m.ProtocolClass, m.HopCounter, m.CalledPartyAddress, m.CallingPartyAddress, m.Data,
		optionalString(m.Segmentation, m.Importance, m.Unrecognized))
}

// LUDTS is the long unitdata service message, ITU-T Q.713 (03/01) §4.21,
// Table 22, encoded like LUDT.
type LUDTS struct {
	ReturnCause         params.ReturnCause
	HopCounter          params.HopCounter
	CalledPartyAddress  params.PartyAddress
	CallingPartyAddress params.PartyAddress
	// Data is the returned long data, 1 to 3952 octets (§3.20).
	Data []byte

	Segmentation *params.Segmentation
	Importance   *params.Importance
	Unrecognized []params.RawParameter
}

// MessageType returns MsgTypeLUDTS.
func (*LUDTS) MessageType() MsgType { return MsgTypeLUDTS }

// MarshalBinary encodes the message; see Message.
func (m *LUDTS) MarshalBinary() ([]byte, error) { return marshalDefault(m) }

// AppendBinary appends the encoded message to b; see Message.
func (m *LUDTS) AppendBinary(b []byte) ([]byte, error) { return appendDefault(b, m) }

func (m *LUDTS) appendMessage(b []byte) ([]byte, error) {
	e := newEncoder(b, MsgTypeLUDTS)
	e.cause("return cause", byte(m.ReturnCause))
	e.fixed("hop counter", m.HopCounter)
	e.pointers(4, 2, true)
	e.address(0, "called party address", m.CalledPartyAddress, false)
	e.address(1, "calling party address", m.CallingPartyAddress, false)
	e.longData(2, "long data", m.Data, MaxLongData)
	e.segmentationImportance(m.Segmentation, m.Importance, m.Unrecognized)
	return e.finish()
}

// UnmarshalBinary decodes an LUDTS; see Message.
func (m *LUDTS) UnmarshalBinary(b []byte) error {
	d, err := newDecoder(b, MsgTypeLUDTS)
	if err != nil {
		return err
	}
	var v LUDTS
	cause, err := d.fixed("return cause", 1)
	if err != nil {
		return err
	}
	v.ReturnCause = params.ReturnCause(cause[0])
	if err := d.fixedParam("hop counter", 1, &v.HopCounter); err != nil {
		return err
	}
	if err := d.pointers(4, 2); err != nil {
		return err
	}
	if err := d.variableParam(0, "called party address", &v.CalledPartyAddress); err != nil {
		return err
	}
	if err := d.variableParam(1, "calling party address", &v.CallingPartyAddress); err != nil {
		return err
	}
	if v.Data, err = d.longData(2, "long data"); err != nil {
		return err
	}
	if v.Unrecognized, err = d.optional(3, segmentationImportanceSpecs(&v.Segmentation, &v.Importance)); err != nil {
		return err
	}
	*m = v
	return nil
}

// String returns the message in a human readable form.
func (m *LUDTS) String() string {
	return fmt.Sprintf("LUDTS{ReturnCause: %s, HopCounter: %d, CalledPartyAddress: %s, CallingPartyAddress: %s, Data: %x%s}",
		m.ReturnCause, m.HopCounter, m.CalledPartyAddress, m.CallingPartyAddress, m.Data,
		optionalString(m.Segmentation, m.Importance, m.Unrecognized))
}

// connectionlessClass appends a protocol class that must be connectionless:
// UDT, XUDT and LUDT exist for classes 0 and 1 only (ITU-T Q.713 (03/01)
// Table 1).
func (e *encoder) connectionlessClass(p params.ProtocolClass) {
	if p.Class > 1 {
		e.invalid("protocol class %d is not a connectionless class", p.Class)
		return
	}
	e.fixed("protocol class", p)
}

var segmentationImportanceCodes = []params.ParameterNameCode{params.PCodeSegmentation, params.PCodeImportance}

// segmentationImportance appends the optional part of XUDT, XUDTS, LUDT and
// LUDTS in the order of Tables 19 to 22.
func (e *encoder) segmentationImportance(seg *params.Segmentation, imp *params.Importance, raw []params.RawParameter) {
	if seg != nil {
		e.optional(params.PCodeSegmentation, "segmentation", *seg)
	}
	if imp != nil {
		e.optional(params.PCodeImportance, "importance", *imp)
	}
	e.raws(raw, segmentationImportanceCodes)
}

func segmentationImportanceSpecs(seg **params.Segmentation, imp **params.Importance) []optSpec {
	return []optSpec{
		{params.PCodeSegmentation, "segmentation", optValue(seg)},
		{params.PCodeImportance, "importance", optValue(imp)},
	}
}

func optionalString(seg *params.Segmentation, imp *params.Importance, raw []params.RawParameter) string {
	s := ""
	if seg != nil {
		s += fmt.Sprintf(", Segmentation: %s", *seg)
	}
	if imp != nil {
		s += fmt.Sprintf(", Importance: %d", *imp)
	}
	if len(raw) > 0 {
		s += fmt.Sprintf(", Unrecognized: %v", raw)
	}
	return s
}
