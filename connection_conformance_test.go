// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/gomaja/go-sccp/params"
)

// ITU-T Q.714 (05/01) §3.1.4.2 items 1, 3 and Table B.3 c3:
// an outgoing request has no associated source reference or originating point code.
func TestConnectionPendingUnassociatedPeer(t *testing.T) {
	for _, kind := range []MsgType{MsgTypeRLSD, MsgTypeRLC, MsgTypeERR, MsgTypeDT1, MsgTypeIT, MsgTypeDT2, MsgTypeAK, MsgTypeED, MsgTypeEA, MsgTypeRSR, MsgTypeRSC} {
		t.Run(kind.String(), func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, _ := coTestOutgoing(t, s)
			c := s.conn.sections[ref]
			est := c.establishment
			if c.RemoteReference != 0 || c.Remote.DPC != 2 {
				t.Fatalf("unrealistic c3 fixture: %+v", c.ConnectionInfo)
			}
			m := conformanceMessage(kind, ref, 99)
			label := RoutingLabel{OPC: 3, DPC: 1, SLS: 13}
			r := coTestReceive(t, s, label, m)
			class3 := kind == MsgTypeDT2 || kind == MsgTypeAK || kind == MsgTypeED || kind == MsgTypeEA || kind == MsgTypeRSR || kind == MsgTypeRSC
			if class3 {
				coTestCounts(t, r, 0, 0, 1)
				coTestState(t, s, ref, ConnectionOutgoingPending)
				if c.establishment != est || est.index < 0 {
					t.Fatal("unknown class 3 input changed T(conn est)")
				}
				return
			}
			wantTransfers := 0
			if kind == MsgTypeRLSD {
				wantTransfers = 1
				coTestTransfer(t, r, 0, replyLabel(label), &RLC{DestinationLocalReference: 99, SourceLocalReference: ref})
			}
			coTestCounts(t, r, wantTransfers, 1, 1)
			want := &DisconnectIndication{LocalReference: ref, Originator: OriginatorProvider, Refused: true, RefusalCause: params.RefusalCauseUnqualified}
			if !reflect.DeepEqual(r.Indications[0], want) {
				t.Fatalf("c3 failure indication: %#v", r.Indications[0])
			}
			coTestState(t, s, ref, ConnectionFrozen)
			if est.index >= 0 || s.conn.active != 0 {
				t.Fatal("c3 failure retained section or establishment timer")
			}
		})
	}
}

func conformanceMessage(kind MsgType, ref, src params.LocalReference) Message {
	switch kind {
	case MsgTypeRLSD:
		return &RLSD{DestinationLocalReference: ref, SourceLocalReference: src}
	case MsgTypeRLC:
		return &RLC{DestinationLocalReference: ref, SourceLocalReference: src}
	case MsgTypeERR:
		return &ERR{DestinationLocalReference: ref}
	case MsgTypeDT1:
		return &DT1{DestinationLocalReference: ref, Data: []byte{1}}
	case MsgTypeIT:
		return &IT{DestinationLocalReference: ref, SourceLocalReference: src, ProtocolClass: params.ProtocolClass{Class: 2}}
	case MsgTypeDT2:
		return &DT2{DestinationLocalReference: ref, Data: []byte{1}}
	case MsgTypeAK:
		return &AK{DestinationLocalReference: ref}
	case MsgTypeED:
		return &ED{DestinationLocalReference: ref, Data: []byte{1}}
	case MsgTypeEA:
		return &EA{DestinationLocalReference: ref}
	case MsgTypeRSR:
		return &RSR{DestinationLocalReference: ref, SourceLocalReference: src}
	case MsgTypeRSC:
		return &RSC{DestinationLocalReference: ref, SourceLocalReference: src}
	default:
		panic("unsupported conformance message")
	}
}

