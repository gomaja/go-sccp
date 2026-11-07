// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"testing"
	"time"

	"github.com/gomaja/go-sccp/params"
)

func TestUnitdataLUDTReferenceFreeze(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	freeze := time.Minute
	s, err := NewStack(StackConfig{PointCode: 1, UseLUDT: true, MaxMessageSize: MaxBroadbandMessageSize,
		SegmentationReferenceFreeze: freeze, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	req := UnitdataRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 8), CallingPartyAddress: params.NewPointCodeAddress(1, 7), Data: make([]byte, 1000)}
	res, err := s.Unitdata(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transfers) != 1 {
		t.Fatalf("got %d transfers, want one LUDT", len(res.Transfers))
	}
	l, ok := res.Transfers[0].Message.(*LUDT)
	if !ok || l.Segmentation == nil {
		t.Fatal("want LUDT with segmentation parameter")
	}
	ref := l.Segmentation.LocalReference
	if got := s.frozenSegmentation[ref]; !got.Equal(now.Add(freeze)) {
		t.Fatalf("reference frozen until %v, want %v", got, now.Add(freeze))
	}
	// Force the allocator to revisit it just before and at expiry.
	now = now.Add(freeze - time.Nanosecond)
	s.nextSegmentationReference = ref
	req.MaxMessageSize = DefaultMaxMessageSize
	res, err = s.Unitdata(req)
	if err != nil {
		t.Fatal(err)
	}
	x, ok := res.Transfers[0].Message.(*XUDT)
	if !ok || x.Segmentation == nil || x.Segmentation.LocalReference == ref {
		t.Fatal("following segmented request reused frozen LUDT reference")
	}
	now = now.Add(time.Nanosecond)
	s.nextSegmentationReference = ref
	res, err = s.Unitdata(req)
	if err != nil {
		t.Fatal(err)
	}
	x = res.Transfers[0].Message.(*XUDT)
	if x.Segmentation.LocalReference != ref {
		t.Fatalf("expired LUDT reference not reusable: got %d, want %d", x.Segmentation.LocalReference, ref)
	}
}
