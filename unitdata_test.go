// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

const (
	pcA = 0x0101 // originating node
	pcB = 0x0202 // destination node
)

func newStack(t *testing.T, clock *fakeClock, pc uint32, cfg sccp.StackConfig, ssns ...uint8) *sccp.Stack {
	t.Helper()
	cfg.PointCode = pc
	if clock != nil {
		cfg.Now = clock.Now
	}
	s, err := sccp.NewStack(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, ssn := range ssns {
		s.RegisterSubsystem(ssn)
	}
	return s
}

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

func request(data []byte) sccp.UnitdataRequest {
	return sccp.UnitdataRequest{
		CalledPartyAddress:  params.NewSSNAddress(8),
		CallingPartyAddress: params.NewSSNAddress(7),
		ProtocolClass:       1,
		SequenceControl:     5,
		ReturnOnError:       true,
		Data:                data,
		DPC:                 pcB,
	}
}

func mustUnitdata(t *testing.T, s *sccp.Stack, req sccp.UnitdataRequest) []sccp.TransferRequest {
	t.Helper()
	res, err := s.Unitdata(req)
	if err != nil {
		t.Fatalf("Unitdata(%d octets): %v", len(req.Data), err)
	}
	return res.Transfers
}

// deliver passes transfers to a stack as the MTP would.
func deliver(s *sccp.Stack, transfers ...sccp.TransferRequest) sccp.Result {
	var all sccp.Result
	for _, tr := range transfers {
		r := s.HandleTransfer(sccp.TransferIndication{RoutingLabel: tr.RoutingLabel, Data: tr.Data})
		all.Transfers = append(all.Transfers, r.Transfers...)
		all.Indications = append(all.Indications, r.Indications...)
		all.Discards = append(all.Discards, r.Discards...)
	}
	return all
}

func segmentsOf(t *testing.T, transfers []sccp.TransferRequest) []*sccp.XUDT {
	t.Helper()
	out := make([]*sccp.XUDT, len(transfers))
	for i, tr := range transfers {
		x, ok := tr.Message.(*sccp.XUDT)
		if !ok || x.Segmentation == nil {
			t.Fatalf("transfer %d is %v, want a segmented XUDT", i, tr.Message)
		}
		out[i] = x
	}
	return out
}

func onlyUnitdata(t *testing.T, res sccp.Result) *sccp.UnitdataIndication {
	t.Helper()
	if len(res.Indications) != 1 || len(res.Transfers) != 0 || len(res.Discards) != 0 {
		t.Fatalf("want one indication, got %d indications, %d transfers, discards %v", len(res.Indications), len(res.Transfers), res.Discards)
	}
	u, ok := res.Indications[0].(*sccp.UnitdataIndication)
	if !ok {
		t.Fatalf("indication %T, want *UnitdataIndication", res.Indications[0])
	}
	return u
}

func onlyReturn(t *testing.T, res sccp.Result) sccp.TransferRequest {
	t.Helper()
	if len(res.Transfers) != 1 || len(res.Indications) != 0 {
		t.Fatalf("want one returned message, got transfers %v, indications %v", res.Transfers, res.Indications)
	}
	return res.Transfers[0]
}

func TestUnitdataSingleMessage(t *testing.T) {
	s := newStack(t, newClock(), pcA, sccp.StackConfig{})
	req := request([]byte("hello"))
	tr := mustUnitdata(t, s, req)
	if len(tr) != 1 {
		t.Fatalf("%d transfers, want 1", len(tr))
	}
	want := &sccp.UDT{ProtocolClass: params.ProtocolClass{Class: 1, ReturnOnError: true}, CalledPartyAddress: req.CalledPartyAddress, CallingPartyAddress: req.CallingPartyAddress, Data: req.Data}
	if diff := cmp.Diff(sccp.Message(want), tr[0].Message, cmpOpts); diff != "" {
		t.Fatalf("message (-want +got):\n%s", diff)
	}
	if tr[0].RoutingLabel != (sccp.RoutingLabel{OPC: pcA, DPC: pcB, SLS: 5}) {
		t.Fatalf("label %+v; class 1 must use the sequence control as SLS", tr[0].RoutingLabel)
	}
	if b, _ := want.MarshalBinary(); !bytes.Equal(b, tr[0].Data) {
		t.Fatalf("Data % x does not encode the message % x", tr[0].Data, b)
	}

	// Class 0 rotates the SLS; the DPC defaults to the called point code.
	req = request([]byte("x"))
	req.ProtocolClass, req.DPC = 0, 0
	req.CalledPartyAddress = params.NewPointCodeAddress(0x0303, 8)
	first := mustUnitdata(t, s, req)[0].RoutingLabel
	second := mustUnitdata(t, s, req)[0].RoutingLabel
	if first.DPC != 0x0303 || first.SLS == second.SLS {
		t.Fatalf("class 0 labels %+v then %+v", first, second)
	}

	// An importance, or UseXUDT, selects an XUDT with the configured hop counter.
	req = request([]byte("x"))
	req.Importance = ptr(params.Importance(3))
	if x, ok := mustUnitdata(t, s, req)[0].Message.(*sccp.XUDT); !ok || x.HopCounter != 15 || x.Importance == nil || *x.Importance != 3 || x.Segmentation != nil {
		t.Fatalf("request with importance sent %v", x)
	}
	s2 := newStack(t, newClock(), pcA, sccp.StackConfig{UseXUDT: true, HopCounter: 9})
	if x, ok := mustUnitdata(t, s2, request([]byte("x")))[0].Message.(*sccp.XUDT); !ok || x.HopCounter != 9 {
		t.Fatalf("UseXUDT sent %v", x)
	}
}

func TestClassZeroLinkSelectionRotates(t *testing.T) {
	s := newStack(t, newClock(), pcA, sccp.StackConfig{})
	req := request([]byte("x"))
	req.ProtocolClass = 0
	for i := range 40 {
		if sls := mustUnitdata(t, s, req)[0].SLS; sls != uint8(i%16) {
			t.Fatalf("request %d: SLS %d, want %d", i, sls, i%16)
		}
	}
	// The segments of one message share one SLS.
	transfers := mustUnitdata(t, s, sccp.UnitdataRequest{CalledPartyAddress: req.CalledPartyAddress, CallingPartyAddress: req.CallingPartyAddress, Data: payload(600), DPC: pcB})
	for _, tr := range transfers {
		if tr.SLS != transfers[0].SLS {
			t.Fatalf("segments on SLS %d and %d", transfers[0].SLS, tr.SLS)
		}
	}
}

func TestHandleTransferOfNothing(t *testing.T) {
	s := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
	res := s.HandleTransfer(sccp.TransferIndication{})
	if len(res.Discards) != 1 || res.Discards[0].MessageType != 0 || !errors.Is(res.Discards[0].Reason, sccp.ErrSyntax) {
		t.Fatalf("empty transfer: %+v", res)
	}
}

// TestSegmentCapacityOfOne segments with addresses that leave room for exactly
// one octet of user data per segment.
func TestSegmentCapacityOfOne(t *testing.T) {
	s := newStack(t, newClock(), pcA, sccp.StackConfig{})
	// A 248-octet called address and a 2-octet calling address: the pointer
	// to the optional part reaches 4 + 248 + 2 + n <= 255 octets, so n <= 1.
	gt := &params.GlobalTitle{Indicator: params.GTITTNPESNAI, NumberingPlan: params.NPISDNTelephony, EncodingScheme: params.ESBCDEven, NatureOfAddress: params.NAIInternationalNumber,
		AddressInformation: bytes.Repeat([]byte{0x11}, 243)}
	req := request(payload(12))
	req.CalledPartyAddress = params.NewGlobalTitleAddress(gt, 8)
	transfers := mustUnitdata(t, s, req)
	if _, single := transfers[0].Message.(*sccp.UDT); single {
		t.Skipf("the request fits one UDT of %d octets", len(transfers[0].Data))
	}
	segs := segmentsOf(t, transfers)
	if len(segs) != 12 {
		t.Fatalf("%d segments, want 12 of one octet", len(segs))
	}
	for _, x := range segs {
		if len(x.Data) != 1 {
			t.Fatalf("segment of %d octets", len(x.Data))
		}
	}
}

func TestUnitdataRejectsInvalidRequests(t *testing.T) {
	s := newStack(t, newClock(), pcA, sccp.StackConfig{})
	tests := []struct {
		name string
		edit func(*sccp.UnitdataRequest)
		want error
	}{
		{"class 2", func(r *sccp.UnitdataRequest) { r.ProtocolClass = 2 }, sccp.ErrInvalidRequest},
		{"no data", func(r *sccp.UnitdataRequest) { r.Data = nil }, sccp.ErrInvalidRequest},
		{"3953 octets", func(r *sccp.UnitdataRequest) { r.Data = payload(3953) }, sccp.ErrSegmentation},
		{"invalid called address", func(r *sccp.UnitdataRequest) { r.CalledPartyAddress.PointCode = 1 }, sccp.ErrInvalidRequest},
		{"invalid calling address", func(r *sccp.UnitdataRequest) { r.CallingPartyAddress.SSN, r.CallingPartyAddress.HasSSN = 1, false }, sccp.ErrInvalidRequest},
		{"invalid importance", func(r *sccp.UnitdataRequest) { r.Importance = ptr(params.Importance(9)) }, sccp.ErrInvalidRequest},
		{"no destination", func(r *sccp.UnitdataRequest) { r.DPC = 0 }, sccp.ErrInvalidRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := request(payload(10))
			tt.edit(&req)
			res, err := s.Unitdata(req)
			if !errors.Is(err, tt.want) || len(res.Transfers) != 0 {
				t.Fatalf("Unitdata = %v, %v; want %v and nothing sent", res.Transfers, err, tt.want)
			}
		})
	}
}

