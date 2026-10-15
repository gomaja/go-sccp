// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp_test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

// FuzzParseMessage checks that decoding never panics and that a decoded
// message which encodes again decodes to itself.
func FuzzParseMessage(f *testing.F) {
	for _, v := range vectors {
		f.Add(unhex(f, v.wire))
	}
	f.Fuzz(func(t *testing.T, wire []byte) {
		m, err := sccp.ParseMessage(wire)
		if err != nil {
			if _, unsupported := errors.AsType[sccp.UnsupportedTypeError](err); !errors.Is(err, sccp.ErrMalformedMessage) && !unsupported {
				t.Fatalf("ParseMessage(% x) returned an unclassified error: %v", wire, err)
			}
			return
		}
		_ = m.String()
		b, err := (sccp.MarshalOptions{MaxMessageSize: sccp.MaxBroadbandMessageSize}).Marshal(m)
		if err != nil {
			// The decoder accepts values the encoder refuses, such as hop
			// counter 0 or empty data, and messages of any size; that is
			// liberal decoding.
			if !errors.Is(err, sccp.ErrInvalidMessage) && !errors.Is(err, sccp.ErrMessageTooLong) {
				t.Fatalf("re-encoding %v: %v", m, err)
			}
			return
		}
		again, err := sccp.ParseMessage(b)
		if err != nil {
			t.Fatalf("re-encoded % x of %v does not decode: %v", b, m, err)
		}
		if diff := cmp.Diff(m, again, cmpOpts); diff != "" {
			t.Fatalf("re-encoding changed the message (-decoded +re-decoded):\n%s", diff)
		}
	})
}

