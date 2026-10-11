// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"errors"
	"testing"
	"time"

	"github.com/gomaja/go-sccp/params"
)

// ITU-T Q.714 (05/01) Tables B.1-B.3, §3.8.4: arbitrary incoming messages
// cannot bypass bounded state, syntax checking or conservative output encoding.
func FuzzConnectionTransfer(f *testing.F) {
	seeds := []Message{
		&CR{SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: params.NewPointCodeAddress(1, 6)},
		&CC{DestinationLocalReference: 1, SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}},
		&CREF{DestinationLocalReference: 1, RefusalCause: params.RefusalCauseSCCPUserOriginated},
		&DT1{DestinationLocalReference: 1, MoreData: true, Data: []byte{1}},
		&RLSD{DestinationLocalReference: 1, SourceLocalReference: 9},
		&RLC{DestinationLocalReference: 1, SourceLocalReference: 9},
		&IT{DestinationLocalReference: 1, SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}},
		&ERR{DestinationLocalReference: 1, ErrorCause: params.ErrorCauseServiceClassMismatch},
		&RSR{DestinationLocalReference: 1, SourceLocalReference: 9},
		&RSC{DestinationLocalReference: 1, SourceLocalReference: 9},
		&DT2{DestinationLocalReference: 1, Data: []byte{1}},
		// ITU-T Q.714 (05/01) §2.2.2.1 item 4: a documented bare discard reason.
		&UDT{CalledPartyAddress: params.NewSSNAddress(0), CallingPartyAddress: params.NewSSNAddress(6), Data: []byte{1}},
		// Two segments exhaust the capacity budget and return bare ErrReassembly.
		&DT1{DestinationLocalReference: 1, MoreData: true, Data: make([]byte, 255)},
	}
	for _, state := range []ConnectionState{ConnectionIncomingPending, ConnectionOutgoingPending, ConnectionEstablished, ConnectionReleasePending, ConnectionFrozen} {
		for _, m := range seeds {
			wire, err := m.MarshalBinary()
			if err != nil {
				f.Fatal(err)
			}
			f.Add(uint8(state), wire)
			f.Add(uint8(state)|0x80, wire)
		}
	}
	f.Add(uint8(ConnectionEstablished), []byte{byte(MsgTypeDT1), 1, 0, 0, 0, 1, 0})
	f.Add(uint8(ConnectionEstablished), []byte{0xff})
	f.Fuzz(func(t *testing.T, state uint8, wire []byte) {
		if len(wire) > 4096 {
			t.Skip()
		}
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		s, err := NewStack(StackConfig{PointCode: 1, Now: func() time.Time { return now }, Connection: ConnectionConfig{MaxConnections: 4, MaxDataLength: 512, MaxReassemblyOctets: 256}})
		if err != nil {
			t.Fatal(err)
		}
		s.RegisterSubsystem(6)
		states := []ConnectionState{ConnectionIncomingPending, ConnectionOutgoingPending, ConnectionEstablished, ConnectionReleasePending, ConnectionFrozen}
		selected := states[(int(state&0x7f)+len(states)-1)%len(states)]
		// ITU-T Q.714 (05/01) §3.1.4.2: Connect leaves c3 with no remote reference.
		if selected == ConnectionIncomingPending {
			coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: 3}, &CR{SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: params.NewSSNAddress(6)})
		} else {
			if _, _, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6)}); err != nil {
				t.Fatal(err)
			}
			if selected != ConnectionOutgoingPending {
				coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &CC{DestinationLocalReference: 1, SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}})
				if selected == ConnectionReleasePending || selected == ConnectionFrozen {
					if _, err := s.Disconnect(1, DisconnectRequest{}); err != nil {
						t.Fatal(err)
					}
					if selected == ConnectionFrozen {
						coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &RLC{DestinationLocalReference: 1, SourceLocalReference: 9})
					}
				}
			}
		}
		indicated := make(map[params.LocalReference]bool)
		userDisconnected := make(map[params.LocalReference]bool)
		if selected == ConnectionReleasePending || selected == ConnectionFrozen {
			userDisconnected[1] = true
		}
		check := func(r Result) {
			for _, out := range r.Transfers {
				if len(out.Data) > s.cfg.MaxMessageSize {
					t.Fatal("message exceeded maximum size")
				}
				if _, err := ParseMessage(out.Data); err != nil {
					t.Fatalf("outbound syntax: %v", err)
				}
			}
			for _, d := range r.Discards {
				if !errors.Is(d.Reason, ErrSyntax) && !errors.Is(d.Reason, ErrUndeliverable) && !errors.Is(d.Reason, ErrConnectionState) && !errors.Is(d.Reason, ErrConnectionNotFound) && !errors.Is(d.Reason, ErrConnectionReference) && !errors.Is(d.Reason, ErrIncompleteAddress) && !errors.Is(d.Reason, ErrReassembly) {
					t.Fatalf("unclassified discard: %v", d.Reason)
				}
			}
			// ITU-T Q.711 (03/01) Figure 8: disconnect is indicated at most once,
			// and never after the user's own N-DISCONNECT request.
			for _, indication := range r.Indications {
				if d, ok := indication.(*DisconnectIndication); ok {
					if indicated[d.LocalReference] || userDisconnected[d.LocalReference] {
						t.Fatalf("duplicate or user-requested disconnect indication: %#x", d.LocalReference)
					}
					indicated[d.LocalReference] = true
				}
			}
			held := 0
			for _, c := range s.conn.sections {
				held += cap(c.reassembly)
			}
			if held != s.conn.reassemblyOctets || held > s.cfg.Connection.MaxReassemblyOctets {
				t.Fatalf("reassembly accounting: capacity %d, held %d, limit %d", held, s.conn.reassemblyOctets, s.cfg.Connection.MaxReassemblyOctets)
			}
			if s.conn.active < 0 || s.conn.active > s.cfg.Connection.MaxConnections {
				t.Fatalf("invalid active count: %d", s.conn.active)
			}
		}
		if state&0x80 != 0 && selected != ConnectionReleasePending && selected != ConnectionFrozen {
			r, err := s.Disconnect(1, DisconnectRequest{ReleaseCause: params.ReleaseCauseEndUserOriginated})
			if err != nil {
				t.Fatal(err)
			}
			userDisconnected[1] = true
			check(r)
		}
		check(s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1, SLS: 8}, Data: wire}))
		// A second transfer exercises partial reassembly, duplicate messages and released references.
		check(s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1, SLS: 8}, Data: wire}))
		now = now.Add(time.Hour)
		check(s.HandleTimeout())
		now = now.Add(time.Hour)
		check(s.HandleTimeout())
	})
}
