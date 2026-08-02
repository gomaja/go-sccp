// Copyright 2019-2024 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp_test

import (
	"errors"
	"testing"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

func TestEncodeSequenceNumberMasksAndShiftsModulo128(t *testing.T) {
	tests := []struct {
		name string
		in   uint8
		want uint8
	}{
		{name: "zero", in: 0, want: 0x00},
		{name: "one", in: 1, want: 0x02},
		{name: "sixty four", in: 64, want: 0x80},
		{name: "one hundred twenty seven", in: 127, want: 0xfe},
		{name: "one hundred twenty eight wraps", in: 128, want: 0x00},
		{name: "all bits wraps to one hundred twenty seven", in: 255, want: 0xfe},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sccp.EncodeSequenceNumber(tt.in); got != tt.want {
				t.Fatalf("EncodeSequenceNumber(%d) = %#02x, want %#02x", tt.in, got, tt.want)
			}
		})
	}
}

func TestConnectionSectionSendDataUsesDT1ForClass2(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 2, 0)

	message, err := section.SendData([]byte{0xaa, 0xbb}, true)
	if err != nil {
		t.Fatalf("SendData() error = %v", err)
	}
	dt1, ok := message.(*sccp.DT1)
	if !ok {
		t.Fatalf("SendData() = %T, want *sccp.DT1", message)
	}
	if got, want := dt1.DestinationLocalReference.Uint32(), uint32(0x040506); got != want {
		t.Fatalf("DT1 destination reference = %#06x, want %#06x", got, want)
	}
	if !dt1.SegmentingReassembling.MoreData() {
		t.Fatal("DT1 more-data = false, want true")
	}
	if got, want := string(dt1.Data.Value()), string([]byte{0xaa, 0xbb}); got != want {
		t.Fatalf("DT1 data = %x, want %x", dt1.Data.Value(), []byte{0xaa, 0xbb})
	}
}

func TestConnectionSectionAcceptsDT1ForClass2(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 2, 0)

	indication, err := section.HandleDataForm1(sccp.NewDT1(0x010203, false, []byte{0xaa}))
	if err != nil {
		t.Fatalf("HandleDataForm1() error = %v", err)
	}
	if indication.MoreData {
		t.Fatal("MoreData = true, want false")
	}
	if got, want := string(indication.Data), string([]byte{0xaa}); got != want {
		t.Fatalf("Data = %x, want %x", indication.Data, []byte{0xaa})
	}
}

func TestConnectionSectionSendDataUsesDT2AndSequenceForClass3(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 3, 2)

	message, err := section.SendData([]byte{0xaa}, false)
	if err != nil {
		t.Fatalf("SendData() error = %v", err)
	}
	dt2, ok := message.(*sccp.DT2)
	if !ok {
		t.Fatalf("SendData() = %T, want *sccp.DT2", message)
	}
	if got, want := dt2.DestinationLocalReference.Uint32(), uint32(0x040506); got != want {
		t.Fatalf("DT2 destination reference = %#06x, want %#06x", got, want)
	}
	if got, want := dt2.SequencingSegmenting.SendSequenceNumber, uint8(0x00); got != want {
		t.Fatalf("DT2 P(S) = %d, want %d", got, want)
	}
	if got, want := dt2.SequencingSegmenting.ReceiveSequenceNumber, uint8(0x00); got != want {
		t.Fatalf("DT2 P(R) = %d, want %d", got, want)
	}
	if got, want := section.SendSequenceNumber, uint8(1); got != want {
		t.Fatalf("SendSequenceNumber = %d, want %d", got, want)
	}
}

func TestConnectionSectionClass3SendWindowBlocksUntilAK(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 3, 1)

	if _, err := section.SendData([]byte{0xaa}, false); err != nil {
		t.Fatalf("first SendData() error = %v", err)
	}
	_, err := section.SendData([]byte{0xbb}, false)
	if !errors.Is(err, sccp.ErrFlowControlBlocked) {
		t.Fatalf("second SendData() error = %v, want %v", err, sccp.ErrFlowControlBlocked)
	}

	if err := section.HandleAcknowledgement(sccp.NewAK(0x010203, 0x02, 1)); err != nil {
		t.Fatalf("HandleAcknowledgement() error = %v", err)
	}
	message, err := section.SendData([]byte{0xbb}, false)
	if err != nil {
		t.Fatalf("third SendData() error = %v", err)
	}
	dt2 := message.(*sccp.DT2)
	if got, want := dt2.SequencingSegmenting.SendSequenceNumber, uint8(0x02); got != want {
		t.Fatalf("DT2 P(S) after AK = %d, want %d", got, want)
	}
}

