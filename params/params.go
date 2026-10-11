// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

// Package params implements the SCCP parameters of ITU-T Q.713 (03/01) clause 3.
//
// Every type encodes and decodes the parameter contents only. The framing that
// depends on how a message carries a parameter — the pointer and length
// indicator of a mandatory variable parameter, the name and length indicator of
// an optional one (ITU-T Q.713 (03/01) §1.3 to §1.5) — belongs to the message
// codecs of package sccp.
//
// Encoding is conservative: a value that does not fit its field, or a field
// that the parameter cannot carry, is an error wrapping ErrInvalidValue. Nothing
// is masked or truncated. Decoding is liberal: spare bits are not examined
// (ITU-T Q.714 (05/01) §1.1.4.4), and only contents that cannot be decoded at
// all are rejected.
package params

import (
	"errors"
	"fmt"
	"io"
)

var (
	// ErrInvalidValue reports a parameter value that cannot be encoded without
	// changing it: out of range, too long for its field, or a field the
	// parameter format does not carry.
	ErrInvalidValue = errors.New("sccp/params: invalid value")

	// ErrMalformed reports parameter contents that cannot be decoded.
	ErrMalformed = errors.New("sccp/params: malformed parameter")
)

// ParameterNameCode is a parameter name code, ITU-T Q.713 (03/01) Table 2.
type ParameterNameCode uint8

// Parameter name codes, ITU-T Q.713 (03/01) Table 2.
const (
	PCodeEndOfOptionalParameters   ParameterNameCode = 0x00 // end of optional parameters
	PCodeDestinationLocalReference ParameterNameCode = 0x01 // destination local reference
	PCodeSourceLocalReference      ParameterNameCode = 0x02 // source local reference
	PCodeCalledPartyAddress        ParameterNameCode = 0x03 // called party address
	PCodeCallingPartyAddress       ParameterNameCode = 0x04 // calling party address
	PCodeProtocolClass             ParameterNameCode = 0x05 // protocol class
	PCodeSegmentingReassembling    ParameterNameCode = 0x06 // segmenting/reassembling
	PCodeReceiveSequenceNumber     ParameterNameCode = 0x07 // receive sequence number
	PCodeSequencingSegmenting      ParameterNameCode = 0x08 // sequencing/segmenting
	PCodeCredit                    ParameterNameCode = 0x09 // credit
	PCodeReleaseCause              ParameterNameCode = 0x0a // release cause
	PCodeReturnCause               ParameterNameCode = 0x0b // return cause
	PCodeResetCause                ParameterNameCode = 0x0c // reset cause
	PCodeErrorCause                ParameterNameCode = 0x0d // error cause
	PCodeRefusalCause              ParameterNameCode = 0x0e // refusal cause
	PCodeData                      ParameterNameCode = 0x0f // data
	PCodeSegmentation              ParameterNameCode = 0x10 // segmentation
	PCodeHopCounter                ParameterNameCode = 0x11 // hop counter
	PCodeImportance                ParameterNameCode = 0x12 // importance
	PCodeLongData                  ParameterNameCode = 0x13 // long data
	PCodeReserved                  ParameterNameCode = 0xff // reserved
)

// MaxVariableLength is the largest parameter content a one-octet length
// indicator can describe, ITU-T Q.713 (03/01) §2.2.
const MaxVariableLength = 255

// RawParameter is an optional parameter carried without interpretation.
//
// The decoder keeps every optional parameter it does not recognise for the
// message type here. ITU-T Q.714 (05/01) §1.1.4.2: unrecognized parameters are
// not acted upon, and an unrecognized optional parameter is transported
// transparently when the message is relayed.
type RawParameter struct {
	Code  ParameterNameCode
	Value []byte
}

// Validate reports whether r can be encoded as an optional parameter: its name
// is neither the end of optional parameters octet (ITU-T Q.713 (03/01) §3.1)
// nor the code reserved for future use (§1.9: "Code 11111111 is reserved for
// future use"), and its contents fit a one-octet length indicator (§2.2).
func (r RawParameter) Validate() error {
	if r.Code == PCodeEndOfOptionalParameters {
		return fmt.Errorf("%w: raw parameter cannot use the end of optional parameters code", ErrInvalidValue)
	}
	if r.Code == PCodeReserved {
		return fmt.Errorf("%w: parameter name %#02x is reserved", ErrInvalidValue, uint8(r.Code))
	}
	if len(r.Value) > MaxVariableLength {
		return fmt.Errorf("%w: raw parameter %s: %d octets exceeds %d", ErrInvalidValue, r.Code, len(r.Value), MaxVariableLength)
	}
	return nil
}

// String returns the parameter in a human readable form.
func (r RawParameter) String() string {
	return fmt.Sprintf("{%s: %x}", r.Code, r.Value)
}

// truncated reports parameter contents that end too early. It matches both
// ErrMalformed and io.ErrUnexpectedEOF.
func truncated(name string) error {
	return fmt.Errorf("%w: %s: %w", ErrMalformed, name, io.ErrUnexpectedEOF)
}

func checkLength(name string, b []byte, n int) error {
	switch {
	case len(b) < n:
		return truncated(name)
	case len(b) > n:
		return fmt.Errorf("%w: %s: %d octets, want %d", ErrMalformed, name, len(b), n)
	}
	return nil
}
