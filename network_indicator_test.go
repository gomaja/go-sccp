// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gomaja/go-sccp/params"
)

func TestStackRoutingConfig(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     StackConfig
		invalid bool
	}{
		{"default", StackConfig{}, false},
		{"spare", StackConfig{NetworkIndicator: 1}, false},
		{"national", StackConfig{NetworkIndicator: 2}, false},
		{"reserved", StackConfig{NetworkIndicator: 3}, false},
		{"invalid_network", StackConfig{NetworkIndicator: 4}, true},
		{"maximum_point_code", StackConfig{PointCode: 0x3fff}, false},
		{"wide_point_code", StackConfig{PointCode: 0x4000}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewStack(tc.cfg)
			if tc.invalid {
				if !errors.Is(err, ErrInvalidRequest) || s != nil {
					t.Fatalf("NewStack = %v, %v", s, err)
				}
			} else if err != nil || s.cfg.NetworkIndicator != tc.cfg.NetworkIndicator || s.cfg.PointCode != tc.cfg.PointCode {
				t.Fatalf("NewStack = %v, %v", s, err)
			}
		})
	}
}

func TestRoutingOrigination(t *testing.T) {
	for _, ni := range []NetworkIndicator{NIInternational, NINational} {
		for _, dpc := range []uint32{2, 0x3fff, 0x4000} {
			for _, connect := range []bool{false, true} {
				t.Run(fmt.Sprintf("ni_%d/dpc_%x/connect_%t", ni, dpc, connect), func(t *testing.T) {
					s, _ := coTestStack(t, StackConfig{NetworkIndicator: ni})
					var res Result
					var err error
					if connect {
						_, res, err = s.Connect(ConnectRequest{DPC: dpc, CalledPartyAddress: params.NewSSNAddress(6)})
					} else {
						res, err = s.Unitdata(UnitdataRequest{DPC: dpc, CalledPartyAddress: params.NewSSNAddress(6), CallingPartyAddress: params.NewSSNAddress(7), Data: []byte{1}})
					}
					if dpc > params.MaxPointCode {
						if !errors.Is(err, ErrInvalidRequest) {
							t.Fatalf("wide DPC: %v", err)
						}
						coTestCounts(t, res, 0, 0, 0)
						if s.conn.active != 0 || s.nextSLS != 0 {
							t.Fatal("invalid request allocated state")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					coTestCounts(t, res, 1, 0, 0)
					if got := res.Transfers[0].RoutingLabel; got.NI != ni || got.DPC != dpc || got.OPC != 1 {
						t.Fatalf("label %+v", got)
					}
				})
			}
		}
	}
}

func TestTransferRoutingBounds(t *testing.T) {
	gt, err := params.NewGlobalTitle1(params.NAIInternationalNumber, "1234")
	if err != nil {
		t.Fatal(err)
	}
	m := &UDT{CalledPartyAddress: params.NewGlobalTitleAddress(gt, 6), CallingPartyAddress: params.NewSSNAddress(7), Data: []byte{1}}
	for _, tc := range []struct {
		name    string
		label   RoutingLabel
		invalid string
	}{
		{"zero", RoutingLabel{}, ""},
		{"maximum", RoutingLabel{OPC: 0x3fff, DPC: 0x3fff, SLS: 15, NI: 3}, ""},
		{"opc", RoutingLabel{OPC: 0x4000}, "originating point code"},
		{"dpc", RoutingLabel{DPC: 0x4000}, "destination point code"},
		{"sls", RoutingLabel{SLS: 16}, "signalling link selection"},
		{"ni", RoutingLabel{NI: 4}, "network indicator 4, want 0 to 3"},
	} {
		for _, malformed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/malformed_%t", tc.name, malformed), func(t *testing.T) {
				calls, fired := 0, false
				s, clock := coTestStack(t, StackConfig{LocalGlobalTitle: func(params.PartyAddress) bool { calls++; return true }})
				s.schedule(clock.now().Add(time.Second), func(_ time.Time, _ *Result) { fired = true })
				clock.advance(time.Second)
				data := must(m.MarshalBinary())
				if malformed {
					data = []byte{0xff}
				}
				res := s.HandleTransfer(TransferIndication{RoutingLabel: tc.label, Data: data})
				if !fired {
					t.Fatal("due timer did not run")
				}
				if tc.invalid != "" {
					coTestCounts(t, res, 0, 0, 1)
					d := res.Discards[0]
					if !errors.Is(d.Reason, ErrInvalidRequest) || errors.Is(d.Reason, ErrSyntax) || !strings.Contains(d.Reason.Error(), tc.invalid) || d.RoutingLabel != tc.label || d.MessageType != MsgType(data[0]) || calls != 0 {
						t.Fatalf("discard %+v, callback calls %d", d, calls)
					}
				} else if malformed {
					coTestCounts(t, res, 0, 0, 1)
					if !errors.Is(res.Discards[0].Reason, ErrSyntax) {
						t.Fatal(res.Discards[0].Reason)
					}
				} else {
					coTestCounts(t, res, 0, 1, 0)
					if calls != 1 || res.Indications[0].(*UnitdataIndication).RoutingLabel != tc.label {
						t.Fatalf("result %+v, calls %d", res, calls)
					}
				}
			})
		}
	}
}

