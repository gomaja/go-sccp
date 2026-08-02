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
	LocalReference  uint32
	RemoteReference uint32
	ProtocolClass   int
	Credit          uint8
	State           ConnectionState
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
