// Copyright 2019-2024 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"errors"
	"fmt"
	"sync"

	"github.com/gomaja/go-sccp/params"
)

var (
	// ErrConnectionNotFound reports a connection-oriented message for an unknown local reference.
	ErrConnectionNotFound = errors.New("sccp: connection not found")

	// ErrLocalReferenceExhausted reports that no non-frozen local reference can be allocated.
	ErrLocalReferenceExhausted = errors.New("sccp: local reference exhausted")

	// ErrInvalidStackMessage reports a message missing fields required by SCCP stack procedures.
	ErrInvalidStackMessage = errors.New("sccp: invalid stack message")
)

// StackConfig configures an in-memory SCCP stack.
type StackConfig struct {
	LocalPointCode        uint16
	InitialLocalReference uint32
}

// Stack owns local subsystem availability and connection-section state.
type Stack struct {
	mu                 sync.RWMutex
	localPointCode     uint16
	nextLocalReference uint32
	subsystems         map[uint8]struct{}
	connections        map[uint32]*ConnectionSection
}

// StackResult contains local delivery, notice, and outbound messages produced by HandleMessage.
type StackResult struct {
	Deliveries      []UnitdataIndication
	DataIndications []DataIndication
	Notices         []NoticeIndication
	Outbound        []Message
}

// UnitdataIndication represents an N-UNITDATA indication delivered to a local SCCP user.
type UnitdataIndication struct {
	Message             Message
	ProtocolClass       int
	ReturnOnError       bool
	CalledPartyAddress  *params.PartyAddress
	CallingPartyAddress *params.PartyAddress
	Data                []byte
}

// NoticeIndication represents an N-NOTICE indication for a received service message.
type NoticeIndication struct {
	Message             Message
	ReturnCause         params.ReturnCauseValue
	CalledPartyAddress  *params.PartyAddress
	CallingPartyAddress *params.PartyAddress
	Data                []byte
}

// NewStack creates an SCCP stack with local subsystem and connection-section state.
func NewStack(config StackConfig) *Stack {
	initialReference := config.InitialLocalReference
	if initialReference == 0 {
		initialReference = 1
	}

	return &Stack{
		localPointCode:     config.LocalPointCode,
		nextLocalReference: initialReference & 0x00ffffff,
		subsystems:         make(map[uint8]struct{}),
		connections:        make(map[uint32]*ConnectionSection),
	}
}

// RegisterSubsystem marks a local subsystem as available for connectionless delivery.
func (s *Stack) RegisterSubsystem(ssn uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.subsystems[ssn] = struct{}{}
}

// UnregisterSubsystem marks a local subsystem as unavailable.
func (s *Stack) UnregisterSubsystem(ssn uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.subsystems, ssn)
}

// SubsystemAvailable reports whether a local subsystem is registered.
func (s *Stack) SubsystemAvailable(ssn uint8) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.subsystems[ssn]
	return ok
}

// HandleMessage applies SCCP stack procedures to an already decoded message.
func (s *Stack) HandleMessage(message Message) (StackResult, error) {
	switch msg := message.(type) {
	case *UDT:
		return s.handleUnitdata(msg, msg.ProtocolClass, nil, msg.CalledPartyAddress, msg.CallingPartyAddress, msg.Data.Value())
	case *XUDT:
		return s.handleUnitdata(msg, msg.ProtocolClass, msg.HopCounter, msg.CalledPartyAddress, msg.CallingPartyAddress, msg.Data.Value())
	case *LUDT:
		return s.handleUnitdata(msg, msg.ProtocolClass, msg.HopCounter, msg.CalledPartyAddress, msg.CallingPartyAddress, msg.LongData.Value())
	case *UDTS:
		return serviceNotice(msg, msg.ReturnCause, msg.CalledPartyAddress, msg.CallingPartyAddress, msg.Data.Value()), nil
	case *XUDTS:
		return serviceNotice(msg, msg.ReturnCause, msg.CalledPartyAddress, msg.CallingPartyAddress, msg.Data.Value()), nil
	case *LUDTS:
		return serviceNotice(msg, msg.ReturnCause, msg.CalledPartyAddress, msg.CallingPartyAddress, msg.LongData.Value()), nil
	case *CC:
		return s.handleConnectionConfirm(msg)
	case *CREF:
		return s.handleConnectionRefused(msg)
	case *RLSD:
		return s.handleReleased(msg)
	case *RLC:
		return s.handleReleaseComplete(msg)
	case *DT1:
		return s.handleDataForm1(msg)
	case *DT2:
		return s.handleDataForm2(msg)
	case *AK:
		return s.handleAcknowledgement(msg)
	default:
		return StackResult{}, nil
	}
}

