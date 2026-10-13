// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"encoding/binary"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/gomaja/go-sccp/params"
)

// FuzzUnitdataTransfer runs scripts of received messages, segments among them,
// clock advances and subsystem changes against a stack with small reassembly
// bounds. After every step it checks the reassembly bookkeeping (ITU-T Q.714
// (05/01) §4.1.1.2) and that every message the stack sends is a valid service
// message within the maximum message size.
//
// A script is a sequence of steps: an operation octet, then for a message a
// two-octet length, least significant octet first, and the message octets.
func FuzzUnitdataTransfer(f *testing.F) {
	segment := func(ref uint32, first bool, rem uint8, n int, roe bool) []byte {
		x := &XUDT{ProtocolClass: params.ProtocolClass{Class: 1, ReturnOnError: roe}, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8),
			CallingPartyAddress: params.NewSSNAddress(7), Data: make([]byte, n), Segmentation: &params.Segmentation{FirstSegment: first, RemainingSegments: rem, LocalReference: ref}}
		b, err := x.MarshalBinary()
		if err != nil {
			f.Fatal(err)
		}
		return b
	}
	step := func(op byte, msg []byte) []byte {
		return append([]byte{op, byte(len(msg)), byte(len(msg) >> 8)}, msg...)
	}
	script := func(steps ...[]byte) []byte {
		var b []byte
		for _, s := range steps {
			b = append(b, s...)
		}
		return b
	}
	f.Add(script(step(0, segment(1, true, 2, 200, true)), step(0, segment(1, false, 1, 200, false)), step(0, segment(1, false, 0, 100, false))))
	f.Add(script(step(0, segment(2, true, 2, 200, true)), step(0, segment(2, false, 0, 200, true)), step(0, segment(2, false, 1, 200, true))))
	f.Add(script(step(0, segment(3, false, 3, 100, true)), step(0, segment(3, false, 2, 100, true)), step(2, nil), step(0, segment(3, false, 1, 100, true))))
	f.Add(script(step(0, segment(4, true, 3, 240, true)), step(3, nil), step(0, segment(4, false, 2, 240, true)), step(3, nil), step(0, segment(4, true, 1, 50, false))))
	f.Add(script(step(0, segment(5, true, 15, 240, true)), step(0, segment(6, true, 15, 240, true)), step(0, segment(7, true, 15, 240, true)), step(0, segment(8, true, 1, 10, true))))

	f.Fuzz(func(t *testing.T, script []byte) {
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		s, err := NewStack(StackConfig{PointCode: 2, MaxReassemblies: 3, MaxReassemblyOctets: 600, MaxReassemblyOctetsPerPeer: 400,
			ReassemblyTimeout: 10 * time.Second, Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		s.RegisterSubsystem(8)
		available := true
		check := func(res Result) {
			t.Helper()
			for _, out := range res.Transfers {
				m, err := ParseMessage(out.Data)
				if err != nil || len(out.Data) > s.cfg.MaxMessageSize {
					t.Fatalf("sent %d octets that do not decode: %v", len(out.Data), err)
				}
				switch m.MessageType() {
				case MsgTypeUDTS, MsgTypeXUDTS, MsgTypeLUDTS, MsgTypeCREF, MsgTypeERR, MsgTypeRLC:
				default:
					t.Fatalf("an end node sent %s", m.MessageType())
				}
			}
			for _, ind := range res.Indications {
				if u, ok := ind.(*UnitdataIndication); ok && (len(u.Data) == 0 || len(u.Data) > MaxLongData) {
					t.Fatalf("delivered %d octets", len(u.Data))
				}
			}
			for _, d := range res.Discards {
				if !errors.Is(d.Reason, ErrSyntax) && !errors.Is(d.Reason, ErrUndeliverable) && !errors.Is(d.Reason, ErrReassembly) &&
					!errors.Is(d.Reason, ErrIncompleteAddress) && !errors.Is(d.Reason, ErrNotSupported) &&
					!errors.Is(d.Reason, ErrConnectionNotFound) && !errors.Is(d.Reason, ErrConnectionReference) && !errors.Is(d.Reason, ErrConnectionState) {
					t.Fatalf("unclassified discard: %v", d.Reason)
				}
			}
			slots, orphans, octets := 0, 0, 0
			peers := make(map[uint32]int)
			for key, p := range s.reassemblies {
				switch {
				case p.key != key:
					t.Fatal("a process is filed under another key")
				case p.timer == nil || p.timer.index < 0:
					t.Fatal("a process or record has no running timer")
				case p.failed && p.data != nil:
					t.Fatal("a record of a failed message holds data")
				case p.orphan && !p.failed:
					t.Fatal("a process marked as having none")
				case p.orphan:
					orphans++
				default:
					// A process, or the record that took its place.
					slots++
					octets += len(p.data)
					peers[key.label.OPC] += len(p.data)
					if !p.failed && (len(p.data) > p.bound || len(p.data) > MaxLongData || p.firstLen > len(p.data)) {
						t.Fatalf("process holds %d octets, first %d, bound %d", len(p.data), p.firstLen, p.bound)
					}
				}
			}
			if slots != s.reassemblySlots || slots > s.cfg.MaxReassemblies || orphans != s.orphanRecords || orphans > s.cfg.MaxReassemblies ||
				octets != s.reassemblyOctets || octets > s.cfg.MaxReassemblyOctets {
				t.Fatalf("%d slots hold %d octets (accounted %d slots, %d octets), %d records of messages with no process (accounted %d)",
					slots, octets, s.reassemblySlots, s.reassemblyOctets, orphans, s.orphanRecords)
			}
			for opc, held := range peers {
				if held == 0 {
					delete(peers, opc)
				} else if held > s.cfg.MaxReassemblyOctetsPerPeer {
					t.Fatalf("peer %d holds %d octets, more than %d", opc, held, s.cfg.MaxReassemblyOctetsPerPeer)
				}
			}
			if !maps.Equal(peers, s.peerOctets) {
				t.Fatalf("octets by peer %v, accounted %v", peers, s.peerOctets)
			}
		}
		for len(script) > 0 {
			op := script[0]
			script = script[1:]
			switch op % 4 {
			case 0, 1:
				if len(script) < 2 {
					return
				}
				n := min(int(binary.LittleEndian.Uint16(script)), len(script)-2)
				msg := script[2 : 2+n]
				script = script[2+n:]
				// Bit 4 moves the message to another link, another SLS; bit 5
				// to another peer, another OPC.
				check(s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 1 + uint32(op>>5&1), DPC: 2, SLS: op >> 4 & 1}, Data: msg}))
			case 2:
				now = now.Add(time.Duration(op>>2) * time.Second)
				check(s.HandleTimeout())
			case 3:
				available = !available
				s.SetSubsystemAvailable(8, available)
			}
		}
		now = now.Add(time.Hour)
		check(s.HandleTimeout())
		// A process that T(reassembly) ends leaves the record of its message
		// for one more period.
		now = now.Add(time.Hour)
		check(s.HandleTimeout())
		if len(s.reassemblies) != 0 || s.reassemblyOctets != 0 || len(s.peerOctets) != 0 || s.reassemblySlots != 0 || s.orphanRecords != 0 {
			t.Fatalf("after every timer: %d entries, %d octets, %d slots, %d records", len(s.reassemblies), s.reassemblyOctets, s.reassemblySlots, s.orphanRecords)
		}
	})
}
