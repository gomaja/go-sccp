// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/gomaja/go-sccp/params"
)

// TestTimerHeap schedules, stops and fires timers in random order and checks
// that the stopped ones never fire and the others fire once, in deadline order.
func TestTimerHeap(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for round := range 50 {
		s := &Stack{wake: make(chan struct{}, 1)}
		var fired []time.Time
		var timers []*timer
		stopped := map[*timer]bool{}
		for range 1 + r.Intn(300) {
			d := base.Add(time.Duration(r.Intn(1000)) * time.Millisecond)
			var tm *timer
			tm = s.schedule(d, func(now time.Time, _ *Result) {
				if stopped[tm] {
					t.Fatalf("round %d: a stopped timer fired", round)
				}
				if now.Before(tm.deadline) {
					t.Fatalf("round %d: fired at %v before its deadline %v", round, now, tm.deadline)
				}
				fired = append(fired, tm.deadline)
			})
			timers = append(timers, tm)
			if r.Intn(4) == 0 {
				victim := timers[r.Intn(len(timers))]
				s.stop(victim)
				s.stop(victim) // stopping twice does nothing
				stopped[victim] = true
			}
		}
		if next, ok := s.NextTimeout(); ok {
			for _, tm := range timers {
				if !stopped[tm] && tm.deadline.Before(next) {
					t.Fatalf("round %d: NextTimeout %v after a pending deadline %v", round, next, tm.deadline)
				}
			}
		}
		for now := base; now.Before(base.Add(1100 * time.Millisecond)); now = now.Add(time.Duration(r.Intn(50)) * time.Millisecond) {
			var res Result
			s.expire(now, &res)
		}
		if !slices.IsSortedFunc(fired, func(a, b time.Time) int { return a.Compare(b) }) {
			t.Fatalf("round %d: timers fired out of deadline order", round)
		}
		if len(fired) != len(timers)-len(stopped) || len(s.timers) != 0 {
			t.Fatalf("round %d: %d fired of %d scheduled with %d stopped, %d pending", round, len(fired), len(timers), len(stopped), len(s.timers))
		}
	}
}