// ITU-T Q.714 (05/01) Figure C.2 sheet 3, Q.711 (03/01) Figure 8:
// wait for setup to end after N-DISCONNECT and never indicate the user's own release.
func TestConnectionDeferredDisconnectFailures(t *testing.T) {
	for _, event := range []string{"CC2", "CC3", "CREF", "timeout", "RLSD", "RLC", "ERR", "DT1", "IT"} {
		t.Run(event, func(t *testing.T) {
			s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, label := coTestOutgoing(t, s)
			importance := params.Importance(2)
			req := DisconnectRequest{ReleaseCause: params.ReleaseCauseEndUserOriginated, Data: []byte{0x41}, Importance: &importance}
			r, err := s.Disconnect(ref, req)
			if err != nil {
				t.Fatal(err)
			}
			coTestCounts(t, r, 0, 0, 0)
			est := s.conn.sections[ref].establishment
			receive := RoutingLabel{OPC: 3, DPC: 1, SLS: 14}
			wantTransfers, wantDiscards := 0, 0
			switch event {
			case "CC2", "CC3":
				class := uint8(2)
				if event == "CC3" {
					class, wantDiscards = 3, 1
				}
				r = coTestReceive(t, s, receive, &CC{DestinationLocalReference: ref, SourceLocalReference: 99, ProtocolClass: params.ProtocolClass{Class: class}})
				label.DPC = 3
				wantTransfers = 1
				coTestTransfer(t, r, 0, label, &RLSD{DestinationLocalReference: 99, SourceLocalReference: ref, ReleaseCause: req.ReleaseCause, Data: req.Data, Importance: req.Importance})
				coTestState(t, s, ref, ConnectionReleasePending)
			case "CREF":
				r = coTestReceive(t, s, receive, &CREF{DestinationLocalReference: ref, RefusalCause: params.RefusalCauseEndUserFailure})
			case "timeout":
				clock.advance(s.cfg.Connection.EstablishmentTimeout)
				r = s.HandleTimeout()
			default:
				kind := map[string]MsgType{"RLSD": MsgTypeRLSD, "RLC": MsgTypeRLC, "ERR": MsgTypeERR, "DT1": MsgTypeDT1, "IT": MsgTypeIT}[event]
				r = coTestReceive(t, s, receive, conformanceMessage(kind, ref, 99))
				wantDiscards = 1
				if event == "RLSD" {
					wantTransfers = 1
					coTestTransfer(t, r, 0, replyLabel(receive), &RLC{DestinationLocalReference: 99, SourceLocalReference: ref})
				}
			}
			coTestCounts(t, r, wantTransfers, 0, wantDiscards)
			if est.index >= 0 {
				t.Fatal("setup failure retained T(conn est)")
			}
			if event != "CC2" && event != "CC3" {
				coTestState(t, s, ref, ConnectionFrozen)
			}
		})
	}
}

// ITU-T Q.714 (05/01) Table B.2 Note 3, §3.4 Table 3: release the associated
// incoming section as well as responding with the received references.
func TestConnectionIncomingITSourceMismatchReleases(t *testing.T) {
	s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
	ref := coTestIncoming(t, s, 2)
	c := s.conn.sections[ref]
	label := RoutingLabel{OPC: 2, DPC: 1, SLS: 12}
	r := coTestReceive(t, s, label, &IT{DestinationLocalReference: ref, SourceLocalReference: 99, ProtocolClass: params.ProtocolClass{Class: 2}})
	coTestCounts(t, r, 2, 1, 1)
	coTestTransfer(t, r, 0, replyLabel(label), &RLSD{DestinationLocalReference: 99, SourceLocalReference: ref, ReleaseCause: params.ReleaseCauseInconsistentConnectionData})
	coTestTransfer(t, r, 1, c.Remote, &RLSD{DestinationLocalReference: 0x102, SourceLocalReference: ref, ReleaseCause: params.ReleaseCauseInconsistentConnectionData})
	coTestState(t, s, ref, ConnectionReleasePending)
	if !errors.Is(r.Discards[0].Reason, ErrConnectionReference) || c.releaseTimer == nil || !c.releaseTimer.deadline.Equal(clock.now().Add(s.cfg.Connection.ReleaseTimeout)) {
		t.Fatalf("incoming IT mismatch did not start release: %+v", r)
	}
	if d, ok := r.Indications[0].(*DisconnectIndication); !ok || d.ReleaseCause != params.ReleaseCauseInconsistentConnectionData || d.Originator != OriginatorProvider {
		t.Fatalf("release indication: %#v", r.Indications[0])
	}
}