// NewConnectionRequest allocates a local reference, stores a pending section, and returns a CR.
func (s *Stack) NewConnectionRequest(
	protocolClass int,
	returnOnError bool,
	credit uint8,
	calledPartyAddress *params.PartyAddress,
	callingPartyAddress *params.PartyAddress,
	data []byte,
) (*CR, uint32, error) {
	if calledPartyAddress == nil {
		return nil, 0, fmt.Errorf("%w: missing called party address", ErrInvalidStackMessage)
	}

	localReference, err := s.allocateLocalReference()
	if err != nil {
		return nil, 0, err
	}

	opts := make([]params.Parameter, 0, 3)
	if credit != 0 {
		opts = append(opts, params.NewCreditOptional(credit))
	}
	if callingPartyAddress != nil {
		opts = append(opts, clonePartyAddress(callingPartyAddress, params.PCodeCallingPartyAddress))
	}
	if data != nil {
		opts = append(opts, params.NewDataOptional(cloneBytes(data)))
	}

	request := NewCR(localReference, protocolClass, returnOnError, calledPartyAddress, opts...)

	s.mu.Lock()
	s.connections[localReference] = NewPendingConnectionSection(localReference, protocolClass, credit)
	s.mu.Unlock()

	return request, localReference, nil
}

// Connection returns a copy of the connection section for the local reference.
func (s *Stack) Connection(localReference uint32) (ConnectionSection, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	section, ok := s.connections[localReference]
	if !ok {
		return ConnectionSection{}, false
	}
	return *section, true
}

// ReleaseConnection starts release for an established connection section.
func (s *Stack) ReleaseConnection(localReference uint32, cause params.ReleaseCauseValue, opts ...params.Parameter) (*RLSD, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	section, ok := s.connections[localReference]
	if !ok {
		return nil, fmt.Errorf("%w: local reference %#06x", ErrConnectionNotFound, localReference)
	}
	return section.Release(cause, opts...)
}

func (s *Stack) handleUnitdata(
	message Message,
	protocolClass *params.ProtocolClass,
	hopCounter *params.HopCounter,
	calledPartyAddress *params.PartyAddress,
	callingPartyAddress *params.PartyAddress,
	data []byte,
) (StackResult, error) {
	// ITU-T Q.714 (05/01), section 4.2 returns UDT/XUDT/LUDT only when
	// return-message-on-error is requested; otherwise the message is discarded.
	if hopCounter != nil && hopCounter.Value() == 0 {
		return s.returnConnectionless(message, params.ReturnCauseHopCounterViolation, protocolClass, hopCounter, calledPartyAddress, callingPartyAddress, data)
	}

	if calledPartyAddress == nil || !calledPartyAddress.HasSSN() {
		return s.returnConnectionless(message, params.ReturnCauseNoTranslationForThisSpecificAddress, protocolClass, hopCounter, calledPartyAddress, callingPartyAddress, data)
	}

	s.mu.RLock()
	_, ok := s.subsystems[calledPartyAddress.SubsystemNumber]
	s.mu.RUnlock()
	if !ok || !s.isLocalAddress(calledPartyAddress) {
		return s.returnConnectionless(message, params.ReturnCauseSubsystemFailure, protocolClass, hopCounter, calledPartyAddress, callingPartyAddress, data)
	}

	return StackResult{
		Deliveries: []UnitdataIndication{
			{
				Message:             message,
				ProtocolClass:       protocolClass.Class(),
				ReturnOnError:       protocolClass.ReturnOnError(),
				CalledPartyAddress:  clonePartyAddress(calledPartyAddress, params.PCodeCalledPartyAddress),
				CallingPartyAddress: clonePartyAddress(callingPartyAddress, params.PCodeCallingPartyAddress),
				Data:                cloneBytes(data),
			},
		},
	}, nil
}

