// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"encoding"
	"fmt"
	"io"

	"github.com/gomaja/go-sccp/params"
)

// encoder builds one message in the canonical layout of ITU-T Q.713 (03/01)
// §1.2 to §1.6: the message type, the mandatory fixed part, the pointers, the
// mandatory variable parameters in pointer order with no gaps, then the
// optional part closed by the end of optional parameters octet. The first
// error stops encoding and finish returns it with the caller's buffer.
type encoder struct {
	msg     MsgType
	orig    []byte
	b       []byte
	ptrAt   int // offset in b of the first pointer
	ptrSize int // octets per pointer: 1, or 2 for LUDT and LUDTS (§1.4)
	optIdx  int // index of the pointer to the optional part, -1 if none
	optAt   int // offset in b of the optional part, -1 until written
	err     error
}

func newEncoder(b []byte, t MsgType) encoder {
	return encoder{msg: t, orig: b, b: append(b, byte(t)), optIdx: -1, optAt: -1}
}

func (e *encoder) invalid(format string, args ...any) {
	if e.err == nil {
		e.err = fmt.Errorf("%w: %s: %s", ErrInvalidMessage, e.msg, fmt.Sprintf(format, args...))
	}
}

func (e *encoder) paramError(name string, err error) {
	if e.err == nil {
		e.err = fmt.Errorf("%w: %s %s: %w", ErrInvalidMessage, e.msg, name, err)
	}
}

// fixed appends a mandatory fixed parameter (§1.3).
func (e *encoder) fixed(name string, p encoding.BinaryAppender) {
	if e.err != nil {
		return
	}
	var err error
	if e.b, err = p.AppendBinary(e.b); err != nil {
		e.paramError(name, err)
	}
}

// octet appends a one-octet mandatory fixed field that cannot be invalid.
func (e *encoder) octet(v byte) {
	if e.err == nil {
		e.b = append(e.b, v)
	}
}

// pointers reserves n pointers of size octets; when optional is set the last
// one points to the optional part and stays zero while there is none (§1.4).
func (e *encoder) pointers(n, size int, optional bool) {
	if e.err != nil {
		return
	}
	e.ptrAt, e.ptrSize = len(e.b), size
	if optional {
		e.optIdx = n - 1
	}
	e.b = append(e.b, make([]byte, n*size)...)
}

// point sets pointer i to the current end of the message. A pointer counts the
// octets from its most significant octet, included, to the first octet of its
// parameter, excluded (§2.3); a two-octet pointer is sent less significant
// octet first (§1.4).
func (e *encoder) point(i int, name string) {
	if e.err != nil {
		return
	}
	at := e.ptrAt + i*e.ptrSize
	v := len(e.b) - (at + e.ptrSize - 1)
	limit := 1<<(8*e.ptrSize) - 1
	if v > limit {
		e.invalid("pointer to %s is %d octets, more than a %d-octet pointer can reach", name, v, e.ptrSize)
		return
	}
	e.b[at] = byte(v)
	if e.ptrSize == 2 {
		e.b[at+1] = byte(v >> 8)
	}
}

// lengthOf appends contents behind a one-octet length indicator (§2.2).
func (e *encoder) lengthOf(name string, p encoding.BinaryAppender) {
	if e.err != nil {
		return
	}
	lenAt := len(e.b)
	e.b = append(e.b, 0)
	var err error
	if e.b, err = p.AppendBinary(e.b); err != nil {
		e.paramError(name, err)
		return
	}
	n := len(e.b) - lenAt - 1
	if n > params.MaxVariableLength {
		e.invalid("%s is %d octets, more than its length indicator can carry (%d)", name, n, params.MaxVariableLength)
		return
	}
	e.b[lenAt] = byte(n)
}

// variable appends mandatory variable parameter i (§1.4).
func (e *encoder) variable(i int, name string, p encoding.BinaryAppender) {
	e.point(i, name)
	e.lengthOf(name, p)
}