func TestNetworkReturns(t *testing.T) {
	called, calling := params.NewSSNAddress(8), params.NewSSNAddress(7)
	pc := params.ProtocolClass{Class: 1, ReturnOnError: true}
	for _, tc := range []struct {
		m    Message
		want MsgType
	}{
		{&UDT{ProtocolClass: pc, CalledPartyAddress: called, CallingPartyAddress: calling, Data: []byte{1}}, MsgTypeUDTS},
		{&XUDT{ProtocolClass: pc, HopCounter: 15, CalledPartyAddress: called, CallingPartyAddress: calling, Data: []byte{1}, Segmentation: &params.Segmentation{FirstSegment: true, RemainingSegments: 1, LocalReference: 9}}, MsgTypeXUDTS},
		{&LUDT{ProtocolClass: pc, HopCounter: 15, CalledPartyAddress: called, CallingPartyAddress: calling, Data: []byte{1}}, MsgTypeLUDTS},
	} {
		t.Run(tc.want.String(), func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{})
			in := RoutingLabel{OPC: 2, DPC: 1, SLS: 7, NI: NINational}
			res := coTestReceive(t, s, in, tc.m)
			coTestCounts(t, res, 1, 0, 1)
			want := RoutingLabel{OPC: 1, DPC: 2, SLS: 7, NI: NINational}
			if res.Transfers[0].RoutingLabel != want || res.Transfers[0].Message.MessageType() != tc.want || res.Discards[0].RoutingLabel != in {
				t.Fatalf("return %+v", res)
			}
		})
	}
}

func TestNetworkReassembly(t *testing.T) {
	for _, budget := range []int{0, 8} {
		t.Run(fmt.Sprintf("peer_budget_%d", budget), func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{MaxReassemblyOctetsPerPeer: budget})
			segment := func(ni NetworkIndicator, first bool) Result {
				rem := uint8(0)
				if first {
					rem = 1
				}
				return coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: 7, NI: ni}, &XUDT{ProtocolClass: params.ProtocolClass{Class: 1, ReturnOnError: true}, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(6), CallingPartyAddress: params.NewSSNAddress(7), Data: bytes.Repeat([]byte{byte(ni)}, 4), Segmentation: &params.Segmentation{FirstSegment: first, RemainingSegments: rem, LocalReference: 9}})
			}
			for _, ni := range []NetworkIndicator{NIInternational, NINational} {
				coTestCounts(t, segment(ni, true), 0, 0, 0)
			}
			for _, ni := range []NetworkIndicator{NIInternational, NINational} {
				res := segment(ni, false)
				coTestCounts(t, res, 0, 1, 0)
				u := res.Indications[0].(*UnitdataIndication)
				if u.NI != ni || u.Segments != 2 || !bytes.Equal(u.Data, bytes.Repeat([]byte{byte(ni)}, 8)) {
					t.Fatalf("reassembled %+v", u)
				}
			}
			if s.reassemblyOctets != 0 || len(s.peerOctets) != 0 {
				t.Fatal("reassembly accounting retained completed messages")
			}
		})
	}
}

func TestNetworkPeerBudget(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{MaxReassemblyOctetsPerPeer: 8})
	for _, tc := range []struct {
		ni     NetworkIndicator
		sls    uint8
		reject bool
	}{
		{NIInternational, 0, false}, {NINational, 0, false}, {NIInternational, 1, true}, {NINational, 1, true},
	} {
		res := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: tc.sls, NI: tc.ni}, &XUDT{ProtocolClass: params.ProtocolClass{Class: 1, ReturnOnError: true}, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(6), CallingPartyAddress: params.NewSSNAddress(7), Data: make([]byte, 5), Segmentation: &params.Segmentation{FirstSegment: true, RemainingSegments: 1, LocalReference: 9}})
		if tc.reject {
			coTestCounts(t, res, 1, 0, 1)
			if res.Transfers[0].Message.(*XUDTS).ReturnCause != params.ReturnCauseNetworkCongestion || !errors.Is(res.Discards[0].Reason, ErrReassembly) {
				t.Fatalf("congestion result %+v", res)
			}
		} else {
			coTestCounts(t, res, 0, 0, 0)
		}
	}
}

