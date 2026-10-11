// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gomaja/go-sccp/params"
)

const (
	annexTestLocalPC  uint32                = 11
	annexTestRemotePC uint32                = 22
	annexTestLocalRef params.LocalReference = 0x010203
	annexTestPeerRef  params.LocalReference = 0x040506
)

func annexTestStack(t *testing.T) (*Stack, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s, err := NewStack(StackConfig{PointCode: annexTestLocalPC, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	s.RegisterSubsystem(6)
	return s, &now
}

func annexTestReceive(t *testing.T, s *Stack, label RoutingLabel, m Message) Result {
	t.Helper()
	wire, err := m.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return s.HandleTransfer(TransferIndication{RoutingLabel: label, Data: wire})
}

func annexTestMessages(t *testing.T, res Result, label RoutingLabel, want ...Message) {
	t.Helper()
	if len(res.Transfers) != len(want) {
		t.Fatalf("transfers = %v, want %v", res.Transfers, want)
	}
	for i, expected := range want {
		got := res.Transfers[i]
		if got.RoutingLabel != label {
			t.Errorf("transfer %d label = %+v, want %+v", i, got.RoutingLabel, label)
		}
		if !reflect.DeepEqual(got.Message, expected) {
			t.Errorf("transfer %d message = %#v, want %#v", i, got.Message, expected)
		}
		decoded, err := ParseMessage(got.Data)
		if err != nil {
			t.Fatalf("transfer %d cannot be decoded: %v", i, err)
		}
		if !reflect.DeepEqual(decoded, expected) {
			t.Errorf("transfer %d encoded message = %#v, want %#v", i, decoded, expected)
		}
	}
}

func annexTestNoIndications(t *testing.T, res Result) {
	t.Helper()
	if len(res.Indications) != 0 {
		t.Errorf("indications = %v, want none", res.Indications)
	}
}

func annexTestDiscard(t *testing.T, res Result, label RoutingLabel, typ MsgType) {
	t.Helper()
	if len(res.Discards) != 1 {
		t.Fatalf("discards = %v, want exactly one", res.Discards)
	}
	d := res.Discards[0]
	if d.RoutingLabel != label || d.MessageType != typ {
		t.Errorf("discard = %+v, want label %+v and type %s", d, label, typ)
	}
	if !errors.Is(d.Reason, ErrUndeliverable) && !errors.Is(d.Reason, ErrSyntax) &&
		!errors.Is(d.Reason, ErrConnectionNotFound) && !errors.Is(d.Reason, ErrConnectionState) &&
		!errors.Is(d.Reason, ErrConnectionReference) {
		t.Errorf("discard reason = %v, want a specific connection, syntax, or undeliverable error", d.Reason)
	}
}

func annexTestSection(t *testing.T, s *Stack, state ConnectionState) {
	t.Helper()
	// ITU-T Q.714 (05/01) §3.1.4.2 items 1 and 3: c3 has no associated peer.
	if state == ConnectionOutgoingPending {
		s.conn.next = annexTestLocalRef
		s.nextSLS = 7
		ref, res, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(uint16(annexTestRemotePC), 6)})
		if err != nil || ref != annexTestLocalRef {
			t.Fatalf("Connect fixture = %#x, %+v, %v", ref, res, err)
		}
		coTestCounts(t, res, 1, 0, 0)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := &connectionSection{ConnectionInfo: ConnectionInfo{
		LocalReference: annexTestLocalRef, RemoteReference: annexTestPeerRef, State: state,
		Remote: RoutingLabel{OPC: annexTestLocalPC, DPC: annexTestRemotePC, SLS: 7},
	}}
	s.conn.sections[annexTestLocalRef] = c
	if state != ConnectionIdle && state != ConnectionFrozen {
		s.conn.active++
	}
	if state == ConnectionEstablished {
		s.startConnectionInactivity(s.cfg.Now(), c)
	}
	if state == ConnectionReleasePending {
		// ITU-T Q.714 (05/01) §3.3.3.2: c6 retains its original release.
		var res Result
		s.beginConnectionRelease(s.cfg.Now(), c, s.connectionReleaseMessage(c, DisconnectRequest{ReleaseCause: params.ReleaseCauseEndUserOriginated, Data: []byte{0x71}}), false, &res)
	}
}

type annexTestTimerState struct {
	timer    *timer
	deadline time.Time
	index    int
}

func annexTestTimers(s *Stack) []annexTestTimerState {
	states := make([]annexTestTimerState, len(s.timers))
	for i, timer := range s.timers {
		states[i] = annexTestTimerState{timer: timer, deadline: timer.deadline, index: timer.index}
	}
	return states
}

func annexTestState(t *testing.T, s *Stack, ref params.LocalReference, want ConnectionState) {
	t.Helper()
	info, ok := s.Connection(ref)
	if !ok || info.State != want {
		t.Fatalf("Connection(%#x) = %+v, %v, want state %s", ref, info, ok, want)
	}
}

// ITU-T Q.714 (05/01) Table B.2: responses for an unassigned DLR use the
// references in the received message. The class 3 messages are unknown
// types, discarded before these checks (Table B.1).
func TestConnectionAnnexB2UnassignedReferences(t *testing.T) {
	label := RoutingLabel{OPC: annexTestRemotePC, DPC: annexTestLocalPC, SLS: 7}
	reply := RoutingLabel{OPC: annexTestLocalPC, DPC: annexTestRemotePC, SLS: 7}
	unassigned := &ERR{DestinationLocalReference: annexTestPeerRef, ErrorCause: params.ErrorCauseUnassignedDestinationLRN}
	tests := []struct {
		name string
		msg  Message
		want Message
	}{
		{"CC", &CC{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef, ProtocolClass: params.ProtocolClass{Class: 2}}, unassigned},
		{"CREF", &CREF{DestinationLocalReference: annexTestLocalRef}, nil},
		{"RLSD", &RLSD{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef}, &RLC{DestinationLocalReference: annexTestPeerRef, SourceLocalReference: annexTestLocalRef}},
		{"RLC", &RLC{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef}, nil},
		{"DT1", &DT1{DestinationLocalReference: annexTestLocalRef, Data: []byte{0x71}}, nil},
		{"DT2", &DT2{DestinationLocalReference: annexTestLocalRef, Data: []byte{0x71}}, nil},
		{"AK", &AK{DestinationLocalReference: annexTestLocalRef}, nil},
		{"ED", &ED{DestinationLocalReference: annexTestLocalRef, Data: []byte{0x71}}, nil},
		{"EA", &EA{DestinationLocalReference: annexTestLocalRef}, nil},
		{"ERR", &ERR{DestinationLocalReference: annexTestLocalRef}, nil},
		{"IT", &IT{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef, ProtocolClass: params.ProtocolClass{Class: 2}}, unassigned},
	}
	for _, refState := range []string{"unknown", "frozen"} {
		t.Run(refState, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					s, _ := annexTestStack(t)
					if refState == "frozen" {
						annexTestSection(t, s, ConnectionFrozen)
					}
					res := annexTestReceive(t, s, label, tt.msg)
					if tt.want == nil {
						annexTestMessages(t, res, reply)
					} else {
						annexTestMessages(t, res, reply, tt.want)
					}
					annexTestDiscard(t, res, label, tt.msg.MessageType())
					annexTestNoIndications(t, res)
					if refState == "frozen" {
						annexTestState(t, s, annexTestLocalRef, ConnectionFrozen)
					}
				})
			}
		})
	}
}

