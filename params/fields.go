// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package params

import "fmt"

// ProtocolClass is the protocol class parameter, ITU-T Q.713 (03/01) §3.6.
type ProtocolClass struct {
	// Class is the protocol class in bits 1-4: 0 and 1 are connectionless,
	// 2 and 3 connection-oriented.
	Class uint8

	// ReturnOnError is the "return message on error" message handling of
	// bits 5-8 (coded 1000). It exists for the connectionless classes only;
	// bits 5-8 are spare for classes 2 and 3.
	ReturnOnError bool
}

const protocolClassReturnOnError = 0x80

// AppendBinary appends the one-octet parameter contents to b.
func (p ProtocolClass) AppendBinary(b []byte) ([]byte, error) {
	if p.Class > 3 {
		return b, fmt.Errorf("%w: protocol class %d, want 0 to 3", ErrInvalidValue, p.Class)
	}
	if p.ReturnOnError && p.Class > 1 {
		return b, fmt.Errorf("%w: return on error with connection-oriented protocol class %d", ErrInvalidValue, p.Class)
	}
	v := p.Class
	if p.ReturnOnError {
		v |= protocolClassReturnOnError
	}
	return append(b, v), nil
}

// MarshalBinary returns the parameter contents.
func (p ProtocolClass) MarshalBinary() ([]byte, error) { return p.AppendBinary(nil) }

// UnmarshalBinary decodes the one-octet parameter contents.
//
// Classes 4 to 15 are returned as received for the procedures to reject. For
// classes 0 and 1, bits 5-8 are the message handling code: 1000 is return
// message on error, and 0000 and the spare codes are no special options, the
// default (ITU-T Q.714 (05/01) §1.1.4.3: other values "may be reset to default
// values"). For classes 2 and 3 they are spare.
func (p *ProtocolClass) UnmarshalBinary(b []byte) error {
	if err := checkLength("protocol class", b, 1); err != nil {
		return err
	}
	p.Class = b[0] & 0x0f
	p.ReturnOnError = p.Class <= 1 && b[0]&0xf0 == protocolClassReturnOnError
	return nil
}

// String returns the protocol class in a human readable form.
func (p ProtocolClass) String() string {
	return fmt.Sprintf("{Class: %d, ReturnOnError: %t}", p.Class, p.ReturnOnError)
}

// LocalReference is a destination or source local reference, ITU-T Q.713
// (03/01) §3.2 and §3.3.
//
// It is a 24-bit value transmitted least significant octet first, the SS7
// order for multi-octet fields that Wireshark also uses for these fields. The
// value with all bits set is reserved for future use.
type LocalReference uint32

// LocalReferenceReserved is the reserved all-ones local reference.
const LocalReferenceReserved LocalReference = 0xffffff

// AppendBinary appends the three-octet parameter contents to b.
func (r LocalReference) AppendBinary(b []byte) ([]byte, error) {
	if r > LocalReferenceReserved {
		return b, fmt.Errorf("%w: local reference %#x exceeds 24 bits", ErrInvalidValue, uint32(r))
	}
	if r == LocalReferenceReserved {
		return b, fmt.Errorf("%w: local reference %#x is reserved", ErrInvalidValue, uint32(r))
	}
	return appendUint24(b, uint32(r)), nil
}

// MarshalBinary returns the parameter contents.
func (r LocalReference) MarshalBinary() ([]byte, error) { return r.AppendBinary(nil) }

// UnmarshalBinary decodes the three-octet parameter contents.
func (r *LocalReference) UnmarshalBinary(b []byte) error {
	if err := checkLength("local reference", b, 3); err != nil {
		return err
	}
	*r = LocalReference(uint24(b))
	return nil
}

// Credit is the credit parameter, ITU-T Q.713 (03/01) §3.10: the window size
// of protocol class 3, coded in pure binary.
type Credit uint8

// MaxCredit is the largest window size: "The maximum size shall not exceed
// 127", ITU-T Q.714 (05/01) §3.5.2.3.
const MaxCredit Credit = 127

// AppendBinary appends the one-octet parameter contents to b.
func (c Credit) AppendBinary(b []byte) ([]byte, error) {
	if c > MaxCredit {
		return b, fmt.Errorf("%w: credit %d exceeds %d", ErrInvalidValue, uint8(c), uint8(MaxCredit))
	}
	return append(b, uint8(c)), nil
}