func TestNetworkConnections(t *testing.T) {
	in := RoutingLabel{OPC: 2, DPC: 1, SLS: 7, NI: NINational}
	out := RoutingLabel{OPC: 1, DPC: 2, SLS: 7, NI: NINational}
	cr := &CR{SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: params.NewSSNAddress(6)}
	t.Run("refuse", func(t *testing.T) {
		s, _ := coTestStack(t, StackConfig{})
		s.UnregisterSubsystem(6)
		res := coTestReceive(t, s, in, cr)
		coTestCounts(t, res, 1, 0, 0)
		if res.Transfers[0].RoutingLabel != out || res.Transfers[0].Message.MessageType() != MsgTypeCREF {
			t.Fatalf("refusal %+v", res)
		}
	})
	t.Run("accept_and_section_messages", func(t *testing.T) {
		s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
		res := coTestReceive(t, s, in, cr)
		coTestCounts(t, res, 0, 1, 0)
		ind := res.Indications[0].(*ConnectIndication)
		if ind.RoutingLabel != in {
			t.Fatalf("indication %+v", ind)
		}
		res, err := s.Accept(ind.LocalReference, ConnectResponse{})
		if err != nil {
			t.Fatal(err)
		}
		coTestCounts(t, res, 1, 0, 0)
		if res.Transfers[0].RoutingLabel != out || res.Transfers[0].Message.MessageType() != MsgTypeCC {
			t.Fatalf("confirmation %+v", res)
		}
		info, _ := s.Connection(ind.LocalReference)
		if info.Remote != out {
			t.Fatalf("remote %+v", info.Remote)
		}
		res, err = s.SendData(ind.LocalReference, []byte{1})
		if err != nil {
			t.Fatal(err)
		}
		coTestCounts(t, res, 1, 0, 0)
		if res.Transfers[0].RoutingLabel != out || res.Transfers[0].Message.MessageType() != MsgTypeDT1 {
			t.Fatalf("data transfer %+v", res)
		}

		clock.advance(30 * time.Second)
		res = s.HandleTimeout()
		coTestCounts(t, res, 1, 0, 0)
		if res.Transfers[0].RoutingLabel != out || res.Transfers[0].Message.MessageType() != MsgTypeIT {
			t.Fatalf("inactivity %+v", res)
		}
		res, err = s.Disconnect(ind.LocalReference, DisconnectRequest{})
		if err != nil {
			t.Fatal(err)
		}
		coTestCounts(t, res, 1, 0, 0)
		if res.Transfers[0].RoutingLabel != out || res.Transfers[0].Message.MessageType() != MsgTypeRLSD {
			t.Fatalf("release %+v", res)
		}
	})
	t.Run("confirming_network", func(t *testing.T) {
		s, _ := coTestStack(t, StackConfig{})
		ref, _ := coTestOutgoing(t, s)
		res := coTestReceive(t, s, RoutingLabel{OPC: 3, DPC: 1, NI: NINational}, &CC{DestinationLocalReference: ref, SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}})
		coTestCounts(t, res, 0, 1, 0)
		info, _ := s.Connection(ref)
		if info.Remote.DPC != 3 || info.Remote.NI != NINational {
			t.Fatalf("confirming peer %+v", info.Remote)
		}
		res = coTestReceive(t, s, RoutingLabel{OPC: 3, DPC: 1, NI: NINational}, &DT1{DestinationLocalReference: ref, Data: []byte{1}})
		coTestCounts(t, res, 0, 1, 0)
		res, err := s.Disconnect(ref, DisconnectRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Transfers[0].NI != NINational || res.Transfers[0].DPC != 3 {
			t.Fatalf("release %+v", res)
		}
	})
}