// TestSegmentation checks ITU-T Q.714 (05/01) §4.1.1.1.2 on the segments of
// one N-UNITDATA request.
func TestSegmentation(t *testing.T) {
	s := newStack(t, newClock(), pcA, sccp.StackConfig{})
	data := payload(600)
	transfers := mustUnitdata(t, s, request(data))
	segs := segmentsOf(t, transfers)
	if len(segs) != 3 {
		t.Fatalf("%d segments for 600 octets, want 3", len(segs))
	}
	var joined []byte
	for i, x := range segs {
		// The transmitted octets, not only the message value, carry the
		// segment: decode them.
		wire, err := sccp.ParseMessage(transfers[i].Data)
		if err != nil {
			t.Fatalf("segment %d does not decode: %v", i, err)
		}
		if diff := cmp.Diff(sccp.Message(x), wire, cmpOpts); diff != "" {
			t.Fatalf("segment %d: transmitted octets differ from the message:\n%s", i, diff)
		}
		joined = append(joined, x.Data...)
		if len(transfers[i].Data) > sccp.DefaultMaxMessageSize {
			t.Errorf("segment %d is %d octets", i, len(transfers[i].Data))
		}
		if transfers[i].RoutingLabel != transfers[0].RoutingLabel {
			t.Errorf("segment %d label %+v differs from %+v", i, transfers[i].RoutingLabel, transfers[0].RoutingLabel)
		}
		if x.ProtocolClass.Class != 1 || x.ProtocolClass.ReturnOnError != (i == 0) {
			t.Errorf("segment %d protocol class %v; want class 1, return on error in the first only", i, x.ProtocolClass)
		}
		want := params.Segmentation{FirstSegment: i == 0, InSequenceDelivery: true, RemainingSegments: uint8(len(segs) - 1 - i), LocalReference: segs[0].Segmentation.LocalReference}
		if *x.Segmentation != want {
			t.Errorf("segment %d segmentation %v, want %v", i, *x.Segmentation, want)
		}
		if !cmp.Equal(x.CalledPartyAddress, segs[0].CalledPartyAddress) || !cmp.Equal(x.CallingPartyAddress, segs[0].CallingPartyAddress) || x.HopCounter != 15 {
			t.Errorf("segment %d addresses or hop counter differ", i)
		}
		// §4.1.1.1.2: the first segment is large enough that first x n >= total.
		if len(x.Data) > len(segs[0].Data) {
			t.Errorf("segment %d is longer than the first", i)
		}
	}
	if !bytes.Equal(joined, data) {
		t.Fatal("segments do not concatenate to the user data")
	}
	if len(segs[0].Data)*len(segs) < len(data) {
		t.Fatalf("first segment %d x %d segments < %d", len(segs[0].Data), len(segs), len(data))
	}

	// Class 0 requests set the in-sequence delivery option to class 0.
	req := request(data)
	req.ProtocolClass = 0
	for _, x := range segmentsOf(t, mustUnitdata(t, s, req)) {
		if x.Segmentation.InSequenceDelivery || x.ProtocolClass.Class != 1 {
			t.Fatalf("class 0 segment %v", x)
		}
	}
}

// TestSegmentationBoundaries checks where segmentation starts and the
// 16-segment limit: 3952 = 16 x 247 octets with three-octet addresses.
func TestSegmentationBoundaries(t *testing.T) {
	s := newStack(t, newClock(), pcA, sccp.StackConfig{})
	count := func(t *testing.T, req sccp.UnitdataRequest) int {
		t.Helper()
		transfers := mustUnitdata(t, s, req)
		if len(transfers) == 1 {
			if _, ok := transfers[0].Message.(*sccp.UDT); !ok {
				t.Fatalf("single message %v", transfers[0].Message)
			}
		} else {
			segmentsOf(t, transfers)
		}
		return len(transfers)
	}
	pc := func(data []byte) sccp.UnitdataRequest {
		r := request(data)
		r.CalledPartyAddress, r.CallingPartyAddress = params.NewPointCodeAddress(pcB, 8), params.NewPointCodeAddress(pcA, 7)
		return r
	}
	tests := []struct {
		name string
		req  sccp.UnitdataRequest
		want int
	}{
		{"255 octets in one UDT", request(payload(255)), 1},
		{"256 octets segmented", request(payload(256)), 2},
		{"PC+SSN 252 octets in one UDT", pc(payload(252)), 1},
		{"PC+SSN 253 octets segmented", pc(payload(253)), 2},
		{"494 octets in 2 segments", request(payload(2 * 247)), 2},
		{"495 octets in 3 segments", request(payload(2*247 + 1)), 3},
		{"3952 octets in 16 segments", request(payload(16 * 247)), 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if n := count(t, tt.req); n != tt.want {
				t.Fatalf("%d messages, want %d", n, tt.want)
			}
		})
	}
	// With point codes the segment holds 243 octets: 3952 needs 17 segments.
	if _, err := s.Unitdata(pc(payload(3952))); !errors.Is(err, sccp.ErrSegmentation) {
		t.Fatalf("3952 octets with PC+SSN addresses: %v, want ErrSegmentation", err)
	}
	if n := count(t, pc(payload(16*243))); n != 16 {
		t.Fatalf("16 x 243 octets with PC+SSN addresses: %d segments", n)
	}
	// A 125-octet called address leaves 124 octets per segment.
	gt, _ := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, string(bytes.Repeat([]byte("1"), 240)))
	req := request(payload(300))
	req.CalledPartyAddress = params.NewGlobalTitleAddress(gt, 8)
	if n := count(t, req); n != 3 {
		t.Fatalf("300 octets with a 125-octet called address: %d segments, want 3", n)
	}
	// A 250-octet called address leaves no room for user data in a segment:
	// the pointer to the optional part cannot reach past it.
	gt, _ = params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, string(bytes.Repeat([]byte("1"), 490)))
	req.CalledPartyAddress = params.NewGlobalTitleAddress(gt, 8)
	if _, err := s.Unitdata(req); !errors.Is(err, sccp.ErrSegmentation) {
		t.Fatalf("segmenting with a 250-octet called address: %v, want ErrSegmentation", err)
	}
}

// TestSegmentationThreshold checks Z of ITU-T Q.714 (05/01) §4.1.1.1.1 and the
// 2560 octets in 16 segments of 160 that Q.715 (04/02) §8.3.2 derives from it.
func TestSegmentationThreshold(t *testing.T) {
	if _, err := sccp.NewStack(sccp.StackConfig{SegmentationThreshold: 159}); !errors.Is(err, sccp.ErrInvalidRequest) {
		t.Fatalf("Z = 159: %v, want ErrInvalidRequest", err)
	}
	s := newStack(t, newClock(), pcA, sccp.StackConfig{SegmentationThreshold: 160})
	if n := len(mustUnitdata(t, s, request(payload(159)))); n != 1 {
		t.Fatalf("159 octets below Z: %d messages", n)
	}
	// At Z the data is segmented into segments of at most Z octets; one
	// segment is sent as an ordinary message of the requested class, without
	// the segmentation parameter (Q.713 Table 19 Note 2).
	req := request(payload(160))
	req.ProtocolClass = 0
	tr := mustUnitdata(t, s, req)
	if u, ok := tr[0].Message.(*sccp.UDT); len(tr) != 1 || !ok || u.ProtocolClass.Class != 0 {
		t.Fatalf("160 octets at Z: %d messages, first %v", len(tr), tr[0].Message)
	}
	if segs := segmentsOf(t, mustUnitdata(t, s, request(payload(161)))); len(segs) != 2 || len(segs[0].Data) != 81 {
		t.Fatalf("161 octets at Z: %d segments", len(segs))
	}
	segs := segmentsOf(t, mustUnitdata(t, s, request(payload(2560))))
	if len(segs) != 16 {
		t.Fatalf("2560 octets: %d segments, want 16", len(segs))
	}
	for _, x := range segs {
		if len(x.Data) != 160 {
			t.Fatalf("segment of %d octets, want 160", len(x.Data))
		}
	}
	if _, err := s.Unitdata(request(payload(2561))); !errors.Is(err, sccp.ErrSegmentation) {
		t.Fatalf("2561 octets with Z = 160: %v, want ErrSegmentation", err)
	}
}

func TestSegmentationReferences(t *testing.T) {
	clock := newClock()
	s := newStack(t, clock, pcA, sccp.StackConfig{SegmentationReferenceFreeze: time.Minute})
	seen := map[uint32]bool{}
	for range 50 {
		ref := segmentsOf(t, mustUnitdata(t, s, request(payload(300))))[0].Segmentation.LocalReference
		if seen[ref] {
			t.Fatalf("reference %#x reused while frozen", ref)
		}
		seen[ref] = true
	}
}

// TestReassembly sends segmented messages from one stack to another.
func TestReassembly(t *testing.T) {
	for _, n := range []int{256, 600, 3952} {
		clock := newClock()
		a := newStack(t, clock, pcA, sccp.StackConfig{})
		b := newStack(t, clock, pcB, sccp.StackConfig{}, 8)
		data := payload(n)
		transfers := mustUnitdata(t, a, request(data))
		res := deliver(b, transfers...)
		u := onlyUnitdata(t, res)
		if !bytes.Equal(u.Data, data) || u.Segments != len(transfers) || u.ProtocolClass != 1 || !u.ReturnOnError || u.MessageType != sccp.MsgTypeXUDT {
			t.Fatalf("%d octets: indication %+v", n, u)
		}
		if u.RoutingLabel != transfers[0].RoutingLabel {
			t.Fatalf("indication label %+v", u.RoutingLabel)
		}
		if _, pending := b.NextTimeout(); pending {
			t.Fatalf("%d octets: a timer still runs after reassembly", n)
		}
	}
}

// failedReassembly checks that the result returns the first segment as an
// XUDTS with the cause error in message transport and its segmentation
// parameter unchanged (ITU-T Q.714 (05/01) §4.1.1.2.3).
func failedReassembly(t *testing.T, res sccp.Result, first *sccp.XUDT, firstLabel sccp.RoutingLabel) {
	t.Helper()
	if len(res.Indications) != 0 {
		t.Fatalf("delivered %v", res.Indications)
	}
	if len(res.Transfers) != 1 {
		t.Fatalf("%d transfers, want the XUDTS", len(res.Transfers))
	}
	x, ok := res.Transfers[0].Message.(*sccp.XUDTS)
	if !ok {
		t.Fatalf("returned %v, want XUDTS", res.Transfers[0].Message)
	}
	want := &sccp.XUDTS{
		ReturnCause: params.ReturnCauseErrorInMessageTransport, HopCounter: 15,
		CalledPartyAddress: first.CallingPartyAddress, CallingPartyAddress: first.CalledPartyAddress,
		Data: first.Data, Segmentation: first.Segmentation,
	}
	if diff := cmp.Diff(want, x, cmpOpts); diff != "" {
		t.Fatalf("XUDTS (-want +got):\n%s", diff)
	}
	if res.Transfers[0].RoutingLabel != (sccp.RoutingLabel{OPC: firstLabel.DPC, DPC: firstLabel.OPC, SLS: firstLabel.SLS}) {
		t.Fatalf("XUDTS label %+v", res.Transfers[0].RoutingLabel)
	}
	found := false
	for _, d := range res.Discards {
		found = found || errors.Is(d.Reason, sccp.ErrReassembly)
	}
	if !found {
		t.Fatalf("no reassembly discard in %v", res.Discards)
	}
}

