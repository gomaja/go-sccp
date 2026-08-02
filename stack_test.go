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

func TestStackDeliversUDTToRegisteredSubsystem(t *testing.T) {
	stack := sccp.NewStack(sccp.StackConfig{LocalPointCode: 0x0102})
	stack.RegisterSubsystem(6)
	msg := sccp.NewUDT(0, false, calledAddress(0x0102, 6), callingAddress(0x0203, 7), []byte{0xaa, 0xbb})

	result, err := stack.HandleMessage(msg)
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}
	if got := len(result.Outbound); got != 0 {
		t.Fatalf("Outbound messages = %d, want 0", got)
	}
	if got := len(result.Deliveries); got != 1 {
		t.Fatalf("Deliveries = %d, want 1", got)
	}
	delivery := result.Deliveries[0]
	if got, want := delivery.ProtocolClass, 0; got != want {
		t.Fatalf("ProtocolClass = %d, want %d", got, want)
	}
	if delivery.ReturnOnError {
		t.Fatal("ReturnOnError = true, want false")
	}
	if got, want := delivery.CalledPartyAddress.SubsystemNumber, uint8(6); got != want {
		t.Fatalf("called SSN = %d, want %d", got, want)
	}
	if got, want := delivery.CallingPartyAddress.SubsystemNumber, uint8(7); got != want {
		t.Fatalf("calling SSN = %d, want %d", got, want)
	}
	if got, want := string(delivery.Data), string([]byte{0xaa, 0xbb}); got != want {
		t.Fatalf("Data = %x, want %x", delivery.Data, []byte{0xaa, 0xbb})
	}
}

func TestStackReturnsUDTSForUnavailableSubsystemWhenReturnOnErrorRequested(t *testing.T) {
	stack := sccp.NewStack(sccp.StackConfig{LocalPointCode: 0x0102})
	msg := sccp.NewUDT(0, true, calledAddress(0x0102, 6), callingAddress(0x0203, 7), []byte{0xaa, 0xbb})

	result, err := stack.HandleMessage(msg)
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}
	if got := len(result.Deliveries); got != 0 {
		t.Fatalf("Deliveries = %d, want 0", got)
	}
	if got := len(result.Outbound); got != 1 {
		t.Fatalf("Outbound messages = %d, want 1", got)
	}
	returned, ok := result.Outbound[0].(*sccp.UDTS)
	if !ok {
		t.Fatalf("Outbound[0] = %T, want *sccp.UDTS", result.Outbound[0])
	}
	if got, want := returned.ReturnCause.Value(), params.ReturnCauseSubsystemFailure; got != want {
		t.Fatalf("ReturnCause = %v, want %v", got, want)
	}
	if got, want := returned.CalledPartyAddress.SubsystemNumber, uint8(7); got != want {
		t.Fatalf("returned called SSN = %d, want original calling SSN %d", got, want)
	}
	if got, want := returned.CallingPartyAddress.SubsystemNumber, uint8(6); got != want {
		t.Fatalf("returned calling SSN = %d, want original called SSN %d", got, want)
	}
	if got, want := string(returned.Data.Value()), string([]byte{0xaa, 0xbb}); got != want {
		t.Fatalf("returned Data = %x, want %x", returned.Data.Value(), []byte{0xaa, 0xbb})
	}
}

func TestStackCannotReturnUDTWithoutCallingPartyAddress(t *testing.T) {
	stack := sccp.NewStack(sccp.StackConfig{LocalPointCode: 0x0102})
	msg := sccp.NewUDT(0, true, calledAddress(0x0102, 6), callingAddress(0x0203, 7), []byte{0xaa})
	msg.CallingPartyAddress = nil

	_, err := stack.HandleMessage(msg)
	if !errors.Is(err, sccp.ErrInvalidStackMessage) {
		t.Fatalf("HandleMessage() error = %v, want %v", err, sccp.ErrInvalidStackMessage)
	}
}

