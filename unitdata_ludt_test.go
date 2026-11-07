// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

func ludtFor(req sccp.UnitdataRequest, hop params.HopCounter) *sccp.LUDT {
	return &sccp.LUDT{
		ProtocolClass: params.ProtocolClass{Class: req.ProtocolClass, ReturnOnError: req.ReturnOnError},
		HopCounter:    hop, CalledPartyAddress: req.CalledPartyAddress, CallingPartyAddress: req.CallingPartyAddress,
		Data: req.Data, Importance: req.Importance,
		Segmentation: &params.Segmentation{FirstSegment: true, InSequenceDelivery: req.ProtocolClass == 1},
	}
}

func encodedSize(t *testing.T, m sccp.Message) int {
	t.Helper()
	b, err := (sccp.MarshalOptions{MaxMessageSize: sccp.MaxBroadbandMessageSize}).Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return len(b)
}

// singleCapacity derives the boundary from the codec's lengths and pointers.
func singleCapacity(t *testing.T, xudt bool, size int) int {
	t.Helper()
	req := request(nil)
	for n := 1; n <= sccp.MaxLongData; n++ {
		var m sccp.Message = &sccp.UDT{CalledPartyAddress: req.CalledPartyAddress, CallingPartyAddress: req.CallingPartyAddress, Data: payload(n)}
		if xudt {
			m = &sccp.XUDT{HopCounter: 15, CalledPartyAddress: req.CalledPartyAddress, CallingPartyAddress: req.CallingPartyAddress, Data: payload(n)}
		}
		if _, err := (sccp.MarshalOptions{MaxMessageSize: size}).Marshal(m); err != nil {
			return n - 1
		}
	}
	t.Fatal("ordinary message carried all long data")
	return 0
}

func TestUnitdataLUDTSelection(t *testing.T) {
	broad := sccp.MaxBroadbandMessageSize
	narrow := sccp.DefaultMaxMessageSize
	longSize := encodedSize(t, ludtFor(request(payload(600)), 15))
	udtCapacity := singleCapacity(t, false, broad)
	xudtCapacity := singleCapacity(t, true, broad)
	for _, tc := range []struct {
		name                            string
		useLUDT, useXUDT                bool
		stackSize, requestSize, z, data int
		want                            sccp.MsgType
		count                           int
	}{
		{"reproducer", true, false, broad, 0, 0, 1000, sccp.MsgTypeLUDT, 1},
		{"opt out", false, false, broad, 0, 0, 1000, sccp.MsgTypeXUDT, 5},
		{"UDT fits", true, false, broad, 0, 0, udtCapacity, sccp.MsgTypeUDT, 1},
		{"XUDT fits", true, true, broad, 0, 0, xudtCapacity, sccp.MsgTypeXUDT, 1},
		{"UDT overflow", true, false, broad, 0, 0, udtCapacity + 1, sccp.MsgTypeLUDT, 1},
		{"XUDT overflow", true, true, broad, 0, 0, xudtCapacity + 1, sccp.MsgTypeLUDT, 1},
		{"narrowband", true, false, narrow, 0, 0, 600, sccp.MsgTypeXUDT, 0},
		{"request narrowband", true, false, broad, narrow, 0, 600, sccp.MsgTypeXUDT, 0},
		{"request broadband", true, false, narrow, broad, 0, 600, sccp.MsgTypeLUDT, 1},
		{"exact fit", true, false, broad, longSize, 0, 600, sccp.MsgTypeLUDT, 1},
		{"one octet too large", true, false, broad, longSize - 1, 0, 600, sccp.MsgTypeXUDT, 0},
		{"below Z", true, false, broad, 0, 160, 159, sccp.MsgTypeUDT, 1},
		{"at Z", true, false, broad, 0, 160, 160, sccp.MsgTypeUDT, 1},
		{"above Z ordinary fits", true, false, broad, 0, 160, 161, sccp.MsgTypeXUDT, 2},
		{"above Z XUDT fits", true, true, broad, 0, 160, xudtCapacity, sccp.MsgTypeXUDT, 2},
		{"above Z ordinary overflows", true, false, broad, 0, 160, max(udtCapacity, xudtCapacity) + 1, sccp.MsgTypeLUDT, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStack(t, newClock(), pcA, sccp.StackConfig{UseLUDT: tc.useLUDT, UseXUDT: tc.useXUDT, MaxMessageSize: tc.stackSize, SegmentationThreshold: tc.z})
			req := request(payload(tc.data))
			req.MaxMessageSize = tc.requestSize
			tr := mustUnitdata(t, s, req)
			if tc.count != 0 && len(tr) != tc.count {
				t.Fatalf("got %d transfers, want %d (%s)", len(tr), tc.count, tc.want)
			}
			m := tc.requestSize
			if m == 0 {
				m = tc.stackSize
			}
			for _, out := range tr {
				if out.Message.MessageType() != tc.want || len(out.Data) > m {
					t.Fatalf("got %s of %d octets, want %s within %d", out.Message.MessageType(), len(out.Data), tc.want, m)
				}
			}
			if tc.want == sccp.MsgTypeXUDT && tc.count != 1 {
				segmentsOf(t, tr)
			}
			far := newStack(t, newClock(), pcB, sccp.StackConfig{MaxMessageSize: broad}, 8)
			ind := onlyUnitdata(t, deliver(far, tr...))
			if !bytes.Equal(ind.Data, req.Data) || ind.ProtocolClass != req.ProtocolClass || ind.Segments != len(tr) {
				t.Fatalf("delivered class %d, %d segments, %d octets", ind.ProtocolClass, ind.Segments, len(ind.Data))
			}
		})
	}
}