// ITU-T Q.714 (05/01) Table B.2 Note 2: non-IT SLR mismatches leave
// the associated local section unchanged; c3 has no associated reference.
func TestConnectionAnnexB2SourceMismatch(t *testing.T) {
	const wrongSource params.LocalReference = 0x070809
	label := RoutingLabel{OPC: annexTestRemotePC, DPC: annexTestLocalPC, SLS: 7}
	reply := RoutingLabel{OPC: annexTestLocalPC, DPC: annexTestRemotePC, SLS: 7}
	sourceError := &ERR{DestinationLocalReference: wrongSource, ErrorCause: params.ErrorCauseInconsistentSourceLRN}
	tests := []struct {
		name string
		msg  Message
		want []Message
	}{
		{"RLSD", &RLSD{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: wrongSource}, []Message{sourceError}},
		{"RLC", &RLC{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: wrongSource}, nil},
	}
	states := []ConnectionState{ConnectionIncomingPending, ConnectionEstablished, ConnectionReleasePending}
	for _, state := range states {
		t.Run(state.String(), func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					s, _ := annexTestStack(t)
					annexTestSection(t, s, state)
					before, _ := s.Connection(annexTestLocalRef)
					beforeTimers := annexTestTimers(s)
					res := annexTestReceive(t, s, label, tt.msg)
					annexTestMessages(t, res, reply, tt.want...)
					annexTestDiscard(t, res, label, tt.msg.MessageType())
					annexTestNoIndications(t, res)
					after, ok := s.Connection(annexTestLocalRef)
					if !ok || !reflect.DeepEqual(after, before) || !reflect.DeepEqual(annexTestTimers(s), beforeTimers) {
						t.Errorf("section changed after mismatch: before %+v, after %+v, exists %v", before, after, ok)
					}
				})
			}
		})
	}
}