// MarshalBinary returns the parameter contents.
func (c Credit) MarshalBinary() ([]byte, error) { return c.AppendBinary(nil) }

// UnmarshalBinary decodes the one-octet parameter contents.
func (c *Credit) UnmarshalBinary(b []byte) error {
	if err := checkLength("credit", b, 1); err != nil {
		return err
	}
	*c = Credit(b[0])
	return nil
}

// HopCounter is the hop counter parameter, ITU-T Q.713 (03/01) §3.18.
//
// It is decremented on each global title translation and "should be in range
// 15 to 1", so the encoder refuses 0 and values above 15. The decoder accepts
// any value and leaves its treatment to the procedures.
type HopCounter uint8

// MaxHopCounter is the largest hop counter the encoder accepts, and the usual
// value set by an originating node, ITU-T Q.713 (03/01) §3.18.
const MaxHopCounter HopCounter = 15

// AppendBinary appends the one-octet parameter contents to b.
func (h HopCounter) AppendBinary(b []byte) ([]byte, error) {
	if h == 0 || h > MaxHopCounter {
		return b, fmt.Errorf("%w: hop counter %d, want 1 to %d", ErrInvalidValue, uint8(h), uint8(MaxHopCounter))
	}
	return append(b, uint8(h)), nil
}

// MarshalBinary returns the parameter contents.
func (h HopCounter) MarshalBinary() ([]byte, error) { return h.AppendBinary(nil) }

// UnmarshalBinary decodes the one-octet parameter contents.
func (h *HopCounter) UnmarshalBinary(b []byte) error {
	if err := checkLength("hop counter", b, 1); err != nil {
		return err
	}
	*h = HopCounter(b[0])
	return nil
}

// Importance is the importance parameter, ITU-T Q.713 (03/01) §3.19: 0 for the
// least important message to 7 for the most important, in bits 1-3.
type Importance uint8

// MaxImportance is the most important value, ITU-T Q.713 (03/01) §3.19.
const MaxImportance Importance = 7

// AppendBinary appends the one-octet parameter contents to b.
func (i Importance) AppendBinary(b []byte) ([]byte, error) {
	if i > MaxImportance {
		return b, fmt.Errorf("%w: importance %d, want 0 to %d", ErrInvalidValue, uint8(i), uint8(MaxImportance))
	}
	return append(b, uint8(i)), nil
}

// MarshalBinary returns the parameter contents.
func (i Importance) MarshalBinary() ([]byte, error) { return i.AppendBinary(nil) }

// UnmarshalBinary decodes the one-octet parameter contents. Bits 4-8 are spare.
func (i *Importance) UnmarshalBinary(b []byte) error {
	if err := checkLength("importance", b, 1); err != nil {
		return err
	}
	*i = Importance(b[0] & 0x07)
	return nil
}

// MaxSequenceNumber is the largest send or receive sequence number: protocol
// class 3 numbers messages modulo 128 in seven bits, ITU-T Q.713 (03/01) §3.8
// and §3.9.
const MaxSequenceNumber = 127

// SequencingSegmenting is the sequencing/segmenting parameter, ITU-T Q.713
// (03/01) §3.9, used by protocol class 3.
type SequencingSegmenting struct {
	// SendSequenceNumber is P(S), 0 to 127 (octet 1, bits 8-2).
	SendSequenceNumber uint8
	// ReceiveSequenceNumber is P(R), 0 to 127 (octet 2, bits 8-2).
	ReceiveSequenceNumber uint8
	// MoreData is the M bit (octet 2, bit 1).
	MoreData bool
}

// AppendBinary appends the two-octet parameter contents to b.
func (s SequencingSegmenting) AppendBinary(b []byte) ([]byte, error) {
	if s.SendSequenceNumber > MaxSequenceNumber {
		return b, fmt.Errorf("%w: send sequence number %d exceeds %d", ErrInvalidValue, s.SendSequenceNumber, MaxSequenceNumber)
	}
	if s.ReceiveSequenceNumber > MaxSequenceNumber {
		return b, fmt.Errorf("%w: receive sequence number %d exceeds %d", ErrInvalidValue, s.ReceiveSequenceNumber, MaxSequenceNumber)
	}
	second := s.ReceiveSequenceNumber << 1
	if s.MoreData {
		second |= 1
	}
	return append(b, s.SendSequenceNumber<<1, second), nil
}