// ITU-T Q.714 (05/01) Figure C.2 sheet 6 and Figure C.3 sheet 5: ERR ends
// disconnect pending without another N-DISCONNECT for any error cause.
func TestConnectionReleaseERRNoDuplicateIndication(t *testing.T) {
	for _, cause := range []params.ErrorCause{params.ErrorCauseUnassignedDestinationLRN, params.ErrorCauseInconsistentSourceLRN, params.ErrorCauseServiceClassMismatch, params.ErrorCauseUnqualified} {
		t.Run(cause.String(), func(t *testing.T) {
			s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, _ := coTestEstablished(t, s)
			if _, err := s.Disconnect(ref, DisconnectRequest{}); err != nil {
				t.Fatal(err)
			}
			clock.advance(s.cfg.Connection.ReleaseTimeout)
			s.HandleTimeout()
			c := s.conn.sections[ref]
			timers := []*timer{c.releaseTimer, c.repeatRelease, c.giveUp}
			r := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &ERR{DestinationLocalReference: ref, ErrorCause: cause})
			coTestCounts(t, r, 0, 0, 1)
			coTestState(t, s, ref, ConnectionFrozen)
			for _, timer := range timers {
				if timer == nil || timer.index >= 0 {
					t.Fatal("ERR retained a release timer")
				}
			}
		})
	}
}

// ITU-T Q.714 (05/01) §§3.8.3.3 a2, 3.8.4: invalid CC/IT classes
// discard before reference checks, setup completion or restarting T(iar).
func TestConnectionInvalidClassPreservesSection(t *testing.T) {
	for _, kind := range []MsgType{MsgTypeCC, MsgTypeIT} {
		for _, state := range []ConnectionState{ConnectionIncomingPending, ConnectionOutgoingPending, ConnectionEstablished, ConnectionReleasePending} {
			for _, class := range []byte{0, 1, 4, 15} {
				t.Run(fmt.Sprintf("%s/%s/%d", kind, state, class), func(t *testing.T) {
					s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
					var ref params.LocalReference
					switch state {
					case ConnectionIncomingPending:
						ref = coTestIncoming(t, s, 2)
					case ConnectionOutgoingPending:
						ref, _ = coTestOutgoing(t, s)
					default:
						ref, _ = coTestEstablished(t, s)
						if state == ConnectionReleasePending {
							if _, err := s.Disconnect(ref, DisconnectRequest{}); err != nil {
								t.Fatal(err)
							}
						}
					}
					c := s.conn.sections[ref]
					before := *c
					deadline, scheduled := s.NextTimeout()
					clock.advance(time.Second)
					var m Message = &CC{DestinationLocalReference: ref, SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}}
					if kind == MsgTypeIT {
						m = &IT{DestinationLocalReference: ref, SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}}
					}
					wire, err := m.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					wire[7] = class
					r := s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1}, Data: wire})
					coTestCounts(t, r, 0, 0, 1)
					if !errors.Is(r.Discards[0].Reason, ErrSyntax) {
						t.Fatalf("invalid class discard: %v", r.Discards[0].Reason)
					}
					if !reflect.DeepEqual(*c, before) {
						t.Fatal("invalid class changed section or timer pointers")
					}
					if got, ok := s.NextTimeout(); got != deadline || ok != scheduled {
						t.Fatal("invalid class changed timer deadline")
					}
				})
			}
		}
	}
}