func TestReassemblyFailures(t *testing.T) {
	setup := func(t *testing.T, n int) (*fakeClock, *sccp.Stack, []sccp.TransferRequest, []*sccp.XUDT) {
		clock := newClock()
		a := newStack(t, clock, pcA, sccp.StackConfig{})
		b := newStack(t, clock, pcB, sccp.StackConfig{ReassemblyTimeout: 10 * time.Second}, 8)
		transfers := mustUnitdata(t, a, request(payload(n)))
		return clock, b, transfers, segmentsOf(t, transfers)
	}

	t.Run("lost middle segment", func(t *testing.T) {
		_, b, tr, segs := setup(t, 900)
		if r := deliver(b, tr[0], tr[1]); len(r.Indications)+len(r.Transfers) != 0 {
			t.Fatalf("partial delivery produced %v", r)
		}
		failedReassembly(t, deliver(b, tr[3]), segs[0], tr[0].RoutingLabel)
		// The rest of the message has no process left: discarded, and not
		// returned since only the first segment asks for return.
		r := deliver(b, tr[2])
		if len(r.Transfers) != 0 || len(r.Indications) != 0 || len(r.Discards) != 1 || !errors.Is(r.Discards[0].Reason, sccp.ErrReassembly) {
			t.Fatalf("stray segment: %+v", r)
		}
	})
	t.Run("lost last segment, T(reassembly) expiry", func(t *testing.T) {
		clock, b, tr, segs := setup(t, 900)
		deliver(b, tr[:len(tr)-1]...)
		deadline, ok := b.NextTimeout()
		if !ok || !deadline.Equal(clock.Now().Add(10*time.Second)) {
			t.Fatalf("NextTimeout = %v, %t", deadline, ok)
		}
		clock.Advance(10*time.Second - time.Nanosecond)
		if r := b.HandleTimeout(); len(r.Transfers)+len(r.Discards) != 0 {
			t.Fatalf("timer fired early: %+v", r)
		}
		clock.Advance(time.Nanosecond)
		failedReassembly(t, b.HandleTimeout(), segs[0], tr[0].RoutingLabel)
		// The last segment, late: no process, and the failed message keeps
		// its record for another T(reassembly).
		if r := deliver(b, tr[len(tr)-1]); len(r.Indications)+len(r.Transfers) != 0 {
			t.Fatal("late segment delivered or returned")
		}
		if deadline, ok := b.NextTimeout(); !ok || !deadline.Equal(clock.Now().Add(10*time.Second)) {
			t.Fatalf("record deadline %v, %t", deadline, ok)
		}
	})
	t.Run("expiry observed by the next message", func(t *testing.T) {
		clock, b, tr, segs := setup(t, 600)
		deliver(b, tr[0], tr[1])
		clock.Advance(11 * time.Second)
		// No HandleTimeout call: the expired process must not complete.
		r := deliver(b, tr[2])
		if len(r.Indications) != 0 {
			t.Fatal("an expired reassembly completed")
		}
		if len(r.Transfers) != 1 {
			t.Fatalf("transfers %v", r.Transfers)
		}
		if x := r.Transfers[0].Message.(*sccp.XUDTS); x.ReturnCause != params.ReturnCauseErrorInMessageTransport || !bytes.Equal(x.Data, segs[0].Data) {
			t.Fatalf("returned %v", x)
		}
	})
	t.Run("lost first segment", func(t *testing.T) {
		_, b, tr, _ := setup(t, 600)
		r := deliver(b, tr[1:]...)
		if len(r.Indications)+len(r.Transfers) != 0 || len(r.Discards) != 2 {
			t.Fatalf("segments without a first one: %+v", r)
		}
	})
	t.Run("duplicated segment", func(t *testing.T) {
		_, b, tr, segs := setup(t, 900)
		deliver(b, tr[0], tr[1])
		failedReassembly(t, deliver(b, tr[1]), segs[0], tr[0].RoutingLabel)
	})
	t.Run("reordered segments", func(t *testing.T) {
		_, b, tr, segs := setup(t, 900)
		deliver(b, tr[0])
		failedReassembly(t, deliver(b, tr[2]), segs[0], tr[0].RoutingLabel)
	})
	t.Run("new first segment for a busy reference", func(t *testing.T) {
		_, b, tr, segs := setup(t, 900)
		deliver(b, tr[0], tr[1])
		// A different first segment with the same reference.
		fresh := *segs[0]
		fresh.Data = bytes.Repeat([]byte{0x5a}, len(segs[0].Data))
		wire, err := fresh.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		r := b.HandleTransfer(sccp.TransferIndication{RoutingLabel: tr[0].RoutingLabel, Data: wire})
		// The process is discarded and the new message returned.
		failedReassembly(t, r, &fresh, tr[0].RoutingLabel)
		if len(r.Discards) != 2 {
			t.Fatalf("discards %v, want the old process and the new segment", r.Discards)
		}
	})
	t.Run("segment longer than first segment allows", func(t *testing.T) {
		_, b, tr, segs := setup(t, 300)
		// Re-encode the second segment with more data than the first.
		second := *segs[1]
		second.Data = payload(len(segs[0].Data) + len(segs[1].Data) + 1)[:len(segs[0].Data)+1]
		wire, err := second.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		deliver(b, tr[0])
		failedReassembly(t, b.HandleTransfer(sccp.TransferIndication{RoutingLabel: tr[1].RoutingLabel, Data: wire}), segs[0], tr[0].RoutingLabel)
	})
}

func TestReassemblyKeys(t *testing.T) {
	clock := newClock()
	a := newStack(t, clock, pcA, sccp.StackConfig{})
	b := newStack(t, clock, pcB, sccp.StackConfig{}, 8)
	tr1 := mustUnitdata(t, a, request(payload(300)))
	req := request(payload(300))
	req.CallingPartyAddress = params.NewSSNAddress(9)
	tr2 := mustUnitdata(t, a, req)
	// Same reference from another calling address and from another OPC.
	ref := segmentsOf(t, tr1)[0].Segmentation.LocalReference
	for _, x := range segmentsOf(t, tr2) {
		x.Segmentation.LocalReference = ref
	}
	reencode := func(tr []sccp.TransferRequest, opc uint32) []sccp.TransferRequest {
		out := make([]sccp.TransferRequest, len(tr))
		for i, t0 := range tr {
			b, err := t0.Message.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			out[i] = sccp.TransferRequest{RoutingLabel: sccp.RoutingLabel{OPC: opc, DPC: t0.DPC, SLS: t0.SLS}, Message: t0.Message, Data: b}
		}
		return out
	}
	tr2 = reencode(tr2, pcA)
	tr3 := reencode(tr1, 0x0303)
	// Interleave the three messages: each is its own process.
	r := deliver(b, tr1[0], tr2[0], tr3[0], tr1[1], tr2[1], tr3[1])
	if len(r.Indications) != 3 || len(r.Transfers) != 0 || len(r.Discards) != 0 {
		t.Fatalf("interleaved processes: %d indications, %v transfers, %v discards", len(r.Indications), r.Transfers, r.Discards)
	}
	// The SLS is part of the MTP routing information that identifies the
	// process (Q.714 §4.1.1.2.2): a segment on another SLS has no process.
	tr4 := mustUnitdata(t, a, request(payload(300)))
	tr4[1].SLS ^= 1
	r = deliver(b, tr4...)
	if len(r.Indications) != 0 || len(r.Discards) != 1 || !errors.Is(r.Discards[0].Reason, sccp.ErrReassembly) {
		t.Fatalf("segment on another SLS: %+v", r)
	}
}

func TestReassemblyLimits(t *testing.T) {
	clock := newClock()
	a := newStack(t, clock, pcA, sccp.StackConfig{})
	b := newStack(t, clock, pcB, sccp.StackConfig{MaxReassemblies: 1}, 8)
	tr1 := mustUnitdata(t, a, request(payload(300)))
	tr2 := mustUnitdata(t, a, request(payload(300)))
	deliver(b, tr1[0])
	// §4.1.1.2.3.4: no buffer space, returned with the cause network congestion.
	ret := onlyReturn(t, deliver(b, tr2[0]))
	if x := ret.Message.(*sccp.XUDTS); x.ReturnCause != params.ReturnCauseNetworkCongestion {
		t.Fatalf("cause %s", x.ReturnCause)
	}
	onlyUnitdata(t, deliver(b, tr1[1]))

	c := newStack(t, clock, pcB, sccp.StackConfig{MaxReassemblyOctets: 200}, 8)
	tr3 := mustUnitdata(t, a, request(payload(300)))
	deliver(c, tr3[0])
	r := deliver(c, tr3[1])
	if len(r.Transfers) != 1 || r.Transfers[0].Message.(*sccp.XUDTS).ReturnCause != params.ReturnCauseNetworkCongestion || len(r.Indications) != 0 {
		t.Fatalf("buffer limit: %+v", r)
	}
	// The aborted process released its buffer and its T(reassembly): what
	// remains is the record of the failed message, which holds no data.
	onlyUnitdata(t, deliver(c, mustUnitdata(t, a, request(payload(200)))...))
}