// ITU-T Q.714 (05/01) Table B.2: OPC mismatch checking is implemented for
// each applicable row and must precede normal state-dependent actions.
func TestConnectionAnnexB2PointCodeMismatch(t *testing.T) {
	label := RoutingLabel{OPC: 33, DPC: annexTestLocalPC, SLS: 7}
	reply := RoutingLabel{OPC: annexTestLocalPC, DPC: 33, SLS: 7}
	pcError := &ERR{DestinationLocalReference: annexTestPeerRef, ErrorCause: params.ErrorCausePointCodeMismatch}
	tests := []struct {
		name string
		msg  Message
		want Message
	}{
		{"RLSD", &RLSD{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef}, pcError},
		{"RLC", &RLC{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef}, nil},
		{"DT1", &DT1{DestinationLocalReference: annexTestLocalRef, Data: []byte{0x71}}, nil},
		{"DT2", &DT2{DestinationLocalReference: annexTestLocalRef, Data: []byte{0x71}}, nil},
		{"AK", &AK{DestinationLocalReference: annexTestLocalRef}, nil},
		{"ED", &ED{DestinationLocalReference: annexTestLocalRef, Data: []byte{0x71}}, nil},
		{"EA", &EA{DestinationLocalReference: annexTestLocalRef}, nil},
		{"ERR", &ERR{DestinationLocalReference: annexTestLocalRef, ErrorCause: params.ErrorCauseServiceClassMismatch}, nil},
		{"IT", &IT{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef, ProtocolClass: params.ProtocolClass{Class: 2}}, nil},
	}
	states := []ConnectionState{ConnectionIncomingPending, ConnectionEstablished, ConnectionReleasePending}
	for _, state := range states {
		t.Run(state.String(), func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					s, _ := annexTestStack(t)
					annexTestSection(t, s, state)
					before, _ := s.Connection(annexTestLocalRef)
					beforeTimers := annexTestTimers(s)
					res := annexTestReceive(t, s, label, tt.msg)
					if tt.want == nil {
						annexTestMessages(t, res, reply)
					} else {
						annexTestMessages(t, res, reply, tt.want)
					}
					annexTestDiscard(t, res, label, tt.msg.MessageType())
					annexTestNoIndications(t, res)
					after, ok := s.Connection(annexTestLocalRef)
					if !ok || !reflect.DeepEqual(after, before) || !reflect.DeepEqual(annexTestTimers(s), beforeTimers) {
						t.Errorf("section changed after mismatch: before %+v, after %+v, exists %v", before, after, ok)
					}
				})
			}
		})
	}
}