func (s *Stack) returnConnectionless(
	message Message,
	cause params.ReturnCauseValue,
	protocolClass *params.ProtocolClass,
	hopCounter *params.HopCounter,
	calledPartyAddress *params.PartyAddress,
	callingPartyAddress *params.PartyAddress,
	data []byte,
) (StackResult, error) {
	if protocolClass == nil || !protocolClass.ReturnOnError() {
		return StackResult{}, nil
	}
	if calledPartyAddress == nil {
		return StackResult{}, fmt.Errorf("%w: missing called party address", ErrInvalidStackMessage)
	}
	if callingPartyAddress == nil {
		return StackResult{}, fmt.Errorf("%w: missing calling party address", ErrInvalidStackMessage)
	}

	var response Message
	switch message.(type) {
	case *UDT:
		response = NewUDTS(
			cause,
			clonePartyAddress(callingPartyAddress, params.PCodeCalledPartyAddress),
			clonePartyAddress(calledPartyAddress, params.PCodeCallingPartyAddress),
			cloneBytes(data),
		)
	case *XUDT:
		response = NewXUDTS(
			cause,
			nextHopCounter(hopCounter),
			clonePartyAddress(callingPartyAddress, params.PCodeCalledPartyAddress),
			clonePartyAddress(calledPartyAddress, params.PCodeCallingPartyAddress),
			cloneBytes(data),
		)
	case *LUDT:
		response = NewLUDTS(
			cause,
			nextHopCounter(hopCounter),
			clonePartyAddress(callingPartyAddress, params.PCodeCalledPartyAddress),
			clonePartyAddress(calledPartyAddress, params.PCodeCallingPartyAddress),
			cloneBytes(data),
		)
	default:
		return StackResult{}, nil
	}
	return StackResult{Outbound: []Message{response}}, nil
}

func (s *Stack) handleConnectionConfirm(confirm *CC) (StackResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	section, err := s.connectionForDestinationLocked(confirm.DestinationLocalReference)
	if err != nil {
		return StackResult{}, err
	}
	return StackResult{}, section.HandleConnectionConfirm(confirm)
}

func (s *Stack) handleConnectionRefused(refusal *CREF) (StackResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	section, err := s.connectionForDestinationLocked(refusal.DestinationLocalReference)
	if err != nil {
		return StackResult{}, err
	}
	return StackResult{}, section.HandleConnectionRefused(refusal)
}

func (s *Stack) handleReleased(released *RLSD) (StackResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	section, err := s.connectionForDestinationLocked(released.DestinationLocalReference)
	if err != nil {
		return StackResult{}, err
	}
	complete, err := section.HandleReleased(released)
	if err != nil {
		return StackResult{}, err
	}
	return StackResult{Outbound: []Message{complete}}, nil
}

func (s *Stack) handleReleaseComplete(complete *RLC) (StackResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	section, err := s.connectionForDestinationLocked(complete.DestinationLocalReference)
	if err != nil {
		return StackResult{}, err
	}
	return StackResult{}, section.HandleReleaseComplete(complete)
}

func (s *Stack) handleDataForm1(data *DT1) (StackResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if data == nil {
		return StackResult{}, fmt.Errorf("%w: nil DT1", ErrReferenceMismatch)
	}
	section, err := s.connectionForDestinationLocked(data.DestinationLocalReference)
	if err != nil {
		return StackResult{}, err
	}
	indication, err := section.HandleDataForm1(data)
	if err != nil {
		return StackResult{}, err
	}
	return StackResult{DataIndications: []DataIndication{indication}}, nil
}