// TestReassemblyOctetAccounting checks the buffer limit at its boundary and
// that finished and failed processes release what they buffered.
func TestReassemblyOctetAccounting(t *testing.T) {
	clock := newClock()
	a := newStack(t, clock, pcA, sccp.StackConfig{})
	// 450 octets travel as two segments of 225: exactly the limit.
	b := newStack(t, clock, pcB, sccp.StackConfig{MaxReassemblyOctets: 450}, 8)
	for i := range 3 {
		onlyUnitdata(t, deliver(b, mustUnitdata(t, a, request(payload(450)))...))
		_ = i
	}
	// One octet more fails at the last segment, and frees the buffer.
	tr := mustUnitdata(t, a, request(payload(451)))
	segs := segmentsOf(t, tr)
	r := deliver(b, tr...)
	if len(r.Indications) != 0 || len(r.Transfers) != 1 || r.Transfers[0].Message.(*sccp.XUDTS).ReturnCause != params.ReturnCauseNetworkCongestion {
		t.Fatalf("451 octets with a 450-octet buffer: %+v", r)
	}
	if !bytes.Equal(r.Transfers[0].Message.(*sccp.XUDTS).Data, segs[0].Data) {
		t.Fatal("the return does not carry the first segment")
	}
	onlyUnitdata(t, deliver(b, mustUnitdata(t, a, request(payload(450)))...))

	// A first segment larger than the whole buffer is refused at once.
	c := newStack(t, clock, pcB, sccp.StackConfig{MaxReassemblyOctets: 100}, 8)
	ret := onlyReturn(t, deliver(c, mustUnitdata(t, a, request(payload(300)))[0]))
	if ret.Message.(*sccp.XUDTS).ReturnCause != params.ReturnCauseNetworkCongestion {
		t.Fatalf("first segment over the buffer: %v", ret.Message)
	}

	// Three segments: the third exceeds the buffer the first two filled.
	d := newStack(t, clock, pcB, sccp.StackConfig{MaxReassemblyOctets: 500}, 8)
	tr = mustUnitdata(t, a, request(payload(600)))
	if len(tr) != 3 {
		t.Fatalf("%d segments", len(tr))
	}
	if r := deliver(d, tr[0], tr[1]); len(r.Transfers) != 0 {
		t.Fatalf("first two segments: %+v", r)
	}
	if r := deliver(d, tr[2]); len(r.Transfers) != 1 || len(r.Indications) != 0 {
		t.Fatalf("third segment over the buffer: %+v", r)
	}
	// A timer expiry releases the buffer too.
	deliver(d, mustUnitdata(t, a, request(payload(450)))[0])
	clock.Advance(sccp.DefaultReassemblyTimeout)
	d.HandleTimeout()
	onlyUnitdata(t, deliver(d, mustUnitdata(t, a, request(payload(450)))...))
}

// segmentTransfer encodes a hand-built segment for delivery.
func segmentTransfer(t *testing.T, m sccp.Message) sccp.TransferIndication {
	t.Helper()
	b, err := (sccp.MarshalOptions{MaxMessageSize: sccp.MaxBroadbandMessageSize}).Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return sccp.TransferIndication{RoutingLabel: sccp.RoutingLabel{OPC: pcA, DPC: pcB, SLS: 2}, Data: b}
}

// TestReassemblyOfUnequalSegments checks Q.714 §4.1.1.2.2: "Segments can be
// any length", up to the first segment's length times the number of segments.
func TestReassemblyOfUnequalSegments(t *testing.T) {
	b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
	part := func(n, rem int, first bool) sccp.TransferIndication {
		return segmentTransfer(t, &sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1}, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8), CallingPartyAddress: params.NewSSNAddress(7),
			Data: bytes.Repeat([]byte{byte(n)}, n), Segmentation: &params.Segmentation{FirstSegment: first, RemainingSegments: uint8(rem), LocalReference: 77}})
	}
	var res sccp.Result
	for _, in := range []sccp.TransferIndication{part(100, 2, true), part(150, 1, false), part(50, 0, false)} {
		res = b.HandleTransfer(in)
	}
	if u := onlyUnitdata(t, res); len(u.Data) != 300 || u.Segments != 3 {
		t.Fatalf("unequal segments: %+v", u)
	}
}

// TestReassemblyAboveMaximumDataLength checks Q.714 §4.1.1.2.3.1: reassembly
// of more than the 3952 octets the connectionless service carries fails with
// the cause destination cannot perform reassembly.
func TestReassemblyAboveMaximumDataLength(t *testing.T) {
	b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
	part := func(first bool, rem uint8) sccp.TransferIndication {
		return segmentTransfer(t, &sccp.LUDT{ProtocolClass: params.ProtocolClass{Class: 1, ReturnOnError: first}, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8),
			CallingPartyAddress: params.NewSSNAddress(7), Data: make([]byte, 2500), Segmentation: &params.Segmentation{FirstSegment: first, RemainingSegments: rem, LocalReference: 5}})
	}
	b.HandleTransfer(part(true, 1))
	res := b.HandleTransfer(part(false, 0))
	if len(res.Indications) != 0 || len(res.Transfers) != 1 {
		t.Fatalf("5000 octets: %+v", res)
	}
	if l, ok := res.Transfers[0].Message.(*sccp.LUDTS); !ok || l.ReturnCause != params.ReturnCauseDestinationCannotPerformReassembly || len(res.Transfers[0].Data) > sccp.DefaultMaxMessageSize {
		t.Fatalf("returned %v", res.Transfers[0].Message)
	}
}

// TestReassemblyFirstSegmentAtTheBufferLimit checks the buffer limit at the
// first segment: a first segment exactly as long as the limit is accepted.
func TestReassemblyFirstSegmentAtTheBufferLimit(t *testing.T) {
	clock := newClock()
	a := newStack(t, clock, pcA, sccp.StackConfig{})
	tr := mustUnitdata(t, a, request(payload(300)))
	first := len(segmentsOf(t, tr)[0].Data)
	b := newStack(t, clock, pcB, sccp.StackConfig{MaxReassemblyOctets: first}, 8)
	if r := deliver(b, tr[0]); len(r.Transfers)+len(r.Discards) != 0 {
		t.Fatalf("first segment of %d octets with a %d-octet buffer: %+v", first, first, r)
	}
}

// TestReturnRoutedToOriginatingNode checks Q.714 §2.7.5.1 c): the point code
// of a calling party address routed on SSN identifies the originating node,
// which a return goes to instead of the relay that delivered the message.
func TestReturnRoutedToOriginatingNode(t *testing.T) {
	b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
	gt, _ := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "4479")
	for _, tc := range []struct {
		calling params.PartyAddress
		dpc     uint32
	}{
		{params.NewPointCodeAddress(0x0101, 7), 0x0101},
		{params.NewSSNAddress(7), 0x0303},
		{params.PartyAddress{HasPointCode: true, PointCode: 0x0101, HasSSN: true, SSN: 7, GlobalTitle: gt}, 0x0303}, // routed on GT
	} {
		udt := &sccp.UDT{ProtocolClass: params.ProtocolClass{ReturnOnError: true}, CalledPartyAddress: params.NewSSNAddress(9), CallingPartyAddress: tc.calling, Data: []byte{1}}
		in := encodeUnitdata(t, udt)
		in.OPC = 0x0303
		ret := onlyReturn(t, b.HandleTransfer(in))
		if ret.DPC != tc.dpc || ret.OPC != pcB {
			t.Fatalf("calling %v: returned to %+v, want DPC %#x", tc.calling, ret.RoutingLabel, tc.dpc)
		}
	}
}

// TestServiceMessageForUnavailableSubsystem checks that a UDTS, XUDTS or LUDTS
// for a prohibited subsystem is discarded (Q.714 §2.3.1 item 2 b), §4.2 b)).
func TestServiceMessageForUnavailableSubsystem(t *testing.T) {
	b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
	b.SetSubsystemAvailable(8, false)
	for _, m := range []sccp.Message{
		&sccp.UDTS{CalledPartyAddress: params.NewSSNAddress(8), CallingPartyAddress: params.NewSSNAddress(7), Data: []byte{1}},
		&sccp.XUDTS{HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8), CallingPartyAddress: params.NewSSNAddress(7), Data: []byte{1}},
		&sccp.LUDTS{HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8), CallingPartyAddress: params.NewSSNAddress(7), Data: []byte{1}},
	} {
		res := b.HandleTransfer(encodeUnitdata(t, m))
		if len(res.Indications)+len(res.Transfers) != 0 || len(res.Discards) != 1 || !errors.Is(res.Discards[0].Reason, sccp.ErrUndeliverable) {
			t.Fatalf("%s for a prohibited subsystem: %+v", m.MessageType(), res)
		}
	}
}

// TestGlobalTitleCallbackMayUseTheStack checks that LocalGlobalTitle runs
// without the stack locked: a callback that reads the stack does not deadlock.
func TestGlobalTitleCallbackMayUseTheStack(t *testing.T) {
	var b *sccp.Stack
	b = newStack(t, newClock(), pcB, sccp.StackConfig{LocalGlobalTitle: func(params.PartyAddress) bool {
		_, available := b.SubsystemStatus(8)
		return available
	}}, 8)
	gt, _ := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "4479")
	in := encodeUnitdata(t, &sccp.UDT{CalledPartyAddress: params.NewGlobalTitleAddress(gt, 8), CallingPartyAddress: params.NewSSNAddress(7), Data: []byte{1}})
	done := make(chan sccp.Result, 1)
	go func() { done <- b.HandleTransfer(in) }()
	select {
	case res := <-done:
		onlyUnitdata(t, res)
	case <-time.After(5 * time.Second):
		t.Fatal("HandleTransfer did not return: LocalGlobalTitle ran with the stack locked")
	}
}

// TestSingleSegmentMessage checks that an XUDT with the segmentation parameter
// of a complete message (first segment, none remaining) is delivered at once.
func TestSingleSegmentMessage(t *testing.T) {
	b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
	x := &sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 0}, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8), CallingPartyAddress: params.NewSSNAddress(7),
		Data: []byte{1, 2, 3}, Segmentation: &params.Segmentation{FirstSegment: true, LocalReference: 9}}
	wire, err := x.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	u := onlyUnitdata(t, b.HandleTransfer(sccp.TransferIndication{RoutingLabel: sccp.RoutingLabel{OPC: pcA, DPC: pcB}, Data: wire}))
	if u.Segments != 1 || !bytes.Equal(u.Data, x.Data) {
		t.Fatalf("indication %+v", u)
	}
}

func encodeUnitdata(t *testing.T, m sccp.Message) sccp.TransferIndication {
	t.Helper()
	b, err := (sccp.MarshalOptions{MaxMessageSize: sccp.MaxBroadbandMessageSize}).Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return sccp.TransferIndication{RoutingLabel: sccp.RoutingLabel{OPC: pcA, DPC: pcB, SLS: 3}, Data: b}
}

