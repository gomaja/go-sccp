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

func resourceTestTimers(c *connectionSection) []*timer {
	var timers []*timer
	for _, tm := range []*timer{c.establishment, c.releaseTimer, c.repeatRelease, c.giveUp, c.sendInactivity, c.receiveInactivity} {
		if tm != nil {
			timers = append(timers, tm)
		}
	}
	return timers
}

func resourceTestStopped(t *testing.T, s *Stack, timers []*timer) {
	t.Helper()
	for _, tm := range timers {
		if tm.index != -1 {
			t.Errorf("released section timer at %v has index %d, want -1", tm.deadline, tm.index)
		}
		for _, scheduled := range s.timers {
			if scheduled == tm {
				t.Errorf("released section timer at %v remains in heap", tm.deadline)
			}
		}
	}
}

// ITU-T Q.714 (05/01) §§3.1.2, 3.3.2: freezing reserves only the reference;
// every section timer and the active allocation must be released.
func resourceTestFrozenAndReusable(t *testing.T, s *Stack, clock *coTestClock, old *connectionSection, timers []*timer, activeBefore int, incoming bool) {
	t.Helper()
	if s.conn.active != activeBefore-1 {
		t.Fatalf("active sections = %d, want %d after freeing %#x", s.conn.active, activeBefore-1, old.LocalReference)
	}
	resourceTestStopped(t, s, timers)
	if len(resourceTestTimers(old)) != 0 || old.reassembly != nil || old.disconnect != nil {
		t.Fatalf("released section retains timers or user resources: %+v", old)
	}
	frozen := s.conn.sections[old.LocalReference]
	if frozen == nil || frozen == old || frozen.State != ConnectionFrozen || len(resourceTestTimers(frozen)) != 0 {
		t.Fatalf("reference is not a separate frozen section: %+v", frozen)
	}
	wantScheduled := 1 // The frozen reference has only its reuse timer.
	for ref, c := range s.conn.sections {
		if ref != old.LocalReference {
			wantScheduled += len(resourceTestTimers(c))
		}
	}
	if len(s.timers) != wantScheduled {
		t.Fatalf("scheduled timers = %d, want %d; freed section left an orphan", len(s.timers), wantScheduled)
	}
	clock.advance(s.cfg.Connection.ReferenceFreeze - time.Nanosecond)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	coTestState(t, s, old.LocalReference, ConnectionFrozen)
	s.conn.next = old.LocalReference
	if ref, err := s.candidateReference(); err != nil || ref == old.LocalReference {
		t.Fatalf("frozen reference is allocatable: ref %#x, error %v", ref, err)
	}
	clock.advance(time.Nanosecond)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	if _, ok := s.Connection(old.LocalReference); ok {
		t.Fatal("reference remains allocated at freeze endpoint")
	}
	s.conn.next = old.LocalReference
	var reused params.LocalReference
	if incoming {
		reused = coTestIncoming(t, s, 2)
	} else {
		reused, _ = coTestOutgoing(t, s)
	}
	if reused != old.LocalReference || s.conn.active != activeBefore {
		t.Fatalf("reuse = %#x, active %d; want %#x, %d", reused, s.conn.active, old.LocalReference, activeBefore)
	}
}

