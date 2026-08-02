// Copyright 2019-2024 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"errors"
	"fmt"

	"github.com/gomaja/go-sccp/params"
)

var (
	// ErrInvalidConnectionState reports a Q.714 procedure invoked in the wrong state.
	ErrInvalidConnectionState = errors.New("sccp: invalid connection state")

	// ErrReferenceMismatch reports a message that does not address this connection section.
	ErrReferenceMismatch = errors.New("sccp: connection reference mismatch")

	// ErrFlowControlBlocked reports that class 3 flow control does not authorize more DT2 transfer.
	ErrFlowControlBlocked = errors.New("sccp: flow control blocked")

	// ErrSequenceMismatch reports a class 3 sequence number outside the valid send or receive window.
	ErrSequenceMismatch = errors.New("sccp: sequence mismatch")

	// ErrInvalidCredit reports a class 3 credit outside the Q.714 window-size range.
	ErrInvalidCredit = errors.New("sccp: invalid credit")
)

const (
	sccpSequenceModulus = 128
	maxConnectionCredit = 127
)

// ConnectionState identifies the local procedure state of a connection section.
type ConnectionState uint8

// Connection section states used by the Q.714 procedure helpers.
const (
	ConnectionStateIdle ConnectionState = iota
	ConnectionStatePending
	ConnectionStateEstablished
	ConnectionStateReleasePending
	ConnectionStateFrozen
)

// String returns the connection state name.
func (s ConnectionState) String() string {
	switch s {
	case ConnectionStateIdle:
		return "idle"
	case ConnectionStatePending:
		return "pending"
	case ConnectionStateEstablished:
		return "established"
	case ConnectionStateReleasePending:
		return "release pending"
	case ConnectionStateFrozen:
		return "frozen"
	default:
		return fmt.Sprintf("ConnectionState(%d)", s)
	}
}

// ConnectionSection stores the local state for one SCCP connection section.
//
// ITU-T Q.714 (05/01), section 3.1.2 assigns local reference numbers
// independently at each end and makes the destination reference mandatory once
// known. Section 3.3.2 requires released local references to remain frozen
// before reuse; this helper keeps the reference visible while State is frozen.
type ConnectionSection struct {
	LocalReference         uint32
	RemoteReference        uint32
	ProtocolClass          int
	Credit                 uint8
	SendCredit             uint8
	State                  ConnectionState
	SendSequenceNumber     uint8
	ReceiveSequenceNumber  uint8
	SendWindowLowerEdge    uint8
	ReceiveWindowLowerEdge uint8
	receivedSinceAck       uint8
}

// DataIndication represents an N-DATA indication delivered to a local SCCP user.
type DataIndication struct {
	Message         Message
	LocalReference  uint32
	RemoteReference uint32
	ProtocolClass   int
	MoreData        bool
	Data            []byte
	Acknowledgement *AK
}

// NewPendingConnectionSection creates an originating section after CR transfer.
//
// ITU-T Q.714 (05/01), section 3.1.4.1 assigns the source local reference,
// proposed protocol class, and initial credit before starting T(conn est).
func NewPendingConnectionSection(localReference uint32, protocolClass int, credit uint8) *ConnectionSection {
	return &ConnectionSection{
		LocalReference: localReference,
		ProtocolClass:  protocolClass,
		Credit:         credit,
		SendCredit:     credit,
		State:          ConnectionStatePending,
	}
}

// NewEstablishedConnectionSection creates a section whose two local references are known.
func NewEstablishedConnectionSection(localReference, remoteReference uint32, protocolClass int, credit uint8) *ConnectionSection {
	return &ConnectionSection{
		LocalReference:  localReference,
		RemoteReference: remoteReference,
		ProtocolClass:   protocolClass,
		Credit:          credit,
		SendCredit:      credit,
		State:           ConnectionStateEstablished,
	}
}

