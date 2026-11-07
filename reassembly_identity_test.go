// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gomaja/go-sccp/params"
)

func TestReassemblyIdentityFields(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[*UnitdataIndication]().Elem(), reflect.TypeFor[*Discard]().Elem()} {
		if _, ok := typ.FieldByName("Segmentation"); !ok {
			t.Errorf("%s has no Segmentation", typ)
		}
	}
	if !reflect.TypeFor[*Discard]().Elem().Comparable() {
		t.Fatal("Discard must stay comparable")
	}
}

func identityStack(t *testing.T, cfg StackConfig) (*Stack, *time.Time) {
	t.Helper()
	now := time.Unix(0, 0)
	cfg.PointCode = 100
	cfg.Now = func() time.Time { return now }
	s, err := NewStack(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.RegisterSubsystem(8)
	return s, &now
}

func identitySegment(ref uint32, first bool, rem uint8, n int) unitdata {
	return unitdata{label: RoutingLabel{OPC: 200, DPC: 100, SLS: 1}, typ: MsgTypeXUDT,
		class: params.ProtocolClass{Class: 1}, hop: 15, hasHop: true,
		called: params.NewPointCodeAddress(100, 8), calling: params.NewPointCodeAddress(200, 8),
		data:         bytes.Repeat([]byte{byte(ref)}, n),
		segmentation: &params.Segmentation{FirstSegment: first, InSequenceDelivery: true, RemainingSegments: rem, LocalReference: ref}}
}

func identityFeed(t *testing.T, s *Stack, u unitdata) Result {
	t.Helper()
	m := &XUDT{ProtocolClass: u.class, HopCounter: u.hop, CalledPartyAddress: u.called,
		CallingPartyAddress: u.calling, Data: u.data, Segmentation: u.segmentation}
	b, err := m.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return s.HandleTransfer(TransferIndication{RoutingLabel: u.label, Data: b})
}

func checkDiscardIdentity(t *testing.T, d Discard, u unitdata) {
	t.Helper()
	if d.RoutingLabel != u.label || d.MessageType != u.typ || d.CallingPartyAddress != u.calling || !reflect.DeepEqual(d.Segmentation, u.segmentation) {
		t.Fatalf("discard identity: %+v segmentation %+v, want %+v segmentation %+v", d, d.Segmentation, u, u.segmentation)
	}
	if errors.Is(d.Reason, ErrReassembly) && !strings.Contains(d.Reason.Error(), fmt.Sprintf("reference %#06x", u.segmentation.LocalReference)) {
		t.Fatalf("reason does not name reference: %v", d.Reason)
	}
}

// Two messages share the calling party and label, Q.714 (05/01) §4.1.1.2.2.
func TestReassemblyIdentityConcurrent(t *testing.T) {
	s, now := identityStack(t, StackConfig{})
	first := identitySegment(1, true, 2, 100)
	identityFeed(t, s, first)
	second := identitySegment(2, true, 1, 100)
	identityFeed(t, s, second)
	identityFeed(t, s, identitySegment(1, false, 1, 100))
	res := identityFeed(t, s, identitySegment(2, false, 0, 100))
	if len(res.Indications) != 1 {
		t.Fatalf("completion: %+v", res)
	}
	u := res.Indications[0].(*UnitdataIndication)
	if u.Segments != 2 || len(u.Data) != 200 || u.RoutingLabel != second.label || u.CallingPartyAddress != second.calling || !reflect.DeepEqual(u.Segmentation, second.segmentation) {
		t.Fatalf("completion identity: %+v", u)
	}
	*now = now.Add(DefaultReassemblyTimeout)
	res = s.HandleTimeout()
	if len(res.Discards) != 1 {
		t.Fatalf("expiry: %+v", res)
	}
	checkDiscardIdentity(t, res.Discards[0], first)
}

func TestReassemblyIdentityEnds(t *testing.T) {
	for _, roe := range []bool{false, true} {
		for _, name := range []string{"expiry", "sequence", "bound", "maximum", "buffer", "peer buffer", "subsystem", "busy", "slots", "first buffer", "first peer buffer", "first subsystem", "orphan", "record full", "failed rest", "failed first", "unknown first", "calling syntax"} {
			t.Run(fmt.Sprintf("%s/return=%v", name, roe), func(t *testing.T) {
				cfg := StackConfig{MaxMessageSize: MaxBroadbandMessageSize}
				switch name {
				case "buffer":
					cfg.MaxReassemblyOctets = 100
				case "peer buffer":
					cfg.MaxReassemblyOctetsPerPeer = 100
				case "first buffer":
					cfg.MaxReassemblyOctets = 99
				case "first peer buffer":
					cfg.MaxReassemblyOctetsPerPeer = 99
				case "slots", "record full":
					cfg.MaxReassemblies = 1
				}
				s, now := identityStack(t, cfg)
				first := identitySegment(0x123456, true, 2, 100)
				first.class.ReturnOnError = roe
				trigger := identitySegment(0x123456, false, 1, 100)
				trigger.class.ReturnOnError = roe
				var res Result
				var want []unitdata
				var cause params.ReturnCause
				cause = params.ReturnCauseErrorInMessageTransport
				switch name {
				case "expiry", "sequence", "bound", "maximum", "buffer", "peer buffer", "subsystem", "busy":
					if name == "maximum" {
						first = identitySegment(0x123456, true, 15, 250)
						first.class.ReturnOnError = roe
						trigger = identitySegment(0x123456, false, 0, 250)
						trigger.class.ReturnOnError = roe
					}
					s.reassemble(*now, &first, 8, &res)
					if !res.empty() {
						t.Fatalf("first: %+v", res)
					}
					if name == "maximum" {
						for rem := uint8(14); rem > 0; rem-- {
							middle := identitySegment(0x123456, false, rem, 250)
							s.reassemble(*now, &middle, 8, &res)
						}
						if !res.empty() {
							t.Fatalf("middle segments: %+v", res)
						}
					}
					switch name {
					case "expiry":
						*now = now.Add(DefaultReassemblyTimeout)
						res = s.HandleTimeout()
						want = []unitdata{first}
					default:
						switch name {
						case "sequence":
							trigger.segmentation.RemainingSegments = 0
						case "bound":
							trigger.data = make([]byte, 201)
						case "maximum":
							cause = params.ReturnCauseDestinationCannotPerformReassembly
						case "buffer", "peer buffer":
							cause = params.ReturnCauseNetworkCongestion
						case "subsystem":
							s.SetSubsystemAvailable(8, false)
							cause = params.ReturnCauseSubsystemFailure
						case "busy":
							trigger = identitySegment(0x123456, true, 1, 99)
							trigger.class.ReturnOnError = roe
						}
						s.reassemble(*now, &trigger, 8, &res)
						want = []unitdata{first, trigger}
					}
				case "slots":
					other := identitySegment(99, true, 1, 100)
					s.reassemble(*now, &other, 8, &res)
					trigger = first
					s.reassemble(*now, &trigger, 8, &res)
					want = []unitdata{trigger}
					cause = params.ReturnCauseNetworkCongestion
				case "first buffer", "first peer buffer", "first subsystem":
					if name == "first subsystem" {
						s.SetSubsystemAvailable(8, false)
						cause = params.ReturnCauseSubsystemFailure
					} else {
						cause = params.ReturnCauseNetworkCongestion
					}
					trigger = first
					s.reassemble(*now, &trigger, 8, &res)
					want = []unitdata{trigger}
				case "orphan", "record full", "failed rest", "failed first", "unknown first":
					switch name {
					case "record full":
						other := identitySegment(99, false, 0, 100)
						s.reassemble(*now, &other, 8, &res)
					case "failed rest", "failed first":
						s.reassemble(*now, &first, 8, &res)
						*now = now.Add(DefaultReassemblyTimeout)
						res = s.HandleTimeout()
						if name == "failed first" {
							trigger = first
						}
					case "unknown first":
						s.reassemble(*now, &trigger, 8, &res)
						trigger = first
					}
					res = Result{}
					s.reassemble(*now, &trigger, 8, &res)
					want = []unitdata{trigger}
				case "calling syntax":
					trigger.calling.HasPointCode = true
					trigger.calling.PointCode = params.MaxPointCode + 1
					s.reassemble(*now, &trigger, 8, &res)
					want = []unitdata{trigger}
				}
				if len(res.Discards) != len(want) {
					t.Fatalf("got %d discards, want %d: %+v", len(res.Discards), len(want), res)
				}
				for i, u := range want {
					checkDiscardIdentity(t, res.Discards[i], u)
					reason := ErrReassembly
					if name == "subsystem" || name == "first subsystem" {
						reason = ErrUndeliverable
					}
					if !errors.Is(res.Discards[i].Reason, reason) {
						t.Fatalf("reason: %v, want %v", res.Discards[i].Reason, reason)
					}
				}
				returns := roe && name != "record full" && name != "failed rest" && name != "failed first" && name != "unknown first" && name != "calling syntax"
				if !returns {
					if len(res.Transfers) != 0 {
						t.Fatalf("unexpected return: %+v", res)
					}
					return
				}
				if len(res.Transfers) != 1 {
					t.Fatalf("want one return: %+v", res)
				}
				returned, ok := res.Transfers[0].Message.(*XUDTS)
				if !ok {
					t.Fatalf("return: %T", res.Transfers[0].Message)
				}
				idx := 0
				if name == "busy" {
					idx = 1
				}
				if returned.ReturnCause != cause || !reflect.DeepEqual(returned.Segmentation, res.Discards[idx].Segmentation) {
					t.Fatalf("return differs from discard: %+v", returned)
				}
				// The returned message and discard own separate copies.
				saved := *res.Discards[idx].Segmentation
				returned.Segmentation.LocalReference++
				if *res.Discards[idx].Segmentation != saved {
					t.Fatal("return shares discard segmentation")
				}
			})
		}
	}
}

func TestUnitdataIdentitySingle(t *testing.T) {
	for _, typ := range []MsgType{MsgTypeUDT, MsgTypeXUDT, MsgTypeLUDT} {
		for _, path := range []string{"deliver", "subsystem", "class", "empty", "route discard", "route return", "return build"} {
			t.Run(fmt.Sprintf("%s/%s", typ, path), func(t *testing.T) {
				s, now := identityStack(t, StackConfig{MaxMessageSize: MaxBroadbandMessageSize})
				u := identitySegment(42, true, 0, 100)
				u.typ = typ
				if typ == MsgTypeUDT {
					u.segmentation = nil
				}
				switch path {
				case "subsystem":
					s.SetSubsystemAvailable(8, false)
				case "class":
					u.class.Class = 2
				case "empty":
					u.data = nil
				case "route discard":
					u.called = params.PartyAddress{RouteOnSSN: true}
				case "route return":
					u.called = params.NewSSNAddress(0)
					u.called.RouteOnSSN = false
				case "return build":
					s.SetSubsystemAvailable(8, false)
					u.class.ReturnOnError = true
					u.calling.HasPointCode = true
					u.calling.PointCode = params.MaxPointCode + 1
				}
				var m Message
				switch typ {
				case MsgTypeUDT:
					m = &UDT{ProtocolClass: u.class, CalledPartyAddress: u.called, CallingPartyAddress: u.calling, Data: u.data}
				case MsgTypeXUDT:
					m = &XUDT{ProtocolClass: u.class, HopCounter: u.hop, CalledPartyAddress: u.called, CallingPartyAddress: u.calling, Data: u.data, Segmentation: u.segmentation}
				case MsgTypeLUDT:
					m = &LUDT{ProtocolClass: u.class, HopCounter: u.hop, CalledPartyAddress: u.called, CallingPartyAddress: u.calling, Data: u.data, Segmentation: u.segmentation}
				}
				if path == "deliver" || path == "subsystem" {
					b, err := m.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					wire := s.HandleTransfer(TransferIndication{RoutingLabel: u.label, Data: b})
					if path == "deliver" {
						if len(wire.Indications) != 1 || !reflect.DeepEqual(wire.Indications[0].(*UnitdataIndication).Segmentation, u.segmentation) {
							t.Fatalf("wire indication: %+v", wire)
						}
					} else {
						if len(wire.Discards) != 1 {
							t.Fatalf("wire discard: %+v", wire)
						}
						checkDiscardIdentity(t, wire.Discards[0], u)
					}
				}
				var res Result
				// Exercise decoded input directly too: syntax and return construction
				// errors need values the encoder refuses.
				s.handleUnitdata(*now, u.label, m, true, &res)
				if path == "deliver" {
					if len(res.Indications) != 1 || len(res.Discards) != 0 {
						t.Fatalf("delivery: %+v", res)
					}
					ind := res.Indications[0].(*UnitdataIndication)
					if ind.Segments != 1 || !reflect.DeepEqual(ind.Segmentation, u.segmentation) {
						t.Fatalf("indication: %+v", ind)
					}
					if ind.Segmentation != nil {
						saved := *u.segmentation
						ind.Segmentation.LocalReference++
						var later Result
						s.handleUnitdata(*now, u.label, m, true, &later)
						if !reflect.DeepEqual(later.Indications[0].(*UnitdataIndication).Segmentation, &saved) {
							t.Fatal("indication changed input segmentation")
						}
					}
				} else {
					count := 1
					if path == "return build" {
						count = 2
					}
					if len(res.Discards) != count {
						t.Fatalf("discard: %+v", res)
					}
					for _, d := range res.Discards {
						checkDiscardIdentity(t, d, u)
					}
					if u.segmentation != nil {
						saved := *u.segmentation
						res.Discards[0].Segmentation.LocalReference++
						if *u.segmentation != saved {
							t.Fatal("discard changed input segmentation")
						}
						if count == 2 && *res.Discards[1].Segmentation != saved {
							t.Fatal("discards share segmentation")
						}
					}
				}
			})
		}
	}
}

func TestDiscardIdentityOtherMessages(t *testing.T) {
	for _, name := range []string{"empty", "undecodable", "UDTS", "XUDTS", "LUDTS", "DT1"} {
		t.Run(name, func(t *testing.T) {
			s, _ := identityStack(t, StackConfig{MaxMessageSize: MaxBroadbandMessageSize})
			s.SetSubsystemAvailable(8, false)
			u := identitySegment(42, true, 1, 100)
			var m Message
			var b []byte
			switch name {
			case "undecodable":
				b = []byte{byte(MsgTypeXUDT)}
			case "UDTS":
				m = &UDTS{CalledPartyAddress: u.called, CallingPartyAddress: u.calling, Data: u.data}
			case "XUDTS":
				m = &XUDTS{HopCounter: 15, CalledPartyAddress: u.called, CallingPartyAddress: u.calling, Data: u.data, Segmentation: u.segmentation}
			case "LUDTS":
				m = &LUDTS{HopCounter: 15, CalledPartyAddress: u.called, CallingPartyAddress: u.calling, Data: u.data, Segmentation: u.segmentation}
			case "DT1":
				m = &DT1{DestinationLocalReference: 1, Data: u.data}
			}
			if m != nil {
				var err error
				b, err = m.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
			}
			res := s.HandleTransfer(TransferIndication{RoutingLabel: u.label, Data: b})
			if len(res.Discards) != 1 {
				t.Fatalf("want discard: %+v", res)
			}
			d := res.Discards[0]
			if d.CallingPartyAddress != (params.PartyAddress{}) || d.Segmentation != nil {
				t.Fatalf("non-unitdata identity: %+v", d)
			}
		})
	}
}

func TestReassemblyIdentityCopies(t *testing.T) {
	s, now := identityStack(t, StackConfig{})
	first := identitySegment(42, true, 2, 100)
	var res Result
	s.reassemble(*now, &first, 8, &res)
	// A busy reference produces a discard while its first segment remains
	// the input to the message return procedure.
	newer := identitySegment(42, true, 1, 99)
	newer.class.ReturnOnError = true
	s.reassemble(*now, &newer, 8, &res)
	checkDiscardIdentity(t, res.Discards[0], first)
	checkDiscardIdentity(t, res.Discards[1], newer)
	res.Discards[1].Segmentation.LocalReference++
	later := Result{}
	s.reassemble(*now, &newer, 8, &later)
	checkDiscardIdentity(t, later.Discards[0], newer)
	if got := res.Transfers[0].Message.(*XUDTS).Segmentation; !reflect.DeepEqual(got, newer.segmentation) {
		t.Fatal("discard changed returned message")
	}
}

func TestReassemblyIdentityCompletionCopy(t *testing.T) {
	s, now := identityStack(t, StackConfig{})
	first := identitySegment(42, true, 1, 100)
	var res Result
	s.reassemble(*now, &first, 8, &res)
	last := identitySegment(42, false, 0, 100)
	s.reassemble(*now, &last, 8, &res)
	if len(res.Indications) != 1 {
		t.Fatalf("completion: %+v", res)
	}
	saved := *first.segmentation
	res.Indications[0].(*UnitdataIndication).Segmentation.LocalReference++
	if *first.segmentation != saved {
		t.Fatal("completion shares first segment's parameter")
	}
	res = Result{}
	s.reassemble(*now, &first, 8, &res)
	*now = now.Add(DefaultReassemblyTimeout)
	res = s.HandleTimeout()
	if len(res.Discards) != 1 {
		t.Fatalf("expiry after completion: %+v", res)
	}
	checkDiscardIdentity(t, res.Discards[0], first)
}

// A discard owns its calling party address: the returned XUDTS, built from
// the same segment, must not change when the discard is modified.
func TestDiscardOwnsCallingAddress(t *testing.T) {
	s, _ := identityStack(t, StackConfig{})
	s.SetSubsystemAvailable(8, false)
	gt, err := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "447700900099")
	if err != nil {
		t.Fatal(err)
	}
	u := identitySegment(7, true, 1, 100)
	u.calling = params.NewGlobalTitleAddress(gt, 8)
	u.class.ReturnOnError = true
	res := identityFeed(t, s, u)
	if len(res.Discards) != 1 || len(res.Transfers) != 1 {
		t.Fatalf("want one discard and one return: %+v", res)
	}
	d := res.Discards[0]
	if !reflect.DeepEqual(d.CallingPartyAddress, u.calling) {
		t.Fatalf("discard calling party %+v, want %+v", d.CallingPartyAddress, u.calling)
	}
	returned := res.Transfers[0].Message.(*XUDTS)
	want := returned.CalledPartyAddress.Clone()
	d.CallingPartyAddress.GlobalTitle.AddressInformation[0] ^= 0xff
	if !reflect.DeepEqual(returned.CalledPartyAddress, want) {
		t.Fatal("discard shares its calling party address with the returned message")
	}
}