func TestUnitdataLUDTFieldsAndReturn(t *testing.T) {
	for _, class := range []uint8{0, 1} {
		for _, roe := range []bool{false, true} {
			t.Run(fmt.Sprintf("class%d/return%t", class, roe), func(t *testing.T) {
				cfg := sccp.StackConfig{UseLUDT: true, MaxMessageSize: sccp.MaxBroadbandMessageSize, HopCounter: 9}
				a := newStack(t, newClock(), pcA, cfg, 7)
				req := request(payload(1000))
				req.CalledPartyAddress = params.NewPointCodeAddress(pcB, 8)
				req.CallingPartyAddress = params.NewPointCodeAddress(pcA, 7)
				req.ProtocolClass, req.ReturnOnError = class, roe
				req.Importance = new(params.Importance(7))
				tr := mustUnitdata(t, a, req)
				if len(tr) != 1 {
					t.Fatalf("got %d transfers, want one LUDT", len(tr))
				}
				l, ok := tr[0].Message.(*sccp.LUDT)
				if !ok {
					t.Fatalf("got %T, want LUDT", tr[0].Message)
				}
				if l.Segmentation == nil {
					t.Fatal("LUDT lacks relay segmentation parameter")
				}
				want := ludtFor(req, 9)
				want.Importance = new(params.Importance(6))
				want.Segmentation.LocalReference = l.Segmentation.LocalReference
				if diff := cmp.Diff(want, l, cmpOpts); diff != "" {
					t.Fatalf("LUDT (-want +got):\n%s", diff)
				}
				if len(tr[0].Data) != encodedSize(t, want) {
					t.Fatal("LUDT encoded size differs")
				}
				decoded, err := sccp.ParseMessage(tr[0].Data)
				if err != nil {
					t.Fatal(err)
				}
				if diff := cmp.Diff(sccp.Message(want), decoded, cmpOpts); diff != "" {
					t.Fatalf("encoded LUDT (-want +got):\n%s", diff)
				}
				b := newStack(t, newClock(), pcB, cfg, 8)
				ind := onlyUnitdata(t, deliver(b, tr...))
				if ind.MessageType != sccp.MsgTypeLUDT || ind.Segments != 1 || ind.ProtocolClass != class || ind.ReturnOnError != roe || !bytes.Equal(ind.Data, req.Data) {
					t.Fatalf("LUDT indication: %+v", ind)
				}
				missing := newStack(t, newClock(), pcB, cfg)
				res := deliver(missing, tr...)
				if !roe {
					if len(res.Transfers) != 0 || len(res.Discards) != 1 {
						t.Fatalf("no return requested: %+v", res)
					}
				} else {
					ret := onlyReturn(t, res)
					if ret.Message.MessageType() != sccp.MsgTypeLUDTS {
						t.Fatalf("return is %s", ret.Message.MessageType())
					}
					got := deliver(a, ret)
					if len(got.Indications) != 1 || len(got.Discards) != 0 || len(got.Transfers) != 0 {
						t.Fatalf("N-NOTICE result: %+v", got)
					}
					notice, ok := got.Indications[0].(*sccp.NoticeIndication)
					if !ok || notice.MessageType != sccp.MsgTypeLUDTS || notice.ReasonForReturn != params.ReturnCauseUnequippedUser || !bytes.Equal(notice.Data, req.Data) {
						t.Fatalf("N-NOTICE: %+v", got.Indications[0])
					}
				}
				// The returned message owns the addresses, importance and data.
				req.Data[0] ^= 0xff
				*req.Importance = 0
				req.CalledPartyAddress.SSN = 99
				if l.Data[0] == req.Data[0] || *l.Importance != 6 || l.CalledPartyAddress.SSN != 8 {
					t.Fatal("LUDT aliases request")
				}
			})
		}
	}
}

func TestUnitdataLUDTFallbackReference(t *testing.T) {
	req := request(payload(600))
	req.MaxMessageSize = encodedSize(t, ludtFor(req, 15)) - 1
	with := newStack(t, newClock(), pcA, sccp.StackConfig{UseLUDT: true, MaxMessageSize: sccp.MaxBroadbandMessageSize})
	without := newStack(t, newClock(), pcA, sccp.StackConfig{MaxMessageSize: sccp.MaxBroadbandMessageSize})
	got := segmentsOf(t, mustUnitdata(t, with, req))
	want := segmentsOf(t, mustUnitdata(t, without, req))
	if got[0].Segmentation.LocalReference != want[0].Segmentation.LocalReference {
		t.Fatalf("failed LUDT consumed reference: got %d, want %d", got[0].Segmentation.LocalReference, want[0].Segmentation.LocalReference)
	}
}