// HandleConnectionConfirm applies a CC to a pending originating section.
//
// ITU-T Q.714 (05/01), section 3.1.4.2 associates the received local reference
// with the section, updates the assigned protocol class and credit, and
// establishes the connection section.
func (s *ConnectionSection) HandleConnectionConfirm(confirm *CC) error {
	if err := s.requireState(ConnectionStatePending, "connection confirm"); err != nil {
		return err
	}
	if confirm == nil {
		return fmt.Errorf("%w: nil CC", ErrReferenceMismatch)
	}
	if err := s.matchDestinationReference(confirm.DestinationLocalReference); err != nil {
		return err
	}
	remoteReference, err := sourceReference(confirm.SourceLocalReference)
	if err != nil {
		return err
	}

	s.RemoteReference = remoteReference
	if confirm.ProtocolClass != nil {
		s.ProtocolClass = confirm.ProtocolClass.Class()
	}
	if confirm.Credit != nil {
		s.Credit = confirm.Credit.Value()
	}
	s.SendCredit = s.Credit
	s.State = ConnectionStateEstablished
	return nil
}

// HandleConnectionRefused applies a CREF to a pending originating section.
//
// ITU-T Q.714 (05/01), sections 3.1.4.2 and 3.2.3 release the resources for
// the failed setup and freeze the local reference.
func (s *ConnectionSection) HandleConnectionRefused(refusal *CREF) error {
	if err := s.requireState(ConnectionStatePending, "connection refused"); err != nil {
		return err
	}
	if refusal == nil {
		return fmt.Errorf("%w: nil CREF", ErrReferenceMismatch)
	}
	if err := s.matchDestinationReference(refusal.DestinationLocalReference); err != nil {
		return err
	}

	s.freeze()
	return nil
}

// Release starts end-node connection release and returns the RLSD to transfer.
//
// ITU-T Q.714 (05/01), section 3.3.3.1 initiates release by sending RLSD and
// starting T(rel). Timer ownership is intentionally left to the caller.
func (s *ConnectionSection) Release(cause params.ReleaseCauseValue, opts ...params.Parameter) (*RLSD, error) {
	if err := s.requireState(ConnectionStateEstablished, "release"); err != nil {
		return nil, err
	}

	s.State = ConnectionStateReleasePending
	return NewRLSD(s.RemoteReference, s.LocalReference, cause, opts...), nil
}

// HandleReleaseComplete applies an RLC received after local release initiation.
//
// ITU-T Q.714 (05/01), section 3.3.3.2 completes release by freeing resources,
// stopping T(rel), and freezing the local reference.
func (s *ConnectionSection) HandleReleaseComplete(complete *RLC) error {
	if err := s.requireState(ConnectionStateReleasePending, "release complete"); err != nil {
		return err
	}
	if complete == nil {
		return fmt.Errorf("%w: nil RLC", ErrReferenceMismatch)
	}
	if err := s.matchDestinationReference(complete.DestinationLocalReference); err != nil {
		return err
	}
	if err := s.matchSourceReference(complete.SourceLocalReference); err != nil {
		return err
	}

	s.freeze()
	return nil
}

// HandleReleased applies an incoming RLSD and returns the RLC response.
//
// ITU-T Q.714 (05/01), sections 3.3.3.2 and 3.3.5 require release completion
// and reference freezing when RLSD is received. Section 3.8.2.2 specifies RLC
// with reversed local reference numbers for an RLSD containing both references.
func (s *ConnectionSection) HandleReleased(released *RLSD) (*RLC, error) {
	if err := s.requireOneOfStates("released", ConnectionStateEstablished, ConnectionStateReleasePending); err != nil {
		return nil, err
	}
	if released == nil {
		return nil, fmt.Errorf("%w: nil RLSD", ErrReferenceMismatch)
	}
	if err := s.matchDestinationReference(released.DestinationLocalReference); err != nil {
		return nil, err
	}
	if err := s.matchSourceReference(released.SourceLocalReference); err != nil {
		return nil, err
	}

	complete := NewRLC(
		released.SourceLocalReference.Uint32(),
		released.DestinationLocalReference.Uint32(),
	)
	s.freeze()
	return complete, nil
}

