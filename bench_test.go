// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp_test

import (
	"testing"
	"time"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

func benchmarkGTAddress(b *testing.B, ssn uint8, digits string) params.PartyAddress {
	b.Helper()
	gt, err := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, digits)
	if err != nil {
		b.Fatal(err)
	}
	return params.NewGlobalTitleAddress(gt, ssn)
}

func benchmarkMessage(b *testing.B, name string, m sccp.Message, options sccp.MarshalOptions) {
	b.Helper()
	wire, err := options.Marshal(m)
	if err != nil {
		b.Fatal(err)
	}
	b.Run(name+"/ParseMessage", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		for b.Loop() {
			if _, err := sccp.ParseMessage(wire); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run(name+"/Append", func(b *testing.B) {
		buf := make([]byte, 0, len(wire))
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		for b.Loop() {
			var err error
			buf, err = options.Append(buf[:0], m)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkCodec(b *testing.B) {
	called := benchmarkGTAddress(b, 8, "447700900001")
	calling := benchmarkGTAddress(b, 7, "447700900002")
	benchmarkMessage(b, "UDTGlobalTitle180", &sccp.UDT{
		ProtocolClass:       params.ProtocolClass{Class: 1, ReturnOnError: true},
		CalledPartyAddress:  called,
		CallingPartyAddress: calling,
		Data:                payload(180),
	}, sccp.MarshalOptions{})
	benchmarkMessage(b, "XUDTSegment", &sccp.XUDT{
		ProtocolClass:       params.ProtocolClass{Class: 1},
		HopCounter:          params.MaxHopCounter,
		CalledPartyAddress:  ssn8,
		CallingPartyAddress: ssn7,
		Data:                payload(180),
		Segmentation:        &params.Segmentation{FirstSegment: true, InSequenceDelivery: true, RemainingSegments: 1, LocalReference: 1},
	}, sccp.MarshalOptions{})
	benchmarkMessage(b, "LUDT3952", &sccp.LUDT{
		ProtocolClass:       params.ProtocolClass{Class: 1},
		HopCounter:          params.MaxHopCounter,
		CalledPartyAddress:  ssn8,
		CallingPartyAddress: ssn7,
		Data:                payload(3952),
	}, sccp.MarshalOptions{MaxMessageSize: sccp.MaxBroadbandMessageSize})
	benchmarkMessage(b, "CROptional", &sccp.CR{
		SourceLocalReference: 0x030201,
		ProtocolClass:        params.ProtocolClass{Class: 3},
		CalledPartyAddress:   ssn6,
		Credit:               new(params.Credit(5)),
		CallingPartyAddress:  &ssn7,
		Data:                 []byte{0xaa, 0xbb},
		HopCounter:           new(params.HopCounter(4)),
		Importance:           new(params.Importance(2)),
	}, sccp.MarshalOptions{})

	scmg := sccp.SCMG{Type: sccp.SCMGTypeSSC, AffectedSSN: 1, AffectedPointCode: 0x0102, CongestionLevel: 5, AffectedService: sccp.SCCPServiceConnectionless}
	wire, err := scmg.MarshalBinary()
	if err != nil {
		b.Fatal(err)
	}
	b.Run("SCMGSSC/UnmarshalBinary", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		var got sccp.SCMG
		for b.Loop() {
			if err := got.UnmarshalBinary(wire); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("SCMGSSC/Append", func(b *testing.B) {
		buf := make([]byte, 0, len(wire))
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		for b.Loop() {
			var err error
			buf, err = scmg.AppendBinary(buf[:0])
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkStackUnitdata(b *testing.B) {
	b.Run("UDT200", func(b *testing.B) {
		clock := newClock()
		s := newStack(b, clock, pcA, sccp.StackConfig{})
		req := request(payload(200))
		b.ReportAllocs()
		b.SetBytes(int64(len(req.Data)))
		for b.Loop() {
			if _, err := s.Unitdata(req); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("XUDT16x3952", func(b *testing.B) {
		clock := newClock()
		s := newStack(b, clock, pcA, sccp.StackConfig{})
		req := request(payload(3952))
		b.ReportAllocs()
		b.SetBytes(int64(len(req.Data)))
		for b.Loop() {
			res, err := s.Unitdata(req)
			if err != nil || len(res.Transfers) != sccp.MaxSegments {
				b.Fatalf("Unitdata transfers = %d, %v", len(res.Transfers), err)
			}
			// Expired frozen references are purged by the next allocation.
			clock.Advance(sccp.DefaultSegmentationReferenceFreeze + time.Nanosecond)
		}
	})
}

func BenchmarkStackHandleTransferUDT(b *testing.B) {
	clock := newClock()
	s := newStack(b, clock, pcB, sccp.StackConfig{}, 8)
	m := &sccp.UDT{ProtocolClass: params.ProtocolClass{Class: 1}, CalledPartyAddress: ssn8, CallingPartyAddress: ssn7, Data: payload(200)}
	wire, err := m.MarshalBinary()
	if err != nil {
		b.Fatal(err)
	}
	ind := sccp.TransferIndication{RoutingLabel: sccp.RoutingLabel{OPC: pcA, DPC: pcB}, Data: wire}
	b.ReportAllocs()
	b.SetBytes(int64(len(wire)))
	for b.Loop() {
		if got := s.HandleTransfer(ind); len(got.Indications) != 1 {
			b.Fatalf("indications = %d", len(got.Indications))
		}
	}
}

func BenchmarkStackReassembly16Segments(b *testing.B) {
	clock := newClock()
	sender := newStack(b, clock, pcA, sccp.StackConfig{})
	receiver := newStack(b, clock, pcB, sccp.StackConfig{}, 8)
	transfers, err := sender.Unitdata(request(payload(3952)))
	if err != nil || len(transfers.Transfers) != sccp.MaxSegments {
		b.Fatalf("Unitdata transfers = %d, %v", len(transfers.Transfers), err)
	}
	b.ReportAllocs()
	b.SetBytes(3952)
	for b.Loop() {
		if got := deliver(receiver, transfers.Transfers...); len(got.Indications) != 1 {
			b.Fatalf("indications = %d", len(got.Indications))
		}
		// Delivering the final segment removes the reassembly and its timer.
	}
}

func benchmarkEstablishedPair(b *testing.B) (*sccp.Stack, params.LocalReference, *sccp.Stack, *fakeClock) {
	b.Helper()
	clock := newClock()
	a := newStack(b, clock, pcA, sccp.StackConfig{})
	peer := newStack(b, clock, pcB, sccp.StackConfig{}, 6)
	ref, connect, err := a.Connect(sccp.ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(uint16(pcB), 6)})
	if err != nil {
		b.Fatal(err)
	}
	incoming := deliver(peer, connect.Transfers...)
	if len(incoming.Indications) != 1 {
		b.Fatalf("connect indications = %d", len(incoming.Indications))
	}
	ind, ok := incoming.Indications[0].(*sccp.ConnectIndication)
	if !ok {
		b.Fatalf("connect indication = %T", incoming.Indications[0])
	}
	accepted, err := peer.Accept(ind.LocalReference, sccp.ConnectResponse{})
	if err != nil {
		b.Fatal(err)
	}
	if confirmed := deliver(a, accepted.Transfers...); len(confirmed.Indications) != 1 {
		b.Fatalf("connect confirmations = %d", len(confirmed.Indications))
	}
	return a, ref, peer, clock
}

func BenchmarkStackConnectionData(b *testing.B) {
	b.Run("SendData1000", func(b *testing.B) {
		a, ref, _, _ := benchmarkEstablishedPair(b)
		data := payload(1000)
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			if _, err := a.SendData(ref, data); err != nil {
				b.Fatal(err)
			}
			// Restarting the inactivity timer replaces the prior timer entry.
		}
	})
	b.Run("ReceiveDT1x1000", func(b *testing.B) {
		a, ref, peer, _ := benchmarkEstablishedPair(b)
		data := payload(1000)
		out, err := a.SendData(ref, data)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			if got := deliver(peer, out.Transfers...); len(got.Indications) != 1 {
				b.Fatalf("data indications = %d", len(got.Indications))
			}
			// The last DT1 completes the NSDU and frees its reassembly buffer; the
			// fake clock never advances, so no timer fires.
		}
	})
}