// ITU-T Q.714 (05/01) Table B.2: CC and CREF have N/A source-reference
// and OPC checks; the originating node associates the CC sender in §3.1.4.
func TestConnectionAnnexB2NotApplicableChecks(t *testing.T) {
	label := RoutingLabel{OPC: 33, DPC: annexTestLocalPC, SLS: 7}
	tests := []struct {
		name  string
		msg   Message
		state ConnectionState
	}{
		{"CC", &CC{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: 0x070809, ProtocolClass: params.ProtocolClass{Class: 2}}, ConnectionEstablished},
		{"CREF", &CREF{DestinationLocalReference: annexTestLocalRef, RefusalCause: params.RefusalCauseEndUserOriginated}, ConnectionFrozen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := annexTestStack(t)
			annexTestSection(t, s, ConnectionOutgoingPending)
			res := annexTestReceive(t, s, label, tt.msg)
			annexTestMessages(t, res, RoutingLabel{})
			if len(res.Discards) != 0 || len(res.Indications) != 1 {
				t.Fatalf("result = %+v, want one indication and no discard", res)
			}
			annexTestState(t, s, annexTestLocalRef, tt.state)
			if tt.name == "CC" {
				info, _ := s.Connection(annexTestLocalRef)
				if info.Remote.DPC != 33 || info.RemoteReference != 0x070809 {
					t.Errorf("confirmed peer = %+v, want point code 33 and reference 0x070809", info)
				}
			}
		})
	}
}

// ITU-T Q.714 (05/01) Table B.1 Note: valid class 3-only messages are
// unknown for this class 2 implementation, regardless of connection state
// and of their references, and discarded as syntax errors (§1.1.4.1).
func TestConnectionAnnexB1Class3MessagesDiscarded(t *testing.T) {
	label := RoutingLabel{OPC: annexTestRemotePC, DPC: annexTestLocalPC, SLS: 7}
	msgs := []Message{
		&DT2{DestinationLocalReference: annexTestLocalRef, Data: []byte{0x71}},
		&AK{DestinationLocalReference: annexTestLocalRef},
		&ED{DestinationLocalReference: annexTestLocalRef, Data: []byte{0x71}},
		&EA{DestinationLocalReference: annexTestLocalRef},
		&RSR{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef},
		&RSC{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef},
		// An unassigned destination or a wrong source reference does not
		// make them known.
		&RSR{DestinationLocalReference: annexTestLocalRef + 1, SourceLocalReference: annexTestPeerRef},
		&RSC{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef + 1},
	}
	states := []ConnectionState{ConnectionIncomingPending, ConnectionOutgoingPending, ConnectionEstablished, ConnectionReleasePending}
	for _, state := range states {
		t.Run(state.String(), func(t *testing.T) {
			for _, msg := range msgs {
				t.Run(msg.MessageType().String(), func(t *testing.T) {
					s, _ := annexTestStack(t)
					annexTestSection(t, s, state)
					res := annexTestReceive(t, s, label, msg)
					annexTestMessages(t, res, RoutingLabel{})
					annexTestDiscard(t, res, label, msg.MessageType())
					if !errors.Is(res.Discards[0].Reason, ErrSyntax) || !errors.Is(res.Discards[0].Reason, ErrNotSupported) {
						t.Fatalf("discard reason %v, want a syntax error for an unsupported message", res.Discards[0].Reason)
					}
					annexTestNoIndications(t, res)
					annexTestState(t, s, annexTestLocalRef, state)
				})
			}
		})
	}
}

// ITU-T Q.714 (05/01) Table B.3: the columns are c2 incoming pending,
// c3 outgoing pending, c4 established and c6 local release; §3.3.5 completes
// a remote release immediately at an end node.
func TestConnectionAnnexB3StateActions(t *testing.T) {
	label := RoutingLabel{OPC: annexTestRemotePC, DPC: annexTestLocalPC, SLS: 7}
	reply := RoutingLabel{OPC: annexTestLocalPC, DPC: annexTestRemotePC, SLS: 7}
	columns := []struct {
		name  string
		state ConnectionState
	}{
		{"c2", ConnectionIncomingPending},
		{"c3", ConnectionOutgoingPending},
		{"c4", ConnectionEstablished},
		{"c6", ConnectionReleasePending},
	}
	tests := []struct {
		name   string
		msg    Message
		action [4]string
	}{
		{"CC", &CC{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef, ProtocolClass: params.ProtocolClass{Class: 2}}, [4]string{"discard", "confirm", "discard", "discard"}},
		{"CREF", &CREF{DestinationLocalReference: annexTestLocalRef, RefusalCause: params.RefusalCauseEndUserOriginated}, [4]string{"discard", "refuse", "discard", "discard"}},
		{"RLSD", &RLSD{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef, ReleaseCause: params.ReleaseCauseEndUserOriginated}, [4]string{"discard", "error3", "release", "complete"}},
		{"RLC", &RLC{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef}, [4]string{"discard", "error3", "discard", "complete"}},
		{"ERR", &ERR{DestinationLocalReference: annexTestLocalRef, ErrorCause: params.ErrorCauseUnqualified}, [4]string{"discard", "error3", "error", "errorcomplete"}},
		{"DT1", &DT1{DestinationLocalReference: annexTestLocalRef, Data: []byte{0x71}}, [4]string{"discard", "error3", "data", "discard"}},
		{"IT", &IT{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef, ProtocolClass: params.ProtocolClass{Class: 2}}, [4]string{"discard", "error3", "inactivity", "discard"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i, col := range columns {
				t.Run(col.name, func(t *testing.T) {
					s, _ := annexTestStack(t)
					annexTestSection(t, s, col.state)
					res := annexTestReceive(t, s, label, tt.msg)
					annexTestB3Action(t, s, res, label, reply, tt.msg, col.state, tt.action[i])
				})
			}
		})
	}
}