func TestNetworkConnectionMismatch(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{})
	ref, _ := coTestEstablished(t, s)
	in := RoutingLabel{OPC: 2, DPC: 1, SLS: 7, NI: NINational}
	res := coTestReceive(t, s, in, &RLSD{DestinationLocalReference: ref, SourceLocalReference: 0x102})
	coTestCounts(t, res, 1, 0, 1)
	if !errors.Is(res.Discards[0].Reason, ErrConnectionReference) || res.Discards[0].RoutingLabel != in {
		t.Fatalf("discard %+v", res)
	}
	coTestTransfer(t, res, 0, RoutingLabel{OPC: 1, DPC: 2, SLS: 7, NI: NINational}, &ERR{DestinationLocalReference: 0x102, ErrorCause: params.ErrorCausePointCodeMismatch})
	coTestState(t, s, ref, ConnectionEstablished)
}

func TestNetworkIndications(t *testing.T) {
	in := RoutingLabel{OPC: 2, DPC: 1, SLS: 7, NI: NINational}
	t.Run("notice", func(t *testing.T) {
		s, _ := coTestStack(t, StackConfig{})
		res := coTestReceive(t, s, in, &UDTS{ReturnCause: params.ReturnCauseUnequippedUser, CalledPartyAddress: params.NewSSNAddress(6), CallingPartyAddress: params.NewSSNAddress(7), Data: []byte{1}})
		coTestCounts(t, res, 0, 1, 0)
		if res.Indications[0].(*NoticeIndication).RoutingLabel != in {
			t.Fatalf("notice %+v", res)
		}
	})
	t.Run("maintenance", func(t *testing.T) {
		gt, err := params.NewGlobalTitle1(params.NAIInternationalNumber, "1234")
		if err != nil {
			t.Fatal(err)
		}
		s, _ := coTestStack(t, StackConfig{})
		res := coTestReceive(t, s, in, &XUDT{ProtocolClass: params.ProtocolClass{ReturnOnError: true}, HopCounter: 1, CalledPartyAddress: params.NewGlobalTitleAddress(gt, 6), CallingPartyAddress: params.NewPointCodeAddress(3, 7), Data: []byte{1}})
		coTestCounts(t, res, 1, 1, 1)
		if res.Indications[0].(*MaintenanceIndication).RoutingLabel != in || res.Transfers[0].NI != NINational || res.Transfers[0].DPC != 3 {
			t.Fatalf("maintenance and return %+v", res)
		}
	})
	t.Run("empty_invalid_transfer", func(t *testing.T) {
		s, _ := coTestStack(t, StackConfig{})
		res := s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{NI: 4}})
		coTestCounts(t, res, 0, 0, 1)
		if res.Discards[0].NI != 4 || res.Discards[0].MessageType != 0 || !errors.Is(res.Discards[0].Reason, ErrInvalidRequest) {
			t.Fatalf("empty discard %+v", res)
		}
	})
}

func TestNetworkMismatchResponses(t *testing.T) {
	in := RoutingLabel{OPC: 2, DPC: 1, SLS: 7, NI: NINational}
	out := RoutingLabel{OPC: 1, DPC: 2, SLS: 7, NI: NINational}
	for _, tc := range []struct {
		name           string
		incoming, want Message
	}{
		{"unassigned_release", &RLSD{DestinationLocalReference: 99, SourceLocalReference: 9}, &RLC{DestinationLocalReference: 9, SourceLocalReference: 99}},
		{"unassigned_confirmation", &CC{DestinationLocalReference: 99, SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}}, &ERR{DestinationLocalReference: 9, ErrorCause: params.ErrorCauseUnassignedDestinationLRN}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{})
			res := coTestReceive(t, s, in, tc.incoming)
			coTestCounts(t, res, 1, 0, 1)
			coTestTransfer(t, res, 0, out, tc.want)
		})
	}
	t.Run("inconsistent_inactivity", func(t *testing.T) {
		s, _ := coTestStack(t, StackConfig{})
		res := coTestReceive(t, s, in, &CR{SourceLocalReference: 9, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: params.NewSSNAddress(6)})
		ref := res.Indications[0].(*ConnectIndication).LocalReference
		if _, err := s.Accept(ref, ConnectResponse{}); err != nil {
			t.Fatal(err)
		}
		res = coTestReceive(t, s, in, &IT{DestinationLocalReference: ref, SourceLocalReference: 10, ProtocolClass: params.ProtocolClass{Class: 2}})
		coTestCounts(t, res, 2, 1, 1)
		coTestTransfer(t, res, 0, out, &RLSD{DestinationLocalReference: 10, SourceLocalReference: ref, ReleaseCause: params.ReleaseCauseInconsistentConnectionData})
		if res.Transfers[1].RoutingLabel != out {
			t.Fatalf("section release %+v", res)
		}
	})
}