// SendData applies Q.714 data-transfer procedures and returns the DT message to transfer.
//
// ITU-T Q.714 (05/01), section 3.5.1 uses DT1 for basic class 2 data transfer.
// Sections 3.5.2.1 through 3.5.2.4 apply modulo-128 DT2 sequence numbering
// and per-direction flow-control windows to class 3 only.
func (s *ConnectionSection) SendData(data []byte, moreData bool) (Message, error) {
	if err := s.requireState(ConnectionStateEstablished, "data transfer"); err != nil {
		return nil, err
	}

	switch s.ProtocolClass {
	case 2:
		return NewDT1(s.RemoteReference, moreData, cloneBytes(data)), nil
	case 3:
		if err := validateCredit(s.Credit); err != nil {
			return nil, err
		}
		if s.SendCredit == 0 {
			return nil, ErrFlowControlBlocked
		}
		if s.SendCredit > maxConnectionCredit {
			return nil, fmt.Errorf("%w: send credit %d exceeds %d", ErrInvalidCredit, s.SendCredit, maxConnectionCredit)
		}
		if !sequenceWithinWindow(s.SendWindowLowerEdge, s.SendSequenceNumber, s.SendCredit) {
			return nil, fmt.Errorf("%w: send sequence %d outside window [%d,%d)",
				ErrFlowControlBlocked,
				s.SendSequenceNumber,
				s.SendWindowLowerEdge,
				int(s.SendWindowLowerEdge)+int(s.SendCredit),
			)
		}

		message := NewDT2(
			s.RemoteReference,
			EncodeSequenceNumber(s.SendSequenceNumber),
			EncodeSequenceNumber(s.ReceiveSequenceNumber),
			moreData,
			cloneBytes(data),
		)
		s.SendSequenceNumber = nextSequenceNumber(s.SendSequenceNumber)
		return message, nil
	default:
		return nil, fmt.Errorf("%w: data transfer requires protocol class 2 or 3, got %d", ErrInvalidConnectionState, s.ProtocolClass)
	}
}

// HandleDataForm1 applies class 2 DT1 receive procedures and returns the local data indication.
//
// ITU-T Q.714 (05/01), sections 3.5.1.3 and 3.5.3 deliver complete or
// segmented user data to the destination SCCP user according to the M-bit.
func (s *ConnectionSection) HandleDataForm1(data *DT1) (DataIndication, error) {
	if err := s.requireState(ConnectionStateEstablished, "data form 1"); err != nil {
		return DataIndication{}, err
	}
	if s.ProtocolClass != 2 {
		return DataIndication{}, fmt.Errorf("%w: DT1 requires protocol class 2, got %d", ErrInvalidConnectionState, s.ProtocolClass)
	}
	if data == nil {
		return DataIndication{}, fmt.Errorf("%w: nil DT1", ErrReferenceMismatch)
	}
	if err := s.matchDestinationReference(data.DestinationLocalReference); err != nil {
		return DataIndication{}, err
	}
	if data.SegmentingReassembling == nil {
		return DataIndication{}, fmt.Errorf("%w: DT1 missing segmenting/reassembling", ErrInvalidStackMessage)
	}
	payload, err := connectionDataValue("DT1", data.Data)
	if err != nil {
		return DataIndication{}, err
	}

	return DataIndication{
		Message:         data,
		LocalReference:  s.LocalReference,
		RemoteReference: s.RemoteReference,
		ProtocolClass:   s.ProtocolClass,
		MoreData:        data.SegmentingReassembling.MoreData(),
		Data:            payload,
	}, nil
}