// ITU-T Q.714 (05/01) Figure C.2 sheets 2, 3 and 6, §§3.2.3, 3.3.5:
// exercise each path through real CR/CC and user primitives, with a live peer
// section whose timers must survive freeing the target section.
func TestConnectionResourceReleasePaths(t *testing.T) {
	tests := []struct {
		name  string
		state ConnectionState
		event string
	}{
		{"outgoing_CREF", ConnectionOutgoingPending, "CREF"},
		{"establishment_expiry", ConnectionOutgoingPending, "establishment"},
		{"outgoing_RLSD", ConnectionOutgoingPending, "RLSD"},
		{"outgoing_RLC", ConnectionOutgoingPending, "RLC"},
		{"outgoing_ERR", ConnectionOutgoingPending, "ERR"},
		{"outgoing_DT1", ConnectionOutgoingPending, "DT1"},
		{"outgoing_IT", ConnectionOutgoingPending, "IT"},
		{"established_RLSD", ConnectionEstablished, "RLSD"},
		{"established_ERR", ConnectionEstablished, "ERR"},
		{"release_pending_RLC", ConnectionReleasePending, "RLC"},
		{"release_pending_RLSD", ConnectionReleasePending, "RLSD"},
		{"release_pending_ERR", ConnectionReleasePending, "ERR"},
		{"incoming_user_refusal", ConnectionIncomingPending, "user_refusal"},
		{"user_release_completed_RLC", ConnectionReleasePending, "user_RLC"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := coTestConfig()
			cfg.InactivitySendTimeout = time.Hour
			cfg.InactivityReceiveTimeout = 2 * time.Hour
			s, clock := coTestStack(t, StackConfig{Connection: cfg})
			guard, _ := coTestEstablished(t, s)
			guardSection := s.conn.sections[guard]
			guardTimers := resourceTestTimers(guardSection)
			var ref params.LocalReference
			var timers []*timer
			if tc.state == ConnectionIncomingPending {
				ref = coTestIncoming(t, s, 2)
			} else {
				ref, _ = coTestOutgoing(t, s)
				timers = append(timers, resourceTestTimers(s.conn.sections[ref])...)
				if tc.state != ConnectionOutgoingPending {
					coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &CC{DestinationLocalReference: ref, SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}})
					coTestState(t, s, ref, ConnectionEstablished)
					timers = append(timers, resourceTestTimers(s.conn.sections[ref])...)
					coTestCounts(t, coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &DT1{DestinationLocalReference: ref, MoreData: true, Data: []byte{1, 2, 3}}), 0, 0, 0)
					if tc.state == ConnectionReleasePending {
						timers = append(timers, resourceTestTimers(s.conn.sections[ref])...)
						res, err := s.Disconnect(ref, DisconnectRequest{ReleaseCause: params.ReleaseCauseEndUserOriginated})
						if err != nil {
							t.Fatal(err)
						}
						coTestCounts(t, res, 1, 0, 0)
						coTestState(t, s, ref, ConnectionReleasePending)
						timers = append(timers, resourceTestTimers(s.conn.sections[ref])...)
						if tc.event != "user_RLC" {
							clock.advance(cfg.ReleaseTimeout)
							coTestCounts(t, s.HandleTimeout(), 1, 0, 0)
						}
					}
				}
			}
			old := s.conn.sections[ref]
			timers = append(timers, resourceTestTimers(old)...)
			activeBefore := s.conn.active
			label := RoutingLabel{OPC: 2, DPC: 1}
			var res Result
			switch tc.event {
			case "establishment":
				clock.advance(cfg.EstablishmentTimeout)
				res = s.HandleTimeout()
			case "user_refusal":
				var err error
				res, err = s.Disconnect(ref, DisconnectRequest{RefusalCause: params.RefusalCauseEndUserOriginated})
				if err != nil {
					t.Fatal(err)
				}
			case "CREF":
				res = coTestReceive(t, s, label, &CREF{DestinationLocalReference: ref})
			case "RLSD":
				res = coTestReceive(t, s, label, &RLSD{DestinationLocalReference: ref, SourceLocalReference: 0x102})
			case "RLC", "user_RLC":
				res = coTestReceive(t, s, label, &RLC{DestinationLocalReference: ref, SourceLocalReference: 0x102})
			case "ERR":
				res = coTestReceive(t, s, label, &ERR{DestinationLocalReference: ref, ErrorCause: params.ErrorCauseUnqualified})
			case "DT1":
				res = coTestReceive(t, s, label, &DT1{DestinationLocalReference: ref, Data: []byte{1}})
			case "IT":
				res = coTestReceive(t, s, label, &IT{DestinationLocalReference: ref, SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}})
			}
			if tc.state == ConnectionReleasePending && len(res.Indications) != 0 {
				t.Fatalf("completed user release indicated twice: %+v", res)
			}
			resourceTestFrozenAndReusable(t, s, clock, old, timers, activeBefore, tc.state == ConnectionIncomingPending)
			if s.conn.sections[guard] != guardSection || guardSection.State != ConnectionEstablished || !reflect.DeepEqual(resourceTestTimers(guardSection), guardTimers) {
				t.Fatal("freeing the target altered the other established section")
			}
			for _, tm := range guardTimers {
				if tm.index < 0 || tm.index >= len(s.timers) || s.timers[tm.index] != tm {
					t.Fatal("another section's timer was stopped")
				}
			}
		})
	}
}