func TestStackDiscardsUnavailableUDTWhenReturnOnErrorNotRequested(t *testing.T) {
	stack := sccp.NewStack(sccp.StackConfig{LocalPointCode: 0x0102})
	msg := sccp.NewUDT(0, false, calledAddress(0x0102, 6), callingAddress(0x0203, 7), []byte{0xaa, 0xbb})

	result, err := stack.HandleMessage(msg)
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}
	if got := len(result.Deliveries); got != 0 {
		t.Fatalf("Deliveries = %d, want 0", got)
	}
	if got := len(result.Outbound); got != 0 {
		t.Fatalf("Outbound messages = %d, want 0", got)
	}
}

func TestStackInvokesNoticeForServiceMessage(t *testing.T) {
	stack := sccp.NewStack(sccp.StackConfig{LocalPointCode: 0x0102})
	msg := sccp.NewUDTS(
		params.ReturnCauseSubsystemFailure,
		calledAddress(0x0102, 6),
		callingAddress(0x0203, 7),
		[]byte{0xaa, 0xbb},
	)

	result, err := stack.HandleMessage(msg)
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}
	if got := len(result.Notices); got != 1 {
		t.Fatalf("Notices = %d, want 1", got)
	}
	notice := result.Notices[0]
	if got, want := notice.ReturnCause, params.ReturnCauseSubsystemFailure; got != want {
		t.Fatalf("ReturnCause = %v, want %v", got, want)
	}
	if got, want := string(notice.Data), string([]byte{0xaa, 0xbb}); got != want {
		t.Fatalf("Notice Data = %x, want %x", notice.Data, []byte{0xaa, 0xbb})
	}
}

func TestStackReturnsXUDTSOnHopCounterViolation(t *testing.T) {
	stack := sccp.NewStack(sccp.StackConfig{LocalPointCode: 0x0102})
	stack.RegisterSubsystem(6)
	msg := sccp.NewXUDT(0, true, 0, calledAddress(0x0102, 6), callingAddress(0x0203, 7), []byte{0xaa})

	result, err := stack.HandleMessage(msg)
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}
	if got := len(result.Deliveries); got != 0 {
		t.Fatalf("Deliveries = %d, want 0", got)
	}
	if got := len(result.Outbound); got != 1 {
		t.Fatalf("Outbound messages = %d, want 1", got)
	}
	returned, ok := result.Outbound[0].(*sccp.XUDTS)
	if !ok {
		t.Fatalf("Outbound[0] = %T, want *sccp.XUDTS", result.Outbound[0])
	}
	if got, want := returned.ReturnCause.Value(), params.ReturnCauseHopCounterViolation; got != want {
		t.Fatalf("ReturnCause = %v, want %v", got, want)
	}
}

