// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/gomaja/go-sccp/params"
	"reflect"
	"testing"
	"time"
)

// ITU-T Q.714 (05/01) Table B.2: unassigned RLSD is answered using received references.
func TestConnectionUnknownReleaseRegression(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{})
	b, err := (&RLSD{DestinationLocalReference: 99, SourceLocalReference: 7, ReleaseCause: params.ReleaseCauseSCCPUserOriginated}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	r := s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1, SLS: 3}, Data: b})
	coTestCounts(t, r, 1, 0, 1)
	annexTestDiscard(t, r, RoutingLabel{OPC: 2, DPC: 1, SLS: 3}, MsgTypeRLSD)
	m, ok := r.Transfers[0].Message.(*RLC)
	if !ok || m.DestinationLocalReference != 7 || m.SourceLocalReference != 99 {
		t.Fatalf("response: %v", r.Transfers[0].Message)
	}
}

// ITU-T Q.714 (05/01) §3.1.2: a returned CR must not let the caller alter section identity.
func TestConnectionReturnedCRDoesNotAliasSection(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{})
	address := params.NewPointCodeAddress(2, 6)
	address.RouteOnSSN = false
	address.GlobalTitle = &params.GlobalTitle{Indicator: params.GTITTOnly, AddressInformation: []byte{0x21}}
	ref, r, err := s.Connect(ConnectRequest{CalledPartyAddress: address})
	if err != nil {
		t.Fatal(err)
	}
	cr := r.Transfers[0].Message.(*CR)
	cr.CalledPartyAddress.GlobalTitle.AddressInformation[0] = 0xff
	info, ok := s.Connection(ref)
	if !ok || info.CalledPartyAddress.GlobalTitle.AddressInformation[0] != 0x21 {
		t.Fatalf("returned CR aliases section: %+v", info)
	}
}

// ITU-T Q.714 (05/01) §3.3.3.2: repeats carry the original release cause and data.
func TestConnectionReleaseRepeatDoesNotAliasResult(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s, err := NewStack(StackConfig{PointCode: 1, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6)})
	if err != nil {
		t.Fatal(err)
	}
	cc, err := (&CC{DestinationLocalReference: ref, SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1}, Data: cc})
	r, err := s.Disconnect(ref, DisconnectRequest{ReleaseCause: params.ReleaseCauseSCCPUserOriginated, Data: []byte{0x12}})
	if err != nil {
		t.Fatal(err)
	}
	sent := r.Transfers[0].Message.(*RLSD)
	sent.ReleaseCause = params.ReleaseCauseEndUserFailure
	sent.Data[0] = 0xff
	now = now.Add(15 * time.Second)
	r = s.HandleTimeout()
	if len(r.Transfers) != 1 {
		t.Fatalf("repeat: %+v", r)
	}
	repeated := r.Transfers[0].Message.(*RLSD)
	if repeated.ReleaseCause != params.ReleaseCauseSCCPUserOriginated || !bytes.Equal(repeated.Data, []byte{0x12}) {
		t.Fatalf("repeat aliases returned RLSD: %+v", repeated)
	}
	repeated.Data[0] = 0xee
	now = now.Add(15 * time.Second)
	r = s.HandleTimeout()
	if len(r.Transfers) != 1 || !bytes.Equal(r.Transfers[0].Message.(*RLSD).Data, []byte{0x12}) {
		t.Fatalf("second repeat: %+v", r)
	}
}

// ITU-T Q.714 (05/01) §3.1.6.1 item 2: calling-address PC takes precedence over MTP OPC.
func TestConnectionCallingPointCodePrecedence(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s, err := NewStack(StackConfig{PointCode: 1, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	s.RegisterSubsystem(6)
	calling := params.NewPointCodeAddress(2, 7)
	cr := &CR{SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: params.NewPointCodeAddress(1, 6), CallingPartyAddress: &calling}
	wire, err := cr.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	r := s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 3, DPC: 1, SLS: 5}, Data: wire})
	if len(r.Indications) != 1 {
		t.Fatalf("CR: %+v", r)
	}
	ref := r.Indications[0].(*ConnectIndication).LocalReference
	r, err = s.Accept(ref, ConnectResponse{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Transfers) != 1 || r.Transfers[0].RoutingLabel != (RoutingLabel{OPC: 1, DPC: 2, SLS: 5}) {
		t.Fatalf("CC must go to calling point code: %+v", r)
	}
	wire, err = (&DT1{DestinationLocalReference: ref, Data: []byte{0x11}}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	r = s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1, SLS: 8}, Data: wire})
	if len(r.Indications) != 1 || len(r.Discards) != 0 {
		t.Fatalf("calling peer DT1: %+v", r)
	}
}