// TestRouting checks ITU-T Q.714 (05/01) §2.3.1 at an end node.
func TestRouting(t *testing.T) {
	gt, _ := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "4479000000")
	gtSSN8 := params.NewGlobalTitleAddress(gt, 8)
	gtNoSSN := params.PartyAddress{GlobalTitle: gt}
	calling := params.NewSSNAddress(7)
	xudt := func(called params.PartyAddress, hop params.HopCounter) *sccp.XUDT {
		return &sccp.XUDT{ProtocolClass: params.ProtocolClass{ReturnOnError: true}, HopCounter: hop, CalledPartyAddress: called, CallingPartyAddress: calling, Data: []byte{1}}
	}
	tests := []struct {
		name    string
		cfg     sccp.StackConfig
		msg     sccp.Message
		deliver bool
		cause   params.ReturnCause
	}{
		{"route on SSN", sccp.StackConfig{}, xudt(params.NewSSNAddress(8), 15), true, 0},
		// The hop counter is not processed when routing on SSN.
		{"route on SSN, hop counter 1", sccp.StackConfig{}, xudt(params.NewSSNAddress(8), 1), true, 0},
		{"route on GT to local SSN", sccp.StackConfig{}, xudt(gtSSN8, 15), true, 0},
		{"route on GT, UDT", sccp.StackConfig{}, &sccp.UDT{ProtocolClass: params.ProtocolClass{ReturnOnError: true}, CalledPartyAddress: gtSSN8, CallingPartyAddress: calling, Data: []byte{1}}, true, 0},
		// §2.3.1 3): decrementing 1 reaches zero, §2.8.6.
		{"route on GT, hop counter 1", sccp.StackConfig{}, xudt(gtSSN8, 1), false, params.ReturnCauseHopCounterViolation},
		{"route on GT, hop counter 2", sccp.StackConfig{}, xudt(gtSSN8, 2), true, 0},
		{"route on GT, translation refuses", sccp.StackConfig{LocalGlobalTitle: func(params.PartyAddress) bool { return false }}, xudt(gtSSN8, 15), false, params.ReturnCauseNoTranslationForThisSpecificAddress},
		{"route on GT, no SSN", sccp.StackConfig{}, xudt(gtNoSSN, 15), false, params.ReturnCauseNoTranslationForThisSpecificAddress},
		{"route on SSN with GT and SSN 0", sccp.StackConfig{}, xudt(params.PartyAddress{RouteOnSSN: true, HasSSN: true, GlobalTitle: gt}, 15), false, params.ReturnCauseNoTranslationForThisSpecificAddress},
		{"unequipped user", sccp.StackConfig{}, xudt(params.NewSSNAddress(9), 15), false, params.ReturnCauseUnequippedUser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newStack(t, newClock(), pcB, tt.cfg, 8)
			in := encodeUnitdata(t, tt.msg)
			res := b.HandleTransfer(in)
			if tt.deliver {
				onlyUnitdata(t, res)
				return
			}
			if tt.cause == params.ReturnCauseHopCounterViolation {
				// §2.3.1 item 3: "In addition, maintenance functions are
				// alerted."
				m, ok := res.Indications[0].(*sccp.MaintenanceIndication)
				if len(res.Indications) != 1 || !ok || m.RoutingLabel != in.RoutingLabel || m.MessageType != tt.msg.MessageType() || !errors.Is(m.Reason, sccp.ErrHopCounterViolation) {
					t.Fatalf("indications %v, want a maintenance alert", res.Indications)
				}
				res.Indications = nil
			}
			ret := onlyReturn(t, res)
			var cause params.ReturnCause
			switch m := ret.Message.(type) {
			case *sccp.XUDTS:
				cause = m.ReturnCause
				if m.HopCounter != 15 {
					t.Errorf("XUDTS hop counter %d, want a fresh 15", m.HopCounter)
				}
			case *sccp.UDTS:
				cause = m.ReturnCause
			}
			if cause != tt.cause {
				t.Fatalf("return cause %s, want %s", cause, tt.cause)
			}
		})
	}

	// Discarded, never returned: §2.2.2.1 item 4 (route on SSN, SSN 0, no GT)
	// and the construction errors of §3.8.3.3 b6 (route on SSN without SSN)
	// and b7 (route on GT without GT).
	for _, tc := range []struct {
		called params.PartyAddress
		reason error
	}{
		{params.PartyAddress{RouteOnSSN: true, HasSSN: true}, sccp.ErrIncompleteAddress},
		{params.PartyAddress{RouteOnSSN: true, GlobalTitle: gt}, sccp.ErrSyntax},
		{params.PartyAddress{RouteOnSSN: true, HasPointCode: true, PointCode: 1}, sccp.ErrSyntax},
		{params.PartyAddress{HasSSN: true, SSN: 8}, sccp.ErrSyntax},
	} {
		b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
		res := b.HandleTransfer(encodeUnitdata(t, xudt(tc.called, 15)))
		if len(res.Transfers)+len(res.Indications) != 0 || len(res.Discards) != 1 || !errors.Is(res.Discards[0].Reason, tc.reason) {
			t.Fatalf("called %v: %+v, want a discard for %v", tc.called, res, tc.reason)
		}
	}
}

// TestMessageReturn checks ITU-T Q.714 (05/01) §4.2.
func TestMessageReturn(t *testing.T) {
	called, calling := params.NewPointCodeAddress(pcB, 9), params.NewPointCodeAddress(pcA, 7)
	b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)

	// UDT to a prohibited subsystem: UDTS, addresses swapped, back to the OPC.
	b.RegisterSubsystem(9)
	b.SetSubsystemAvailable(9, false)
	udt := &sccp.UDT{ProtocolClass: params.ProtocolClass{ReturnOnError: true}, CalledPartyAddress: called, CallingPartyAddress: calling, Data: []byte("data")}
	ret := onlyReturn(t, b.HandleTransfer(encodeUnitdata(t, udt)))
	want := &sccp.UDTS{ReturnCause: params.ReturnCauseSubsystemFailure, CalledPartyAddress: calling, CallingPartyAddress: called, Data: []byte("data")}
	if diff := cmp.Diff(sccp.Message(want), ret.Message, cmpOpts); diff != "" {
		t.Fatalf("UDTS (-want +got):\n%s", diff)
	}
	if ret.RoutingLabel != (sccp.RoutingLabel{OPC: pcB, DPC: pcA, SLS: 3}) {
		t.Fatalf("UDTS label %+v", ret.RoutingLabel)
	}

	// Without return on error the message is only discarded.
	udt.ProtocolClass.ReturnOnError = false
	res := b.HandleTransfer(encodeUnitdata(t, udt))
	if len(res.Transfers) != 0 || len(res.Discards) != 1 || !errors.Is(res.Discards[0].Reason, sccp.ErrUndeliverable) {
		t.Fatalf("no return requested: %+v", res)
	}

	// A received UDT above the maximum size (decoding is liberal) comes back
	// truncated to fit (§4.1.2: UDTS with possible truncation).
	long := &sccp.UDT{ProtocolClass: params.ProtocolClass{ReturnOnError: true}, CalledPartyAddress: params.NewPointCodeAddress(pcB, 10), CallingPartyAddress: calling, Data: payload(255)}
	ret = onlyReturn(t, b.HandleTransfer(encodeUnitdata(t, long)))
	udts := ret.Message.(*sccp.UDTS)
	if len(ret.Data) != sccp.DefaultMaxMessageSize || !bytes.Equal(udts.Data, long.Data[:len(udts.Data)]) || udts.ReturnCause != params.ReturnCauseUnequippedUser {
		t.Fatalf("truncated UDTS of %d octets: %v", len(ret.Data), udts)
	}

	// An LUDT comes back as a short LUDTS, truncated to fit (Q.714 §4.2,
	// Q.715 §8.3.1.6).
	ludt := &sccp.LUDT{ProtocolClass: params.ProtocolClass{ReturnOnError: true}, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(10), CallingPartyAddress: params.NewSSNAddress(7), Data: payload(1000)}
	ret = onlyReturn(t, b.HandleTransfer(encodeUnitdata(t, ludt)))
	l, ok := ret.Message.(*sccp.LUDTS)
	if !ok || len(ret.Data) != sccp.DefaultMaxMessageSize || !bytes.Equal(l.Data, ludt.Data[:len(l.Data)]) || l.ReturnCause != params.ReturnCauseUnequippedUser {
		t.Fatalf("LUDT return: %d octets %v", len(ret.Data), ret.Message)
	}
	// With a broadband maximum the LUDTS carries everything.
	bb := newStack(t, newClock(), pcB, sccp.StackConfig{MaxMessageSize: sccp.MaxBroadbandMessageSize}, 8)
	ret = onlyReturn(t, bb.HandleTransfer(encodeUnitdata(t, ludt)))
	if l, ok := ret.Message.(*sccp.LUDTS); !ok || !bytes.Equal(l.Data, ludt.Data) {
		t.Fatalf("broadband LUDT return: %v", ret.Message)
	}
	onlyUnitdata(t, bb.HandleTransfer(encodeUnitdata(t, &sccp.LUDT{HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8), CallingPartyAddress: calling, Data: payload(3000)})))

	// Undecodable messages and connection-oriented classes are discarded.
	res = b.HandleTransfer(sccp.TransferIndication{Data: []byte{0x09, 0x80}})
	if len(res.Discards) != 1 || !errors.Is(res.Discards[0].Reason, sccp.ErrSyntax) || len(res.Transfers) != 0 {
		t.Fatalf("truncated UDT: %+v", res)
	}
	res = b.HandleTransfer(sccp.TransferIndication{Data: []byte{0x42}})
	if len(res.Discards) != 1 || !errors.Is(res.Discards[0].Reason, sccp.ErrSyntax) {
		t.Fatalf("unknown type: %+v", res)
	}
	classTwo := []byte{0x09, 0x82, 0x03, 0x05, 0x07, 0x02, 0x42, 0x08, 0x02, 0x42, 0x07, 0x01, 0x01}
	res = b.HandleTransfer(sccp.TransferIndication{Data: classTwo})
	if len(res.Discards) != 1 || !errors.Is(res.Discards[0].Reason, sccp.ErrSyntax) || len(res.Transfers) != 0 {
		t.Fatalf("class 2 UDT: %+v", res)
	}
}

