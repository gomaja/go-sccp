// Copyright 2019-2024 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/gomaja/go-sccp/params"
)

// UDTS represents an SCCP Unitdata Service message; see ITU-T Q.713 (03/01), section 4.11.
type UDTS struct {
	Type                MsgType
	ReturnCause         *params.ReturnCause
	CalledPartyAddress  *params.PartyAddress
	CallingPartyAddress *params.PartyAddress
	Data                *params.Data

	ptr1, ptr2, ptr3 uint8
}

// NewUDTS creates a new UDTS.
func NewUDTS(rc params.ReturnCauseValue, cdpa, cgpa *params.PartyAddress, data []byte) *UDTS {
	u := &UDTS{
		Type:                MsgTypeUDTS,
		ReturnCause:         params.NewCause(rc),
		CalledPartyAddress:  cdpa,
		CallingPartyAddress: cgpa,
		Data:                params.NewData(data),
	}

	u.ptr1 = 3
	u.ptr2 = u.ptr1 + uint8(cdpa.MarshalLen()) - 1
	u.ptr3 = u.ptr2 + uint8(cgpa.MarshalLen()) - 1

	return u
}

// MarshalBinary returns the byte sequence generated from a UDTS instance.
func (u *UDTS) MarshalBinary() ([]byte, error) {
	b := make([]byte, u.MarshalLen())
	if err := u.MarshalTo(b); err != nil {
		return nil, err
	}

	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (u *UDTS) MarshalTo(b []byte) error {
	l := len(b)
	if l < 5 {
		return io.ErrUnexpectedEOF
	}

	b[0] = uint8(u.Type)

	n := 1
	m, err := u.ReturnCause.Write(b[n:])
	if err != nil {
		return err
	}
	n += m

	b[n] = u.ptr1
	b[n+1] = u.ptr2
	b[n+2] = u.ptr3
	n += 3

	cdpaEnd := int(u.ptr2) + 3
	if l < cdpaEnd {
		return io.ErrUnexpectedEOF
	}
	cgpaEnd := int(u.ptr3) + 4
	if l < cgpaEnd {
		return io.ErrUnexpectedEOF
	}

	if _, err := u.CalledPartyAddress.Write(b[n:cdpaEnd]); err != nil {
		return err
	}
	if _, err := u.CallingPartyAddress.Write(b[cdpaEnd:cgpaEnd]); err != nil {
		return err
	}
	if _, err := u.Data.Write(b[cgpaEnd:]); err != nil {
		return err
	}

	return nil
}

// ParseUDTS decodes given byte sequence as a SCCP UDTS.
func ParseUDTS(b []byte) (*UDTS, error) {
	u := &UDTS{}
	if err := u.UnmarshalBinary(b); err != nil {
		return nil, err
	}

	return u, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP UDTS.
func (u *UDTS) UnmarshalBinary(b []byte) error {
	l := len(b)
	if l <= 5 {
		return io.ErrUnexpectedEOF
	}

	u.Type = MsgType(b[0])

	offset := 1
	u.ReturnCause = &params.ReturnCause{}
	n, err := u.ReturnCause.Read(b[offset:])
	if err != nil {
		return err
	}
	offset += n

	u.ptr1 = b[offset]
	offsetPtr1 := 2 + int(u.ptr1)
	if l < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	u.ptr2 = b[offset+1]
	offsetPtr2 := 3 + int(u.ptr2)
	if l < offsetPtr2+1 {
		return io.ErrUnexpectedEOF
	}
	u.ptr3 = b[offset+2]
	offsetPtr3 := 4 + int(u.ptr3)
	if l < offsetPtr3+1 {
		return io.ErrUnexpectedEOF
	}

	cdpaEnd := offsetPtr1 + int(b[offsetPtr1]) + 1
	if l < cdpaEnd {
		return io.ErrUnexpectedEOF
	}
	cgpaEnd := offsetPtr2 + int(b[offsetPtr2]) + 1
	if l < cgpaEnd {
		return io.ErrUnexpectedEOF
	}
	dataEnd := offsetPtr3 + int(b[offsetPtr3]) + 1
	if l < dataEnd {
		return io.ErrUnexpectedEOF
	}

	u.CalledPartyAddress, _, err = params.ParseCalledPartyAddress(b[offsetPtr1:cdpaEnd])
	if err != nil {
		return err
	}
	u.CallingPartyAddress, _, err = params.ParseCallingPartyAddress(b[offsetPtr2:cgpaEnd])
	if err != nil {
		return err
	}
	u.Data, _, err = params.ParseData(b[offsetPtr3:dataEnd])
	if err != nil {
		return err
	}

	return nil
}

// MarshalLen returns the serial length.
func (u *UDTS) MarshalLen() int {
	l := 5

	l += int(u.ptr3) - 1
	if param := u.Data; param != nil {
		l += param.MarshalLen()
	}

	return l
}

// String returns the UDTS values in human readable format.
func (u *UDTS) String() string {
	return fmt.Sprintf("%s: {ReturnCause: %s, CalledPartyAddress: %v, CallingPartyAddress: %v, Data: %s}",
		u.Type,
		u.ReturnCause,
		u.CalledPartyAddress,
		u.CallingPartyAddress,
		u.Data,
	)
}

// MessageType returns the Message Type in int.
func (u *UDTS) MessageType() MsgType {
	return MsgTypeUDTS
}

// MessageTypeName returns the Message Type in string.
func (u *UDTS) MessageTypeName() string {
	return u.MessageType().String()
}

// CdGT returns the GT in CalledPartyAddress in human readable string.
func (u *UDTS) CdGT() string {
	if u.CalledPartyAddress.GlobalTitle == nil {
		return ""
	}
	return u.CalledPartyAddress.Address()
}

// CgGT returns the GT in CallingPartyAddress in human readable string.
func (u *UDTS) CgGT() string {
	if u.CallingPartyAddress.GlobalTitle == nil {
		return ""
	}
	return u.CallingPartyAddress.Address()
}

// XUDTS represents an SCCP Extended Unitdata Service message; see ITU-T Q.713 (03/01), section 4.19.
type XUDTS struct {
	Type                    MsgType
	ReturnCause             *params.ReturnCause
	HopCounter              *params.HopCounter
	CalledPartyAddress      *params.PartyAddress
	CallingPartyAddress     *params.PartyAddress
	Data                    *params.Data
	Segmentation            *params.Segmentation
	Importance              *params.Importance
	EndOfOptionalParameters *params.EndOfOptionalParameters

	ptr1, ptr2, ptr3, ptr4 uint8
}

// NewXUDTS creates a new XUDTS.
func NewXUDTS(rc params.ReturnCauseValue, hc uint8, cdpa, cgpa *params.PartyAddress, data []byte, opts ...params.Parameter) *XUDTS {
	x := &XUDTS{
		Type:                MsgTypeXUDTS,
		ReturnCause:         params.NewCause(rc),
		HopCounter:          params.NewHopCounter(hc),
		CalledPartyAddress:  cdpa,
		CallingPartyAddress: cgpa,
		Data:                params.NewData(data),
	}

	x.ptr1 = 4
	x.ptr2 = x.ptr1 + uint8(cdpa.MarshalLen()) - 1
	x.ptr3 = x.ptr2 + uint8(cgpa.MarshalLen()) - 1
	x.ptr4 = 0

	assignConnectionlessOptionalParameters("NewXUDTS", opts, &x.Segmentation, &x.Importance, &x.EndOfOptionalParameters)
	if len(opts) > 0 {
		x.ptr4 = x.ptr3 + uint8(x.Data.MarshalLen()) - 1
	}

	return x
}

// MarshalBinary returns the byte sequence generated from a XUDTS instance.
func (x *XUDTS) MarshalBinary() ([]byte, error) {
	b := make([]byte, x.MarshalLen())
	if err := x.MarshalTo(b); err != nil {
		return nil, err
	}

	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (x *XUDTS) MarshalTo(b []byte) error {
	l := len(b)
	if l < 7 {
		return io.ErrUnexpectedEOF
	}

	b[0] = uint8(x.Type)

	n := 1
	m, err := x.ReturnCause.Write(b[n:])
	if err != nil {
		return err
	}
	n += m

	m, err = x.HopCounter.Write(b[n:])
	if err != nil {
		return err
	}
	n += m

	b[n] = x.ptr1
	b[n+1] = x.ptr2
	b[n+2] = x.ptr3
	b[n+3] = x.ptr4
	n += 4

	cdpaEnd := int(x.ptr2) + 4
	if l < cdpaEnd {
		return io.ErrUnexpectedEOF
	}
	cgpaEnd := int(x.ptr3) + 5
	if l < cgpaEnd {
		return io.ErrUnexpectedEOF
	}
	dataEnd := l
	if x.ptr4 != 0 {
		dataEnd = int(x.ptr4) + 6
		if l < dataEnd {
			return io.ErrUnexpectedEOF
		}
	}

	if _, err := x.CalledPartyAddress.Write(b[n:cdpaEnd]); err != nil {
		return err
	}
	if _, err := x.CallingPartyAddress.Write(b[cdpaEnd:cgpaEnd]); err != nil {
		return err
	}
	if _, err := x.Data.Write(b[cgpaEnd:dataEnd]); err != nil {
		return err
	}

	if x.ptr4 == 0 {
		return nil
	}
	return writeConnectionlessOptionalParameters(b[dataEnd:], x.Segmentation, x.Importance, x.EndOfOptionalParameters)
}

// ParseXUDTS decodes given byte sequence as a SCCP XUDTS.
func ParseXUDTS(b []byte) (*XUDTS, error) {
	x := &XUDTS{}
	if err := x.UnmarshalBinary(b); err != nil {
		return nil, err
	}

	return x, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP XUDTS.
func (x *XUDTS) UnmarshalBinary(b []byte) error {
	l := len(b)
	if l < 7 {
		return io.ErrUnexpectedEOF
	}

	x.Type = MsgType(b[0])

	offset := 1
	x.ReturnCause = &params.ReturnCause{}
	n, err := x.ReturnCause.Read(b[offset:])
	if err != nil {
		return err
	}
	offset += n

	x.HopCounter = &params.HopCounter{}
	n, err = x.HopCounter.Read(b[offset:])
	if err != nil {
		return err
	}
	offset += n

	x.ptr1 = b[offset]
	offsetPtr1 := 3 + int(x.ptr1)
	if l < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	x.ptr2 = b[offset+1]
	offsetPtr2 := 4 + int(x.ptr2)
	if l < offsetPtr2+1 {
		return io.ErrUnexpectedEOF
	}
	x.ptr3 = b[offset+2]
	offsetPtr3 := 5 + int(x.ptr3)
	if l < offsetPtr3+1 {
		return io.ErrUnexpectedEOF
	}
	x.ptr4 = b[offset+3]

	cdpaEnd := offsetPtr1 + int(b[offsetPtr1]) + 1
	if l < cdpaEnd {
		return io.ErrUnexpectedEOF
	}
	cgpaEnd := offsetPtr2 + int(b[offsetPtr2]) + 1
	if l < cgpaEnd {
		return io.ErrUnexpectedEOF
	}
	dataEnd := offsetPtr3 + int(b[offsetPtr3]) + 1
	if l < dataEnd {
		return io.ErrUnexpectedEOF
	}

	x.CalledPartyAddress, _, err = params.ParseCalledPartyAddress(b[offsetPtr1:cdpaEnd])
	if err != nil {
		return err
	}
	x.CallingPartyAddress, _, err = params.ParseCallingPartyAddress(b[offsetPtr2:cgpaEnd])
	if err != nil {
		return err
	}
	x.Data, _, err = params.ParseData(b[offsetPtr3:dataEnd])
	if err != nil {
		return err
	}

	if x.ptr4 == 0 {
		return nil
	}
	offsetPtr4 := 6 + int(x.ptr4)
	if l < offsetPtr4+1 {
		return io.ErrUnexpectedEOF
	}
	if offsetPtr4 != dataEnd {
		return fmt.Errorf("invalid XUDTS optional pointer: expected %d, got %d", dataEnd-6, x.ptr4)
	}

	return parseConnectionlessOptionalParameters(b[offsetPtr4:], &x.Segmentation, &x.Importance, &x.EndOfOptionalParameters)
}

// MarshalLen returns the serial length.
func (x *XUDTS) MarshalLen() int {
	l := 7

	if x.ptr4 != 0 {
		l += int(x.ptr4) - 1
		l += connectionlessOptionalParametersLen(x.Segmentation, x.Importance, x.EndOfOptionalParameters)

		return l
	}

	l += int(x.ptr3) - 2
	if param := x.Data; param != nil {
		l += param.MarshalLen()
	}

	return l
}

// String returns the XUDTS values in human readable format.
func (x *XUDTS) String() string {
	return fmt.Sprintf("%s: {ReturnCause: %s, HopCounter: %s, CalledPartyAddress: %v, CallingPartyAddress: %v, Data: %s, Segmentation: %s, Importance: %s}",
		x.Type,
		x.ReturnCause,
		x.HopCounter,
		x.CalledPartyAddress,
		x.CallingPartyAddress,
		x.Data,
		x.Segmentation,
		x.Importance,
	)
}

// MessageType returns the Message Type in int.
func (x *XUDTS) MessageType() MsgType {
	return MsgTypeXUDTS
}

// MessageTypeName returns the Message Type in string.
func (x *XUDTS) MessageTypeName() string {
	return x.MessageType().String()
}

// CdGT returns the GT in CalledPartyAddress in human readable string.
func (x *XUDTS) CdGT() string {
	if x.CalledPartyAddress.GlobalTitle == nil {
		return ""
	}
	return x.CalledPartyAddress.Address()
}

// CgGT returns the GT in CallingPartyAddress in human readable string.
func (x *XUDTS) CgGT() string {
	if x.CallingPartyAddress.GlobalTitle == nil {
		return ""
	}
	return x.CallingPartyAddress.Address()
}

// LUDT represents an SCCP Long Unitdata message; see ITU-T Q.713 (03/01), section 4.20.
type LUDT struct {
	Type                    MsgType
	ProtocolClass           *params.ProtocolClass
	HopCounter              *params.HopCounter
	CalledPartyAddress      *params.PartyAddress
	CallingPartyAddress     *params.PartyAddress
	LongData                *params.LongData
	Segmentation            *params.Segmentation
	Importance              *params.Importance
	EndOfOptionalParameters *params.EndOfOptionalParameters

	ptr1, ptr2, ptr3, ptr4 uint16
}

// NewLUDT creates a new LUDT.
func NewLUDT(pcls int, retOnErr bool, hc uint8, cdpa, cgpa *params.PartyAddress, data []byte, opts ...params.Parameter) *LUDT {
	l := &LUDT{
		Type:                MsgTypeLUDT,
		ProtocolClass:       params.NewProtocolClass(pcls, retOnErr),
		HopCounter:          params.NewHopCounter(hc),
		CalledPartyAddress:  cdpa,
		CallingPartyAddress: cgpa,
		LongData:            params.NewLongData(data),
	}

	l.ptr1 = 8
	l.ptr2 = l.ptr1 + uint16(cdpa.MarshalLen()) - 2
	l.ptr3 = l.ptr2 + uint16(cgpa.MarshalLen()) - 2
	l.ptr4 = 0

	assignConnectionlessOptionalParameters("NewLUDT", opts, &l.Segmentation, &l.Importance, &l.EndOfOptionalParameters)
	if len(opts) > 0 {
		l.ptr4 = l.ptr3 + uint16(l.LongData.MarshalLen()) - 2
	}

	return l
}

// MarshalBinary returns the byte sequence generated from a LUDT instance.
func (l *LUDT) MarshalBinary() ([]byte, error) {
	b := make([]byte, l.MarshalLen())
	if err := l.MarshalTo(b); err != nil {
		return nil, err
	}

	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (l *LUDT) MarshalTo(b []byte) error {
	length := len(b)
	if length < 11 {
		return io.ErrUnexpectedEOF
	}

	b[0] = uint8(l.Type)

	n := 1
	m, err := l.ProtocolClass.Write(b[n:])
	if err != nil {
		return err
	}
	n += m

	m, err = l.HopCounter.Write(b[n:])
	if err != nil {
		return err
	}
	n += m

	binary.BigEndian.PutUint16(b[n:], l.ptr1)
	binary.BigEndian.PutUint16(b[n+2:], l.ptr2)
	binary.BigEndian.PutUint16(b[n+4:], l.ptr3)
	binary.BigEndian.PutUint16(b[n+6:], l.ptr4)
	n += 8

	cdpaEnd := int(l.ptr2) + 5
	if length < cdpaEnd {
		return io.ErrUnexpectedEOF
	}
	cgpaEnd := int(l.ptr3) + 7
	if length < cgpaEnd {
		return io.ErrUnexpectedEOF
	}
	longDataEnd := length
	if l.ptr4 != 0 {
		longDataEnd = int(l.ptr4) + 9
		if length < longDataEnd {
			return io.ErrUnexpectedEOF
		}
	}

	if _, err := l.CalledPartyAddress.Write(b[n:cdpaEnd]); err != nil {
		return err
	}
	if _, err := l.CallingPartyAddress.Write(b[cdpaEnd:cgpaEnd]); err != nil {
		return err
	}
	if _, err := l.LongData.Write(b[cgpaEnd:longDataEnd]); err != nil {
		return err
	}

	if l.ptr4 == 0 {
		return nil
	}
	return writeConnectionlessOptionalParameters(b[longDataEnd:], l.Segmentation, l.Importance, l.EndOfOptionalParameters)
}

// ParseLUDT decodes given byte sequence as a SCCP LUDT.
func ParseLUDT(b []byte) (*LUDT, error) {
	l := &LUDT{}
	if err := l.UnmarshalBinary(b); err != nil {
		return nil, err
	}

	return l, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP LUDT.
func (l *LUDT) UnmarshalBinary(b []byte) error {
	length := len(b)
	if length < 11 {
		return io.ErrUnexpectedEOF
	}

	l.Type = MsgType(b[0])

	offset := 1
	l.ProtocolClass = &params.ProtocolClass{}
	n, err := l.ProtocolClass.Read(b[offset:])
	if err != nil {
		return err
	}
	offset += n

	l.HopCounter = &params.HopCounter{}
	n, err = l.HopCounter.Read(b[offset:])
	if err != nil {
		return err
	}
	offset += n

	l.ptr1 = binary.BigEndian.Uint16(b[offset:])
	offsetPtr1 := 3 + int(l.ptr1)
	if length < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	l.ptr2 = binary.BigEndian.Uint16(b[offset+2:])
	offsetPtr2 := 5 + int(l.ptr2)
	if length < offsetPtr2+1 {
		return io.ErrUnexpectedEOF
	}
	l.ptr3 = binary.BigEndian.Uint16(b[offset+4:])
	offsetPtr3 := 7 + int(l.ptr3)
	if length < offsetPtr3+2 {
		return io.ErrUnexpectedEOF
	}
	l.ptr4 = binary.BigEndian.Uint16(b[offset+6:])

	cdpaEnd := offsetPtr1 + int(b[offsetPtr1]) + 1
	if length < cdpaEnd {
		return io.ErrUnexpectedEOF
	}
	cgpaEnd := offsetPtr2 + int(b[offsetPtr2]) + 1
	if length < cgpaEnd {
		return io.ErrUnexpectedEOF
	}

	l.CalledPartyAddress, _, err = params.ParseCalledPartyAddress(b[offsetPtr1:cdpaEnd])
	if err != nil {
		return err
	}
	l.CallingPartyAddress, _, err = params.ParseCallingPartyAddress(b[offsetPtr2:cgpaEnd])
	if err != nil {
		return err
	}

	l.LongData = &params.LongData{}
	n, err = l.LongData.Read(b[offsetPtr3:])
	if err != nil {
		return err
	}
	longDataEnd := offsetPtr3 + n

	if l.ptr4 == 0 {
		return nil
	}
	offsetPtr4 := 9 + int(l.ptr4)
	if length < offsetPtr4+1 {
		return io.ErrUnexpectedEOF
	}
	if offsetPtr4 != longDataEnd {
		return fmt.Errorf("invalid LUDT optional pointer: expected %d, got %d", longDataEnd-9, l.ptr4)
	}

	return parseConnectionlessOptionalParameters(b[offsetPtr4:], &l.Segmentation, &l.Importance, &l.EndOfOptionalParameters)
}

// MarshalLen returns the serial length.
func (l *LUDT) MarshalLen() int {
	length := 11

	if l.ptr4 != 0 {
		length += int(l.ptr4) - 2
		length += connectionlessOptionalParametersLen(l.Segmentation, l.Importance, l.EndOfOptionalParameters)

		return length
	}

	length += int(l.ptr3) - 4
	if param := l.LongData; param != nil {
		length += param.MarshalLen()
	}

	return length
}

// String returns the LUDT values in human readable format.
func (l *LUDT) String() string {
	return fmt.Sprintf("%s: {ProtocolClass: %s, HopCounter: %s, CalledPartyAddress: %v, CallingPartyAddress: %v, LongData: %s, Segmentation: %s, Importance: %s}",
		l.Type,
		l.ProtocolClass,
		l.HopCounter,
		l.CalledPartyAddress,
		l.CallingPartyAddress,
		l.LongData,
		l.Segmentation,
		l.Importance,
	)
}

// MessageType returns the Message Type in int.
func (l *LUDT) MessageType() MsgType {
	return MsgTypeLUDT
}

// MessageTypeName returns the Message Type in string.
func (l *LUDT) MessageTypeName() string {
	return l.MessageType().String()
}

// CdGT returns the GT in CalledPartyAddress in human readable string.
func (l *LUDT) CdGT() string {
	if l.CalledPartyAddress.GlobalTitle == nil {
		return ""
	}
	return l.CalledPartyAddress.Address()
}

// CgGT returns the GT in CallingPartyAddress in human readable string.
func (l *LUDT) CgGT() string {
	if l.CallingPartyAddress.GlobalTitle == nil {
		return ""
	}
	return l.CallingPartyAddress.Address()
}

// LUDTS represents an SCCP Long Unitdata Service message; see ITU-T Q.713 (03/01), section 4.21.
type LUDTS struct {
	Type                    MsgType
	ReturnCause             *params.ReturnCause
	HopCounter              *params.HopCounter
	CalledPartyAddress      *params.PartyAddress
	CallingPartyAddress     *params.PartyAddress
	LongData                *params.LongData
	Segmentation            *params.Segmentation
	Importance              *params.Importance
	EndOfOptionalParameters *params.EndOfOptionalParameters

	ptr1, ptr2, ptr3, ptr4 uint16
}

// NewLUDTS creates a new LUDTS.
func NewLUDTS(rc params.ReturnCauseValue, hc uint8, cdpa, cgpa *params.PartyAddress, data []byte, opts ...params.Parameter) *LUDTS {
	l := &LUDTS{
		Type:                MsgTypeLUDTS,
		ReturnCause:         params.NewCause(rc),
		HopCounter:          params.NewHopCounter(hc),
		CalledPartyAddress:  cdpa,
		CallingPartyAddress: cgpa,
		LongData:            params.NewLongData(data),
	}

	l.ptr1 = 8
	l.ptr2 = l.ptr1 + uint16(cdpa.MarshalLen()) - 2
	l.ptr3 = l.ptr2 + uint16(cgpa.MarshalLen()) - 2
	l.ptr4 = 0

	assignConnectionlessOptionalParameters("NewLUDTS", opts, &l.Segmentation, &l.Importance, &l.EndOfOptionalParameters)
	if len(opts) > 0 {
		l.ptr4 = l.ptr3 + uint16(l.LongData.MarshalLen()) - 2
	}

	return l
}

// MarshalBinary returns the byte sequence generated from a LUDTS instance.
func (l *LUDTS) MarshalBinary() ([]byte, error) {
	b := make([]byte, l.MarshalLen())
	if err := l.MarshalTo(b); err != nil {
		return nil, err
	}

	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (l *LUDTS) MarshalTo(b []byte) error {
	length := len(b)
	if length < 11 {
		return io.ErrUnexpectedEOF
	}

	b[0] = uint8(l.Type)

	n := 1
	m, err := l.ReturnCause.Write(b[n:])
	if err != nil {
		return err
	}
	n += m

	m, err = l.HopCounter.Write(b[n:])
	if err != nil {
		return err
	}
	n += m

	binary.BigEndian.PutUint16(b[n:], l.ptr1)
	binary.BigEndian.PutUint16(b[n+2:], l.ptr2)
	binary.BigEndian.PutUint16(b[n+4:], l.ptr3)
	binary.BigEndian.PutUint16(b[n+6:], l.ptr4)
	n += 8

	cdpaEnd := int(l.ptr2) + 5
	if length < cdpaEnd {
		return io.ErrUnexpectedEOF
	}
	cgpaEnd := int(l.ptr3) + 7
	if length < cgpaEnd {
		return io.ErrUnexpectedEOF
	}
	longDataEnd := length
	if l.ptr4 != 0 {
		longDataEnd = int(l.ptr4) + 9
		if length < longDataEnd {
			return io.ErrUnexpectedEOF
		}
	}

	if _, err := l.CalledPartyAddress.Write(b[n:cdpaEnd]); err != nil {
		return err
	}
	if _, err := l.CallingPartyAddress.Write(b[cdpaEnd:cgpaEnd]); err != nil {
		return err
	}
	if _, err := l.LongData.Write(b[cgpaEnd:longDataEnd]); err != nil {
		return err
	}

	if l.ptr4 == 0 {
		return nil
	}
	return writeConnectionlessOptionalParameters(b[longDataEnd:], l.Segmentation, l.Importance, l.EndOfOptionalParameters)
}

// ParseLUDTS decodes given byte sequence as a SCCP LUDTS.
func ParseLUDTS(b []byte) (*LUDTS, error) {
	l := &LUDTS{}
	if err := l.UnmarshalBinary(b); err != nil {
		return nil, err
	}

	return l, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP LUDTS.
func (l *LUDTS) UnmarshalBinary(b []byte) error {
	length := len(b)
	if length < 11 {
		return io.ErrUnexpectedEOF
	}

	l.Type = MsgType(b[0])

	offset := 1
	l.ReturnCause = &params.ReturnCause{}
	n, err := l.ReturnCause.Read(b[offset:])
	if err != nil {
		return err
	}
	offset += n

	l.HopCounter = &params.HopCounter{}
	n, err = l.HopCounter.Read(b[offset:])
	if err != nil {
		return err
	}
	offset += n

	l.ptr1 = binary.BigEndian.Uint16(b[offset:])
	offsetPtr1 := 3 + int(l.ptr1)
	if length < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	l.ptr2 = binary.BigEndian.Uint16(b[offset+2:])
	offsetPtr2 := 5 + int(l.ptr2)
	if length < offsetPtr2+1 {
		return io.ErrUnexpectedEOF
	}
	l.ptr3 = binary.BigEndian.Uint16(b[offset+4:])
	offsetPtr3 := 7 + int(l.ptr3)
	if length < offsetPtr3+2 {
		return io.ErrUnexpectedEOF
	}
	l.ptr4 = binary.BigEndian.Uint16(b[offset+6:])

	cdpaEnd := offsetPtr1 + int(b[offsetPtr1]) + 1
	if length < cdpaEnd {
		return io.ErrUnexpectedEOF
	}
	cgpaEnd := offsetPtr2 + int(b[offsetPtr2]) + 1
	if length < cgpaEnd {
		return io.ErrUnexpectedEOF
	}

	l.CalledPartyAddress, _, err = params.ParseCalledPartyAddress(b[offsetPtr1:cdpaEnd])
	if err != nil {
		return err
	}
	l.CallingPartyAddress, _, err = params.ParseCallingPartyAddress(b[offsetPtr2:cgpaEnd])
	if err != nil {
		return err
	}

	l.LongData = &params.LongData{}
	n, err = l.LongData.Read(b[offsetPtr3:])
	if err != nil {
		return err
	}
	longDataEnd := offsetPtr3 + n

	if l.ptr4 == 0 {
		return nil
	}
	offsetPtr4 := 9 + int(l.ptr4)
	if length < offsetPtr4+1 {
		return io.ErrUnexpectedEOF
	}
	if offsetPtr4 != longDataEnd {
		return fmt.Errorf("invalid LUDTS optional pointer: expected %d, got %d", longDataEnd-9, l.ptr4)
	}

	return parseConnectionlessOptionalParameters(b[offsetPtr4:], &l.Segmentation, &l.Importance, &l.EndOfOptionalParameters)
}

// MarshalLen returns the serial length.
func (l *LUDTS) MarshalLen() int {
	length := 11

	if l.ptr4 != 0 {
		length += int(l.ptr4) - 2
		length += connectionlessOptionalParametersLen(l.Segmentation, l.Importance, l.EndOfOptionalParameters)

		return length
	}

	length += int(l.ptr3) - 4
	if param := l.LongData; param != nil {
		length += param.MarshalLen()
	}

	return length
}

// String returns the LUDTS values in human readable format.
func (l *LUDTS) String() string {
	return fmt.Sprintf("%s: {ReturnCause: %s, HopCounter: %s, CalledPartyAddress: %v, CallingPartyAddress: %v, LongData: %s, Segmentation: %s, Importance: %s}",
		l.Type,
		l.ReturnCause,
		l.HopCounter,
		l.CalledPartyAddress,
		l.CallingPartyAddress,
		l.LongData,
		l.Segmentation,
		l.Importance,
	)
}

// MessageType returns the Message Type in int.
func (l *LUDTS) MessageType() MsgType {
	return MsgTypeLUDTS
}

// MessageTypeName returns the Message Type in string.
func (l *LUDTS) MessageTypeName() string {
	return l.MessageType().String()
}

// CdGT returns the GT in CalledPartyAddress in human readable string.
func (l *LUDTS) CdGT() string {
	if l.CalledPartyAddress.GlobalTitle == nil {
		return ""
	}
	return l.CalledPartyAddress.Address()
}

// CgGT returns the GT in CallingPartyAddress in human readable string.
func (l *LUDTS) CgGT() string {
	if l.CallingPartyAddress.GlobalTitle == nil {
		return ""
	}
	return l.CallingPartyAddress.Address()
}

func assignConnectionlessOptionalParameters(messageName string, opts []params.Parameter, segmentation **params.Segmentation, importance **params.Importance, eop **params.EndOfOptionalParameters) {
	for _, opt := range opts {
		if opt == nil {
			logf("nil optional parameter in %s", messageName)
			continue
		}
		if err := setConnectionlessOptionalParameter(messageName, opt, segmentation, importance, eop); err != nil {
			logf("%v", err)
		}
	}
	if len(opts) > 0 && *eop == nil {
		*eop = params.NewEndOfOptionalParameters()
	}
}

func parseConnectionlessOptionalParameters(b []byte, segmentation **params.Segmentation, importance **params.Importance, eop **params.EndOfOptionalParameters) error {
	opts, _, err := params.ParseOptionalParameters(b)
	if err != nil {
		return err
	}

	for _, opt := range opts {
		if err := setConnectionlessOptionalParameter("connectionless optional parameters", opt, segmentation, importance, eop); err != nil {
			return err
		}
	}

	return nil
}

func setConnectionlessOptionalParameter(messageName string, opt params.Parameter, segmentation **params.Segmentation, importance **params.Importance, eop **params.EndOfOptionalParameters) error {
	switch opt.Code() {
	case params.PCodeSegmentation:
		param, ok := opt.(*params.Segmentation)
		if !ok {
			return fmt.Errorf("%s: invalid segmentation parameter type %T", messageName, opt)
		}
		*segmentation = param
	case params.PCodeImportance:
		param, ok := opt.(*params.Importance)
		if !ok {
			return fmt.Errorf("%s: invalid importance parameter type %T", messageName, opt)
		}
		*importance = param
	case params.PCodeEndOfOptionalParameters:
		param, ok := opt.(*params.EndOfOptionalParameters)
		if !ok {
			return fmt.Errorf("%s: invalid end-of-optional-parameters type %T", messageName, opt)
		}
		*eop = param
	default:
		return fmt.Errorf("%s: unexpected parameter: %s", messageName, opt.Code())
	}

	return nil
}

func writeConnectionlessOptionalParameters(b []byte, segmentation *params.Segmentation, importance *params.Importance, eop *params.EndOfOptionalParameters) error {
	offset := 0
	if param := segmentation; param != nil {
		n, err := param.Write(b[offset:])
		if err != nil {
			return err
		}
		offset += n
	}
	if param := importance; param != nil {
		n, err := param.Write(b[offset:])
		if err != nil {
			return err
		}
		offset += n
	}
	if param := eop; param != nil {
		_, err := param.Write(b[offset:])
		if err != nil {
			return err
		}
	}

	return nil
}

func connectionlessOptionalParametersLen(segmentation *params.Segmentation, importance *params.Importance, eop *params.EndOfOptionalParameters) int {
	length := 0
	if param := segmentation; param != nil {
		length += param.MarshalLen()
	}
	if param := importance; param != nil {
		length += param.MarshalLen()
	}
	if param := eop; param != nil {
		length += param.MarshalLen()
	}
	return length
}