// ITU-T Q.714 (05/01) §3.4: valid class 2 section messages received in
// data transfer reset T(iar), even when their state-table action is DISCARD.
func TestConnectionDiscardedSectionMessageRefreshesReceiveTimer(t *testing.T) {
	for _, kind := range []MsgType{MsgTypeCC, MsgTypeCREF, MsgTypeRLC} {
		t.Run(kind.String(), func(t *testing.T) {
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			s, err := NewStack(StackConfig{PointCode: 1, Now: func() time.Time { return now }, Connection: ConnectionConfig{InactivitySendTimeout: 10 * time.Second, InactivityReceiveTimeout: 12 * time.Second}})
			if err != nil {
				t.Fatal(err)
			}
			ref, _, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6)})
			if err != nil {
				t.Fatal(err)
			}
			cc := &CC{DestinationLocalReference: ref, SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}}
			wire, err := cc.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1}, Data: wire})
			now = now.Add(11 * time.Second)
			var m Message = cc
			switch kind {
			case MsgTypeCREF:
				m = &CREF{DestinationLocalReference: ref}
			case MsgTypeRLC:
				m = &RLC{DestinationLocalReference: ref, SourceLocalReference: 9}
			}
			wire, err = m.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			r := s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1}, Data: wire})
			if len(r.Discards) != 1 || len(r.Indications) != 0 {
				t.Fatalf("discard: %+v", r)
			}
			now = now.Add(time.Second)
			r = s.HandleTimeout()
			if !r.empty() {
				t.Fatalf("old receive deadline fired: %+v", r)
			}
			now = now.Add(11 * time.Second)
			r = s.HandleTimeout()
			found := false
			for _, transfer := range r.Transfers {
				if release, ok := transfer.Message.(*RLSD); ok && release.ReleaseCause == params.ReleaseCauseExpirationOfReceiveInactivityTimer {
					found = true
				}
			}
			if !found {
				t.Fatalf("new receive deadline did not release: %+v", r)
			}
		})
	}
}

// ITU-T Q.714 (05/01) §3.8.4: checking optional control data length
// beyond connection-oriented routing requirements is not mandatory.
func TestConnectionOversizedControlDataDelivered(t *testing.T) {
	for _, kind := range []MsgType{MsgTypeCR, MsgTypeCC, MsgTypeCREF, MsgTypeRLSD} {
		for _, size := range []int{129, 255} {
			t.Run(fmt.Sprintf("%s/%d", kind, size), func(t *testing.T) {
				s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
				ref, _ := coTestOutgoing(t, s)
				var wire []byte
				wantState := ConnectionFrozen
				// ITU-T Q.713 (03/01) Tables 3-6: construct octets directly;
				// the sender API limits optional data to 128 octets.
				switch kind {
				case MsgTypeCR:
					wire = []byte{0x01, 9, 0, 0, 2, 2, 4, 2, 0x42, 6}
					wantState = ConnectionOutgoingPending
				case MsgTypeCC:
					wire = []byte{0x02, byte(ref), byte(ref >> 8), byte(ref >> 16), 9, 0, 0, 2, 1}
					wantState = ConnectionEstablished
				case MsgTypeCREF:
					wire = []byte{0x03, byte(ref), byte(ref >> 8), byte(ref >> 16), 0, 1}
				case MsgTypeRLSD:
					coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &CC{DestinationLocalReference: ref, SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}})
					wire = []byte{0x04, byte(ref), byte(ref >> 8), byte(ref >> 16), 9, 0, 0, 0, 1}
				}
				data := bytes.Repeat([]byte{0x71}, size)
				wire = append(wire, 0x0f, byte(size))
				wire = append(wire, data...)
				wire = append(wire, 0)
				r := s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1}, Data: wire})
				wantTransfers := 0
				if kind == MsgTypeRLSD {
					wantTransfers = 1
				}
				coTestCounts(t, r, wantTransfers, 1, 0)
				var delivered []byte
				switch ind := r.Indications[0].(type) {
				case *ConnectIndication:
					if kind != MsgTypeCR {
						t.Fatalf("unexpected connect indication: %+v", ind)
					}
					delivered = ind.Data
					coTestState(t, s, ind.LocalReference, ConnectionIncomingPending)
				case *ConnectConfirmation:
					if kind != MsgTypeCC || ind.LocalReference != ref {
						t.Fatalf("unexpected confirmation: %+v", ind)
					}
					delivered = ind.Data
				case *DisconnectIndication:
					if (kind != MsgTypeCREF && kind != MsgTypeRLSD) || ind.LocalReference != ref || ind.Refused != (kind == MsgTypeCREF) {
						t.Fatalf("unexpected disconnect: %+v", ind)
					}
					delivered = ind.Data
				default:
					t.Fatalf("unexpected indication: %+v", r.Indications[0])
				}
				if !bytes.Equal(delivered, data) {
					t.Fatalf("delivered data = %x, want %d octets", delivered, size)
				}
				coTestState(t, s, ref, wantState)
			})
		}
	}
}