// TestServiceMessages checks that UDTS, XUDTS and LUDTS give N-NOTICE
// indications and are never returned.
func TestServiceMessages(t *testing.T) {
	b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
	seg := &params.Segmentation{FirstSegment: true, RemainingSegments: 2, LocalReference: 4}
	x := &sccp.XUDTS{ReturnCause: params.ReturnCauseSubsystemCongestion, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8), CallingPartyAddress: params.NewSSNAddress(7),
		Data: []byte{1, 2}, Segmentation: seg, Importance: ptr(params.Importance(2))}
	res := b.HandleTransfer(encodeUnitdata(t, x))
	if len(res.Indications) != 1 || len(res.Transfers) != 0 {
		t.Fatalf("XUDTS: %+v", res)
	}
	n := res.Indications[0].(*sccp.NoticeIndication)
	want := &sccp.NoticeIndication{RoutingLabel: sccp.RoutingLabel{OPC: pcA, DPC: pcB, SLS: 3}, MessageType: sccp.MsgTypeXUDTS, CalledPartyAddress: x.CalledPartyAddress,
		CallingPartyAddress: x.CallingPartyAddress, ReasonForReturn: x.ReturnCause, Importance: x.Importance, Data: x.Data, Segmentation: seg}
	if diff := cmp.Diff(want, n, cmpOpts); diff != "" {
		t.Fatalf("notice (-want +got):\n%s", diff)
	}
	for _, m := range []sccp.Message{
		&sccp.UDTS{CalledPartyAddress: params.NewSSNAddress(9), CallingPartyAddress: params.NewSSNAddress(7), Data: []byte{1}},
		&sccp.LUDTS{HopCounter: 1, CalledPartyAddress: params.NewGlobalTitleAddress(&params.GlobalTitle{Indicator: params.GTITTOnly}, 8), CallingPartyAddress: params.NewSSNAddress(7), Data: []byte{1}},
	} {
		res := b.HandleTransfer(encodeUnitdata(t, m))
		if len(res.Transfers) != 0 || len(res.Discards) != 1 {
			t.Fatalf("undeliverable %s: %+v", m.MessageType(), res)
		}
		// The LUDTS reached hop counter zero: maintenance is alerted
		// (§2.3.1 item 3), the SCCP user is not.
		alerts := 0
		if m.MessageType() == sccp.MsgTypeLUDTS {
			alerts = 1
		}
		if len(res.Indications) != alerts {
			t.Fatalf("undeliverable %s: indications %v", m.MessageType(), res.Indications)
		}
		if alerts == 1 {
			if a, ok := res.Indications[0].(*sccp.MaintenanceIndication); !ok || !errors.Is(a.Reason, sccp.ErrHopCounterViolation) {
				t.Fatalf("LUDTS indication %v", res.Indications[0])
			}
		}
	}
	onlyNotice := b.HandleTransfer(encodeUnitdata(t, &sccp.UDTS{ReturnCause: params.ReturnCauseMTPFailure, CalledPartyAddress: params.NewSSNAddress(8), CallingPartyAddress: params.NewSSNAddress(7), Data: []byte{1}}))
	if len(onlyNotice.Indications) != 1 {
		t.Fatalf("UDTS: %+v", onlyNotice)
	}
}

// TestReassembledMessageToUnavailableSubsystem checks Q.714 §2.3.1 item 2 b)
// for every segment: a segment that arrives while the subsystem is prohibited
// cannot be delivered, and neither can the message it belongs to.
func TestReassembledMessageToUnavailableSubsystem(t *testing.T) {
	clock := newClock()
	a := newStack(t, clock, pcA, sccp.StackConfig{})
	b := newStack(t, clock, pcB, sccp.StackConfig{}, 8)
	tr := mustUnitdata(t, a, request(payload(600)))
	segs := segmentsOf(t, tr)
	deliver(b, tr[0])
	b.SetSubsystemAvailable(8, false)
	// The process ends and returns its first segment.
	ret := onlyReturn(t, deliver(b, tr[1]))
	x := ret.Message.(*sccp.XUDTS)
	if x.ReturnCause != params.ReturnCauseSubsystemFailure || !bytes.Equal(x.Data, segs[0].Data) || *x.Segmentation != *segs[0].Segmentation {
		t.Fatalf("returned %v", x)
	}
	b.SetSubsystemAvailable(8, true)
	if r := deliver(b, tr[2]); len(r.Indications)+len(r.Transfers) != 0 {
		t.Fatalf("last segment after the subsystem recovered: %+v", r)
	}

	// Return on error asked in a later segment returns the first segment
	// (§4.1.1.2.3: "if return message on error was requested in an XUDT or
	// LUDT message received as part of the reassembly process").
	part := func(first, roe bool, rem uint8, fill byte) sccp.TransferIndication {
		return segmentTransfer(t, &sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1, ReturnOnError: roe}, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8),
			CallingPartyAddress: params.NewSSNAddress(7), Data: bytes.Repeat([]byte{fill}, 100), Segmentation: &params.Segmentation{FirstSegment: first, RemainingSegments: rem, LocalReference: 9}})
	}
	b.HandleTransfer(part(true, false, 2, 0x11))
	b.SetSubsystemAvailable(8, false)
	ret = onlyReturn(t, b.HandleTransfer(part(false, true, 1, 0x22)))
	if x := ret.Message.(*sccp.XUDTS); x.ReturnCause != params.ReturnCauseSubsystemFailure || x.Data[0] != 0x11 || !x.Segmentation.FirstSegment {
		t.Fatalf("returned %v, want the first segment", x)
	}

	// §4.1.1.2.1: a first segment for a busy reference discards the segments
	// received so far, which are not returned, and the new message is.
	b.SetSubsystemAvailable(8, true)
	b.HandleTransfer(part(true, true, 2, 0x33))
	b.SetSubsystemAvailable(8, false)
	r := b.HandleTransfer(part(true, true, 2, 0x44))
	ret = onlyReturn(t, r)
	if x := ret.Message.(*sccp.XUDTS); x.ReturnCause != params.ReturnCauseSubsystemFailure || x.Data[0] != 0x44 || len(r.Discards) != 2 {
		t.Fatalf("returned %v with discards %v, want the new message", x, r.Discards)
	}
	// The rest of the new message is discarded without a second return
	// (§4.2), although it asks for return.
	b.SetSubsystemAvailable(8, true)
	if r := b.HandleTransfer(part(false, true, 1, 0x44)); len(r.Transfers)+len(r.Indications) != 0 || len(r.Discards) != 1 {
		t.Fatalf("second segment of the returned message: %+v", r)
	}

	// A first segment for an unavailable subsystem is returned at once, and
	// starts no reassembly.
	b.SetSubsystemAvailable(8, false)
	tr = mustUnitdata(t, a, request(payload(300)))
	ret = onlyReturn(t, deliver(b, tr[0]))
	if ret.Message.(*sccp.XUDTS).ReturnCause != params.ReturnCauseSubsystemFailure {
		t.Fatalf("first segment: %v", ret.Message)
	}
	b.SetSubsystemAvailable(8, true)
	if r := deliver(b, tr[1]); len(r.Transfers)+len(r.Indications) != 0 || len(r.Discards) != 1 {
		t.Fatalf("second segment: %+v", r)
	}
}

// TestEmptyUserDataDiscarded checks that a UDT, XUDT or LUDT whose data
// parameter is empty, which the decoder accepts, is discarded as a syntax
// error and neither delivered nor returned (ITU-T Q.714 (05/01) §3.8.3.3 b1,
// §4.3).
func TestEmptyUserDataDiscarded(t *testing.T) {
	b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
	for _, wire := range []string{
		// UDT, class 0 with return on error, data length 0.
		"09 80 03 05 07 02 42 08 02 42 07 00",
		// XUDT, class 1 with return on error, hop counter 15, data length 0.
		"11 81 0f 04 06 08 00 02 42 08 02 42 07 00",
		// LUDT, class 1 with return on error, long data length 0.
		"13 81 0f 07 00 08 00 09 00 00 00 02 42 08 02 42 07 00 00",
	} {
		in := sccp.TransferIndication{RoutingLabel: sccp.RoutingLabel{OPC: pcA, DPC: pcB}, Data: unhex(t, wire)}
		if _, err := sccp.ParseMessage(in.Data); err != nil {
			t.Fatalf("% x does not decode: %v", in.Data, err)
		}
		res := b.HandleTransfer(in)
		if len(res.Indications)+len(res.Transfers) != 0 || len(res.Discards) != 1 || !errors.Is(res.Discards[0].Reason, sccp.ErrSyntax) {
			t.Fatalf("%s: %+v", wire, res)
		}
	}
}

// TestRequestsRefuseUnroutableCalledAddress checks ITU-T Q.714 (05/01)
// §2.2.2.1 at the originating node: a called party address routed on SSN
// carries a subsystem number other than 0, and one with SSN 0 or none carries
// a global title and is routed on GT (items 2, 3 and 5; §3.8.3.3 b6 and b7).
func TestRequestsRefuseUnroutableCalledAddress(t *testing.T) {
	gt, _ := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "4479")
	for _, called := range []params.PartyAddress{
		{RouteOnSSN: true, HasPointCode: true, PointCode: 1},
		{HasSSN: true, SSN: 8},
		{RouteOnSSN: true, HasSSN: true},
		{HasSSN: true, SSN: 8, GlobalTitle: nil, HasPointCode: true, PointCode: 1},
	} {
		a := newStack(t, newClock(), pcA, sccp.StackConfig{})
		req := request(payload(10))
		req.CalledPartyAddress = called
		if res, err := a.Unitdata(req); !errors.Is(err, sccp.ErrInvalidRequest) || len(res.Transfers) != 0 {
			t.Errorf("Unitdata to %v: %v, %+v", called, err, res)
		}
		if _, res, err := a.Connect(sccp.ConnectRequest{CalledPartyAddress: called, DPC: pcB}); !errors.Is(err, sccp.ErrInvalidRequest) || len(res.Transfers) != 0 {
			t.Errorf("Connect to %v: %v, %+v", called, err, res)
		}
	}
	// A global title with SSN 0 is routed on GT (item 3).
	a := newStack(t, newClock(), pcA, sccp.StackConfig{})
	req := request(payload(10))
	req.CalledPartyAddress = params.PartyAddress{RouteOnSSN: true, HasSSN: true, GlobalTitle: gt}
	if _, err := a.Unitdata(req); !errors.Is(err, sccp.ErrInvalidRequest) {
		t.Errorf("GT with SSN 0 routed on SSN: %v", err)
	}
	if _, _, err := a.Connect(sccp.ConnectRequest{CalledPartyAddress: req.CalledPartyAddress, DPC: pcB}); !errors.Is(err, sccp.ErrInvalidRequest) {
		t.Errorf("CR to a GT with SSN 0 routed on SSN: %v", err)
	}
	req.CalledPartyAddress.RouteOnSSN = false
	mustUnitdata(t, a, req)

	// A calling party address is the address indicator alone with bits 1-7
	// zero, or carries an element (ITU-T Q.713 (03/01) Table 11 Note 2).
	for _, n := range []int{10, 1000} {
		req := request(payload(n))
		req.CallingPartyAddress = params.PartyAddress{RouteOnSSN: true}
		if _, err := a.Unitdata(req); !errors.Is(err, sccp.ErrInvalidRequest) {
			t.Errorf("%d octets from a routing indicator alone: %v", n, err)
		}
	}
	req.CallingPartyAddress = params.PartyAddress{}
	mustUnitdata(t, a, req)
}