// ITU-T Q.714 (05/01) §3.3.3.2 and Figure C.2 sheet 6: T(int) begins at
// the first T(rel) expiry, ends repetitions, frees resources and alerts maintenance.
func TestConnectionResourceReleaseIntervalEndpoint(t *testing.T) {
	cfg := coTestConfig()
	cfg.ReleaseGiveUpTimeout = 6 * time.Second
	s, clock := coTestStack(t, StackConfig{Connection: cfg})
	ref, label := coTestEstablished(t, s)
	old := s.conn.sections[ref]
	timers := resourceTestTimers(old)
	activeBefore := s.conn.active
	want := &RLSD{DestinationLocalReference: 0x102, SourceLocalReference: ref, ReleaseCause: params.ReleaseCauseEndUserOriginated}
	res, err := s.Disconnect(ref, DisconnectRequest{ReleaseCause: want.ReleaseCause})
	if err != nil {
		t.Fatal(err)
	}
	coTestCounts(t, res, 1, 0, 0)
	coTestTransfer(t, res, 0, label, want)
	timers = append(timers, resourceTestTimers(old)...)
	for second := 1; second <= 9; second++ {
		clock.advance(time.Second)
		res = s.HandleTimeout()
		switch second {
		case 3, 5, 7:
			coTestCounts(t, res, 1, 0, 0)
			coTestTransfer(t, res, 0, label, want)
			if old.repeatRelease != nil && old.repeatRelease.index >= 0 && !old.repeatRelease.deadline.Before(old.giveUp.deadline) {
				t.Fatal("repeat release timer scheduled at or after T(int) endpoint")
			}
		case 9:
			coTestCounts(t, res, 0, 1, 0)
			ind, ok := res.Indications[0].(*MaintenanceIndication)
			if !ok || ind.LocalReference != ref || ind.RemoteReference != 0x102 || ind.RoutingLabel != label || ind.MessageType != MsgTypeRLSD || !errors.Is(ind.Reason, ErrReleaseIncomplete) {
				t.Fatalf("maintenance indication = %#v", res.Indications[0])
			}
		default:
			coTestCounts(t, res, 0, 0, 0)
		}
		timers = append(timers, resourceTestTimers(old)...)
	}
	resourceTestFrozenAndReusable(t, s, clock, old, timers, activeBefore, false)
}

// ITU-T Q.713 (03/01) §§3.2-3.3 reserve all ones; Q.714 (05/01) §3.8.4
// discards the received bytes without responding or changing the live section.
func TestConnectionResourceReservedReferenceOctets(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"CR_source", []byte{0x01, 0xff, 0xff, 0xff, 0x02, 0x02, 0x00, 0x04, 0x43, 0x01, 0x00, 0x06}},
		{"RLSD_destination", []byte{0x04, 0xff, 0xff, 0xff, 0x02, 0x01, 0x00, 0x00, 0x00}},
		{"RLSD_source", []byte{0x04, 0x01, 0x00, 0x00, 0xff, 0xff, 0xff, 0x00, 0x00}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, _ := coTestEstablished(t, s)
			if ref != 1 {
				t.Fatalf("raw test requires local reference 1, got %#x", ref)
			}
			before, _ := s.Connection(ref)
			old := s.conn.sections[ref]
			timers := resourceTestTimers(old)
			res := s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1}, Data: tc.data})
			coTestCounts(t, res, 0, 0, 1)
			if !errors.Is(res.Discards[0].Reason, ErrSyntax) {
				t.Fatalf("reason = %v, want ErrSyntax", res.Discards[0].Reason)
			}
			after, _ := s.Connection(ref)
			if !reflect.DeepEqual(before, after) || s.conn.active != 1 || !reflect.DeepEqual(timers, resourceTestTimers(old)) {
				t.Fatal("reserved reference changed established resources")
			}
		})
	}
}