// ITU-T Q.714 (05/01) §3.8.4: an empty DT1 cannot refresh inactivity or deliver data.
func TestConnectionEmptyDT1DoesNotRefreshInactivity(t *testing.T) {
	cfg := coTestConfig()
	cfg.InactivitySendTimeout = 10 * time.Second
	cfg.InactivityReceiveTimeout = 12 * time.Second
	s, clock := coTestStack(t, StackConfig{Connection: cfg})
	ref, label := coTestEstablished(t, s)
	wire, err := (&DT1{DestinationLocalReference: ref, Data: []byte{1}}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	wire[len(wire)-2] = 0
	wire = wire[:len(wire)-1]
	clock.advance(11 * time.Second)
	r := s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1}, Data: wire})
	coTestCounts(t, r, 1, 0, 1)
	if !errors.Is(r.Discards[0].Reason, ErrSyntax) {
		t.Fatalf("discard: %+v", r)
	}
	clock.advance(time.Second)
	r = s.HandleTimeout()
	coTestTransfer(t, r, 0, label, &RLSD{DestinationLocalReference: 0x102, SourceLocalReference: ref, ReleaseCause: params.ReleaseCauseExpirationOfReceiveInactivityTimer})
}

// Connection is a pure snapshot; ITU-T Q.714 (05/01) §3.1.4.2 timer
// actions are delivered once by HandleTimeout, without a query running them.
func TestConnectionSnapshotDoesNotRunTimers(t *testing.T) {
	s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
	ref, _ := coTestOutgoing(t, s)
	c := s.conn.sections[ref]
	establishment := c.establishment
	deadline := establishment.deadline
	clock.advance(10 * time.Second)
	for range 2 {
		info, ok := s.Connection(ref)
		if !ok || info.State != ConnectionOutgoingPending {
			t.Fatalf("snapshot changed due setup: %+v %v", info, ok)
		}
		if s.conn.sections[ref] != c || c.establishment != establishment || len(s.timers) != 1 || s.timers[0] != establishment || !establishment.deadline.Equal(deadline) || establishment.index < 0 {
			t.Fatal("snapshot changed the due timer or section")
		}
	}
	if next, ok := s.NextTimeout(); !ok || !next.Equal(deadline) {
		t.Fatalf("due timer deadline = %v %v, want %v", next, ok, deadline)
	}
	r := s.HandleTimeout()
	coTestCounts(t, r, 0, 1, 0)
	want := &DisconnectIndication{LocalReference: ref, Originator: OriginatorProvider, Refused: true, RefusalCause: params.RefusalCauseExpirationOfTheConnectionEstablishmentTimer}
	if !reflect.DeepEqual(r.Indications[0], want) {
		t.Fatalf("timer output: %+v", r)
	}
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	clock.advance(20 * time.Second)
	coTestState(t, s, ref, ConnectionFrozen)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	if _, ok := s.Connection(ref); ok || len(s.conn.sections) != 0 {
		t.Fatal("timer processing retained the expired frozen reference")
	}
}

// ITU-T Q.714 (05/01) §3.4: a failed inactivity encoding must not disable T(ias).
func TestConnectionSmallBudgetInactivityRetries(t *testing.T) {
	cfg := coTestConfig()
	cfg.InactivitySendTimeout = time.Second
	cfg.InactivityReceiveTimeout = 5 * time.Second
	s, clock := coTestStack(t, StackConfig{Connection: cfg})
	ref := coTestIncoming(t, s, 2)
	r, err := s.Accept(ref, ConnectResponse{})
	if err != nil {
		t.Fatal(err)
	}
	coTestCounts(t, r, 1, 0, 0)
	// Below the configurable minimum: an IT of 11 octets cannot be sent.
	s.cfg.MaxMessageSize = 9
	for range 2 {
		clock.advance(time.Second)
		r = s.HandleTimeout()
		coTestCounts(t, r, 0, 0, 1)
		if r.Discards[0].MessageType != MsgTypeIT || !errors.Is(r.Discards[0].Reason, ErrUndeliverable) {
			t.Fatalf("failed inactivity send: %+v", r)
		}
		deadline, ok := s.NextTimeout()
		if !ok || !deadline.Equal(clock.now().Add(time.Second)) {
			t.Fatalf("send timer not restarted: %v %v", deadline, ok)
		}
	}
	clock.advance(3 * time.Second)
	r = s.HandleTimeout()
	coTestCounts(t, r, 1, 1, 1)
	coTestState(t, s, ref, ConnectionReleasePending)
}