func conformanceReassemblyTotal(t *testing.T, s *Stack) int {
	t.Helper()
	total := 0
	for _, c := range s.conn.sections {
		total += cap(c.reassembly)
	}
	if total != s.conn.reassemblyOctets || total > s.cfg.Connection.MaxReassemblyOctets {
		t.Fatalf("reassembly capacity %d, accounted %d, bound %d", total, s.conn.reassemblyOctets, s.cfg.Connection.MaxReassemblyOctets)
	}
	return total
}

// ITU-T Q.714 (05/01) §3.5.3: the budget counts actual capacity of every
// section, and near the bound a buffer grows only to what its data needs;
// §4.1.1.2.3.4: exhaustion releases with network congestion.
func TestConnectionReassemblyCapacityBudget(t *testing.T) {
	cfg := coTestConfig()
	cfg.MaxDataLength, cfg.MaxReassemblyOctets = 7, 11
	s, _ := coTestStack(t, StackConfig{Connection: cfg})
	a, _ := coTestEstablished(t, s)
	b, _ := coTestEstablished(t, s)
	receive := func(ref params.LocalReference, data string, more bool) Result {
		r := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &DT1{DestinationLocalReference: ref, Data: []byte(data), MoreData: more})
		conformanceReassemblyTotal(t, s)
		return r
	}
	for _, step := range []struct {
		ref  params.LocalReference
		data string
		cap  int
		held int
	}{
		{a, "abc", 3, 3}, {b, "def", 3, 6}, {a, "gh", 6, 9}, {a, "i", 6, 9},
		// Doubling b to 6 would exceed 11: it grows to exactly 4.
		{b, "j", 4, 10},
	} {
		coTestCounts(t, receive(step.ref, step.data, true), 0, 0, 0)
		if cap(s.conn.sections[step.ref].reassembly) != step.cap || s.conn.reassemblyOctets != step.held {
			t.Fatalf("doubling capacity after %q: got %d held %d", step.data, cap(s.conn.sections[step.ref].reassembly), s.conn.reassemblyOctets)
		}
	}
	r := receive(b, "klm", true) // even an exact buffer of 7 would exceed 11.
	coTestCounts(t, r, 1, 1, 1)
	if !errors.Is(r.Discards[0].Reason, ErrReassembly) || r.Transfers[0].Message.(*RLSD).ReleaseCause != params.ReleaseCauseNetworkCongestion || r.Indications[0].(*DisconnectIndication).ReleaseCause != params.ReleaseCauseNetworkCongestion {
		t.Fatalf("capacity overflow release: %+v", r)
	}
	coTestState(t, s, b, ConnectionReleasePending)
	if s.conn.reassemblyOctets != 6 {
		t.Fatal("release retained reassembly capacity")
	}
	r = receive(a, "k", false) // capacity is clamped from 12 to MaxDataLength=7.
	coTestCounts(t, r, 0, 1, 0)
	if got := cap(r.Indications[0].(*DataIndication).Data); got != cfg.MaxDataLength {
		t.Fatalf("final buffer capacity %d, want MaxDataLength %d", got, cfg.MaxDataLength)
	}
	if !bytes.Equal(r.Indications[0].(*DataIndication).Data, []byte("abcghik")) || s.conn.reassemblyOctets != 0 {
		t.Fatalf("final delivery/accounting: %+v", r)
	}
	coTestCounts(t, receive(a, "new", true), 0, 0, 0)
	coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &RLSD{DestinationLocalReference: a, SourceLocalReference: 0x102})
	coTestState(t, s, a, ConnectionFrozen)
	if conformanceReassemblyTotal(t, s) != 0 {
		t.Fatal("freeze retained reassembly capacity")
	}
}