func TestConnectionSectionAcknowledgementCreditZeroStopsSendingButKeepsReceiveWindow(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 3, 1)
	if _, err := section.SendData([]byte{0xaa}, false); err != nil {
		t.Fatalf("SendData() error = %v", err)
	}

	if err := section.HandleAcknowledgement(sccp.NewAK(0x010203, 0x02, 0)); err != nil {
		t.Fatalf("HandleAcknowledgement() error = %v", err)
	}
	if _, err := section.SendData([]byte{0xbb}, false); !errors.Is(err, sccp.ErrFlowControlBlocked) {
		t.Fatalf("SendData() after zero-credit AK error = %v, want %v", err, sccp.ErrFlowControlBlocked)
	}

	indication, err := section.HandleDataForm2(sccp.NewDT2(0x010203, 0x00, 0x02, false, []byte{0xcc}))
	if err != nil {
		t.Fatalf("HandleDataForm2() error = %v", err)
	}
	if got, want := string(indication.Data), string([]byte{0xcc}); got != want {
		t.Fatalf("Data = %x, want %x", indication.Data, []byte{0xcc})
	}
}

func TestConnectionSectionRejectsAcknowledgementCreditDifferentFromInitial(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 3, 2)

	err := section.HandleAcknowledgement(sccp.NewAK(0x010203, 0x00, 1))
	if !errors.Is(err, sccp.ErrInvalidCredit) {
		t.Fatalf("HandleAcknowledgement() error = %v, want %v", err, sccp.ErrInvalidCredit)
	}
	if got, want := section.Credit, uint8(2); got != want {
		t.Fatalf("Credit = %d, want %d", got, want)
	}
}

func TestConnectionSectionAcceptsDT2AndReturnsAKAtWindowEdge(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 3, 1)

	indication, err := section.HandleDataForm2(sccp.NewDT2(0x010203, 0x00, 0x00, false, []byte{0xaa}))
	if err != nil {
		t.Fatalf("HandleDataForm2() error = %v", err)
	}
	if got, want := string(indication.Data), string([]byte{0xaa}); got != want {
		t.Fatalf("Data = %x, want %x", indication.Data, []byte{0xaa})
	}
	if indication.Acknowledgement == nil {
		t.Fatal("Acknowledgement = nil, want AK")
	}
	if got, want := indication.Acknowledgement.DestinationLocalReference.Uint32(), uint32(0x040506); got != want {
		t.Fatalf("AK destination reference = %#06x, want %#06x", got, want)
	}
	if got, want := indication.Acknowledgement.ReceiveSequenceNumber.Value(), uint8(0x02); got != want {
		t.Fatalf("AK P(R) = %d, want %d", got, want)
	}
	if got, want := section.ReceiveSequenceNumber, uint8(1); got != want {
		t.Fatalf("ReceiveSequenceNumber = %d, want %d", got, want)
	}
}

func TestConnectionSectionRejectsUnexpectedDT2Sequence(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 3, 2)

	_, err := section.HandleDataForm2(sccp.NewDT2(0x010203, 0x02, 0x00, false, []byte{0xaa}))
	if !errors.Is(err, sccp.ErrSequenceMismatch) {
		t.Fatalf("HandleDataForm2() error = %v, want %v", err, sccp.ErrSequenceMismatch)
	}
}

func TestStackDeliversConnectionDataAndOutboundAK(t *testing.T) {
	stack := sccp.NewStack(sccp.StackConfig{LocalPointCode: 0x0102, InitialLocalReference: 0x010000})
	_, localReference, err := stack.NewConnectionRequest(3, false, 1, calledAddress(0x0102, 6), nil, nil)
	if err != nil {
		t.Fatalf("NewConnectionRequest() error = %v", err)
	}
	if _, err := stack.HandleMessage(sccp.NewCC(localReference, 0x020000, 3, false, params.NewCreditOptional(1))); err != nil {
		t.Fatalf("HandleMessage(CC) error = %v", err)
	}

	result, err := stack.HandleMessage(sccp.NewDT2(localReference, 0x00, 0x00, false, []byte{0xaa}))
	if err != nil {
		t.Fatalf("HandleMessage(DT2) error = %v", err)
	}
	if got := len(result.DataIndications); got != 1 {
		t.Fatalf("DataIndications = %d, want 1", got)
	}
	if got := len(result.Outbound); got != 1 {
		t.Fatalf("Outbound messages = %d, want 1", got)
	}
	if _, ok := result.Outbound[0].(*sccp.AK); !ok {
		t.Fatalf("Outbound[0] = %T, want *sccp.AK", result.Outbound[0])
	}
}
