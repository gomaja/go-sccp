// Copyright 2019-2024 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"fmt"
	"io"

	"github.com/gomaja/go-sccp/params"
)

// CR represents an SCCP Connection Request message; see ITU-T Q.713 (03/01), section 4.2.
type CR struct {
	Type                    MsgType
	SourceLocalReference    *params.LocalReference
	ProtocolClass           *params.ProtocolClass
	CalledPartyAddress      *params.PartyAddress
	Credit                  *params.Credit
	CallingPartyAddress     *params.PartyAddress
	Data                    *params.Data
	HopCounter              *params.HopCounter
	Importance              *params.Importance
	EndOfOptionalParameters *params.EndOfOptionalParameters

	ptr1, ptr2 uint8
}

// NewCR creates a new CR.
func NewCR(src uint32, pcls int, retOnErr bool, cdpa *params.PartyAddress, opts ...params.Parameter) *CR {
	c := &CR{
		Type:                 MsgTypeCR,
		SourceLocalReference: params.NewSourceLocalReference(src),
		ProtocolClass:        params.NewProtocolClass(pcls, retOnErr),
		CalledPartyAddress:   cdpa,
		ptr1:                 2,
	}
	c.ptr2 = 0

	assignCROptionalParameters("NewCR", opts, c)
	if len(opts) > 0 {
		c.ptr2 = c.ptr1 + uint8(cdpa.MarshalLen()) - 1
	}

	return c
}