func annexTestB3Action(t *testing.T, s *Stack, res Result, label, reply RoutingLabel, msg Message, previous ConnectionState, action string) {
	t.Helper()
	switch action {
	case "discard":
		annexTestMessages(t, res, reply)
		annexTestDiscard(t, res, label, msg.MessageType())
		annexTestNoIndications(t, res)
		annexTestState(t, s, annexTestLocalRef, previous)
	case "error3":
		annexTestDiscard(t, res, label, msg.MessageType())
		// ITU-T Q.714 (05/01) Figure C.2 sheet 3: RLSD is acknowledged.
		if released, ok := msg.(*RLSD); ok {
			annexTestMessages(t, res, reply, &RLC{DestinationLocalReference: released.SourceLocalReference, SourceLocalReference: released.DestinationLocalReference})
		} else {
			annexTestMessages(t, res, reply)
		}
		annexTestState(t, s, annexTestLocalRef, ConnectionFrozen)
		if len(res.Indications) != 1 {
			t.Fatalf("indications = %v, want one disconnect", res.Indications)
		}
		d, ok := res.Indications[0].(*DisconnectIndication)
		if !ok || d.LocalReference != annexTestLocalRef || d.Originator != OriginatorProvider || d.Refused != (previous == ConnectionOutgoingPending) {
			t.Errorf("indication = %+v, want provider disconnect for state %s", res.Indications[0], previous)
		}
	case "confirm":
		annexTestMessages(t, res, reply)
		annexTestState(t, s, annexTestLocalRef, ConnectionEstablished)
		if len(res.Indications) != 1 {
			t.Fatalf("indications = %v, want one confirmation", res.Indications)
		}
		c, ok := res.Indications[0].(*ConnectConfirmation)
		if !ok || c.LocalReference != annexTestLocalRef {
			t.Errorf("indication = %+v, want confirmation of %#x", res.Indications[0], annexTestLocalRef)
		}
	case "refuse":
		annexTestMessages(t, res, reply)
		annexTestState(t, s, annexTestLocalRef, ConnectionFrozen)
		annexTestDisconnect(t, res, OriginatorUser, true, 0)
		d := res.Indications[0].(*DisconnectIndication)
		if d.RefusalCause != params.RefusalCauseEndUserOriginated {
			t.Errorf("refusal cause = %s, want end user originated", d.RefusalCause)
		}
	case "release":
		annexTestMessages(t, res, reply, &RLC{DestinationLocalReference: annexTestPeerRef, SourceLocalReference: annexTestLocalRef})
		annexTestState(t, s, annexTestLocalRef, ConnectionFrozen)
		annexTestDisconnect(t, res, OriginatorUser, false, params.ReleaseCauseEndUserOriginated)
	case "complete", "errorcomplete":
		// ITU-T Q.714 (05/01) Figure C.2 sheet 6: ERR ends release silently.
		if action == "errorcomplete" {
			annexTestDiscard(t, res, label, msg.MessageType())
		}
		annexTestMessages(t, res, reply)
		annexTestNoIndications(t, res)
		annexTestState(t, s, annexTestLocalRef, ConnectionFrozen)
	case "error":
		annexTestMessages(t, res, reply)
		annexTestState(t, s, annexTestLocalRef, ConnectionFrozen)
		annexTestDisconnect(t, res, OriginatorProvider, false, params.ReleaseCauseRemoteProcedureError)
	case "data":
		annexTestMessages(t, res, reply)
		annexTestState(t, s, annexTestLocalRef, ConnectionEstablished)
		if len(res.Indications) != 1 {
			t.Fatalf("indications = %v, want one data indication", res.Indications)
		}
		d, ok := res.Indications[0].(*DataIndication)
		if !ok || d.LocalReference != annexTestLocalRef || !reflect.DeepEqual(d.Data, []byte{0x71}) {
			t.Errorf("indication = %+v, want NSDU 0x71 for reference %#x", res.Indications[0], annexTestLocalRef)
		}
	case "inactivity":
		annexTestMessages(t, res, reply)
		annexTestNoIndications(t, res)
		annexTestState(t, s, annexTestLocalRef, ConnectionEstablished)
	default:
		t.Fatalf("unknown Annex B action %q", action)
	}
	if action != "discard" && action != "error3" && action != "errorcomplete" && len(res.Discards) != 0 {
		t.Errorf("discards = %v, want none for normal action", res.Discards)
	}
}