// address appends a mandatory party address. ITU-T Q.713 (03/01) Tables 3,
// 11, 12 and 19 to 22 give "3 minimum" octets with the length indicator: an
// address indicator and at least one address element. Only a calling party
// address of a UDT or XUDT may be the address indicator alone with bits 1-7
// zero (Table 11 Note 2, Table 19 Note 3, §3.5).
func (e *encoder) address(i int, name string, a params.PartyAddress, indicatorOnlyAllowed bool) {
	if e.err == nil && !hasAddressElement(a) && (!indicatorOnlyAllowed || !a.IndicatorOnly()) {
		e.invalid("%s has no address element (%s)", name, a)
		return
	}
	e.variable(i, name, a)
}

// optionalAddress appends an optional party address, which has "4 minimum"
// octets with its name and length indicator (Tables 3 to 5).
func (e *encoder) optionalAddress(code params.ParameterNameCode, name string, a params.PartyAddress) {
	if e.err == nil && !hasAddressElement(a) {
		e.invalid("%s has no address element (%s)", name, a)
		return
	}
	e.optional(code, name, a)
}

func hasAddressElement(a params.PartyAddress) bool {
	return a.HasPointCode || a.HasSSN || a.GlobalTitle != nil
}

// cause appends a one-octet cause. The code 11111111 is reserved in every
// cause table (ITU-T Q.713 (03/01) §3.11 to §3.15).
func (e *encoder) cause(name string, v uint8) {
	if e.err == nil && v == 0xff {
		e.invalid("%s %#02x is reserved", name, v)
		return
	}
	e.octet(v)
}

// segmentClass checks ITU-T Q.714 (05/01) §4.1.1.1.2: "The protocol class for
// each segmented XUDT message shall be set to 1", which §4.1.1.2.2 extends to
// LUDT segments. A message whose segmentation parameter marks it complete
// (first segment, none remaining) is not a segment.
func (e *encoder) segmentClass(class params.ProtocolClass, seg *params.Segmentation) {
	if e.err == nil && seg != nil && (!seg.FirstSegment || seg.RemainingSegments > 0) && class.Class != 1 {
		e.invalid("segment with protocol class %d, want 1", class.Class)
	}
}

// data appends a mandatory variable data parameter of 1 to maxLen octets.
func (e *encoder) data(i int, name string, data []byte, maxLen int) {
	if e.err != nil {
		return
	}
	if len(data) == 0 || len(data) > maxLen {
		e.invalid("%s is %d octets, want 1 to %d", name, len(data), maxLen)
		return
	}
	e.variable(i, name, rawBytes(data))
}

// longData appends the long data parameter, whose length indicator is two
// octets sent less significant octet first (ITU-T Q.713 (03/01) §1.2, §3.20).
func (e *encoder) longData(i int, name string, data []byte, maxLen int) {
	if e.err != nil {
		return
	}
	if len(data) == 0 || len(data) > maxLen {
		e.invalid("%s is %d octets, want 1 to %d", name, len(data), maxLen)
		return
	}
	e.point(i, name)
	if e.err != nil {
		return
	}
	e.b = append(e.b, byte(len(data)), byte(len(data)>>8))
	e.b = append(e.b, data...)
}

// optional appends an optional parameter: name, length indicator and contents
// (§1.5). The first one sets the pointer to the optional part.
func (e *encoder) optional(code params.ParameterNameCode, name string, p encoding.BinaryAppender) {
	if e.err != nil {
		return
	}
	if e.optIdx < 0 {
		e.invalid("%s has no optional part for %s", e.msg, name)
		return
	}
	if e.optAt < 0 {
		e.point(e.optIdx, "optional part")
		e.optAt = len(e.b)
	}
	e.octet(byte(code))
	e.lengthOf(name, p)
}

// optionalData appends an optional data parameter of 1 to maxLen octets when
// data is not empty.
func (e *encoder) optionalData(name string, data []byte, maxLen int) {
	if len(data) == 0 {
		return
	}
	if len(data) > maxLen {
		e.invalid("%s is %d octets, want 1 to %d", name, len(data), maxLen)
		return
	}
	e.optional(params.PCodeData, name, rawBytes(data))
}