// MarshalBinary returns the byte sequence generated from a CR instance.
func (c *CR) MarshalBinary() ([]byte, error) {
	b := make([]byte, c.MarshalLen())
	if err := c.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (c *CR) MarshalTo(b []byte) error {
	l := len(b)
	if l < 7 {
		return io.ErrUnexpectedEOF
	}

	b[0] = uint8(c.Type)
	offset := 1
	n, err := c.SourceLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = c.ProtocolClass.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n

	b[offset] = c.ptr1
	b[offset+1] = c.ptr2
	offset += 2

	cdpaEnd := l
	if c.ptr2 != 0 {
		cdpaEnd = 6 + int(c.ptr2)
		if l < cdpaEnd {
			return io.ErrUnexpectedEOF
		}
	}
	if _, err := c.CalledPartyAddress.Write(b[offset:cdpaEnd]); err != nil {
		return err
	}
	if c.ptr2 == 0 {
		return nil
	}

	return writeOptionalParameterList(
		b[cdpaEnd:],
		c.Credit,
		c.CallingPartyAddress,
		c.Data,
		c.HopCounter,
		c.Importance,
		c.EndOfOptionalParameters,
	)
}

// ParseCR decodes given byte sequence as a SCCP CR.
func ParseCR(b []byte) (*CR, error) {
	c := &CR{}
	if err := c.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return c, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP CR.
func (c *CR) UnmarshalBinary(b []byte) error {
	l := len(b)
	if l < 7 {
		return io.ErrUnexpectedEOF
	}

	c.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	c.SourceLocalReference, n, err = params.ParseSourceLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.ProtocolClass, n, err = params.ParseProtocolClass(b[offset:])
	if err != nil {
		return err
	}
	offset += n

	c.ptr1 = b[offset]
	offsetPtr1 := 5 + int(c.ptr1)
	if l < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	c.ptr2 = b[offset+1]

	cdpaEnd := offsetPtr1 + int(b[offsetPtr1]) + 1
	if l < cdpaEnd {
		return io.ErrUnexpectedEOF
	}
	c.CalledPartyAddress, _, err = params.ParseCalledPartyAddress(b[offsetPtr1:cdpaEnd])
	if err != nil {
		return err
	}

	if c.ptr2 == 0 {
		return nil
	}
	offsetPtr2 := 6 + int(c.ptr2)
	if l < offsetPtr2+1 {
		return io.ErrUnexpectedEOF
	}
	if offsetPtr2 != cdpaEnd {
		return fmt.Errorf("invalid CR optional pointer: expected %d, got %d", cdpaEnd-6, c.ptr2)
	}
	return parseCROptionalParameters(b[offsetPtr2:], c)
}

// MarshalLen returns the serial length.
func (c *CR) MarshalLen() int {
	l := 7
	if param := c.CalledPartyAddress; param != nil {
		l += param.MarshalLen()
	}
	if c.ptr2 != 0 {
		l += optionalParameterListLen(
			c.Credit,
			c.CallingPartyAddress,
			c.Data,
			c.HopCounter,
			c.Importance,
			c.EndOfOptionalParameters,
		)
	}
	return l
}

// String returns the CR values in human readable format.
func (c *CR) String() string {
	return fmt.Sprintf("%s: {SourceLocalReference: %s, ProtocolClass: %s, CalledPartyAddress: %v, Credit: %s, CallingPartyAddress: %v, Data: %s, HopCounter: %s, Importance: %s}",
		c.Type, c.SourceLocalReference, c.ProtocolClass, c.CalledPartyAddress, c.Credit, c.CallingPartyAddress, c.Data, c.HopCounter, c.Importance)
}

// MessageType returns the Message Type in int.
func (c *CR) MessageType() MsgType { return MsgTypeCR }

// MessageTypeName returns the Message Type in string.
func (c *CR) MessageTypeName() string { return c.MessageType().String() }

// CC represents an SCCP Connection Confirm message; see ITU-T Q.713 (03/01), section 4.3.
type CC struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	SourceLocalReference      *params.LocalReference
	ProtocolClass             *params.ProtocolClass
	Credit                    *params.Credit
	CalledPartyAddress        *params.PartyAddress
	Data                      *params.Data
	Importance                *params.Importance
	EndOfOptionalParameters   *params.EndOfOptionalParameters

	ptr1 uint8
}

// NewCC creates a new CC.
func NewCC(dst, src uint32, pcls int, retOnErr bool, opts ...params.Parameter) *CC {
	c := &CC{
		Type:                      MsgTypeCC,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		SourceLocalReference:      params.NewSourceLocalReference(src),
		ProtocolClass:             params.NewProtocolClass(pcls, retOnErr),
	}
	assignCCOptionalParameters("NewCC", opts, c)
	if len(opts) > 0 {
		c.ptr1 = 1
	}
	return c
}

// MarshalBinary returns the byte sequence generated from a CC instance.
func (c *CC) MarshalBinary() ([]byte, error) {
	b := make([]byte, c.MarshalLen())
	if err := c.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (c *CC) MarshalTo(b []byte) error {
	if len(b) < 9 {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(c.Type)
	offset := 1
	n, err := c.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = c.SourceLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = c.ProtocolClass.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	b[offset] = c.ptr1
	offset++
	if c.ptr1 == 0 {
		return nil
	}
	return writeOptionalParameterList(b[offset:], c.Credit, c.CalledPartyAddress, c.Data, c.Importance, c.EndOfOptionalParameters)
}

// ParseCC decodes given byte sequence as a SCCP CC.
func ParseCC(b []byte) (*CC, error) {
	c := &CC{}
	if err := c.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return c, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP CC.
func (c *CC) UnmarshalBinary(b []byte) error {
	l := len(b)
	if l < 9 {
		return io.ErrUnexpectedEOF
	}
	c.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	c.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.SourceLocalReference, n, err = params.ParseSourceLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.ProtocolClass, n, err = params.ParseProtocolClass(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.ptr1 = b[offset]
	if c.ptr1 == 0 {
		return nil
	}
	offsetPtr1 := 8 + int(c.ptr1)
	if l < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	return parseCCOptionalParameters(b[offsetPtr1:], c)
}

// MarshalLen returns the serial length.
func (c *CC) MarshalLen() int {
	l := 9
	if c.ptr1 != 0 {
		l += optionalParameterListLen(c.Credit, c.CalledPartyAddress, c.Data, c.Importance, c.EndOfOptionalParameters)
	}
	return l
}

// String returns the CC values in human readable format.
func (c *CC) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, SourceLocalReference: %s, ProtocolClass: %s, Credit: %s, CalledPartyAddress: %v, Data: %s, Importance: %s}",
		c.Type, c.DestinationLocalReference, c.SourceLocalReference, c.ProtocolClass, c.Credit, c.CalledPartyAddress, c.Data, c.Importance)
}

// MessageType returns the Message Type in int.
func (c *CC) MessageType() MsgType { return MsgTypeCC }

// MessageTypeName returns the Message Type in string.
func (c *CC) MessageTypeName() string { return c.MessageType().String() }

// CREF represents an SCCP Connection Refused message; see ITU-T Q.713 (03/01), section 4.4.
type CREF struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	RefusalCause              *params.RefusalCause
	CalledPartyAddress        *params.PartyAddress
	Data                      *params.Data
	Importance                *params.Importance
	EndOfOptionalParameters   *params.EndOfOptionalParameters

	ptr1 uint8
}

// NewCREF creates a new CREF.
func NewCREF(dst uint32, cause params.RefusalCauseValue, opts ...params.Parameter) *CREF {
	c := &CREF{
		Type:                      MsgTypeCREF,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		RefusalCause:              params.NewCause(cause),
	}
	assignCREFOptionalParameters("NewCREF", opts, c)
	if len(opts) > 0 {
		c.ptr1 = 1
	}
	return c
}

// MarshalBinary returns the byte sequence generated from a CREF instance.
func (c *CREF) MarshalBinary() ([]byte, error) {
	b := make([]byte, c.MarshalLen())
	if err := c.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (c *CREF) MarshalTo(b []byte) error {
	if len(b) < 6 {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(c.Type)
	offset := 1
	n, err := c.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = c.RefusalCause.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	b[offset] = c.ptr1
	offset++
	if c.ptr1 == 0 {
		return nil
	}
	return writeOptionalParameterList(b[offset:], c.CalledPartyAddress, c.Data, c.Importance, c.EndOfOptionalParameters)
}

// ParseCREF decodes given byte sequence as a SCCP CREF.
func ParseCREF(b []byte) (*CREF, error) {
	c := &CREF{}
	if err := c.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return c, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP CREF.
func (c *CREF) UnmarshalBinary(b []byte) error {
	l := len(b)
	if l < 6 {
		return io.ErrUnexpectedEOF
	}
	c.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	c.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.RefusalCause, n, err = params.ParseRefusalCause(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.ptr1 = b[offset]
	if c.ptr1 == 0 {
		return nil
	}
	offsetPtr1 := 5 + int(c.ptr1)
	if l < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	return parseCREFOptionalParameters(b[offsetPtr1:], c)
}

// MarshalLen returns the serial length.
func (c *CREF) MarshalLen() int {
	l := 6
	if c.ptr1 != 0 {
		l += optionalParameterListLen(c.CalledPartyAddress, c.Data, c.Importance, c.EndOfOptionalParameters)
	}
	return l
}

// String returns the CREF values in human readable format.
func (c *CREF) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, RefusalCause: %s, CalledPartyAddress: %v, Data: %s, Importance: %s}",
		c.Type, c.DestinationLocalReference, c.RefusalCause, c.CalledPartyAddress, c.Data, c.Importance)
}

// MessageType returns the Message Type in int.
func (c *CREF) MessageType() MsgType { return MsgTypeCREF }

// MessageTypeName returns the Message Type in string.
func (c *CREF) MessageTypeName() string { return c.MessageType().String() }

// RLSD represents an SCCP Released message; see ITU-T Q.713 (03/01), section 4.5.
type RLSD struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	SourceLocalReference      *params.LocalReference
	ReleaseCause              *params.ReleaseCause
	Data                      *params.Data
	Importance                *params.Importance
	EndOfOptionalParameters   *params.EndOfOptionalParameters

	ptr1 uint8
}

// NewRLSD creates a new RLSD.
func NewRLSD(dst, src uint32, cause params.ReleaseCauseValue, opts ...params.Parameter) *RLSD {
	r := &RLSD{
		Type:                      MsgTypeRLSD,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		SourceLocalReference:      params.NewSourceLocalReference(src),
		ReleaseCause:              params.NewCause(cause),
	}
	assignRLSDOptionalParameters("NewRLSD", opts, r)
	if len(opts) > 0 {
		r.ptr1 = 1
	}
	return r
}

// MarshalBinary returns the byte sequence generated from a RLSD instance.
func (r *RLSD) MarshalBinary() ([]byte, error) {
	b := make([]byte, r.MarshalLen())
	if err := r.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (r *RLSD) MarshalTo(b []byte) error {
	if len(b) < 9 {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(r.Type)
	offset := 1
	n, err := r.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = r.SourceLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = r.ReleaseCause.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	b[offset] = r.ptr1
	offset++
	if r.ptr1 == 0 {
		return nil
	}
	return writeOptionalParameterList(b[offset:], r.Data, r.Importance, r.EndOfOptionalParameters)
}

// ParseRLSD decodes given byte sequence as a SCCP RLSD.
func ParseRLSD(b []byte) (*RLSD, error) {
	r := &RLSD{}
	if err := r.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return r, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP RLSD.
func (r *RLSD) UnmarshalBinary(b []byte) error {
	l := len(b)
	if l < 9 {
		return io.ErrUnexpectedEOF
	}
	r.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	r.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	r.SourceLocalReference, n, err = params.ParseSourceLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	r.ReleaseCause, n, err = params.ParseReleaseCause(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	r.ptr1 = b[offset]
	if r.ptr1 == 0 {
		return nil
	}
	offsetPtr1 := 8 + int(r.ptr1)
	if l < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	return parseRLSDOptionalParameters(b[offsetPtr1:], r)
}

// MarshalLen returns the serial length.
func (r *RLSD) MarshalLen() int {
	l := 9
	if r.ptr1 != 0 {
		l += optionalParameterListLen(r.Data, r.Importance, r.EndOfOptionalParameters)
	}
	return l
}

// String returns the RLSD values in human readable format.
func (r *RLSD) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, SourceLocalReference: %s, ReleaseCause: %s, Data: %s, Importance: %s}",
		r.Type, r.DestinationLocalReference, r.SourceLocalReference, r.ReleaseCause, r.Data, r.Importance)
}

// MessageType returns the Message Type in int.
func (r *RLSD) MessageType() MsgType { return MsgTypeRLSD }

// MessageTypeName returns the Message Type in string.
func (r *RLSD) MessageTypeName() string { return r.MessageType().String() }

// RLC represents an SCCP Release Complete message; see ITU-T Q.713 (03/01), section 4.6.
type RLC struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	SourceLocalReference      *params.LocalReference
}

// NewRLC creates a new RLC.
func NewRLC(dst, src uint32) *RLC {
	return &RLC{
		Type:                      MsgTypeRLC,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		SourceLocalReference:      params.NewSourceLocalReference(src),
	}
}

// MarshalBinary returns the byte sequence generated from a RLC instance.
func (r *RLC) MarshalBinary() ([]byte, error) {
	b := make([]byte, r.MarshalLen())
	if err := r.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (r *RLC) MarshalTo(b []byte) error {
	if len(b) < r.MarshalLen() {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(r.Type)
	offset := 1
	n, err := r.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	_, err = r.SourceLocalReference.Write(b[offset:])
	return err
}

// ParseRLC decodes given byte sequence as a SCCP RLC.
func ParseRLC(b []byte) (*RLC, error) {
	r := &RLC{}
	if err := r.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return r, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP RLC.
func (r *RLC) UnmarshalBinary(b []byte) error {
	if len(b) < 7 {
		return io.ErrUnexpectedEOF
	}
	r.Type = MsgType(b[0])
	var n int
	var err error
	offset := 1
	r.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	r.SourceLocalReference, _, err = params.ParseSourceLocalReference(b[offset:])
	return err
}

// MarshalLen returns the serial length.
func (r *RLC) MarshalLen() int { return 7 }

// String returns the RLC values in human readable format.
func (r *RLC) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, SourceLocalReference: %s}", r.Type, r.DestinationLocalReference, r.SourceLocalReference)
}

// MessageType returns the Message Type in int.
func (r *RLC) MessageType() MsgType { return MsgTypeRLC }

// MessageTypeName returns the Message Type in string.
func (r *RLC) MessageTypeName() string { return r.MessageType().String() }

// DT1 represents an SCCP Data Form 1 message; see ITU-T Q.713 (03/01), section 4.7.
type DT1 struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	SegmentingReassembling    *params.SegmentingReassembling
	Data                      *params.Data

	ptr1 uint8
}

// NewDT1 creates a new DT1.
func NewDT1(dst uint32, moreData bool, data []byte) *DT1 {
	return &DT1{
		Type:                      MsgTypeDT1,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		SegmentingReassembling:    params.NewSegmentingReassembling(moreData),
		Data:                      params.NewData(data),
		ptr1:                      1,
	}
}

// MarshalBinary returns the byte sequence generated from a DT1 instance.
func (d *DT1) MarshalBinary() ([]byte, error) {
	b := make([]byte, d.MarshalLen())
	if err := d.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (d *DT1) MarshalTo(b []byte) error {
	if len(b) < 6 {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(d.Type)
	offset := 1
	n, err := d.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = d.SegmentingReassembling.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	b[offset] = d.ptr1
	offset++
	_, err = d.Data.Write(b[offset:])
	return err
}

// ParseDT1 decodes given byte sequence as a SCCP DT1.
func ParseDT1(b []byte) (*DT1, error) {
	d := &DT1{}
	if err := d.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return d, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP DT1.
func (d *DT1) UnmarshalBinary(b []byte) error {
	l := len(b)
	if l < 7 {
		return io.ErrUnexpectedEOF
	}
	d.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	d.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	d.SegmentingReassembling, n, err = params.ParseSegmentingReassembling(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	d.ptr1 = b[offset]
	offsetPtr1 := 5 + int(d.ptr1)
	if l < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	d.Data, _, err = params.ParseData(b[offsetPtr1:])
	return err
}

// MarshalLen returns the serial length.
func (d *DT1) MarshalLen() int { return 6 + d.Data.MarshalLen() }

// String returns the DT1 values in human readable format.
func (d *DT1) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, SegmentingReassembling: %s, Data: %s}", d.Type, d.DestinationLocalReference, d.SegmentingReassembling, d.Data)
}

// MessageType returns the Message Type in int.
func (d *DT1) MessageType() MsgType { return MsgTypeDT1 }

// MessageTypeName returns the Message Type in string.
func (d *DT1) MessageTypeName() string { return d.MessageType().String() }

// DT2 represents an SCCP Data Form 2 message; see ITU-T Q.713 (03/01), section 4.8.
type DT2 struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	SequencingSegmenting      *params.SequencingSegmenting
	Data                      *params.Data

	ptr1 uint8
}

// NewDT2 creates a new DT2.
func NewDT2(dst uint32, snd, rcv uint8, moreData bool, data []byte) *DT2 {
	return &DT2{
		Type:                      MsgTypeDT2,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		SequencingSegmenting:      params.NewSequencingSegmenting(snd, rcv, moreData),
		Data:                      params.NewData(data),
		ptr1:                      1,
	}
}

// MarshalBinary returns the byte sequence generated from a DT2 instance.
func (d *DT2) MarshalBinary() ([]byte, error) {
	b := make([]byte, d.MarshalLen())
	if err := d.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (d *DT2) MarshalTo(b []byte) error {
	if len(b) < 7 {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(d.Type)
	offset := 1
	n, err := d.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = d.SequencingSegmenting.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	b[offset] = d.ptr1
	offset++
	_, err = d.Data.Write(b[offset:])
	return err
}

// ParseDT2 decodes given byte sequence as a SCCP DT2.
func ParseDT2(b []byte) (*DT2, error) {
	d := &DT2{}
	if err := d.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return d, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP DT2.
func (d *DT2) UnmarshalBinary(b []byte) error {
	l := len(b)
	if l < 8 {
		return io.ErrUnexpectedEOF
	}
	d.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	d.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	d.SequencingSegmenting, n, err = params.ParseSequencingSegmenting(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	d.ptr1 = b[offset]
	offsetPtr1 := 6 + int(d.ptr1)
	if l < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	d.Data, _, err = params.ParseData(b[offsetPtr1:])
	return err
}

// MarshalLen returns the serial length.
func (d *DT2) MarshalLen() int { return 7 + d.Data.MarshalLen() }

// String returns the DT2 values in human readable format.
func (d *DT2) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, SequencingSegmenting: %s, Data: %s}", d.Type, d.DestinationLocalReference, d.SequencingSegmenting, d.Data)
}

// MessageType returns the Message Type in int.
func (d *DT2) MessageType() MsgType { return MsgTypeDT2 }

// MessageTypeName returns the Message Type in string.
func (d *DT2) MessageTypeName() string { return d.MessageType().String() }

// AK represents an SCCP Data Acknowledgement message; see ITU-T Q.713 (03/01), section 4.9.
type AK struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	ReceiveSequenceNumber     *params.ReceiveSequenceNumber
	Credit                    *params.Credit
}

// NewAK creates a new AK.
func NewAK(dst uint32, rcv, credit uint8) *AK {
	return &AK{
		Type:                      MsgTypeAK,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		ReceiveSequenceNumber:     params.NewReceiveSequenceNumber(rcv),
		Credit:                    params.NewCredit(credit),
	}
}

// MarshalBinary returns the byte sequence generated from an AK instance.
func (a *AK) MarshalBinary() ([]byte, error) {
	b := make([]byte, a.MarshalLen())
	if err := a.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (a *AK) MarshalTo(b []byte) error {
	if len(b) < a.MarshalLen() {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(a.Type)
	offset := 1
	n, err := a.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = a.ReceiveSequenceNumber.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	_, err = a.Credit.Write(b[offset:])
	return err
}

// ParseAK decodes given byte sequence as a SCCP AK.
func ParseAK(b []byte) (*AK, error) {
	a := &AK{}
	if err := a.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return a, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP AK.
func (a *AK) UnmarshalBinary(b []byte) error {
	if len(b) < 6 {
		return io.ErrUnexpectedEOF
	}
	a.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	a.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	a.ReceiveSequenceNumber, n, err = params.ParseReceiveSequenceNumber(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	a.Credit, _, err = params.ParseCredit(b[offset:])
	return err
}

// MarshalLen returns the serial length.
func (a *AK) MarshalLen() int { return 6 }

// String returns the AK values in human readable format.
func (a *AK) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, ReceiveSequenceNumber: %s, Credit: %s}", a.Type, a.DestinationLocalReference, a.ReceiveSequenceNumber, a.Credit)
}

// MessageType returns the Message Type in int.
func (a *AK) MessageType() MsgType { return MsgTypeAK }

// MessageTypeName returns the Message Type in string.
func (a *AK) MessageTypeName() string { return a.MessageType().String() }

// ED represents an SCCP Expedited Data message; see ITU-T Q.713 (03/01), section 4.12.
type ED struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	Data                      *params.Data

	ptr1 uint8
}

// NewED creates a new ED.
func NewED(dst uint32, data []byte) *ED {
	return &ED{
		Type:                      MsgTypeED,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		Data:                      params.NewData(data),
		ptr1:                      1,
	}
}

// MarshalBinary returns the byte sequence generated from an ED instance.
func (e *ED) MarshalBinary() ([]byte, error) {
	b := make([]byte, e.MarshalLen())
	if err := e.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (e *ED) MarshalTo(b []byte) error {
	if len(b) < 5 {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(e.Type)
	offset := 1
	n, err := e.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	b[offset] = e.ptr1
	offset++
	_, err = e.Data.Write(b[offset:])
	return err
}

// ParseED decodes given byte sequence as a SCCP ED.
func ParseED(b []byte) (*ED, error) {
	e := &ED{}
	if err := e.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return e, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP ED.
func (e *ED) UnmarshalBinary(b []byte) error {
	l := len(b)
	if l < 6 {
		return io.ErrUnexpectedEOF
	}
	e.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	e.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	e.ptr1 = b[offset]
	offsetPtr1 := 4 + int(e.ptr1)
	if l < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	e.Data, _, err = params.ParseData(b[offsetPtr1:])
	return err
}

// MarshalLen returns the serial length.
func (e *ED) MarshalLen() int { return 5 + e.Data.MarshalLen() }

// String returns the ED values in human readable format.
func (e *ED) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, Data: %s}", e.Type, e.DestinationLocalReference, e.Data)
}

// MessageType returns the Message Type in int.
func (e *ED) MessageType() MsgType { return MsgTypeED }

// MessageTypeName returns the Message Type in string.
func (e *ED) MessageTypeName() string { return e.MessageType().String() }

// EA represents an SCCP Expedited Data Acknowledgement message; see ITU-T Q.713 (03/01), section 4.13.
type EA struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
}

// NewEA creates a new EA.
func NewEA(dst uint32) *EA {
	return &EA{
		Type:                      MsgTypeEA,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
	}
}

// MarshalBinary returns the byte sequence generated from an EA instance.
func (e *EA) MarshalBinary() ([]byte, error) {
	b := make([]byte, e.MarshalLen())
	if err := e.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (e *EA) MarshalTo(b []byte) error {
	if len(b) < e.MarshalLen() {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(e.Type)
	_, err := e.DestinationLocalReference.Write(b[1:])
	return err
}

// ParseEA decodes given byte sequence as a SCCP EA.
func ParseEA(b []byte) (*EA, error) {
	e := &EA{}
	if err := e.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return e, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP EA.
func (e *EA) UnmarshalBinary(b []byte) error {
	if len(b) < 4 {
		return io.ErrUnexpectedEOF
	}
	e.Type = MsgType(b[0])
	var err error
	e.DestinationLocalReference, _, err = params.ParseDestinationLocalReference(b[1:])
	return err
}

// MarshalLen returns the serial length.
func (e *EA) MarshalLen() int { return 4 }

// String returns the EA values in human readable format.
func (e *EA) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s}", e.Type, e.DestinationLocalReference)
}

// MessageType returns the Message Type in int.
func (e *EA) MessageType() MsgType { return MsgTypeEA }

// MessageTypeName returns the Message Type in string.
func (e *EA) MessageTypeName() string { return e.MessageType().String() }

// RSR represents an SCCP Reset Request message; see ITU-T Q.713 (03/01), section 4.14.
type RSR struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	SourceLocalReference      *params.LocalReference
	ResetCause                *params.ResetCause
	OptionalParameters        []params.Parameter

	ptr1 uint8
}

// NewRSR creates a new RSR.
func NewRSR(dst, src uint32, cause params.ResetCauseValue, opts ...params.Parameter) *RSR {
	r := &RSR{
		Type:                      MsgTypeRSR,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		SourceLocalReference:      params.NewSourceLocalReference(src),
		ResetCause:                params.NewCause(cause),
	}
	if len(opts) > 0 {
		r.ptr1 = 1
		r.OptionalParameters = ensureOptionalEnd(opts)
	}
	return r
}

// MarshalBinary returns the byte sequence generated from a RSR instance.
func (r *RSR) MarshalBinary() ([]byte, error) {
	b := make([]byte, r.MarshalLen())
	if err := r.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (r *RSR) MarshalTo(b []byte) error {
	if len(b) < 9 {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(r.Type)
	offset := 1
	n, err := r.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = r.SourceLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = r.ResetCause.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	b[offset] = r.ptr1
	offset++
	if r.ptr1 == 0 {
		return nil
	}
	return writeOptionalParameterList(b[offset:], r.OptionalParameters...)
}

// ParseRSR decodes given byte sequence as a SCCP RSR.
func ParseRSR(b []byte) (*RSR, error) {
	r := &RSR{}
	if err := r.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return r, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP RSR.
func (r *RSR) UnmarshalBinary(b []byte) error {
	l := len(b)
	if l < 9 {
		return io.ErrUnexpectedEOF
	}
	r.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	r.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	r.SourceLocalReference, n, err = params.ParseSourceLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	r.ResetCause, n, err = params.ParseResetCause(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	r.ptr1 = b[offset]
	if r.ptr1 == 0 {
		return nil
	}
	offsetPtr1 := 8 + int(r.ptr1)
	if l < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	r.OptionalParameters, _, err = params.ParseOptionalParameters(b[offsetPtr1:])
	return err
}

// MarshalLen returns the serial length.
func (r *RSR) MarshalLen() int {
	l := 9
	if r.ptr1 != 0 {
		l += optionalParameterListLen(r.OptionalParameters...)
	}
	return l
}

// String returns the RSR values in human readable format.
func (r *RSR) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, SourceLocalReference: %s, ResetCause: %s, OptionalParameters: %v}",
		r.Type, r.DestinationLocalReference, r.SourceLocalReference, r.ResetCause, r.OptionalParameters)
}

// MessageType returns the Message Type in int.
func (r *RSR) MessageType() MsgType { return MsgTypeRSR }

// MessageTypeName returns the Message Type in string.
func (r *RSR) MessageTypeName() string { return r.MessageType().String() }

// RSC represents an SCCP Reset Confirmation message; see ITU-T Q.713 (03/01), section 4.15.
type RSC struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	SourceLocalReference      *params.LocalReference
}

// NewRSC creates a new RSC.
func NewRSC(dst, src uint32) *RSC {
	return &RSC{
		Type:                      MsgTypeRSC,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		SourceLocalReference:      params.NewSourceLocalReference(src),
	}
}

// MarshalBinary returns the byte sequence generated from a RSC instance.
func (r *RSC) MarshalBinary() ([]byte, error) {
	b := make([]byte, r.MarshalLen())
	if err := r.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (r *RSC) MarshalTo(b []byte) error {
	if len(b) < r.MarshalLen() {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(r.Type)
	offset := 1
	n, err := r.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	_, err = r.SourceLocalReference.Write(b[offset:])
	return err
}

// ParseRSC decodes given byte sequence as a SCCP RSC.
func ParseRSC(b []byte) (*RSC, error) {
	r := &RSC{}
	if err := r.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return r, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP RSC.
func (r *RSC) UnmarshalBinary(b []byte) error {
	if len(b) < 7 {
		return io.ErrUnexpectedEOF
	}
	r.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	r.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	r.SourceLocalReference, _, err = params.ParseSourceLocalReference(b[offset:])
	return err
}

// MarshalLen returns the serial length.
func (r *RSC) MarshalLen() int { return 7 }

// String returns the RSC values in human readable format.
func (r *RSC) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, SourceLocalReference: %s}", r.Type, r.DestinationLocalReference, r.SourceLocalReference)
}

// MessageType returns the Message Type in int.
func (r *RSC) MessageType() MsgType { return MsgTypeRSC }

// MessageTypeName returns the Message Type in string.
func (r *RSC) MessageTypeName() string { return r.MessageType().String() }

// ERR represents an SCCP Protocol Data Unit Error message; see ITU-T Q.713 (03/01), section 4.16.
type ERR struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	ErrorCause                *params.ErrorCause
	OptionalParameters        []params.Parameter

	ptr1 uint8
}

// NewERR creates a new ERR.
func NewERR(dst uint32, cause params.ErrorCauseValue, opts ...params.Parameter) *ERR {
	e := &ERR{
		Type:                      MsgTypeERR,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		ErrorCause:                params.NewCause(cause),
	}
	if len(opts) > 0 {
		e.ptr1 = 1
		e.OptionalParameters = ensureOptionalEnd(opts)
	}
	return e
}

// MarshalBinary returns the byte sequence generated from an ERR instance.
func (e *ERR) MarshalBinary() ([]byte, error) {
	b := make([]byte, e.MarshalLen())
	if err := e.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (e *ERR) MarshalTo(b []byte) error {
	if len(b) < 6 {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(e.Type)
	offset := 1
	n, err := e.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = e.ErrorCause.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	b[offset] = e.ptr1
	offset++
	if e.ptr1 == 0 {
		return nil
	}
	return writeOptionalParameterList(b[offset:], e.OptionalParameters...)
}

// ParseERR decodes given byte sequence as a SCCP ERR.
func ParseERR(b []byte) (*ERR, error) {
	e := &ERR{}
	if err := e.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return e, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP ERR.
func (e *ERR) UnmarshalBinary(b []byte) error {
	l := len(b)
	if l < 6 {
		return io.ErrUnexpectedEOF
	}
	e.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	e.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	e.ErrorCause, n, err = params.ParseErrorCause(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	e.ptr1 = b[offset]
	if e.ptr1 == 0 {
		return nil
	}
	offsetPtr1 := 5 + int(e.ptr1)
	if l < offsetPtr1+1 {
		return io.ErrUnexpectedEOF
	}
	e.OptionalParameters, _, err = params.ParseOptionalParameters(b[offsetPtr1:])
	return err
}

// MarshalLen returns the serial length.
func (e *ERR) MarshalLen() int {
	l := 6
	if e.ptr1 != 0 {
		l += optionalParameterListLen(e.OptionalParameters...)
	}
	return l
}

// String returns the ERR values in human readable format.
func (e *ERR) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, ErrorCause: %s, OptionalParameters: %v}", e.Type, e.DestinationLocalReference, e.ErrorCause, e.OptionalParameters)
}

// MessageType returns the Message Type in int.
func (e *ERR) MessageType() MsgType { return MsgTypeERR }

// MessageTypeName returns the Message Type in string.
func (e *ERR) MessageTypeName() string { return e.MessageType().String() }

// IT represents an SCCP Inactivity Test message; see ITU-T Q.713 (03/01), section 4.17.
type IT struct {
	Type                      MsgType
	DestinationLocalReference *params.LocalReference
	SourceLocalReference      *params.LocalReference
	ProtocolClass             *params.ProtocolClass
	SequencingSegmenting      *params.SequencingSegmenting
	Credit                    *params.Credit
}

// NewIT creates a new IT.
func NewIT(dst, src uint32, pcls int, retOnErr bool, snd, rcv uint8, moreData bool, credit uint8) *IT {
	return &IT{
		Type:                      MsgTypeIT,
		DestinationLocalReference: params.NewDestinationLocalReference(dst),
		SourceLocalReference:      params.NewSourceLocalReference(src),
		ProtocolClass:             params.NewProtocolClass(pcls, retOnErr),
		SequencingSegmenting:      params.NewSequencingSegmenting(snd, rcv, moreData),
		Credit:                    params.NewCredit(credit),
	}
}

// MarshalBinary returns the byte sequence generated from an IT instance.
func (i *IT) MarshalBinary() ([]byte, error) {
	b := make([]byte, i.MarshalLen())
	if err := i.MarshalTo(b); err != nil {
		return nil, err
	}
	return b, nil
}

// MarshalTo puts the byte sequence in the byte array given as b.
func (i *IT) MarshalTo(b []byte) error {
	if len(b) < i.MarshalLen() {
		return io.ErrUnexpectedEOF
	}
	b[0] = uint8(i.Type)
	offset := 1
	n, err := i.DestinationLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = i.SourceLocalReference.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = i.ProtocolClass.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	n, err = i.SequencingSegmenting.Write(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	_, err = i.Credit.Write(b[offset:])
	return err
}

// ParseIT decodes given byte sequence as a SCCP IT.
func ParseIT(b []byte) (*IT, error) {
	i := &IT{}
	if err := i.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return i, nil
}

// UnmarshalBinary sets the values retrieved from byte sequence in a SCCP IT.
func (i *IT) UnmarshalBinary(b []byte) error {
	if len(b) < 11 {
		return io.ErrUnexpectedEOF
	}
	i.Type = MsgType(b[0])
	offset := 1
	var n int
	var err error
	i.DestinationLocalReference, n, err = params.ParseDestinationLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	i.SourceLocalReference, n, err = params.ParseSourceLocalReference(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	i.ProtocolClass, n, err = params.ParseProtocolClass(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	i.SequencingSegmenting, n, err = params.ParseSequencingSegmenting(b[offset:])
	if err != nil {
		return err
	}
	offset += n
	i.Credit, _, err = params.ParseCredit(b[offset:])
	return err
}

// MarshalLen returns the serial length.
func (i *IT) MarshalLen() int { return 11 }

// String returns the IT values in human readable format.
func (i *IT) String() string {
	return fmt.Sprintf("%s: {DestinationLocalReference: %s, SourceLocalReference: %s, ProtocolClass: %s, SequencingSegmenting: %s, Credit: %s}",
		i.Type, i.DestinationLocalReference, i.SourceLocalReference, i.ProtocolClass, i.SequencingSegmenting, i.Credit)
}

// MessageType returns the Message Type in int.
func (i *IT) MessageType() MsgType { return MsgTypeIT }

// MessageTypeName returns the Message Type in string.
func (i *IT) MessageTypeName() string { return i.MessageType().String() }

func assignCROptionalParameters(messageName string, opts []params.Parameter, c *CR) {
	for _, opt := range ensureOptionalEnd(opts) {
		switch param := opt.(type) {
		case *params.Credit:
			c.Credit = param
		case *params.PartyAddress:
			if param.Code() == params.PCodeCallingPartyAddress {
				c.CallingPartyAddress = param
			} else {
				logf("%s: unexpected party address parameter: %s", messageName, param.Code())
			}
		case *params.Data:
			c.Data = param
		case *params.HopCounter:
			c.HopCounter = param
		case *params.Importance:
			c.Importance = param
		case *params.EndOfOptionalParameters:
			c.EndOfOptionalParameters = param
		default:
			logf("%s: unexpected parameter: %s", messageName, opt.Code())
		}
	}
}

func parseCROptionalParameters(b []byte, c *CR) error {
	opts, _, err := params.ParseOptionalParameters(b)
	if err != nil {
		return err
	}
	for _, opt := range opts {
		switch param := opt.(type) {
		case *params.Credit:
			c.Credit = param
		case *params.PartyAddress:
			if param.Code() != params.PCodeCallingPartyAddress {
				return fmt.Errorf("CR: unexpected party address parameter: %s", param.Code())
			}
			c.CallingPartyAddress = param
		case *params.Data:
			c.Data = param
		case *params.HopCounter:
			c.HopCounter = param
		case *params.Importance:
			c.Importance = param
		case *params.EndOfOptionalParameters:
			c.EndOfOptionalParameters = param
		default:
			return fmt.Errorf("CR: unexpected parameter: %s", opt.Code())
		}
	}
	return nil
}

func assignCCOptionalParameters(messageName string, opts []params.Parameter, c *CC) {
	for _, opt := range ensureOptionalEnd(opts) {
		switch param := opt.(type) {
		case *params.Credit:
			c.Credit = param
		case *params.PartyAddress:
			if param.Code() == params.PCodeCalledPartyAddress {
				c.CalledPartyAddress = param
			} else {
				logf("%s: unexpected party address parameter: %s", messageName, param.Code())
			}
		case *params.Data:
			c.Data = param
		case *params.Importance:
			c.Importance = param
		case *params.EndOfOptionalParameters:
			c.EndOfOptionalParameters = param
		default:
			logf("%s: unexpected parameter: %s", messageName, opt.Code())
		}
	}
}

func parseCCOptionalParameters(b []byte, c *CC) error {
	opts, _, err := params.ParseOptionalParameters(b)
	if err != nil {
		return err
	}
	for _, opt := range opts {
		switch param := opt.(type) {
		case *params.Credit:
			c.Credit = param
		case *params.PartyAddress:
			if param.Code() != params.PCodeCalledPartyAddress {
				return fmt.Errorf("CC: unexpected party address parameter: %s", param.Code())
			}
			c.CalledPartyAddress = param
		case *params.Data:
			c.Data = param
		case *params.Importance:
			c.Importance = param
		case *params.EndOfOptionalParameters:
			c.EndOfOptionalParameters = param
		default:
			return fmt.Errorf("CC: unexpected parameter: %s", opt.Code())
		}
	}
	return nil
}

func assignCREFOptionalParameters(messageName string, opts []params.Parameter, c *CREF) {
	for _, opt := range ensureOptionalEnd(opts) {
		switch param := opt.(type) {
		case *params.PartyAddress:
			if param.Code() == params.PCodeCalledPartyAddress {
				c.CalledPartyAddress = param
			} else {
				logf("%s: unexpected party address parameter: %s", messageName, param.Code())
			}
		case *params.Data:
			c.Data = param
		case *params.Importance:
			c.Importance = param
		case *params.EndOfOptionalParameters:
			c.EndOfOptionalParameters = param
		default:
			logf("%s: unexpected parameter: %s", messageName, opt.Code())
		}
	}
}

func parseCREFOptionalParameters(b []byte, c *CREF) error {
	opts, _, err := params.ParseOptionalParameters(b)
	if err != nil {
		return err
	}
	for _, opt := range opts {
		switch param := opt.(type) {
		case *params.PartyAddress:
			if param.Code() != params.PCodeCalledPartyAddress {
				return fmt.Errorf("CREF: unexpected party address parameter: %s", param.Code())
			}
			c.CalledPartyAddress = param
		case *params.Data:
			c.Data = param
		case *params.Importance:
			c.Importance = param
		case *params.EndOfOptionalParameters:
			c.EndOfOptionalParameters = param
		default:
			return fmt.Errorf("CREF: unexpected parameter: %s", opt.Code())
		}
	}
	return nil
}

func assignRLSDOptionalParameters(messageName string, opts []params.Parameter, r *RLSD) {
	for _, opt := range ensureOptionalEnd(opts) {
		switch param := opt.(type) {
		case *params.Data:
			r.Data = param
		case *params.Importance:
			r.Importance = param
		case *params.EndOfOptionalParameters:
			r.EndOfOptionalParameters = param
		default:
			logf("%s: unexpected parameter: %s", messageName, opt.Code())
		}
	}
}

func parseRLSDOptionalParameters(b []byte, r *RLSD) error {
	opts, _, err := params.ParseOptionalParameters(b)
	if err != nil {
		return err
	}
	for _, opt := range opts {
		switch param := opt.(type) {
		case *params.Data:
			r.Data = param
		case *params.Importance:
			r.Importance = param
		case *params.EndOfOptionalParameters:
			r.EndOfOptionalParameters = param
		default:
			return fmt.Errorf("RLSD: unexpected parameter: %s", opt.Code())
		}
	}
	return nil
}

func ensureOptionalEnd(opts []params.Parameter) []params.Parameter {
	if len(opts) == 0 {
		return nil
	}
	out := make([]params.Parameter, 0, len(opts)+1)
	out = append(out, opts...)
	if out[len(out)-1].Code() != params.PCodeEndOfOptionalParameters {
		out = append(out, params.NewEndOfOptionalParameters())
	}
	return out
}

func writeOptionalParameterList(b []byte, opts ...params.Parameter) error {
	offset := 0
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		n, err := opt.Write(b[offset:])
		if err != nil {
			return err
		}
		offset += n
	}
	return nil
}

func optionalParameterListLen(opts ...params.Parameter) int {
	l := 0
	for _, opt := range opts {
		if opt != nil {
			l += opt.MarshalLen()
		}
	}
	return l
}