// TestUnitdataImportance checks ITU-T Q.714 (05/01) §2.6.2: the importance
// of an XUDT is the requested one, at most 6 (Table 2); an importance the
// parameter cannot carry is refused (ITU-T Q.713 (03/01) §3.19).
func TestUnitdataImportance(t *testing.T) {
	a := newStack(t, newClock(), pcA, sccp.StackConfig{})
	for _, tc := range []struct {
		requested, sent params.Importance
		n               int
	}{{7, 6, 10}, {7, 6, 600}, {6, 6, 10}, {3, 3, 600}} {
		req := request(payload(tc.n))
		req.Importance = &tc.requested
		for _, tr := range mustUnitdata(t, a, req) {
			m, err := sccp.ParseMessage(tr.Data)
			if err != nil {
				t.Fatal(err)
			}
			if x, ok := m.(*sccp.XUDT); !ok || x.Importance == nil || *x.Importance != tc.sent {
				t.Fatalf("importance %d over %d octets: sent %v", tc.requested, tc.n, m)
			}
		}
	}
	req := request(payload(10))
	req.Importance = ptr(params.Importance(8))
	if _, err := a.Unitdata(req); !errors.Is(err, sccp.ErrInvalidRequest) {
		t.Fatalf("importance 8: %v", err)
	}
}

// TestUnitdataResultsShareNothing checks that the messages of a request share
// no memory with the request or with one another: each TransferRequest.Message
// keeps encoding to its Data.
func TestUnitdataResultsShareNothing(t *testing.T) {
	for _, n := range []int{10, 600} {
		a := newStack(t, newClock(), pcA, sccp.StackConfig{})
		gt, _ := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "4479")
		req := request(payload(n))
		req.CalledPartyAddress = params.NewGlobalTitleAddress(gt, 8)
		tr := mustUnitdata(t, a, req)
		encodes := func(after string, from int) {
			t.Helper()
			for i, x := range tr[from:] {
				b, err := x.Message.MarshalBinary()
				if err != nil || !bytes.Equal(b, x.Data) {
					t.Fatalf("%d octets, after %s: transfer %d no longer encodes to its data", n, after, from+i)
				}
			}
		}
		gt.AddressInformation[0] = 0x99
		req.Data[0] ^= 0xff
		encodes("changing the request", 0)
		var first *params.GlobalTitle
		switch m := tr[0].Message.(type) {
		case *sccp.UDT:
			first = m.CalledPartyAddress.GlobalTitle
		case *sccp.XUDT:
			first = m.CalledPartyAddress.GlobalTitle
		}
		first.AddressInformation[0] = 0x77
		encodes("changing the first message", 1)
	}
}

// TestGlobalTitleCallbackGetsACopy checks that LocalGlobalTitle cannot reach
// the addresses the stack keeps: changing its argument does not change the
// return that T(reassembly) produces later (ITU-T Q.714 (05/01) §4.2 a)).
func TestGlobalTitleCallbackGetsACopy(t *testing.T) {
	clock := newClock()
	b := newStack(t, clock, pcB, sccp.StackConfig{LocalGlobalTitle: func(called params.PartyAddress) bool {
		called.GlobalTitle.AddressInformation[0] = 0x99
		return true
	}}, 8)
	gt, _ := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "4479")
	first := &sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1, ReturnOnError: true}, HopCounter: 15, CalledPartyAddress: params.NewGlobalTitleAddress(gt, 8),
		CallingPartyAddress: params.NewSSNAddress(7), Data: []byte{1, 2}, Segmentation: &params.Segmentation{FirstSegment: true, RemainingSegments: 1, LocalReference: 3}}
	b.HandleTransfer(segmentTransfer(t, first))
	clock.Advance(sccp.DefaultReassemblyTimeout)
	ret := onlyReturn(t, b.HandleTimeout())
	if got := ret.Message.(*sccp.XUDTS).CallingPartyAddress.GlobalTitle.AddressInformation; !bytes.Equal(got, gt.AddressInformation) {
		t.Fatalf("returned with called address % x, want % x", got, gt.AddressInformation)
	}
}

// TestReassemblyBoundPerPeer checks StackConfig.MaxReassemblyOctetsPerPeer:
// the processes of one signalling point hold at most that much user data;
// beyond it the reassembly ends with the cause network congestion (ITU-T
// Q.714 (05/01) §4.1.1.2.3.4), and the other signalling points keep theirs.
func TestReassemblyBoundPerPeer(t *testing.T) {
	const pcC = 0x0303
	clock := newClock()
	a := newStack(t, clock, pcA, sccp.StackConfig{})
	c := newStack(t, clock, pcC, sccp.StackConfig{})
	// 450 octets travel as two segments of 225: exactly the bound.
	b := newStack(t, clock, pcB, sccp.StackConfig{MaxReassemblyOctetsPerPeer: 450}, 8)
	if u := onlyUnitdata(t, deliver(b, mustUnitdata(t, a, request(payload(450)))...)); len(u.Data) != 450 {
		t.Fatalf("450 octets: %+v", u)
	}

	// One octet more fails at the last segment, and frees what it held.
	tr := mustUnitdata(t, a, request(payload(451)))
	r := deliver(b, tr...)
	if len(r.Indications) != 0 || len(r.Transfers) != 1 || r.Transfers[0].Message.(*sccp.XUDTS).ReturnCause != params.ReturnCauseNetworkCongestion {
		t.Fatalf("451 octets from one peer: %+v", r)
	}

	// Two messages in progress from A take its whole share: a third is
	// refused at its first segment, while C reassembles as much.
	first := mustUnitdata(t, a, request(payload(450)))
	second := mustUnitdata(t, a, request(payload(450)))
	deliver(b, first[0], second[0])
	if ret := onlyReturn(t, deliver(b, mustUnitdata(t, a, request(payload(450)))[0])); ret.Message.(*sccp.XUDTS).ReturnCause != params.ReturnCauseNetworkCongestion {
		t.Fatalf("third message from A: %v", ret.Message)
	}
	if u := onlyUnitdata(t, deliver(b, mustUnitdata(t, c, request(payload(450)))...)); u.OPC != pcC {
		t.Fatalf("message from C: %+v", u)
	}
	// A's second message cannot grow beyond the share; the first completes
	// and gives its octets back.
	if r := deliver(b, second[1]); len(r.Indications) != 0 || len(r.Transfers) != 1 {
		t.Fatalf("second message from A over its share: %+v", r)
	}
	onlyUnitdata(t, deliver(b, first[1]))
	onlyUnitdata(t, deliver(b, mustUnitdata(t, a, request(payload(450)))...))

	// T(reassembly) expiry gives the octets back too.
	deliver(b, mustUnitdata(t, a, request(payload(450)))[0])
	clock.Advance(sccp.DefaultReassemblyTimeout)
	b.HandleTimeout()
	onlyUnitdata(t, deliver(b, mustUnitdata(t, a, request(payload(450)))...))
}