func annexTestDisconnect(t *testing.T, res Result, origin Originator, refused bool, cause params.ReleaseCause) {
	t.Helper()
	if len(res.Indications) != 1 {
		t.Fatalf("indications = %v, want one disconnect", res.Indications)
	}
	d, ok := res.Indications[0].(*DisconnectIndication)
	if !ok || d.LocalReference != annexTestLocalRef || d.Originator != origin || d.Refused != refused || (!refused && d.ReleaseCause != cause) {
		t.Fatalf("indication = %+v, want disconnect origin %s, refused %v, cause %s", res.Indications[0], origin, refused, cause)
	}
}

// ITU-T Q.714 (05/01) Table B.3 CR Note: CR never searches for an existing
// DLR, so every arrival initializes a new incoming section independently.
func TestConnectionAnnexB3CRCreatesNewSection(t *testing.T) {
	label := RoutingLabel{OPC: annexTestRemotePC, DPC: annexTestLocalPC, SLS: 7}
	states := []ConnectionState{ConnectionIdle, ConnectionIncomingPending, ConnectionOutgoingPending, ConnectionEstablished, ConnectionReleasePending}
	for _, state := range states {
		t.Run(state.String(), func(t *testing.T) {
			s, _ := annexTestStack(t)
			if state != ConnectionIdle {
				annexTestSection(t, s, state)
			}
			called := params.NewSSNAddress(6)
			res := annexTestReceive(t, s, label, &CR{SourceLocalReference: annexTestPeerRef, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: called})
			annexTestMessages(t, res, RoutingLabel{})
			if len(res.Discards) != 0 || len(res.Indications) != 1 {
				t.Fatalf("result = %+v, want one connect indication and no discard", res)
			}
			c, ok := res.Indications[0].(*ConnectIndication)
			if !ok || c.LocalReference == annexTestLocalRef || c.RoutingLabel != label || !reflect.DeepEqual(c.CalledPartyAddress, called) {
				t.Fatalf("indication = %+v, want new incoming connection to SSN 6", res.Indications[0])
			}
			annexTestState(t, s, c.LocalReference, ConnectionIncomingPending)
			if state != ConnectionIdle {
				annexTestState(t, s, annexTestLocalRef, state)
			}
		})
	}
}