func (s *Stack) handleDataForm2(data *DT2) (StackResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if data == nil {
		return StackResult{}, fmt.Errorf("%w: nil DT2", ErrReferenceMismatch)
	}
	section, err := s.connectionForDestinationLocked(data.DestinationLocalReference)
	if err != nil {
		return StackResult{}, err
	}
	indication, err := section.HandleDataForm2(data)
	if err != nil {
		return StackResult{}, err
	}

	result := StackResult{DataIndications: []DataIndication{indication}}
	if indication.Acknowledgement != nil {
		result.Outbound = []Message{indication.Acknowledgement}
	}
	return result, nil
}

func (s *Stack) handleAcknowledgement(acknowledgement *AK) (StackResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if acknowledgement == nil {
		return StackResult{}, fmt.Errorf("%w: nil AK", ErrReferenceMismatch)
	}
	section, err := s.connectionForDestinationLocked(acknowledgement.DestinationLocalReference)
	if err != nil {
		return StackResult{}, err
	}
	return StackResult{}, section.HandleAcknowledgement(acknowledgement)
}

func (s *Stack) connectionForDestinationLocked(ref *params.LocalReference) (*ConnectionSection, error) {
	if ref == nil {
		return nil, fmt.Errorf("%w: missing destination local reference", ErrReferenceMismatch)
	}

	localReference := ref.Uint32()
	section, ok := s.connections[localReference]
	if !ok {
		return nil, fmt.Errorf("%w: local reference %#06x", ErrConnectionNotFound, localReference)
	}
	return section, nil
}

func (s *Stack) allocateLocalReference() (uint32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := s.nextLocalReference
	for {
		reference := s.nextLocalReference & 0x00ffffff
		if reference == 0 {
			reference = 1
		}
		s.nextLocalReference = (reference + 1) & 0x00ffffff
		if _, exists := s.connections[reference]; !exists {
			return reference, nil
		}
		if s.nextLocalReference == start {
			return 0, ErrLocalReferenceExhausted
		}
	}
}

func (s *Stack) isLocalAddress(address *params.PartyAddress) bool {
	if address == nil {
		return false
	}
	if !address.HasPC() {
		return true
	}
	return address.SignalingPointCode == s.localPointCode
}

func serviceNotice(
	message Message,
	cause *params.ReturnCause,
	calledPartyAddress *params.PartyAddress,
	callingPartyAddress *params.PartyAddress,
	data []byte,
) StackResult {
	returnCause := params.ReturnCauseUnqualified
	if cause != nil {
		returnCause = cause.Value()
	}
	return StackResult{
		Notices: []NoticeIndication{
			{
				Message:             message,
				ReturnCause:         returnCause,
				CalledPartyAddress:  clonePartyAddress(calledPartyAddress, params.PCodeCalledPartyAddress),
				CallingPartyAddress: clonePartyAddress(callingPartyAddress, params.PCodeCallingPartyAddress),
				Data:                cloneBytes(data),
			},
		},
	}
}

func nextHopCounter(hopCounter *params.HopCounter) uint8 {
	if hopCounter == nil {
		return 0
	}
	return hopCounter.Value()
}

func clonePartyAddress(address *params.PartyAddress, code params.ParameterNameCode) *params.PartyAddress {
	if address == nil {
		return nil
	}
	return params.NewPartyAddress(
		code,
		address.Indicator,
		address.SignalingPointCode,
		address.SubsystemNumber,
		cloneGlobalTitle(address.GlobalTitle),
	)
}

func cloneGlobalTitle(globalTitle *params.GlobalTitle) *params.GlobalTitle {
	if globalTitle == nil {
		return nil
	}
	return params.NewGlobalTitle(
		globalTitle.GTI,
		globalTitle.TranslationType,
		globalTitle.NumberingPlan,
		globalTitle.EncodingScheme,
		globalTitle.NatureOfAddressIndicator,
		cloneBytes(globalTitle.AddressInformation),
	)
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte(nil), b...)
}