func TestStackConnectionLifecycle(t *testing.T) {
	stack := sccp.NewStack(sccp.StackConfig{LocalPointCode: 0x0102, InitialLocalReference: 0x010000})

	request, localReference, err := stack.NewConnectionRequest(
		3,
		false,
		7,
		calledAddress(0x0102, 6),
		callingAddress(0x0203, 7),
		[]byte{0xaa},
	)
	if err != nil {
		t.Fatalf("NewConnectionRequest() error = %v", err)
	}
	if got, want := localReference, uint32(0x010000); got != want {
		t.Fatalf("local reference = %#06x, want %#06x", got, want)
	}
	if got, want := request.SourceLocalReference.Uint32(), uint32(0x010000); got != want {
		t.Fatalf("CR source reference = %#06x, want %#06x", got, want)
	}

	if _, err := stack.HandleMessage(sccp.NewCC(localReference, 0x020000, 2, false)); err != nil {
		t.Fatalf("HandleMessage(CC) error = %v", err)
	}
	section, ok := stack.Connection(localReference)
	if !ok {
		t.Fatal("Connection() ok = false, want true")
	}
	if got, want := section.State, sccp.ConnectionStateEstablished; got != want {
		t.Fatalf("State = %v, want %v", got, want)
	}
	if got, want := section.RemoteReference, uint32(0x020000); got != want {
		t.Fatalf("RemoteReference = %#06x, want %#06x", got, want)
	}
	if got, want := section.ProtocolClass, 2; got != want {
		t.Fatalf("ProtocolClass = %d, want %d", got, want)
	}

	released, err := stack.ReleaseConnection(localReference, params.ReleaseCauseSCCPUserOriginated)
	if err != nil {
		t.Fatalf("ReleaseConnection() error = %v", err)
	}
	if got, want := released.DestinationLocalReference.Uint32(), uint32(0x020000); got != want {
		t.Fatalf("RLSD destination reference = %#06x, want %#06x", got, want)
	}
	if _, err := stack.HandleMessage(sccp.NewRLC(localReference, 0x020000)); err != nil {
		t.Fatalf("HandleMessage(RLC) error = %v", err)
	}
	section, ok = stack.Connection(localReference)
	if !ok {
		t.Fatal("Connection() after RLC ok = false, want true")
	}
	if got, want := section.State, sccp.ConnectionStateFrozen; got != want {
		t.Fatalf("State after RLC = %v, want %v", got, want)
	}
}

func TestStackConnectionRequestRequiresCalledPartyAddress(t *testing.T) {
	stack := sccp.NewStack(sccp.StackConfig{LocalPointCode: 0x0102})

	_, _, err := stack.NewConnectionRequest(2, false, 0, nil, nil, nil)
	if !errors.Is(err, sccp.ErrInvalidStackMessage) {
		t.Fatalf("NewConnectionRequest() error = %v, want %v", err, sccp.ErrInvalidStackMessage)
	}
}

func TestStackRepliesToIncomingReleased(t *testing.T) {
	stack := sccp.NewStack(sccp.StackConfig{LocalPointCode: 0x0102, InitialLocalReference: 0x010000})
	_, localReference, err := stack.NewConnectionRequest(2, false, 0, calledAddress(0x0102, 6), nil, nil)
	if err != nil {
		t.Fatalf("NewConnectionRequest() error = %v", err)
	}
	if _, err := stack.HandleMessage(sccp.NewCC(localReference, 0x020000, 2, false)); err != nil {
		t.Fatalf("HandleMessage(CC) error = %v", err)
	}

	result, err := stack.HandleMessage(sccp.NewRLSD(localReference, 0x020000, params.ReleaseCauseSCCPUserOriginated))
	if err != nil {
		t.Fatalf("HandleMessage(RLSD) error = %v", err)
	}
	if got := len(result.Outbound); got != 1 {
		t.Fatalf("Outbound messages = %d, want 1", got)
	}
	complete, ok := result.Outbound[0].(*sccp.RLC)
	if !ok {
		t.Fatalf("Outbound[0] = %T, want *sccp.RLC", result.Outbound[0])
	}
	if got, want := complete.DestinationLocalReference.Uint32(), uint32(0x020000); got != want {
		t.Fatalf("RLC destination reference = %#06x, want %#06x", got, want)
	}
	if got, want := complete.SourceLocalReference.Uint32(), uint32(0x010000); got != want {
		t.Fatalf("RLC source reference = %#06x, want %#06x", got, want)
	}
}

func calledAddress(pointCode uint16, ssn uint8) *params.PartyAddress {
	return params.NewCalledPartyAddress(routeOnSSNIndicator(), pointCode, ssn, nil)
}

func callingAddress(pointCode uint16, ssn uint8) *params.PartyAddress {
	return params.NewCallingPartyAddress(routeOnSSNIndicator(), pointCode, ssn, nil)
}

func routeOnSSNIndicator() uint8 {
	return params.NewAddressIndicator(true, true, true, params.GTINoGT)
}