// TestReassemblyReturnsOnce checks ITU-T Q.714 (05/01) §4.2: "When the
// XUDTS/LUDTS is a result of a reassembly error (see 4.1.1.2.3), then only one
// XUDTS/LUDTS message will be sent", with a sender that asks for return in
// every segment, which §4.1.1.1.3 leaves to the implementation.
func TestReassemblyReturnsOnce(t *testing.T) {
	part := func(ref uint32, first bool, rem uint8) sccp.TransferIndication {
		return segmentTransfer(t, &sccp.XUDT{ProtocolClass: params.ProtocolClass{Class: 1, ReturnOnError: true}, HopCounter: 15, CalledPartyAddress: params.NewSSNAddress(8),
			CallingPartyAddress: params.NewSSNAddress(7), Data: bytes.Repeat([]byte{byte(10*ref) + rem}, 100), Segmentation: &params.Segmentation{FirstSegment: first, RemainingSegments: rem, LocalReference: ref}})
	}
	returned := func(t *testing.T, r sccp.Result, cause params.ReturnCause, fill byte) {
		t.Helper()
		ret := onlyReturn(t, r)
		if x := ret.Message.(*sccp.XUDTS); x.ReturnCause != cause || x.Data[0] != fill {
			t.Fatalf("returned %v, want cause %s with data %#x", x, cause, fill)
		}
	}
	silent := func(t *testing.T, r sccp.Result) {
		t.Helper()
		if len(r.Transfers)+len(r.Indications) != 0 || len(r.Discards) != 1 || !errors.Is(r.Discards[0].Reason, sccp.ErrReassembly) {
			t.Fatalf("want one reassembly discard and no return: %+v", r)
		}
	}

	t.Run("lost first segment", func(t *testing.T) {
		b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
		returned(t, b.HandleTransfer(part(1, false, 2)), params.ReturnCauseErrorInMessageTransport, 12)
		silent(t, b.HandleTransfer(part(1, false, 1)))
		silent(t, b.HandleTransfer(part(1, false, 0)))
		// A duplicate after the last segment is part of the same message.
		silent(t, b.HandleTransfer(part(1, false, 0)))
	})
	t.Run("lost middle segment", func(t *testing.T) {
		b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
		b.HandleTransfer(part(2, true, 3))
		returned(t, b.HandleTransfer(part(2, false, 1)), params.ReturnCauseErrorInMessageTransport, 23)
		silent(t, b.HandleTransfer(part(2, false, 0)))
		// A new message with the reference reassembles.
		b.HandleTransfer(part(2, true, 1))
		if u := onlyUnitdata(t, b.HandleTransfer(part(2, false, 0))); len(u.Data) != 200 {
			t.Fatalf("new message: %+v", u)
		}
	})
	t.Run("first segment refused", func(t *testing.T) {
		b := newStack(t, newClock(), pcB, sccp.StackConfig{MaxReassemblies: 1}, 8)
		b.HandleTransfer(part(3, true, 2))
		returned(t, b.HandleTransfer(part(4, true, 2)), params.ReturnCauseNetworkCongestion, 42)
		silent(t, b.HandleTransfer(part(4, false, 1)))
		// The record of the refused message does not take the process slot.
		b.HandleTransfer(part(3, false, 1))
		if u := onlyUnitdata(t, b.HandleTransfer(part(3, false, 0))); len(u.Data) != 300 {
			t.Fatalf("message in progress: %+v", u)
		}
		// Nor does it keep a new message from starting.
		if r := b.HandleTransfer(part(8, true, 1)); len(r.Transfers)+len(r.Discards) != 0 {
			t.Fatalf("new first segment with a record present: %+v", r)
		}
	})
	t.Run("a replayed first segment stays in the failed message", func(t *testing.T) {
		b := newStack(t, newClock(), pcB, sccp.StackConfig{MaxReassemblies: 1}, 8)
		b.HandleTransfer(part(21, true, 2))
		returned(t, b.HandleTransfer(part(21, false, 0)), params.ReturnCauseErrorInMessageTransport, 212)
		silent(t, b.HandleTransfer(part(21, true, 2)))
		silent(t, b.HandleTransfer(part(21, false, 0)))
		// The same for a first segment refused for want of room.
		returned(t, b.HandleTransfer(part(22, true, 1)), params.ReturnCauseNetworkCongestion, 221)
		silent(t, b.HandleTransfer(part(22, true, 1)))
		// Another first segment with the reference is a new message, refused
		// in turn while the record of reference 21 holds the one place.
		returned(t, b.HandleTransfer(part(22, true, 2)), params.ReturnCauseNetworkCongestion, 222)
	})
	t.Run("another first segment starts a new message", func(t *testing.T) {
		b := newStack(t, newClock(), pcB, sccp.StackConfig{MaxReassemblies: 1}, 8)
		b.HandleTransfer(part(9, true, 3))
		returned(t, b.HandleTransfer(part(9, false, 0)), params.ReturnCauseErrorInMessageTransport, 93)
		// The new message takes the place the record held.
		if r := b.HandleTransfer(part(9, true, 1)); len(r.Transfers)+len(r.Discards) != 0 {
			t.Fatalf("new first segment: %+v", r)
		}
		if u := onlyUnitdata(t, b.HandleTransfer(part(9, false, 0))); len(u.Data) != 200 {
			t.Fatalf("new message: %+v", u)
		}
	})
	t.Run("a late first segment of a message seen through a later one", func(t *testing.T) {
		b := newStack(t, newClock(), pcB, sccp.StackConfig{}, 8)
		returned(t, b.HandleTransfer(part(10, false, 2)), params.ReturnCauseErrorInMessageTransport, 102)
		silent(t, b.HandleTransfer(part(10, true, 3)))
		silent(t, b.HandleTransfer(part(10, false, 1)))
	})
	t.Run("record lasts T(reassembly)", func(t *testing.T) {
		clock := newClock()
		b := newStack(t, clock, pcB, sccp.StackConfig{}, 8)
		returned(t, b.HandleTransfer(part(5, false, 3)), params.ReturnCauseErrorInMessageTransport, 53)
		deadline, ok := b.NextTimeout()
		if !ok || !deadline.Equal(clock.Now().Add(sccp.DefaultReassemblyTimeout)) {
			t.Fatalf("record deadline %v, %v", deadline, ok)
		}
		clock.Advance(sccp.DefaultReassemblyTimeout)
		if r := b.HandleTimeout(); len(r.Transfers)+len(r.Indications)+len(r.Discards) != 0 {
			t.Fatalf("record expiry produced %+v", r)
		}
		if _, ok := b.NextTimeout(); ok {
			t.Fatal("a timer remains after the record expired")
		}
		returned(t, b.HandleTransfer(part(5, false, 2)), params.ReturnCauseErrorInMessageTransport, 52)
	})
	t.Run("records are bounded and kept", func(t *testing.T) {
		clock := newClock()
		b := newStack(t, clock, pcB, sccp.StackConfig{MaxReassemblies: 1}, 8)
		returned(t, b.HandleTransfer(part(6, false, 3)), params.ReturnCauseErrorInMessageTransport, 63)
		// No record is left for reference 7: it is not returned, so that it
		// cannot be returned twice, and reference 6 keeps its record.
		silent(t, b.HandleTransfer(part(7, false, 3)))
		silent(t, b.HandleTransfer(part(6, false, 2)))
		// Once the record of reference 6 ends, reference 7 is returned, once.
		clock.Advance(sccp.DefaultReassemblyTimeout)
		b.HandleTimeout()
		returned(t, b.HandleTransfer(part(7, false, 2)), params.ReturnCauseErrorInMessageTransport, 72)
		silent(t, b.HandleTransfer(part(7, false, 1)))
	})
	t.Run("a failed process keeps its place", func(t *testing.T) {
		b := newStack(t, newClock(), pcB, sccp.StackConfig{MaxReassemblies: 1}, 8)
		b.HandleTransfer(part(11, true, 2))
		returned(t, b.HandleTransfer(part(11, false, 0)), params.ReturnCauseErrorInMessageTransport, 112)
		// The record of reference 11 holds the one place: a new message is
		// refused, and the rest of message 11 is still not returned.
		returned(t, b.HandleTransfer(part(12, true, 1)), params.ReturnCauseNetworkCongestion, 121)
		silent(t, b.HandleTransfer(part(11, false, 1)))
	})
	t.Run("a failure after T(reassembly) expiry", func(t *testing.T) {
		clock := newClock()
		b := newStack(t, clock, pcB, sccp.StackConfig{}, 8)
		b.HandleTransfer(part(15, true, 2))
		clock.Advance(sccp.DefaultReassemblyTimeout)
		returned(t, b.HandleTimeout(), params.ReturnCauseErrorInMessageTransport, 152)
		silent(t, b.HandleTransfer(part(15, false, 1)))
		clock.Advance(sccp.DefaultReassemblyTimeout)
		b.HandleTimeout()
		returned(t, b.HandleTransfer(part(15, false, 0)), params.ReturnCauseErrorInMessageTransport, 150)
	})
}

func TestSubsystemRegistry(t *testing.T) {
	s := newStack(t, nil, pcA, sccp.StackConfig{})
	if reg, avail := s.SubsystemStatus(8); reg || avail {
		t.Fatal("unregistered subsystem reported")
	}
	if s.SetSubsystemAvailable(8, true) {
		t.Fatal("SetSubsystemAvailable succeeded for an unregistered subsystem")
	}
	s.RegisterSubsystem(8)
	if reg, avail := s.SubsystemStatus(8); !reg || !avail {
		t.Fatal("registered subsystem not available")
	}
	s.SetSubsystemAvailable(8, false)
	if reg, avail := s.SubsystemStatus(8); !reg || avail {
		t.Fatal("prohibited subsystem reported available")
	}
	s.UnregisterSubsystem(8)
	if reg, _ := s.SubsystemStatus(8); reg {
		t.Fatal("unregistered subsystem still registered")
	}
}

func TestNewStackRejectsInvalidConfig(t *testing.T) {
	for _, cfg := range []sccp.StackConfig{
		{MaxMessageSize: -1},
		{MaxMessageSize: 10},
		{MaxMessageSize: sccp.MaxBroadbandMessageSize + 1},
		{HopCounter: 16},
		{ReassemblyTimeout: -1},
		{MaxReassemblies: -1},
		{MaxReassemblyOctetsPerPeer: -1},
	} {
		if _, err := sccp.NewStack(cfg); !errors.Is(err, sccp.ErrInvalidRequest) {
			t.Errorf("NewStack(%+v) = %v", cfg, err)
		}
	}
}

// TestRunTimers checks the real-time timer driver.
// TestRunTimers checks that RunTimers delivers whatever the timers produce,
// and wakes up for a timer earlier than the one it is waiting for.
func TestRunTimers(t *testing.T) {
	run := func(s *sccp.Stack) (<-chan sccp.Result, func()) {
		ctx, cancel := context.WithCancel(context.Background())
		got := make(chan sccp.Result, 4)
		done := make(chan struct{})
		go func() {
			s.RunTimers(ctx, func(r sccp.Result) { got <- r })
			close(done)
		}()
		return got, func() { cancel(); <-done }
	}
	wait := func(t *testing.T, got <-chan sccp.Result, timer string) sccp.Result {
		t.Helper()
		select {
		case r := <-got:
			return r
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not fire", timer)
		}
		return sccp.Result{}
	}

	t.Run("return", func(t *testing.T) {
		a := newStack(t, nil, pcA, sccp.StackConfig{})
		b := newStack(t, nil, pcB, sccp.StackConfig{ReassemblyTimeout: 20 * time.Millisecond}, 8)
		got, stop := run(b)
		defer stop()
		deliver(b, mustUnitdata(t, a, request(payload(300)))[0])
		if r := wait(t, got, "T(reassembly)"); len(r.Transfers) != 1 {
			t.Fatalf("timer result %+v", r)
		}
	})
	t.Run("discard only, earlier than the awaited timer", func(t *testing.T) {
		a := newStack(t, nil, pcA, sccp.StackConfig{})
		b := newStack(t, nil, pcB, sccp.StackConfig{ReassemblyTimeout: 20 * time.Millisecond, Connection: sccp.ConnectionConfig{EstablishmentTimeout: time.Hour}}, 8)
		if _, _, err := b.Connect(sccp.ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(pcA, 8)}); err != nil {
			t.Fatal(err)
		}
		got, stop := run(b)
		defer stop()
		// Let RunTimers settle on T(conn est), an hour away.
		time.Sleep(50 * time.Millisecond)
		req := request(payload(300))
		req.ReturnOnError = false
		deliver(b, mustUnitdata(t, a, req)[0])
		if r := wait(t, got, "T(reassembly)"); len(r.Transfers)+len(r.Indications) != 0 || len(r.Discards) != 1 {
			t.Fatalf("timer result %+v", r)
		}
	})
	t.Run("indication only", func(t *testing.T) {
		b := newStack(t, nil, pcB, sccp.StackConfig{Connection: sccp.ConnectionConfig{EstablishmentTimeout: 20 * time.Millisecond}})
		if _, _, err := b.Connect(sccp.ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(pcA, 8)}); err != nil {
			t.Fatal(err)
		}
		got, stop := run(b)
		defer stop()
		if r := wait(t, got, "T(conn est)"); len(r.Transfers)+len(r.Discards) != 0 || len(r.Indications) != 1 {
			t.Fatalf("timer result %+v", r)
		}
	})
}

// TestConcurrentUse runs requests, deliveries and timers from several
// goroutines; run with -race.
func TestConcurrentUse(t *testing.T) {
	clock := newClock()
	a := newStack(t, clock, pcA, sccp.StackConfig{})
	b := newStack(t, clock, pcB, sccp.StackConfig{}, 8)
	var wg sync.WaitGroup
	var mu sync.Mutex
	delivered := 0
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 20 {
				req := request(payload(100 + 37*g + i))
				req.CallingPartyAddress = params.NewSSNAddress(uint8(10 + g))
				res, err := a.Unitdata(req)
				if err != nil {
					t.Error(err)
					return
				}
				r := deliver(b, res.Transfers...)
				b.HandleTimeout()
				_, _ = b.NextTimeout()
				mu.Lock()
				delivered += len(r.Indications)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if delivered != 160 {
		t.Fatalf("delivered %d of 160", delivered)
	}
}