// ITU-T Q.714 (05/01) §§3.1.2, 3.3.2: occupied and frozen references both
// count toward the scan; the sole free reference needs occupied+1 attempts.
func TestConnectionResourceAllocationLastCandidate(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
	s.conn.referenceLimit = 4
	refs := make([]params.LocalReference, 3)
	for i := range refs {
		refs[i], _ = coTestOutgoing(t, s)
	}
	coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &CREF{DestinationLocalReference: refs[1]})
	coTestState(t, s, refs[1], ConnectionFrozen)
	if len(s.conn.sections) != 3 || s.conn.active != 2 {
		t.Fatalf("occupied sections %d, active %d; want 3, 2", len(s.conn.sections), s.conn.active)
	}
	s.conn.next = refs[0]
	ref, _ := coTestOutgoing(t, s)
	if ref != 0 || len(s.conn.sections) != 4 || s.conn.active != 3 {
		t.Fatalf("last candidate = %#x, occupied %d, active %d; want 0, 4, 3", ref, len(s.conn.sections), s.conn.active)
	}
}

// ITU-T Q.714 (05/01) §3.5.3 and Q.713 (03/01) Table 8: a DT1 needs
// seven framing octets plus at least one data octet; reject the whole NSDU otherwise.
func TestConnectionResourceSendDataBoundaries(t *testing.T) {
	cfg := coTestConfig()
	cfg.MaxDataLength = 4
	s, _ := coTestStack(t, StackConfig{Connection: cfg})
	ref, label := coTestEstablished(t, s)
	data := []byte{1, 2, 3, 4}
	res, err := s.SendData(ref, data)
	if err != nil {
		t.Fatal(err)
	}
	coTestCounts(t, res, 1, 0, 0)
	coTestTransfer(t, res, 0, label, &DT1{DestinationLocalReference: 0x102, Data: data})
	res, err = s.SendData(ref, append(bytes.Clone(data), 5))
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("MaxDataLength+1 error = %v", err)
	}
	coTestCounts(t, res, 0, 0, 0)
	// Establishment needs a larger CR/CC budget, so narrow it only after setup.
	s.cfg.MaxMessageSize = 8
	res, err = s.SendData(ref, data)
	if err != nil {
		t.Fatal(err)
	}
	coTestCounts(t, res, len(data), 0, 0)
	for i, octet := range data {
		coTestTransfer(t, res, i, label, &DT1{DestinationLocalReference: 0x102, MoreData: i < len(data)-1, Data: []byte{octet}})
		if len(res.Transfers[i].Data) != 8 {
			t.Fatalf("DT1 size = %d, want 8", len(res.Transfers[i].Data))
		}
	}
	s.cfg.MaxMessageSize = 7
	before := s.conn.sections[ref].sendInactivity
	res, err = s.SendData(ref, data)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("seven-octet budget error = %v", err)
	}
	coTestCounts(t, res, 0, 0, 0)
	if s.conn.sections[ref].sendInactivity != before {
		t.Fatal("rejected send restarted inactivity timer")
	}
}