// raws appends the unrecognised optional parameters after the recognised ones.
// A raw parameter may not use a code the message recognises: decoding would
// take it for that parameter.
func (e *encoder) raws(raw []params.RawParameter, recognized []params.ParameterNameCode) {
	for _, r := range raw {
		if e.err != nil {
			return
		}
		if err := r.Validate(); err != nil {
			e.paramError("unrecognized parameter", err)
			return
		}
		for _, c := range recognized {
			if r.Code == c {
				e.invalid("unrecognized parameter uses the code of %s, which %s carries as a field", c, e.msg)
				return
			}
		}
		e.optional(r.Code, r.Code.String(), rawBytes(r.Value))
	}
}

// finish closes the optional part with the end of optional parameters octet,
// which is present only when optional parameters are (§1.6).
func (e *encoder) finish() ([]byte, error) {
	if e.err == nil && e.optAt >= 0 {
		e.b = append(e.b, byte(params.PCodeEndOfOptionalParameters))
	}
	if e.err != nil {
		return e.orig, e.err
	}
	return e.b, nil
}

type rawBytes []byte

func (r rawBytes) AppendBinary(b []byte) ([]byte, error) { return append(b, r...), nil }

// decoder reads one message. It accepts what ITU-T Q.713 (03/01) §1.4 and
// Q.714 (05/01) §1.1.4.5 require a receiver to accept: gaps, parameters in an
// order different from their pointers, overlapping parameters and octets
// after the last parameter. It rejects a mandatory pointer that is zero or
// points into the fixed part or the pointer area, and any parameter that runs
// past the end of the message.
type decoder struct {
	msg     MsgType
	b       []byte
	off     int // next octet of the mandatory fixed part
	ptrAt   int
	ptrSize int
	ptrEnd  int // first octet after the pointers
}

func newDecoder(b []byte, t MsgType) (decoder, error) {
	d := decoder{msg: t, b: b, off: 1}
	if len(b) < 1 {
		return d, d.truncated("message type")
	}
	if MsgType(b[0]) != t {
		return d, d.malformed("message type code %#02x", b[0])
	}
	return d, nil
}

func (d *decoder) truncated(what string) error {
	return fmt.Errorf("%w: %s %s: %w", ErrMalformedMessage, d.msg, what, io.ErrUnexpectedEOF)
}

func (d *decoder) malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrMalformedMessage, d.msg, fmt.Sprintf(format, args...))
}

func (d *decoder) paramError(name string, err error) error {
	return fmt.Errorf("%w: %s %s: %w", ErrMalformedMessage, d.msg, name, err)
}

// fixed returns the next n octets of the mandatory fixed part.
func (d *decoder) fixed(name string, n int) ([]byte, error) {
	if len(d.b)-d.off < n {
		return nil, d.truncated(name)
	}
	v := d.b[d.off : d.off+n]
	d.off += n
	return v, nil
}

// fixedParam decodes the next n octets of the fixed part into p.
func (d *decoder) fixedParam(name string, n int, p encoding.BinaryUnmarshaler) error {
	v, err := d.fixed(name, n)
	if err != nil {
		return err
	}
	if err := p.UnmarshalBinary(v); err != nil {
		return d.paramError(name, err)
	}
	return nil
}

// pointers reads n pointers of size octets.
func (d *decoder) pointers(n, size int) error {
	if len(d.b)-d.off < n*size {
		return d.truncated("pointers")
	}
	d.ptrAt, d.ptrSize = d.off, size
	d.off += n * size
	d.ptrEnd = d.off
	return nil
}

// target returns the offset pointer i designates and whether it is non-zero.
// The offset is at or after the end of the pointers and at most len(b).
func (d *decoder) target(i int, name string) (int, bool, error) {
	at := d.ptrAt + i*d.ptrSize
	v := int(d.b[at])
	if d.ptrSize == 2 {
		v |= int(d.b[at+1]) << 8
	}
	if v == 0 {
		return 0, false, nil
	}
	t := at + d.ptrSize - 1 + v
	if t < d.ptrEnd {
		return 0, false, d.malformed("pointer to %s points into the pointers", name)
	}
	if t > len(d.b) {
		return 0, false, d.truncated(name)
	}
	return t, true, nil
}

// mandatoryTarget is target for a mandatory variable parameter, which must
// have a non-zero pointer and room for its length indicator.
func (d *decoder) mandatoryTarget(i int, name string) (int, error) {
	t, ok, err := d.target(i, name)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, d.malformed("pointer to mandatory %s is zero", name)
	}
	if t >= len(d.b) {
		return 0, d.truncated(name)
	}
	return t, nil
}