// HandleDataForm2 applies class 3 DT2 receive, acknowledgement, and window procedures.
//
// ITU-T Q.714 (05/01), sections 3.5.2.4.1 and 3.5.2.4.3 require DT2 P(S) to
// be the next expected modulo-128 sequence number and P(R) to advance within
// the sender's outstanding window. Section 3.5.2.4.2 requires AK generation at
// the receiving upper window edge when no DT2 is available for piggybacking.
func (s *ConnectionSection) HandleDataForm2(data *DT2) (DataIndication, error) {
	if err := s.requireState(ConnectionStateEstablished, "data form 2"); err != nil {
		return DataIndication{}, err
	}
	if s.ProtocolClass != 3 {
		return DataIndication{}, fmt.Errorf("%w: DT2 requires protocol class 3, got %d", ErrInvalidConnectionState, s.ProtocolClass)
	}
	if err := validateCredit(s.Credit); err != nil {
		return DataIndication{}, err
	}
	if data == nil {
		return DataIndication{}, fmt.Errorf("%w: nil DT2", ErrReferenceMismatch)
	}
	if err := s.matchDestinationReference(data.DestinationLocalReference); err != nil {
		return DataIndication{}, err
	}
	if data.SequencingSegmenting == nil {
		return DataIndication{}, fmt.Errorf("%w: DT2 missing sequencing/segmenting", ErrInvalidStackMessage)
	}
	payload, err := connectionDataValue("DT2", data.Data)
	if err != nil {
		return DataIndication{}, err
	}

	receivedSequence := DecodeSequenceNumber(data.SequencingSegmenting.SendSequenceNumber)
	if receivedSequence != s.ReceiveSequenceNumber {
		return DataIndication{}, fmt.Errorf("%w: received P(S) %d, want %d", ErrSequenceMismatch, receivedSequence, s.ReceiveSequenceNumber)
	}
	if !sequenceWithinWindow(s.ReceiveWindowLowerEdge, receivedSequence, s.Credit) {
		return DataIndication{}, fmt.Errorf("%w: received P(S) %d outside receive window", ErrSequenceMismatch, receivedSequence)
	}
	if err := s.updateSendingWindow(data.SequencingSegmenting.ReceiveSequenceNumber); err != nil {
		return DataIndication{}, err
	}

	s.ReceiveSequenceNumber = nextSequenceNumber(s.ReceiveSequenceNumber)
	s.receivedSinceAck++

	var acknowledgement *AK
	if s.receivedSinceAck >= s.Credit {
		acknowledgement = NewAK(s.RemoteReference, EncodeSequenceNumber(s.ReceiveSequenceNumber), s.Credit)
		s.ReceiveWindowLowerEdge = s.ReceiveSequenceNumber
		s.receivedSinceAck = 0
	}

	return DataIndication{
		Message:         data,
		LocalReference:  s.LocalReference,
		RemoteReference: s.RemoteReference,
		ProtocolClass:   s.ProtocolClass,
		MoreData:        data.SequencingSegmenting.MoreData,
		Data:            payload,
		Acknowledgement: acknowledgement,
	}, nil
}

// HandleAcknowledgement applies class 3 AK receive procedures.
//
// ITU-T Q.714 (05/01), section 3.5.2.4.3 uses P(R) in AK to set the lower
// edge of the sender window, and section 3.5.2.4.2 uses AK credit zero to stop
// DT2 transfer until a later positive-credit AK or reset.
func (s *ConnectionSection) HandleAcknowledgement(acknowledgement *AK) error {
	if err := s.requireState(ConnectionStateEstablished, "acknowledgement"); err != nil {
		return err
	}
	if s.ProtocolClass != 3 {
		return fmt.Errorf("%w: AK requires protocol class 3, got %d", ErrInvalidConnectionState, s.ProtocolClass)
	}
	if acknowledgement == nil {
		return fmt.Errorf("%w: nil AK", ErrReferenceMismatch)
	}
	if err := s.matchDestinationReference(acknowledgement.DestinationLocalReference); err != nil {
		return err
	}
	if acknowledgement.ReceiveSequenceNumber == nil {
		return fmt.Errorf("%w: AK missing receive sequence number", ErrInvalidStackMessage)
	}
	if acknowledgement.Credit == nil {
		return fmt.Errorf("%w: AK missing credit", ErrInvalidStackMessage)
	}
	credit := acknowledgement.Credit.Value()
	if credit > maxConnectionCredit {
		return fmt.Errorf("%w: AK credit %d exceeds %d", ErrInvalidCredit, credit, maxConnectionCredit)
	}
	if credit != 0 && credit != s.Credit {
		return fmt.Errorf("%w: AK credit %d does not match established credit %d", ErrInvalidCredit, credit, s.Credit)
	}
	if err := s.updateSendingWindow(acknowledgement.ReceiveSequenceNumber.Value()); err != nil {
		return err
	}

	s.SendCredit = credit
	return nil
}

// EncodeSequenceNumber returns the Q.713 sequence-number octet for a decoded modulo-128 value.
func EncodeSequenceNumber(n uint8) uint8 {
	return (n & 0b01111111) << 1
}