func TestConnectionReassemblyBudgetConfiguration(t *testing.T) {
	s, err := NewStack(StackConfig{})
	if err != nil || s.cfg.Connection.MaxReassemblyOctets != 4<<20 {
		t.Fatalf("default reassembly budget: %v %v", s, err)
	}
	if _, err := NewStack(StackConfig{Connection: ConnectionConfig{MaxReassemblyOctets: -1}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("negative reassembly budget: %v", err)
	}
	for _, more := range []bool{false, true} {
		t.Run(fmt.Sprint(more), func(t *testing.T) {
			cfg := coTestConfig()
			cfg.MaxReassemblyOctets = 5
			s, _ := coTestStack(t, StackConfig{Connection: cfg})
			ref, _ := coTestEstablished(t, s)
			coTestCounts(t, coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &DT1{DestinationLocalReference: ref, MoreData: true, Data: []byte("abc")}), 0, 0, 0)
			// Three more octets need a buffer of 6, beyond the bound of 5.
			r := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &DT1{DestinationLocalReference: ref, MoreData: more, Data: []byte("def")})
			coTestCounts(t, r, 1, 1, 1)
			if !errors.Is(r.Discards[0].Reason, ErrReassembly) || conformanceReassemblyTotal(t, s) != 0 {
				t.Fatalf("final/intermediate segment overflow: %+v", r)
			}
		})
	}
}

// A reassembly buffer never grows beyond MaxDataLength, the longest NSDU it
// can hold (ITU-T Q.714 (05/01) §3.5.3).
func TestConnectionReassemblyBufferWithinMaxDataLength(t *testing.T) {
	cfg := coTestConfig()
	cfg.MaxDataLength = 7
	s, _ := coTestStack(t, StackConfig{Connection: cfg})
	ref, _ := coTestEstablished(t, s)
	for _, data := range []string{"abcd", "e"} {
		coTestCounts(t, coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &DT1{DestinationLocalReference: ref, MoreData: true, Data: []byte(data)}), 0, 0, 0)
	}
	if got := cap(s.conn.sections[ref].reassembly); got != cfg.MaxDataLength {
		t.Fatalf("buffer of %d octets, want MaxDataLength %d", got, cfg.MaxDataLength)
	}
	conformanceReassemblyTotal(t, s)
}

// The bound is inclusive: buffers that fill it exactly are accepted.
func TestConnectionReassemblyBudgetExactlyFilled(t *testing.T) {
	cfg := coTestConfig()
	cfg.MaxReassemblyOctets = 6
	s, _ := coTestStack(t, StackConfig{Connection: cfg})
	ref, _ := coTestEstablished(t, s)
	for _, data := range []string{"abc", "def"} {
		coTestCounts(t, coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &DT1{DestinationLocalReference: ref, MoreData: true, Data: []byte(data)}), 0, 0, 0)
	}
	if conformanceReassemblyTotal(t, s) != 6 {
		t.Fatalf("held %d octets, want the whole bound", s.conn.reassemblyOctets)
	}
	r := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &DT1{DestinationLocalReference: ref, Data: []byte("g")})
	coTestCounts(t, r, 1, 1, 1)
}

// ITU-T Q.714 (05/01) Figure C.2 sheet 4: a stale T(ias) callback must
// do nothing outside Active, even if the send timer has already been cleared.
func TestConnectionInactivityCallbackOutsideEstablished(t *testing.T) {
	for _, state := range []ConnectionState{ConnectionIdle, ConnectionIncomingPending, ConnectionOutgoingPending, ConnectionReleasePending, ConnectionFrozen} {
		t.Run(state.String(), func(t *testing.T) {
			s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, _ := coTestEstablished(t, s)
			c := s.conn.sections[ref]
			callback := c.sendInactivity.fire
			s.stopConnectionTimers(c)
			c.State = state
			before := len(s.timers)
			var r Result
			callback(clock.now(), &r)
			coTestCounts(t, r, 0, 0, 0)
			if len(s.timers) != before || c.sendInactivity != nil {
				t.Fatal("stale inactivity callback scheduled a timer")
			}
		})
	}
}