// At the smallest maximum message size every message the procedures must be
// able to send without user data fits: an incoming request can always be
// accepted or refused, and inactivity control and release keep working
// (ITU-T Q.713 (03/01) Tables 4 to 6, 9, 17 and 18).
func TestConnectionSmallestMessageSize(t *testing.T) {
	s, clock := coTestStack(t, StackConfig{Connection: coTestConfig(), MaxMessageSize: minMessageSize})
	refused := coTestIncoming(t, s, 2)
	if _, err := s.Disconnect(refused, DisconnectRequest{RefusalCause: params.RefusalCauseEndUserOriginated}); err != nil {
		t.Fatalf("CREF: %v", err)
	}
	accepted := coTestIncoming(t, s, 2)
	if _, err := s.Accept(accepted, ConnectResponse{}); err != nil {
		t.Fatalf("CC: %v", err)
	}
	clock.advance(s.cfg.Connection.InactivitySendTimeout)
	r := s.HandleTimeout()
	coTestCounts(t, r, 1, 0, 0)
	if r.Transfers[0].Message.MessageType() != MsgTypeIT || len(r.Transfers[0].Data) != minMessageSize {
		t.Fatalf("T(ias) sent %v", r.Transfers[0].Message)
	}
	if _, err := s.Disconnect(accepted, DisconnectRequest{ReleaseCause: params.ReleaseCauseEndUserOriginated}); err != nil {
		t.Fatalf("RLSD: %v", err)
	}
	coTestCounts(t, coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &RLSD{DestinationLocalReference: 0x777, SourceLocalReference: 0x888}), 1, 0, 1)
}

// ITU-T Q.714 (05/01) §2.6.2 and Table 2: the importance of a message is the
// requested one, at most 4 for CR, CC and CREF and 6 for RLSD; a refusal by
// the SCCP itself carries none, so that the default applies. An importance
// the parameter cannot carry is refused (ITU-T Q.713 (03/01) §3.19).
func TestConnectionImportanceLimits(t *testing.T) {
	seven, eight := params.Importance(7), params.Importance(8)
	sent := func(t *testing.T, res Result, want params.Importance) {
		t.Helper()
		m, err := ParseMessage(res.Transfers[0].Data)
		if err != nil {
			t.Fatal(err)
		}
		var got *params.Importance
		switch m := m.(type) {
		case *CR:
			got = m.Importance
		case *CC:
			got = m.Importance
		case *CREF:
			got = m.Importance
		case *RLSD:
			got = m.Importance
		}
		if got == nil || *got != want {
			t.Fatalf("%s sent with importance %v, want %d", m.MessageType(), got, want)
		}
	}
	s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
	if _, _, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6), Importance: &eight}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CR importance 8: %v", err)
	}
	_, res, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6), Importance: &seven})
	if err != nil {
		t.Fatal(err)
	}
	sent(t, res, 4)

	incoming := coTestIncoming(t, s, 2)
	if _, err := s.Accept(incoming, ConnectResponse{Importance: &eight}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CC importance 8: %v", err)
	}
	if res, err = s.Accept(incoming, ConnectResponse{Importance: &seven}); err != nil {
		t.Fatal(err)
	}
	sent(t, res, 4)
	if _, err := s.Disconnect(incoming, DisconnectRequest{Importance: &eight}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("RLSD importance 8: %v", err)
	}
	if res, err = s.Disconnect(incoming, DisconnectRequest{ReleaseCause: params.ReleaseCauseEndUserOriginated, Importance: &seven}); err != nil {
		t.Fatal(err)
	}
	sent(t, res, 6)

	refused := coTestIncoming(t, s, 2)
	if res, err = s.Disconnect(refused, DisconnectRequest{RefusalCause: params.RefusalCauseEndUserOriginated, Importance: &seven}); err != nil {
		t.Fatal(err)
	}
	sent(t, res, 4)

	// Refused by the SCCP: no importance, whatever the CR carried.
	res = coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &CR{SourceLocalReference: 0x303, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: params.NewPointCodeAddress(1, 9), Importance: &seven})
	if m, err := ParseMessage(res.Transfers[0].Data); err != nil || m.(*CREF).Importance != nil {
		t.Fatalf("provider CREF %v, %v", m, err)
	}
}