// ITU-T Q.714 (05/01) §3.4 Table 3: class mismatch releases the connection;
// sequencing/segmenting and credit discrepancies do not apply to class 2.
func TestConnectionAnnexTable3InactivityDiscrepancies(t *testing.T) {
	label := RoutingLabel{OPC: annexTestRemotePC, DPC: annexTestLocalPC, SLS: 7}
	reply := RoutingLabel{OPC: annexTestLocalPC, DPC: annexTestRemotePC, SLS: 7}
	tests := []struct {
		name    string
		class   uint8
		segment params.SequencingSegmenting
		credit  params.Credit
		release bool
	}{
		{"protocol class", 3, params.SequencingSegmenting{}, 0, true},
		{"sequencing and segmenting", 2, params.SequencingSegmenting{SendSequenceNumber: 7, ReceiveSequenceNumber: 13, MoreData: true}, 0, false},
		{"credit", 2, params.SequencingSegmenting{}, 3, false},
		{"all class 2 ignored fields", 2, params.SequencingSegmenting{SendSequenceNumber: 7, ReceiveSequenceNumber: 13, MoreData: true}, 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := annexTestStack(t)
			annexTestSection(t, s, ConnectionEstablished)
			res := annexTestReceive(t, s, label, &IT{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: annexTestPeerRef, ProtocolClass: params.ProtocolClass{Class: tt.class}, SequencingSegmenting: tt.segment, Credit: tt.credit})
			if tt.release {
				annexTestMessages(t, res, reply, &RLSD{DestinationLocalReference: annexTestPeerRef, SourceLocalReference: annexTestLocalRef, ReleaseCause: params.ReleaseCauseInconsistentConnectionData})
				annexTestState(t, s, annexTestLocalRef, ConnectionReleasePending)
				annexTestDisconnect(t, res, OriginatorProvider, false, params.ReleaseCauseInconsistentConnectionData)
			} else {
				annexTestMessages(t, res, reply)
				annexTestNoIndications(t, res)
				annexTestState(t, s, annexTestLocalRef, ConnectionEstablished)
			}
			if tt.release && len(res.Discards) != 0 {
				annexTestDiscard(t, res, label, MsgTypeIT)
			} else if len(res.Discards) != 0 {
				t.Errorf("discards = %v, want none", res.Discards)
			}
		})
	}
}

// ITU-T Q.714 (05/01) Table B.2 Note 3 and §3.4 Table 3: an IT SLR
// mismatch releases c2/c4; c6 replays the stored release without restarting timers.
func TestConnectionAnnexB2ITMismatchReleasesAndPreservesStoredRelease(t *testing.T) {
	for _, state := range []ConnectionState{ConnectionIncomingPending, ConnectionEstablished, ConnectionReleasePending} {
		t.Run(state.String(), func(t *testing.T) {
			s, now := annexTestStack(t)
			annexTestSection(t, s, state)
			c := s.conn.sections[annexTestLocalRef]
			storedLabel := c.Remote
			label := RoutingLabel{OPC: annexTestRemotePC, DPC: annexTestLocalPC, SLS: 13}
			const wrongSource params.LocalReference = 0x070809
			beforeTimers := annexTestTimers(s)
			beforeRelease := c.releaseMessage
			*now = now.Add(time.Second)
			res := annexTestReceive(t, s, label, &IT{DestinationLocalReference: annexTestLocalRef, SourceLocalReference: wrongSource, ProtocolClass: params.ProtocolClass{Class: 2}})
			wantIndications := 1
			if state == ConnectionReleasePending {
				wantIndications = 0
			}
			coTestCounts(t, res, 2, wantIndications, 1)
			annexTestDiscard(t, res, label, MsgTypeIT)
			coTestTransfer(t, res, 0, replyLabel(label), &RLSD{DestinationLocalReference: wrongSource, SourceLocalReference: annexTestLocalRef, ReleaseCause: params.ReleaseCauseInconsistentConnectionData})
			annexTestState(t, s, annexTestLocalRef, ConnectionReleasePending)
			if state == ConnectionReleasePending {
				coTestTransfer(t, res, 1, storedLabel, beforeRelease)
				annexTestNoIndications(t, res)
				if !reflect.DeepEqual(annexTestTimers(s), beforeTimers) || c.releaseMessage != beforeRelease {
					t.Fatal("IT mismatch changed the pending release or its timers")
				}
			} else {
				coTestTransfer(t, res, 1, storedLabel, &RLSD{DestinationLocalReference: annexTestPeerRef, SourceLocalReference: annexTestLocalRef, ReleaseCause: params.ReleaseCauseInconsistentConnectionData})
				annexTestDisconnect(t, res, OriginatorProvider, false, params.ReleaseCauseInconsistentConnectionData)
				if c.sendInactivity != nil || c.receiveInactivity != nil || c.releaseTimer == nil || c.releaseTimer.index < 0 {
					t.Fatal("IT mismatch did not replace inactivity control with T(rel)")
				}
			}
		})
	}
}