func resourceTestMessage(kind string, dst, src params.LocalReference) Message {
	switch kind {
	case "CC":
		return &CC{DestinationLocalReference: dst, SourceLocalReference: src, ProtocolClass: params.ProtocolClass{Class: 2}}
	case "CREF":
		return &CREF{DestinationLocalReference: dst}
	case "RLSD":
		return &RLSD{DestinationLocalReference: dst, SourceLocalReference: src}
	case "RLC":
		return &RLC{DestinationLocalReference: dst, SourceLocalReference: src}
	case "DT1":
		return &DT1{DestinationLocalReference: dst, Data: []byte{1}}
	case "DT2":
		return &DT2{DestinationLocalReference: dst, Data: []byte{1}}
	case "AK":
		return &AK{DestinationLocalReference: dst}
	case "ED":
		return &ED{DestinationLocalReference: dst, Data: []byte{1}}
	case "EA":
		return &EA{DestinationLocalReference: dst}
	case "RSR":
		return &RSR{DestinationLocalReference: dst, SourceLocalReference: src}
	case "RSC":
		return &RSC{DestinationLocalReference: dst, SourceLocalReference: src}
	case "ERR":
		return &ERR{DestinationLocalReference: dst}
	case "IT":
		return &IT{DestinationLocalReference: dst, SourceLocalReference: src, ProtocolClass: params.ProtocolClass{Class: 2}}
	default:
		panic("unknown test message: " + kind)
	}
}

func resourceTestDiscard(t *testing.T, res Result, label RoutingLabel, kind MsgType, sentinel error) {
	t.Helper()
	if len(res.Discards) != 1 {
		t.Fatalf("discards = %+v, want exactly the one received message", res.Discards)
	}
	d := res.Discards[0]
	if d.RoutingLabel != label || d.MessageType != kind || !errors.Is(d.Reason, sentinel) {
		t.Fatalf("discard = %+v, want label %+v, type %s and reason %v", d, label, kind, sentinel)
	}
	for _, documented := range []error{ErrSyntax, ErrIncompleteAddress, ErrUndeliverable, ErrReassembly, ErrNotSupported, ErrConnectionNotFound, ErrConnectionState, ErrConnectionReference} {
		if errors.Is(d.Reason, documented) {
			return
		}
	}
	t.Fatalf("discard reason matches no documented sentinel: %v", d.Reason)
}

// ITU-T Q.714 (05/01) Table B.2, Notes 2-3: each reply accompanies exactly
// one discard of the received NPDU, including both releases for mismatched IT.
func TestConnectionResourceB2ResponseDiscardCounts(t *testing.T) {
	kinds := []string{"CC", "CREF", "RLSD", "RLC", "DT1", "DT2", "AK", "ED", "EA", "RSR", "RSC", "ERR", "IT"}
	class3 := func(kind string) bool {
		return kind == "DT2" || kind == "AK" || kind == "ED" || kind == "EA" || kind == "RSR" || kind == "RSC"
	}
	for _, mode := range []string{"unassigned", "source", "point_code"} {
		states := []ConnectionState{ConnectionIncomingPending, ConnectionEstablished, ConnectionReleasePending}
		if mode == "unassigned" {
			states = []ConnectionState{ConnectionIdle}
		}
		for _, state := range states {
			for _, kind := range kinds {
				if mode == "source" && kind != "RLSD" && kind != "RLC" && kind != "RSR" && kind != "RSC" && kind != "IT" {
					continue
				}
				if mode == "point_code" && (kind == "CC" || kind == "CREF") {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/%s", mode, state, kind), func(t *testing.T) {
					s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
					dst := params.LocalReference(0x44)
					src := params.LocalReference(0x102)
					if state == ConnectionIncomingPending {
						dst = coTestIncoming(t, s, 2)
					} else if state != ConnectionIdle {
						dst, _ = coTestEstablished(t, s)
						if state == ConnectionReleasePending {
							if _, err := s.Disconnect(dst, DisconnectRequest{ReleaseCause: params.ReleaseCauseEndUserOriginated}); err != nil {
								t.Fatal(err)
							}
						}
					}
					label := RoutingLabel{OPC: 2, DPC: 1, SLS: 9}
					cause := params.ErrorCauseUnassignedDestinationLRN
					sentinel := ErrConnectionNotFound
					switch mode {
					case "source":
						src = 0x999
						cause, sentinel = params.ErrorCauseInconsistentSourceLRN, ErrConnectionReference
					case "point_code":
						label.OPC = 3
						cause, sentinel = params.ErrorCausePointCodeMismatch, ErrConnectionReference
					}
					var want []Message
					indications := 0
					switch kind {
					case "CC":
						want = []Message{&ERR{DestinationLocalReference: src, ErrorCause: cause}}
					case "RLSD":
						if mode == "unassigned" {
							want = []Message{&RLC{DestinationLocalReference: src, SourceLocalReference: dst}}
						} else {
							want = []Message{&ERR{DestinationLocalReference: src, ErrorCause: cause}}
						}
					case "IT":
						switch mode {
						case "unassigned":
							want = []Message{&ERR{DestinationLocalReference: src, ErrorCause: cause}}
						case "source":
							storedCause := params.ReleaseCauseInconsistentConnectionData
							if state == ConnectionReleasePending {
								storedCause = params.ReleaseCauseEndUserOriginated
							} else {
								indications = 1
							}
							want = []Message{
								&RLSD{DestinationLocalReference: src, SourceLocalReference: dst, ReleaseCause: params.ReleaseCauseInconsistentConnectionData},
								&RLSD{DestinationLocalReference: 0x102, SourceLocalReference: dst, ReleaseCause: storedCause},
							}
						}
					}
					if class3(kind) {
						// Table B.1: unknown types are discarded first.
						sentinel = ErrNotSupported
					}
					m := resourceTestMessage(kind, dst, src)
					res := coTestReceive(t, s, label, m)
					coTestCounts(t, res, len(want), indications, 1)
					resourceTestDiscard(t, res, label, m.MessageType(), sentinel)
					for i, response := range want {
						responseLabel := replyLabel(label)
						if i == 1 {
							responseLabel = s.conn.sections[dst].Remote
						}
						coTestTransfer(t, res, i, responseLabel, response)
					}
				})
			}
		}
	}
}

