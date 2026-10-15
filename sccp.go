// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

/*
Package sccp implements the Signalling Connection Control Part of SS7: the ITU-T
Q.713 (03/01) message codecs and an end-node Q.714 (05/01) procedure stack.

# Messages

Every message type of ITU-T Q.713 (03/01) Table 1 has a struct with exported
fields only. Mandatory parameters are values; optional parameters are pointers
that are nil when absent. Optional parameters the decoder does not recognise for
the message type are kept in Unrecognized and are encoded again unchanged
(ITU-T Q.714 (05/01) §1.1.4.2), except those named with the reserved code
11111111 (ITU-T Q.713 (03/01) §1.9), which the encoder refuses.

Decoding is liberal. It accepts what ITU-T Q.713 (03/01) §1.4 and Q.714 (05/01)
§1.1.4 require a receiver to accept: gaps between parameters, parameters in an
order different from their pointers, octets after the last parameter, a missing
end of optional parameters octet, unrecognised optional parameters and spare
bits with any value. It places no limit on the message size.

Encoding is conservative. Messages are encoded in the canonical layout — no
gaps, parameters in pointer order, spare bits zero — and every value is checked
against the field that carries it: nothing is masked, wrapped or truncated. The
encoded message is checked against a maximum message size, by default
DefaultMaxMessageSize (268 octets); MarshalOptions selects another. A message
that ParseMessage decodes from the output of a successful encoding is equal to
the message that was encoded, a nil and an empty slice counting as equal.
*/
package sccp

import (
	"errors"
	"fmt"
	"io"
)

//go:generate stringer -type MsgType,SCMGType,SCCPService -linecomment -output constant_string.go

// MsgType is the SCCP message type code, ITU-T Q.713 (03/01) §2.1, Table 1.
type MsgType uint8

// Message type codes, ITU-T Q.713 (03/01) Table 1.
const (
	MsgTypeCR    MsgType = 0x01 // CR
	MsgTypeCC    MsgType = 0x02 // CC
	MsgTypeCREF  MsgType = 0x03 // CREF
	MsgTypeRLSD  MsgType = 0x04 // RLSD
	MsgTypeRLC   MsgType = 0x05 // RLC
	MsgTypeDT1   MsgType = 0x06 // DT1
	MsgTypeDT2   MsgType = 0x07 // DT2
	MsgTypeAK    MsgType = 0x08 // AK
	MsgTypeUDT   MsgType = 0x09 // UDT
	MsgTypeUDTS  MsgType = 0x0a // UDTS
	MsgTypeED    MsgType = 0x0b // ED
	MsgTypeEA    MsgType = 0x0c // EA
	MsgTypeRSR   MsgType = 0x0d // RSR
	MsgTypeRSC   MsgType = 0x0e // RSC
	MsgTypeERR   MsgType = 0x0f // ERR
	MsgTypeIT    MsgType = 0x10 // IT
	MsgTypeXUDT  MsgType = 0x11 // XUDT
	MsgTypeXUDTS MsgType = 0x12 // XUDTS
	MsgTypeLUDT  MsgType = 0x13 // LUDT
	MsgTypeLUDTS MsgType = 0x14 // LUDTS
)

// DefaultMaxMessageSize is the default maximum size of an encoded SCCP message,
// in octets: the 272-octet MTP-SDU of the narrowband MTP-SAP, which includes
// the 4-octet ITU-T routing label (ITU-T Q.711 (03/01) §7.1 a); Q.704 SIF).
const DefaultMaxMessageSize = 272 - 4

// MaxBroadbandMessageSize is the largest SCCP message the broadband MTP-SAP
// carries: a 4095-octet MTP-SDU including the 4-octet ITU-T routing label
// (ITU-T Q.711 (03/01) §7.1 b), Q.2210). Long unitdata messages need it.
const MaxBroadbandMessageSize = 4095 - 4

var (
	// ErrInvalidMessage reports a message that cannot be encoded as it is: a
	// value out of range, a parameter too long for its length indicator or a
	// pointer that cannot reach its parameter.
	ErrInvalidMessage = errors.New("sccp: invalid message")

	// ErrMessageTooLong reports an encoded message larger than the maximum
	// message size.
	ErrMessageTooLong = errors.New("sccp: message too long")

	// ErrMalformedMessage reports a message that cannot be decoded. Truncated
	// input also matches io.ErrUnexpectedEOF.
	ErrMalformedMessage = errors.New("sccp: malformed message")
)