// DecodeSequenceNumber returns the decoded modulo-128 value from a Q.713 sequence-number octet.
func DecodeSequenceNumber(encoded uint8) uint8 {
	return (encoded & 0b11111110) >> 1
}

func (s *ConnectionSection) freeze() {
	s.State = ConnectionStateFrozen
}

func (s *ConnectionSection) requireState(want ConnectionState, procedure string) error {
	if s == nil {
		return fmt.Errorf("%w: %s requires a connection section", ErrInvalidConnectionState, procedure)
	}
	if s.State != want {
		return fmt.Errorf("%w: %s requires %s, got %s", ErrInvalidConnectionState, procedure, want, s.State)
	}
	return nil
}

func (s *ConnectionSection) requireOneOfStates(procedure string, states ...ConnectionState) error {
	if s == nil {
		return fmt.Errorf("%w: %s requires a connection section", ErrInvalidConnectionState, procedure)
	}
	for _, state := range states {
		if s.State == state {
			return nil
		}
	}
	return fmt.Errorf("%w: %s cannot run in %s", ErrInvalidConnectionState, procedure, s.State)
}

func (s *ConnectionSection) matchDestinationReference(ref *params.LocalReference) error {
	got, err := destinationReference(ref)
	if err != nil {
		return err
	}
	if got != s.LocalReference {
		return fmt.Errorf(
			"%w: destination local reference %#06x does not match local reference %#06x",
			ErrReferenceMismatch,
			got,
			s.LocalReference,
		)
	}
	return nil
}

func (s *ConnectionSection) matchSourceReference(ref *params.LocalReference) error {
	got, err := sourceReference(ref)
	if err != nil {
		return err
	}
	if got != s.RemoteReference {
		return fmt.Errorf(
			"%w: source local reference %#06x does not match remote reference %#06x",
			ErrReferenceMismatch,
			got,
			s.RemoteReference,
		)
	}
	return nil
}

func destinationReference(ref *params.LocalReference) (uint32, error) {
	if ref == nil {
		return 0, fmt.Errorf("%w: missing destination local reference", ErrReferenceMismatch)
	}
	return ref.Uint32(), nil
}

func sourceReference(ref *params.LocalReference) (uint32, error) {
	if ref == nil {
		return 0, fmt.Errorf("%w: missing source local reference", ErrReferenceMismatch)
	}
	return ref.Uint32(), nil
}

func connectionDataValue(procedure string, data *params.Data) ([]byte, error) {
	if data == nil {
		return nil, fmt.Errorf("%w: %s missing data", ErrInvalidStackMessage, procedure)
	}
	return cloneBytes(data.Value()), nil
}

func validateCredit(credit uint8) error {
	if credit == 0 {
		return ErrFlowControlBlocked
	}
	if credit > maxConnectionCredit {
		return fmt.Errorf("%w: credit %d exceeds %d", ErrInvalidCredit, credit, maxConnectionCredit)
	}
	return nil
}

func nextSequenceNumber(n uint8) uint8 {
	return (n + 1) % sccpSequenceModulus
}

func sequenceWithinWindow(lowerEdge, sequence, credit uint8) bool {
	if credit == 0 || credit > maxConnectionCredit {
		return false
	}
	return sequenceDistance(lowerEdge, sequence) < int(credit)
}

func (s *ConnectionSection) updateSendingWindow(encodedReceiveSequence uint8) error {
	receiveSequence := DecodeSequenceNumber(encodedReceiveSequence)
	if sequenceDistance(s.SendWindowLowerEdge, receiveSequence) > sequenceDistance(s.SendWindowLowerEdge, s.SendSequenceNumber) {
		return fmt.Errorf(
			"%w: received P(R) %d outside sender window [%d,%d]",
			ErrSequenceMismatch,
			receiveSequence,
			s.SendWindowLowerEdge,
			s.SendSequenceNumber,
		)
	}
	s.SendWindowLowerEdge = receiveSequence
	return nil
}

func sequenceDistance(lowerEdge, sequence uint8) int {
	lower := lowerEdge & 0b01111111
	seq := sequence & 0b01111111
	if seq >= lower {
		return int(seq - lower)
	}
	return sccpSequenceModulus - int(lower) + int(seq)
}