// TestSegmentationReferenceFreeze checks that a reference is not reused while
// frozen, across the 24-bit wrap, and is reused once the freeze ends.
func TestSegmentationReferenceFreeze(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s, err := NewStack(StackConfig{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if s.cfg.SegmentationReferenceFreeze != DefaultSegmentationReferenceFreeze {
		t.Fatalf("default freeze %v", s.cfg.SegmentationReferenceFreeze)
	}
	s.nextSegmentationReference = 0xfffffe
	var got []uint32
	for range 3 {
		ref, err := s.allocateSegmentationReference(now)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ref)
	}
	if !slices.Equal(got, []uint32{0xfffffe, 0xffffff, 0}) {
		t.Fatalf("references %#x", got)
	}
	s.nextSegmentationReference = 0xfffffe
	if ref, _ := s.allocateSegmentationReference(now); ref != 1 {
		t.Fatalf("allocated %#x while 0xfffffe, 0xffffff and 0 are frozen", ref)
	}
	s.nextSegmentationReference = 0xfffffe
	if ref, _ := s.allocateSegmentationReference(now.Add(DefaultSegmentationReferenceFreeze - time.Nanosecond)); ref != 2 {
		t.Fatalf("allocated %#x before the freeze ended", ref)
	}
	s.nextSegmentationReference = 0xfffffe
	if ref, _ := s.allocateSegmentationReference(now.Add(DefaultSegmentationReferenceFreeze)); ref != 0xfffffe {
		t.Fatalf("allocated %#x after the freeze ended, want 0xfffffe", ref)
	}
	if len(s.frozenSegmentationOrder) != len(s.frozenSegmentation) {
		t.Fatalf("freeze bookkeeping: %d ordered, %d mapped", len(s.frozenSegmentationOrder), len(s.frozenSegmentation))
	}
}

// TestBinarySearchesMatchLinearScan checks the two searches for the most user
// data that fits — segmentCapacity and fitReturn — against a linear scan, over
// many address lengths and maximum message sizes.
func TestBinarySearchesMatchLinearScan(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	address := func(n int) params.PartyAddress {
		if n == 0 {
			return params.NewSSNAddress(8)
		}
		return params.NewGlobalTitleAddress(&params.GlobalTitle{Indicator: params.GTITTNPESNAI, AddressInformation: make([]byte, n)}, 8)
	}
	for range 300 {
		limit := 20 + r.Intn(400)
		s, err := NewStack(StackConfig{MaxMessageSize: limit})
		if err != nil {
			t.Fatal(err)
		}
		called, calling := address(r.Intn(60)), address(r.Intn(60))
		req := UnitdataRequest{CalledPartyAddress: called, CallingPartyAddress: calling, ReturnOnError: r.Intn(2) == 0}
		if r.Intn(3) == 0 {
			imp := params.Importance(1)
			req.Importance = &imp
		}

		got := s.segmentCapacity(req)
		want := 0
		for n := 1; n <= maxData; n++ {
			probe := &XUDT{ProtocolClass: params.ProtocolClass{Class: 1, ReturnOnError: req.ReturnOnError}, HopCounter: s.cfg.HopCounter,
				CalledPartyAddress: called, CallingPartyAddress: calling, Data: make([]byte, n),
				Segmentation: &params.Segmentation{FirstSegment: true, RemainingSegments: params.MaxRemainingSegments}, Importance: req.Importance}
			if _, err := (MarshalOptions{MaxMessageSize: limit}).Marshal(probe); err == nil {
				want = n
			}
		}
		if got != want {
			t.Fatalf("segmentCapacity(max %d, addresses %d/%d) = %d, linear scan %d", limit, len(must(called.MarshalBinary())), len(must(calling.MarshalBinary())), got, want)
		}

		data := make([]byte, 1+r.Intn(600))
		build := func(d []byte) Message {
			return &LUDTS{ReturnCause: params.ReturnCauseSubsystemFailure, HopCounter: 15, CalledPartyAddress: calling, CallingPartyAddress: called, Data: d}
		}
		if r.Intn(2) == 0 {
			build = func(d []byte) Message {
				return &UDTS{ReturnCause: params.ReturnCauseSubsystemFailure, CalledPartyAddress: calling, CallingPartyAddress: called, Data: d}
			}
		}
		wantReturn := 0
		for n := 1; n <= len(data); n++ {
			if _, err := (MarshalOptions{MaxMessageSize: limit}).Marshal(build(data[:n])); err == nil {
				wantReturn = n
			}
		}
		m, b, err := s.fitReturn(build, data)
		switch {
		case wantReturn == 0 && err == nil:
			t.Fatalf("fitReturn found %d octets where nothing fits", len(b))
		case wantReturn > 0 && err != nil:
			t.Fatalf("fitReturn: %v, linear scan fits %d", err, wantReturn)
		case wantReturn > 0:
			n := len(dataOf(m))
			if n != wantReturn || len(b) > limit {
				t.Fatalf("fitReturn kept %d octets (%d encoded), linear scan %d (max %d)", n, len(b), wantReturn, limit)
			}
		}
	}
}

func dataOf(m Message) []byte {
	switch m := m.(type) {
	case *UDTS:
		return m.Data
	case *LUDTS:
		return m.Data
	}
	return nil
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

// TestReassemblyMemory checks the buffer limit of ITU-T Q.714 (05/01)
// §4.1.1.2.3.4 against the memory the processes hold: buffers grow with the
// user data received, to its exact length, whatever the first segments
// announce.
func TestReassemblyMemory(t *testing.T) {
	const limit = 2048
	s, err := NewStack(StackConfig{PointCode: 2, MaxReassemblyOctets: limit})
	if err != nil {
		t.Fatal(err)
	}
	s.RegisterSubsystem(8)
	held := func() (n int) {
		for _, p := range s.reassemblies {
			n += cap(p.data)
		}
		return n
	}
	segment := func(ref uint32, first bool, remaining uint8, n int) TransferIndication {
		x := &XUDT{ProtocolClass: params.ProtocolClass{Class: 1, ReturnOnError: true}, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8),
			CallingPartyAddress: params.NewSSNAddress(7), Data: make([]byte, n), Segmentation: &params.Segmentation{FirstSegment: first, RemainingSegments: remaining, LocalReference: ref}}
		return TransferIndication{RoutingLabel: RoutingLabel{OPC: 1, DPC: 2}, Data: must(x.MarshalBinary())}
	}
	congestion := func(res Result) {
		t.Helper()
		if len(res.Transfers) != 1 || res.Transfers[0].Message.(*XUDTS).ReturnCause != params.ReturnCauseNetworkCongestion {
			t.Fatalf("want a return with the cause network congestion, got %+v", res)
		}
	}

	// Sixteen first segments of 128 octets announce 16 segments each, 32768
	// octets if the buffers were sized from the announcement.
	for ref := range uint32(16) {
		if res := s.HandleTransfer(segment(ref, true, 15, 128)); len(res.Transfers)+len(res.Discards) != 0 {
			t.Fatalf("first segment %d: %+v", ref, res)
		}
	}
	if held() != limit || s.reassemblyOctets != limit {
		t.Fatalf("16 first segments of 128 octets hold %d octets, account %d", held(), s.reassemblyOctets)
	}
	// The buffer is full: a new process and a longer one are refused.
	congestion(s.HandleTransfer(segment(16, true, 15, 128)))
	congestion(s.HandleTransfer(segment(0, false, 14, 128)))
	// The ended process becomes the record of its message in its own slot;
	// the refused message, which had no process, gets a record of its own.
	// Records hold nothing.
	if s.reassemblySlots != 16 || s.orphanRecords != 1 || held() != limit-128 || s.reassemblyOctets != limit-128 {
		t.Fatalf("after the refusals: %d slots, %d records hold %d octets, account %d", s.reassemblySlots, s.orphanRecords, held(), s.reassemblyOctets)
	}
	// A 64-octet segment grows a buffer by 64 octets, not to 256.
	if res := s.HandleTransfer(segment(1, false, 14, 64)); len(res.Transfers)+len(res.Discards) != 0 {
		t.Fatalf("second segment: %+v", res)
	}
	if held() != s.reassemblyOctets || s.reassemblyOctets != limit-64 {
		t.Fatalf("after a 64-octet segment: hold %d octets, account %d", held(), s.reassemblyOctets)
	}
	// A first segment of one octet holds one octet, not an allocation class.
	s.HandleTransfer(segment(99, true, 1, 1))
	if held() != s.reassemblyOctets || s.reassemblyOctets != limit-63 {
		t.Fatalf("after a 1-octet first segment: hold %d octets, account %d", held(), s.reassemblyOctets)
	}
}

// TestFailureRecordsSpareProcesses checks that the records of messages that
// failed with no process neither take the place of a reassembly in progress
// nor give way to new ones: when none is left, a failed message is discarded
// without a return, so that no message is returned twice.
func TestFailureRecordsSpareProcesses(t *testing.T) {
	s, err := NewStack(StackConfig{PointCode: 2, MaxReassemblies: 1})
	if err != nil {
		t.Fatal(err)
	}
	s.RegisterSubsystem(8)
	segment := func(ref uint32, first bool, remaining uint8) TransferIndication {
		x := &XUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8),
			CallingPartyAddress: params.NewSSNAddress(7), Data: make([]byte, 10), Segmentation: &params.Segmentation{FirstSegment: first, RemainingSegments: remaining, LocalReference: ref}}
		return TransferIndication{RoutingLabel: RoutingLabel{OPC: 1, DPC: 2}, Data: must(x.MarshalBinary())}
	}
	s.HandleTransfer(segment(1, true, 1))
	// Every orphan segment needs a record, and only the first gets one.
	for ref := uint32(100); ref < 132; ref++ {
		res := s.HandleTransfer(segment(ref, false, 1))
		live := 0
		for _, p := range s.reassemblies {
			if !p.failed {
				live++
			}
		}
		if live != 1 || s.reassemblySlots != 1 || s.orphanRecords != 1 || len(res.Transfers) != 0 {
			t.Fatalf("after orphan %d: %d processes, %d slots, %d records, %+v", ref, live, s.reassemblySlots, s.orphanRecords, res)
		}
	}
	if res := s.HandleTransfer(segment(1, false, 0)); len(res.Indications) != 1 {
		t.Fatalf("the process in progress did not complete: %+v", res)
	}
}