// MarshalBinary returns the parameter contents.
func (s SequencingSegmenting) MarshalBinary() ([]byte, error) { return s.AppendBinary(nil) }

// UnmarshalBinary decodes the two-octet parameter contents. Bit 1 of octet 1
// is spare.
func (s *SequencingSegmenting) UnmarshalBinary(b []byte) error {
	if err := checkLength("sequencing/segmenting", b, 2); err != nil {
		return err
	}
	s.SendSequenceNumber = b[0] >> 1
	s.ReceiveSequenceNumber = b[1] >> 1
	s.MoreData = b[1]&1 != 0
	return nil
}

// String returns the parameter in a human readable form.
func (s SequencingSegmenting) String() string {
	return fmt.Sprintf("{P(S): %d, P(R): %d, MoreData: %t}", s.SendSequenceNumber, s.ReceiveSequenceNumber, s.MoreData)
}

// MaxRemainingSegments is the largest remaining segment count: a message is
// split into at most 16 segments, ITU-T Q.713 (03/01) §3.17 and Q.714 (05/01)
// §4.1.1.1.2.
const MaxRemainingSegments = 15

// MaxSegmentationLocalReference is the largest 24-bit segmentation local
// reference, ITU-T Q.713 (03/01) §3.17.
const MaxSegmentationLocalReference = 0xffffff

// Segmentation is the segmentation parameter, ITU-T Q.713 (03/01) §3.17.
type Segmentation struct {
	// FirstSegment is the F bit (octet 1, bit 8), set in the first segment
	// only.
	FirstSegment bool

	// InSequenceDelivery is the C bit (octet 1, bit 7): the in-sequence
	// delivery option of the SCCP user, set when class 1 was selected.
	InSequenceDelivery bool

	// RemainingSegments is the number of segments after this one, 0 to 15
	// (octet 1, bits 4-1); 0 marks the last segment.
	RemainingSegments uint8

	// LocalReference is the 24-bit segmentation local reference (octets 2-4),
	// transmitted least significant octet first.
	LocalReference uint32
}

// AppendBinary appends the four-octet parameter contents to b.
func (s Segmentation) AppendBinary(b []byte) ([]byte, error) {
	if s.RemainingSegments > MaxRemainingSegments {
		return b, fmt.Errorf("%w: remaining segments %d exceeds %d", ErrInvalidValue, s.RemainingSegments, MaxRemainingSegments)
	}
	if s.LocalReference > MaxSegmentationLocalReference {
		return b, fmt.Errorf("%w: segmentation local reference %#x exceeds 24 bits", ErrInvalidValue, s.LocalReference)
	}
	first := s.RemainingSegments
	if s.FirstSegment {
		first |= 0x80
	}
	if s.InSequenceDelivery {
		first |= 0x40
	}
	return appendUint24(append(b, first), s.LocalReference), nil
}

// MarshalBinary returns the parameter contents.
func (s Segmentation) MarshalBinary() ([]byte, error) { return s.AppendBinary(nil) }

// UnmarshalBinary decodes the four-octet parameter contents. Bits 6-5 of
// octet 1 are spare.
func (s *Segmentation) UnmarshalBinary(b []byte) error {
	if err := checkLength("segmentation", b, 4); err != nil {
		return err
	}
	s.FirstSegment = b[0]&0x80 != 0
	s.InSequenceDelivery = b[0]&0x40 != 0
	s.RemainingSegments = b[0] & 0x0f
	s.LocalReference = uint24(b[1:4])
	return nil
}

// String returns the parameter in a human readable form.
func (s Segmentation) String() string {
	return fmt.Sprintf("{FirstSegment: %t, InSequenceDelivery: %t, RemainingSegments: %d, LocalReference: %#06x}",
		s.FirstSegment, s.InSequenceDelivery, s.RemainingSegments, s.LocalReference)
}

func appendUint24(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16))
}

func uint24(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
}