// FuzzMarshalRoundTrip builds messages from the fuzz input, including values
// out of range, and checks that Parse(Marshal(m)) equals m whenever Marshal
// succeeds: the encoder either reports a value it cannot carry or carries it
// exactly, never changing it silently.
func FuzzMarshalRoundTrip(f *testing.F) {
	f.Add([]byte{0}, uint16(0))
	f.Add([]byte{8, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, uint16(268))
	f.Add([]byte{16, 255, 255, 255, 0, 0, 0, 1, 1, 1}, uint16(4091))
	f.Add([]byte{18, 1, 0x40, 0x10, 200, 7, 9, 1, 0, 0x80, 0xff}, uint16(300))
	f.Fuzz(func(t *testing.T, seed []byte, maxSize uint16) {
		m := newGen(seed).message()
		opts := sccp.MarshalOptions{MaxMessageSize: int(maxSize)}
		b, err := opts.Marshal(m)
		if err != nil {
			if !errors.Is(err, sccp.ErrInvalidMessage) && !errors.Is(err, sccp.ErrMessageTooLong) {
				t.Fatalf("Marshal(%v): unclassified error %v", m, err)
			}
			return
		}
		limit := int(maxSize)
		if limit == 0 {
			limit = sccp.DefaultMaxMessageSize
		}
		if len(b) > limit {
			t.Fatalf("Marshal(%v) = %d octets, above the maximum %d", m, len(b), limit)
		}
		back, err := sccp.ParseMessage(b)
		if err != nil {
			t.Fatalf("Marshal(%v) = % x, which does not decode: %v", m, b, err)
		}
		if diff := cmp.Diff(m, back, cmpOpts); diff != "" {
			t.Fatalf("Parse(Marshal(m)) != m (-m +parsed):\n%s\nwire % x", diff, b)
		}
	})
}

// FuzzSCMG checks the SCMG codec the same way.
func FuzzSCMG(f *testing.F) {
	f.Add([]byte{0x03, 0x08, 0x34, 0x12, 0x00})
	f.Add([]byte{0x06, 0x01, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, wire []byte) {
		var s sccp.SCMG
		if err := s.UnmarshalBinary(wire); err != nil {
			return
		}
		b, err := s.MarshalBinary()
		if err != nil {
			// The decoder returns congestion levels 0 and 9-15 as received.
			return
		}
		var again sccp.SCMG
		if err := again.UnmarshalBinary(b); err != nil || again != s {
			t.Fatalf("SCMG %v -> % x -> %v, %v", s, b, again, err)
		}
	})
}

// gen draws message fields from fuzz input, cycling through it so that short
// inputs still produce long parameters. One message in eight is drawn in chaos
// mode, where every field may be out of range; the others are valid field by
// field, so that the round trip is exercised on messages that encode.
type gen struct {
	b     []byte
	i     int
	chaos bool
}

func newGen(seed []byte) *gen {
	g := &gen{b: seed}
	g.chaos = g.u8()%8 == 0
	return g
}

func (g *gen) u8() uint8 {
	if len(g.b) == 0 {
		return 0
	}
	v := g.b[g.i%len(g.b)] + uint8(g.i/len(g.b))
	g.i++
	return v
}

func (g *gen) u16() uint16 { return uint16(g.u8())<<8 | uint16(g.u8()) }
func (g *gen) u32() uint32 { return uint32(g.u16())<<16 | uint32(g.u16()) }
func (g *gen) flag() bool  { return g.u8()&1 == 1 }

// bad reports whether a chaos-mode field should be out of range.
func (g *gen) bad() bool { return g.chaos && g.u8()%4 == 0 }

// upTo returns a value in 0..max, or just above max in chaos mode.
func (g *gen) upTo(max uint8) uint8 {
	if g.bad() {
		return max + 1 + g.u8()%4
	}
	return g.u8() % (max + 1)
}

func (g *gen) bytes(n int) []byte {
	if n == 0 {
		return nil
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = g.u8()
	}
	return out
}

// length returns a length in 1..max, favouring the edges, or in chaos mode
// sometimes 0 or max+1.
func (g *gen) length(max int) int {
	if g.bad() {
		if g.flag() {
			return 0
		}
		return max + 1
	}
	switch g.u8() % 4 {
	case 0:
		return 1
	case 1:
		return max
	default:
		return 1 + int(g.u16())%max
	}
}

func (g *gen) data(max int) []byte { return g.bytes(g.length(max)) }

// optData returns optional data: absent, or 1..max octets.
func (g *gen) optData(max int) []byte {
	if !g.flag() {
		return nil
	}
	return g.data(max)
}

func (g *gen) address() params.PartyAddress {
	a := params.PartyAddress{RouteOnSSN: g.flag(), NationalUse: g.u8()%8 == 0, HasPointCode: g.flag(), HasSSN: g.flag()}
	if a.HasPointCode {
		a.PointCode = g.u16() & 0x3fff
	}
	if a.HasSSN {
		a.SSN = g.u8()
	}
	if g.bad() {
		a.PointCode = g.u16() | 0x4000
	}
	if g.flag() {
		a.GlobalTitle = g.globalTitle()
	}
	return a
}

func (g *gen) globalTitle() *params.GlobalTitle {
	gt := &params.GlobalTitle{Indicator: params.GlobalTitleIndicator(1 + g.u8()%15)}
	switch gt.Indicator {
	case params.GTINAIOnly:
		gt.NatureOfAddress = params.NatureOfAddressIndicator(g.upTo(0x7f))
		gt.OddDigits = g.flag()
	case params.GTITTOnly:
		gt.TranslationType = g.u8()
	case params.GTITTNPES, params.GTITTNPESNAI:
		gt.TranslationType = g.u8()
		gt.NumberingPlan = params.NumberingPlan(g.upTo(15))
		gt.EncodingScheme = params.EncodingScheme(g.upTo(15))
		if gt.Indicator == params.GTITTNPESNAI {
			gt.NatureOfAddress = params.NatureOfAddressIndicator(g.upTo(0x7f))
		}
	}
	if g.bad() {
		gt.Indicator = params.GlobalTitleIndicator(g.u8() % 17)
		gt.OddDigits = true
	}
	n := int(g.u8() % 12)
	if g.u8()%16 == 0 {
		n = 245 + int(g.u8()%8) // around the 255-octet address limit
	}
	gt.AddressInformation = g.bytes(n)
	return gt
}

func (g *gen) optAddress() *params.PartyAddress {
	if !g.flag() {
		return nil
	}
	a := g.address()
	return &a
}

func (g *gen) class(connectionOriented bool) params.ProtocolClass {
	if g.bad() {
		return params.ProtocolClass{Class: g.u8() % 6, ReturnOnError: g.flag()}
	}
	if connectionOriented {
		return params.ProtocolClass{Class: 2 + g.u8()%2}
	}
	return params.ProtocolClass{Class: g.u8() % 2, ReturnOnError: g.flag()}
}

func (g *gen) ref() params.LocalReference {
	if g.bad() {
		return params.LocalReference(0xffffff + uint32(g.u8()%2)<<24)
	}
	return params.LocalReference(g.u32() % 0xffffff)
}

func (g *gen) hop() params.HopCounter {
	if g.bad() {
		return params.HopCounter(g.u8() % 2 * 16)
	}
	return params.HopCounter(1 + g.u8()%15)
}

func (g *gen) optImportance() *params.Importance {
	if !g.flag() {
		return nil
	}
	i := params.Importance(g.upTo(7))
	return &i
}

func (g *gen) optSegmentation() *params.Segmentation {
	if !g.flag() {
		return nil
	}
	s := params.Segmentation{FirstSegment: g.flag(), InSequenceDelivery: g.flag(), RemainingSegments: g.upTo(15), LocalReference: g.u32() & 0xffffff}
	if g.bad() {
		s.LocalReference |= 0x1000000
	}
	return &s
}

func (g *gen) optCredit() *params.Credit {
	if !g.flag() {
		return nil
	}
	c := params.Credit(g.u8())
	return &c
}

func (g *gen) optHop() *params.HopCounter {
	if !g.flag() {
		return nil
	}
	h := g.hop()
	return &h
}

// raws returns unrecognised optional parameters. Outside chaos mode their codes
// avoid every code a message type recognises.
func (g *gen) raws() []params.RawParameter {
	if g.u8()%4 != 0 {
		return nil
	}
	var out []params.RawParameter
	for range 1 + g.u8()%2 {
		code := params.ParameterNameCode(0x20 + g.u8()%0xdf)
		if g.bad() {
			code = params.ParameterNameCode(g.u8() % 0x20)
		}
		out = append(out, params.RawParameter{Code: code, Value: g.bytes(int(g.u8() % 6))})
	}
	return out
}

func (g *gen) sequencing() params.SequencingSegmenting {
	return params.SequencingSegmenting{SendSequenceNumber: g.upTo(127), ReceiveSequenceNumber: g.upTo(127), MoreData: g.flag()}
}

func (g *gen) message() sccp.Message {
	switch g.u8() % 20 {
	case 0:
		return &sccp.CR{SourceLocalReference: g.ref(), ProtocolClass: g.class(true), CalledPartyAddress: g.address(),
			Credit: g.optCredit(), CallingPartyAddress: g.optAddress(), Data: g.optData(128),
			HopCounter: g.optHop(), Importance: g.optImportance(), Unrecognized: g.raws()}
	case 1:
		return &sccp.CC{DestinationLocalReference: g.ref(), SourceLocalReference: g.ref(), ProtocolClass: g.class(true),
			Credit: g.optCredit(), CalledPartyAddress: g.optAddress(), Data: g.optData(128), Importance: g.optImportance(), Unrecognized: g.raws()}
	case 2:
		return &sccp.CREF{DestinationLocalReference: g.ref(), RefusalCause: params.RefusalCause(g.u8()),
			CalledPartyAddress: g.optAddress(), Data: g.optData(128), Importance: g.optImportance(), Unrecognized: g.raws()}
	case 3:
		return &sccp.RLSD{DestinationLocalReference: g.ref(), SourceLocalReference: g.ref(), ReleaseCause: params.ReleaseCause(g.u8()),
			Data: g.optData(128), Importance: g.optImportance(), Unrecognized: g.raws()}
	case 4:
		return &sccp.RLC{DestinationLocalReference: g.ref(), SourceLocalReference: g.ref()}
	case 5:
		return &sccp.DT1{DestinationLocalReference: g.ref(), MoreData: g.flag(), Data: g.data(255)}
	case 6:
		return &sccp.DT2{DestinationLocalReference: g.ref(), SequencingSegmenting: g.sequencing(), Data: g.data(255)}
	case 7:
		return &sccp.AK{DestinationLocalReference: g.ref(), ReceiveSequenceNumber: g.upTo(127), Credit: params.Credit(g.u8())}
	case 8:
		return &sccp.UDT{ProtocolClass: g.class(false), CalledPartyAddress: g.address(), CallingPartyAddress: g.address(), Data: g.data(255)}
	case 9:
		return &sccp.UDTS{ReturnCause: params.ReturnCause(g.u8()), CalledPartyAddress: g.address(), CallingPartyAddress: g.address(), Data: g.data(255)}
	case 10:
		return &sccp.ED{DestinationLocalReference: g.ref(), Data: g.data(32)}
	case 11:
		return &sccp.EA{DestinationLocalReference: g.ref()}
	case 12:
		return &sccp.RSR{DestinationLocalReference: g.ref(), SourceLocalReference: g.ref(), ResetCause: params.ResetCause(g.u8()), Unrecognized: g.raws()}
	case 13:
		return &sccp.RSC{DestinationLocalReference: g.ref(), SourceLocalReference: g.ref()}
	case 14:
		return &sccp.ERR{DestinationLocalReference: g.ref(), ErrorCause: params.ErrorCause(g.u8()), Unrecognized: g.raws()}
	case 15:
		return &sccp.IT{DestinationLocalReference: g.ref(), SourceLocalReference: g.ref(), ProtocolClass: g.class(true),
			SequencingSegmenting: g.sequencing(), Credit: params.Credit(g.u8())}
	case 16:
		return &sccp.XUDT{ProtocolClass: g.class(false), HopCounter: g.hop(), CalledPartyAddress: g.address(), CallingPartyAddress: g.address(),
			Data: g.data(255), Segmentation: g.optSegmentation(), Importance: g.optImportance(), Unrecognized: g.raws()}
	case 17:
		return &sccp.XUDTS{ReturnCause: params.ReturnCause(g.u8()), HopCounter: g.hop(), CalledPartyAddress: g.address(), CallingPartyAddress: g.address(),
			Data: g.data(255), Segmentation: g.optSegmentation(), Importance: g.optImportance(), Unrecognized: g.raws()}
	case 18:
		return &sccp.LUDT{ProtocolClass: g.class(false), HopCounter: g.hop(), CalledPartyAddress: g.address(), CallingPartyAddress: g.address(),
			Data: g.data(sccp.MaxLongData), Segmentation: g.optSegmentation(), Importance: g.optImportance(), Unrecognized: g.raws()}
	default:
		return &sccp.LUDTS{ReturnCause: params.ReturnCause(g.u8()), HopCounter: g.hop(), CalledPartyAddress: g.address(), CallingPartyAddress: g.address(),
			Data: g.data(sccp.MaxLongData), Segmentation: g.optSegmentation(), Importance: g.optImportance(), Unrecognized: g.raws()}
	}
}