// UnsupportedTypeError reports a message type code that ITU-T Q.713 (03/01)
// Table 1 does not define. ITU-T Q.714 (05/01) §1.1.4.2: such a message is
// discarded.
type UnsupportedTypeError uint8

// Error implements the error interface.
func (e UnsupportedTypeError) Error() string {
	return fmt.Sprintf("sccp: unsupported message type %#02x", uint8(e))
}

// Message is an SCCP message. Every implementation is defined in this package.
// A Message is an encoding.BinaryAppender, an encoding.BinaryMarshaler and an
// encoding.BinaryUnmarshaler.
type Message interface {
	// MessageType returns the message type code.
	MessageType() MsgType

	// MarshalBinary encodes the message, refusing it when it is larger than
	// DefaultMaxMessageSize.
	MarshalBinary() ([]byte, error)

	// AppendBinary appends the encoded message to b, refusing it when it is
	// larger than DefaultMaxMessageSize. On error b is returned unchanged. As
	// with any append, the spare capacity of b must not hold the message's own
	// byte slices.
	AppendBinary(b []byte) ([]byte, error)

	// UnmarshalBinary decodes a message of this type. On error the receiver
	// is left unchanged.
	UnmarshalBinary(b []byte) error

	fmt.Stringer

	appendMessage(b []byte) ([]byte, error)
}

// MarshalOptions configures message encoding.
type MarshalOptions struct {
	// MaxMessageSize is the largest encoded message accepted, in octets.
	// Zero selects DefaultMaxMessageSize. A network whose every MTP-SAP is
	// broadband can use up to MaxBroadbandMessageSize, the most any MTP-SAP
	// carries (ITU-T Q.711 (03/01) §7.1 b)).
	MaxMessageSize int
}

// Marshal encodes m.
func (o MarshalOptions) Marshal(m Message) ([]byte, error) {
	return o.Append(nil, m)
}

// Append appends the encoding of m to b. On error b is returned unchanged.
func (o MarshalOptions) Append(b []byte, m Message) ([]byte, error) {
	limit := o.MaxMessageSize
	if limit == 0 {
		limit = DefaultMaxMessageSize
	}
	if limit < 0 || limit > MaxBroadbandMessageSize {
		return b, fmt.Errorf("%w: maximum message size %d, want 0 to %d", ErrInvalidMessage, limit, MaxBroadbandMessageSize)
	}
	out, err := m.appendMessage(b)
	if err != nil {
		return b, err
	}
	if n := len(out) - len(b); n > limit {
		return b, fmt.Errorf("%w: %s of %d octets exceeds the maximum message size of %d octets", ErrMessageTooLong, m.MessageType(), n, limit)
	}
	return out, nil
}

func marshalDefault(m Message) ([]byte, error) { return MarshalOptions{}.Append(nil, m) }

func appendDefault(b []byte, m Message) ([]byte, error) { return MarshalOptions{}.Append(b, m) }

// ParseMessage decodes b as the SCCP message its message type code names.
func ParseMessage(b []byte) (Message, error) {
	if len(b) < 1 {
		return nil, fmt.Errorf("%w: empty message: %w", ErrMalformedMessage, io.ErrUnexpectedEOF)
	}
	var m Message
	switch MsgType(b[0]) {
	case MsgTypeCR:
		m = &CR{}
	case MsgTypeCC:
		m = &CC{}
	case MsgTypeCREF:
		m = &CREF{}
	case MsgTypeRLSD:
		m = &RLSD{}
	case MsgTypeRLC:
		m = &RLC{}
	case MsgTypeDT1:
		m = &DT1{}
	case MsgTypeDT2:
		m = &DT2{}
	case MsgTypeAK:
		m = &AK{}
	case MsgTypeUDT:
		m = &UDT{}
	case MsgTypeUDTS:
		m = &UDTS{}
	case MsgTypeED:
		m = &ED{}
	case MsgTypeEA:
		m = &EA{}
	case MsgTypeRSR:
		m = &RSR{}
	case MsgTypeRSC:
		m = &RSC{}
	case MsgTypeERR:
		m = &ERR{}
	case MsgTypeIT:
		m = &IT{}
	case MsgTypeXUDT:
		m = &XUDT{}
	case MsgTypeXUDTS:
		m = &XUDTS{}
	case MsgTypeLUDT:
		m = &LUDT{}
	case MsgTypeLUDTS:
		m = &LUDTS{}
	default:
		return nil, UnsupportedTypeError(b[0])
	}
	if err := m.UnmarshalBinary(b); err != nil {
		return nil, err
	}
	return m, nil
}