// ITU-T Q.714 (05/01) Table B.3 and §3.8.4: state, syntax and reassembly
// discards all retain an errors.Is identity; class 3-only discards use B.1 Note.
func TestConnectionResourceDiscardReasons(t *testing.T) {
	for _, tc := range []struct {
		state ConnectionState
		kinds []string
	}{
		{ConnectionIncomingPending, []string{"CC", "CREF", "RLSD", "RLC", "DT1", "ERR", "IT"}},
		{ConnectionOutgoingPending, []string{"RLC", "ERR", "DT1", "IT"}},
		{ConnectionEstablished, []string{"CC", "CREF", "RLC"}},
		{ConnectionReleasePending, []string{"CC", "CREF", "DT1", "IT", "ERR"}},
	} {
		for _, kind := range tc.kinds {
			t.Run(fmt.Sprintf("%s/%s", tc.state, kind), func(t *testing.T) {
				s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
				var ref params.LocalReference
				switch tc.state {
				case ConnectionIncomingPending:
					ref = coTestIncoming(t, s, 2)
				case ConnectionOutgoingPending:
					ref, _ = coTestOutgoing(t, s)
				default:
					ref, _ = coTestEstablished(t, s)
					if tc.state == ConnectionReleasePending {
						if _, err := s.Disconnect(ref, DisconnectRequest{}); err != nil {
							t.Fatal(err)
						}
					}
				}
				label := RoutingLabel{OPC: 2, DPC: 1}
				m := resourceTestMessage(kind, ref, 0x102)
				resourceTestDiscard(t, coTestReceive(t, s, label, m), label, m.MessageType(), ErrConnectionState)
			})
		}
	}
	for _, kind := range []string{"DT2", "AK", "ED", "EA", "RSR", "RSC"} {
		t.Run("unsupported/"+kind, func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, _ := coTestEstablished(t, s)
			label := RoutingLabel{OPC: 2, DPC: 1}
			m := resourceTestMessage(kind, ref, 0x102)
			resourceTestDiscard(t, coTestReceive(t, s, label, m), label, m.MessageType(), ErrNotSupported)
		})
	}
	t.Run("reassembly_limit", func(t *testing.T) {
		cfg := coTestConfig()
		cfg.MaxDataLength = 1
		s, _ := coTestStack(t, StackConfig{Connection: cfg})
		ref, _ := coTestEstablished(t, s)
		label := RoutingLabel{OPC: 2, DPC: 1}
		m := &DT1{DestinationLocalReference: ref, Data: []byte{1, 2}}
		resourceTestDiscard(t, coTestReceive(t, s, label, m), label, MsgTypeDT1, ErrReassembly)
	})
	for _, called := range []params.PartyAddress{{HasSSN: true, SSN: 6}, {RouteOnSSN: true, HasPointCode: true, PointCode: 1}, {RouteOnSSN: true, HasSSN: true, SSN: 0}} {
		t.Run(fmt.Sprintf("incomplete_address/%+v", called), func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{})
			label := RoutingLabel{OPC: 2, DPC: 1}
			m := &CR{SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: called}
			reason := ErrSyntax
			if called.HasSSN && called.SSN == 0 {
				reason = ErrIncompleteAddress
			}
			resourceTestDiscard(t, coTestReceive(t, s, label, m), label, MsgTypeCR, reason)
		})
	}
	for _, tc := range []struct {
		name     string
		kind     string
		outgoing bool
		class    uint8
		want     error
	}{
		{"CC_invalid_class", "CC", true, 1, ErrSyntax},
		{"IT_invalid_class", "IT", false, 1, ErrSyntax},
		{"CC_class_three", "CC", true, 3, ErrConnectionState},
		{"IT_class_three", "IT", false, 3, ErrConnectionState},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
			var ref params.LocalReference
			if tc.outgoing {
				ref, _ = coTestOutgoing(t, s)
			} else {
				ref, _ = coTestEstablished(t, s)
			}
			m := resourceTestMessage(tc.kind, ref, 0x102)
			data, err := m.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			// Q.713 (03/01) Tables 4 and 18: class follows both references.
			data[7] = tc.class
			label := RoutingLabel{OPC: 2, DPC: 1}
			res := s.HandleTransfer(TransferIndication{RoutingLabel: label, Data: data})
			resourceTestDiscard(t, res, label, m.MessageType(), tc.want)
		})
	}
	t.Run("CR_invalid_class", func(t *testing.T) {
		s, _ := coTestStack(t, StackConfig{})
		label := RoutingLabel{OPC: 2, DPC: 1}
		// Q.713 (03/01) Table 3: class 1 is unusable in a CR.
		data := []byte{0x01, 0x02, 0x01, 0x00, 0x01, 0x02, 0x00, 0x04, 0x43, 0x01, 0x00, 0x06}
		resourceTestDiscard(t, s.HandleTransfer(TransferIndication{RoutingLabel: label, Data: data}), label, MsgTypeCR, ErrSyntax)
	})
	t.Run("DT1_empty_data", func(t *testing.T) {
		s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
		ref, _ := coTestEstablished(t, s)
		if ref != 1 {
			t.Fatalf("raw test requires local reference 1, got %#x", ref)
		}
		label := RoutingLabel{OPC: 2, DPC: 1}
		// Q.713 (03/01) Table 8: the data parameter cannot be empty.
		data := []byte{0x06, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00}
		resourceTestDiscard(t, s.HandleTransfer(TransferIndication{RoutingLabel: label, Data: data}), label, MsgTypeDT1, ErrSyntax)
	})
	t.Run("refusal_cannot_fit", func(t *testing.T) {
		s, _ := coTestStack(t, StackConfig{})
		s.cfg.MaxMessageSize = 4
		label := RoutingLabel{OPC: 2, DPC: 1}
		m := &CR{SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: params.NewPointCodeAddress(1, 9)}
		resourceTestDiscard(t, coTestReceive(t, s, label, m), label, MsgTypeCR, ErrUndeliverable)
	})
}