// variable returns the contents of mandatory variable parameter i, which has a
// one-octet length indicator. The contents alias the input.
func (d *decoder) variable(i int, name string) ([]byte, error) {
	t, err := d.mandatoryTarget(i, name)
	if err != nil {
		return nil, err
	}
	n := int(d.b[t])
	if t+1+n > len(d.b) {
		return nil, d.truncated(name)
	}
	return d.b[t+1 : t+1+n], nil
}

// variableParam decodes mandatory variable parameter i into p.
func (d *decoder) variableParam(i int, name string, p encoding.BinaryUnmarshaler) error {
	v, err := d.variable(i, name)
	if err != nil {
		return err
	}
	if err := p.UnmarshalBinary(v); err != nil {
		return d.paramError(name, err)
	}
	return nil
}

// variableData returns a copy of the contents of mandatory variable parameter
// i, nil when empty.
func (d *decoder) variableData(i int, name string) ([]byte, error) {
	v, err := d.variable(i, name)
	return clone(v), err
}

// longData returns a copy of the long data parameter i, whose length indicator
// is two octets sent less significant octet first (§1.2).
func (d *decoder) longData(i int, name string) ([]byte, error) {
	t, err := d.mandatoryTarget(i, name)
	if err != nil {
		return nil, err
	}
	if t+2 > len(d.b) {
		return nil, d.truncated(name)
	}
	n := int(d.b[t]) | int(d.b[t+1])<<8
	if t+2+n > len(d.b) {
		return nil, d.truncated(name)
	}
	return clone(d.b[t+2 : t+2+n]), nil
}

// optSpec describes an optional parameter a message type recognises.
type optSpec struct {
	code params.ParameterNameCode
	name string
	set  func(v []byte) error
}

// optional decodes the optional part pointer i designates. Recognised
// parameters are passed to their spec; a recognised parameter that appears
// twice is an error. The others are returned, copied, for the message's
// Unrecognized field (ITU-T Q.714 (05/01) §1.1.4.2). Decoding stops at the end
// of optional parameters octet, and at the end of the message when that octet
// is missing (ITU-T Q.713 (03/01) §1.6: it is not used to detect the end of a
// message); octets after it are ignored (§1.4).
func (d *decoder) optional(i int, specs []optSpec) ([]params.RawParameter, error) {
	t, ok, err := d.target(i, "optional part")
	if err != nil || !ok {
		return nil, err
	}
	var seen [256]bool
	var raw []params.RawParameter
	for t < len(d.b) {
		code := params.ParameterNameCode(d.b[t])
		if code == params.PCodeEndOfOptionalParameters {
			break
		}
		if t+2 > len(d.b) {
			return nil, d.truncated(code.String() + " length indicator")
		}
		n := int(d.b[t+1])
		if t+2+n > len(d.b) {
			return nil, d.truncated(code.String())
		}
		v := d.b[t+2 : t+2+n]
		t += 2 + n

		spec := findSpec(specs, code)
		if spec == nil {
			raw = append(raw, params.RawParameter{Code: code, Value: clone(v)})
			continue
		}
		if seen[code] {
			return nil, d.malformed("%s appears twice", spec.name)
		}
		seen[code] = true
		if err := spec.set(v); err != nil {
			return nil, d.paramError(spec.name, err)
		}
	}
	return raw, nil
}

func findSpec(specs []optSpec, code params.ParameterNameCode) *optSpec {
	for i := range specs {
		if specs[i].code == code {
			return &specs[i]
		}
	}
	return nil
}

// optValue decodes an optional parameter into a new value and stores it in
// *dst.
func optValue[T any, P interface {
	*T
	encoding.BinaryUnmarshaler
}](dst **T) func([]byte) error {
	return func(v []byte) error {
		p := P(new(T))
		if err := p.UnmarshalBinary(v); err != nil {
			return err
		}
		*dst = p
		return nil
	}
}

// optBytes stores a copy of an optional data parameter in *dst.
func optBytes(dst *[]byte) func([]byte) error {
	return func(v []byte) error {
		*dst = clone(v)
		return nil
	}
}

func clone(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return append([]byte(nil), b...)
}
