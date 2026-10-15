// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/gomaja/go-sccp/params"
)

// Defaults of ConnectionConfig. The timers lie within the ranges of ITU-T
// Q.714 (05/01) Annex C.4.
const (
	// DefaultEstablishmentTimeout is T(conn est): 1 to 2 minutes.
	DefaultEstablishmentTimeout = 90 * time.Second
	// DefaultReleaseTimeout is T(rel): 10 to 20 seconds.
	DefaultReleaseTimeout = 15 * time.Second
	// DefaultRepeatReleaseTimeout is T(repeat rel): 10 to 20 seconds.
	DefaultRepeatReleaseTimeout = 15 * time.Second
	// DefaultReleaseGiveUpTimeout is T(int): up to 1 minute.
	DefaultReleaseGiveUpTimeout = 60 * time.Second
	// DefaultInactivitySendTimeout is T(ias): 5 to 10 minutes.
	DefaultInactivitySendTimeout = 7 * time.Minute
	// DefaultInactivityReceiveTimeout is T(iar): 11 to 21 minutes.
	DefaultInactivityReceiveTimeout = 16 * time.Minute
	// DefaultReferenceFreeze keeps a released local reference out of use
	// longer than the peer's T(rel) and T(int) together (§3.3.2 leaves the
	// duration to the implementation).
	DefaultReferenceFreeze = 2 * time.Minute
	// DefaultMaxConnections bounds the connection sections in use.
	DefaultMaxConnections = 65536
	// DefaultMaxDataLength bounds an NSDU, in octets.
	DefaultMaxDataLength = 65535
	// DefaultMaxConnectionReassemblyOctets bounds the memory the connections
	// hold for reassembly.
	DefaultMaxConnectionReassemblyOctets = 4 << 20
)

// ConnectionConfig configures the protocol class 2 procedures. The zero value
// of every field selects its default.
type ConnectionConfig struct {
	// EstablishmentTimeout is T(conn est).
	EstablishmentTimeout time.Duration
	// ReleaseTimeout is T(rel).
	ReleaseTimeout time.Duration
	// RepeatReleaseTimeout is T(repeat rel).
	RepeatReleaseTimeout time.Duration
	// ReleaseGiveUpTimeout is T(int). When it expires the section is released
	// without a release complete and a MaintenanceIndication is given.
	ReleaseGiveUpTimeout time.Duration
	// InactivitySendTimeout is T(ias).
	InactivitySendTimeout time.Duration
	// InactivityReceiveTimeout is T(iar). It must exceed T(ias) (§3.4).
	InactivityReceiveTimeout time.Duration
	// ReferenceFreeze is how long a released local reference stays out of
	// use (§3.3.2).
	ReferenceFreeze time.Duration
	// MaxConnections bounds the connection sections in use.
	MaxConnections int
	// MaxDataLength bounds an NSDU, sent or reassembled from DT1 segments
	// (§3.5.3). A peer that sends more is released with the cause remote
	// procedure error.
	MaxDataLength int
	// MaxReassemblyOctets bounds the memory all connections hold for
	// reassembly. A DT1 that needs more releases its connection with the
	// cause network congestion.
	MaxReassemblyOctets int
}

// ConnectionState describes a connection section's protocol state.
// ITU-T Q.714 (05/01) Figures A.3 and A.4, Table B.3. State c5 of Table B.3 is
// not represented: this end node answers an RLSD at once (§3.3.5).
type ConnectionState uint8

// Connection section states, with their names in ITU-T Q.714 (05/01) Table
// B.3.
const (
	ConnectionIdle            ConnectionState = iota // c1: ready, no assigned section
	ConnectionIncomingPending                        // c2: peer's connection request pending
	ConnectionOutgoingPending                        // c3: local connection request pending
	ConnectionEstablished                            // c4: data transfer
	ConnectionReleasePending                         // c6: local disconnect pending
	ConnectionFrozen                                 // released reference awaiting reuse (§3.3.2)
)

func (v ConnectionState) String() string {
	switch v {
	case ConnectionIdle:
		return "idle"
	case ConnectionIncomingPending:
		return "incoming pending"
	case ConnectionOutgoingPending:
		return "outgoing pending"
	case ConnectionEstablished:
		return "established"
	case ConnectionReleasePending:
		return "release pending"
	case ConnectionFrozen:
		return "frozen"
	default:
		return fmt.Sprintf("ConnectionState(%d)", v)
	}
}

// Originator identifies the N-DISCONNECT originator, ITU-T Q.713 (03/01) Annex A.
type Originator uint8

// Originators of an N-DISCONNECT, ITU-T Q.711 (03/01) §6.1.2.1.
const (
	OriginatorUndefined Originator = iota // the cause does not tell (ITU-T Q.713 (03/01) Annex A)
	OriginatorUser                        // NSU, the network service user
	OriginatorProvider                    // NSP, the network service provider
)

func (v Originator) String() string {
	switch v {
	case OriginatorUndefined:
		return "undefined"
	case OriginatorUser:
		return "NSU"
	case OriginatorProvider:
		return "NSP"
	default:
		return fmt.Sprintf("Originator(%d)", v)
	}
}

// Errors of the connection-oriented requests, and reasons for the Discard of a
// connection-oriented message. They match with errors.Is.
var (
	// ErrConnectionNotFound: no connection section has the local reference,
	// or it is frozen (ITU-T Q.714 (05/01) §3.3.2). For a received message:
	// its destination local reference is unassigned (Table B.2).
	ErrConnectionNotFound = errors.New("sccp: connection not found")

	// ErrConnectionState: the request or the message does not fit the state
	// of the connection section (Table B.3).
	ErrConnectionState = errors.New("sccp: invalid connection state")

	// ErrConnectionReference: a message for an assigned local reference came
	// from another signalling point, or with another source local reference,
	// than the section's (Table B.2).
	ErrConnectionReference = errors.New("sccp: connection reference mismatch")

	// ErrLocalReferenceExhausted: every local reference is in use or frozen
	// (§3.1.2, §3.3.2). Connect returns it wrapped in ErrInvalidRequest.
	ErrLocalReferenceExhausted = errors.New("sccp: local references exhausted")
)

// ConnectRequest is N-CONNECT request, ITU-T Q.711 (03/01) §6.1.1.1.
type ConnectRequest struct {
	// CalledPartyAddress must be routable, as for UnitdataRequest.
	CalledPartyAddress  params.PartyAddress
	CallingPartyAddress *params.PartyAddress
	// Data is optional user data for the CR.
	Data []byte
	// Importance is at most 4 in the CR (ITU-T Q.714 (05/01) §2.6.2 and
	// Table 2).
	Importance *params.Importance
	// DPC is the 14-bit point code of the SCCP node the CR goes to. Zero
	// selects the point code of CalledPartyAddress.
	DPC uint32
}

// ConnectResponse is N-CONNECT response, ITU-T Q.711 (03/01) §6.1.1.1.
type ConnectResponse struct {
	// RespondingAddress, optional, is the called party address of the CC.
	RespondingAddress *params.PartyAddress
	// Data is optional user data for the CC.
	Data []byte
	// Importance is at most 4 in the CC (ITU-T Q.714 (05/01) Table 2).
	Importance *params.Importance
}

// DisconnectRequest is N-DISCONNECT request, ITU-T Q.711 (03/01) §6.1.2.1.
type DisconnectRequest struct {
	// ReleaseCause is the cause of the RLSD that releases a connection, or a
	// connection request still pending, once it is confirmed.
	ReleaseCause params.ReleaseCause
	// RefusalCause is the cause of the CREF that refuses an incoming
	// connection request.
	RefusalCause params.RefusalCause
	// Data is optional user data for the RLSD or the CREF.
	Data []byte
	// Importance is at most 4 in a CREF and 6 in an RLSD (ITU-T Q.714
	// (05/01) Table 2).
	Importance *params.Importance
}

// ConnectionInfo is a detached snapshot of a section, ITU-T Q.714 (05/01) §3.1.2.
type ConnectionInfo struct {
	LocalReference, RemoteReference params.LocalReference
	State                           ConnectionState
	// Remote is the routing label of the messages the section sends.
	Remote                                  RoutingLabel
	CalledPartyAddress, CallingPartyAddress *params.PartyAddress
}

// ConnectIndication is N-CONNECT indication, ITU-T Q.711 (03/01) §6.1.1.1.
// The connection section waits for its user to answer with Accept or
// Disconnect, with no timer (ITU-T Q.714 (05/01) Figure C.3 sheet 2), and
// counts against MaxConnections until then.
type ConnectIndication struct {
	RoutingLabel
	LocalReference      params.LocalReference
	CalledPartyAddress  params.PartyAddress
	CallingPartyAddress *params.PartyAddress
	Data                []byte
	Importance          *params.Importance
}

// ConnectConfirmation is N-CONNECT confirmation, ITU-T Q.711 (03/01) §6.1.1.1.
type ConnectConfirmation struct {
	LocalReference    params.LocalReference
	RespondingAddress *params.PartyAddress
	Data              []byte
}

// DataIndication is N-DATA indication, ITU-T Q.711 (03/01) §6.1.3.1.
type DataIndication struct {
	LocalReference params.LocalReference
	Data           []byte
}

// DisconnectIndication is N-DISCONNECT indication, ITU-T Q.711 (03/01) §6.1.2.1.
type DisconnectIndication struct {
	LocalReference    params.LocalReference
	Originator        Originator
	Refused           bool
	RefusalCause      params.RefusalCause
	ReleaseCause      params.ReleaseCause
	RespondingAddress *params.PartyAddress
	Data              []byte
}

func (*ConnectIndication) indication()    {}
func (*ConnectConfirmation) indication()  {}
func (*DataIndication) indication()       {}
func (*DisconnectIndication) indication() {}

type connectionState struct {
	sections map[params.LocalReference]*connectionSection
	next     params.LocalReference
	// Exclusive upper bound; tests can exercise exhaustion in a small space.
	referenceLimit   params.LocalReference
	active           int
	reassemblyOctets int
}
type connectionSection struct {
	ConnectionInfo
	establishment, releaseTimer, repeatRelease, giveUp, sendInactivity, receiveInactivity *timer
	reassembly                                                                            []byte
	releaseMessage                                                                        *RLSD
	// ITU-T Q.714 (05/01) §3.1.4.2: defer outgoing user disconnect until setup ends.
	disconnect *DisconnectRequest
}

func (s *Stack) initConnections() error {
	c := &s.cfg.Connection
	// ITU-T Q.714 (05/01) Annex C.4: zero selects the documented defaults.
	durations := []struct {
		value    *time.Duration
		fallback time.Duration
	}{
		{&c.EstablishmentTimeout, DefaultEstablishmentTimeout}, {&c.ReleaseTimeout, DefaultReleaseTimeout},
		{&c.RepeatReleaseTimeout, DefaultRepeatReleaseTimeout}, {&c.ReleaseGiveUpTimeout, DefaultReleaseGiveUpTimeout},
		{&c.InactivitySendTimeout, DefaultInactivitySendTimeout}, {&c.InactivityReceiveTimeout, DefaultInactivityReceiveTimeout},
		{&c.ReferenceFreeze, DefaultReferenceFreeze},
	}
	for _, d := range durations {
		if *d.value < 0 {
			return fmt.Errorf("%w: negative connection timer", ErrInvalidRequest)
		}
		if *d.value == 0 {
			*d.value = d.fallback
		}
	}
	if c.MaxConnections < 0 || c.MaxDataLength < 0 || c.MaxReassemblyOctets < 0 {
		return fmt.Errorf("%w: negative connection limit", ErrInvalidRequest)
	}
	if c.MaxConnections == 0 {
		c.MaxConnections = DefaultMaxConnections
	}
	if c.MaxDataLength == 0 {
		c.MaxDataLength = DefaultMaxDataLength
	}
	if c.MaxReassemblyOctets == 0 {
		c.MaxReassemblyOctets = DefaultMaxConnectionReassemblyOctets
	}
	// ITU-T Q.714 (05/01) §3.4: the receive timer must exceed the send timer.
	if c.InactivityReceiveTimeout <= c.InactivitySendTimeout {
		return fmt.Errorf("%w: receive inactivity timer must exceed send inactivity timer", ErrInvalidRequest)
	}
	s.conn = connectionState{sections: make(map[params.LocalReference]*connectionSection), next: 1, referenceLimit: params.LocalReferenceReserved}
	return nil
}

// candidateReference bounds the search, excluding active and frozen references.
// ITU-T Q.714 (05/01) §3.1.2, §3.3.2; ITU-T Q.713 (03/01) §3.2: all ones is reserved.
func (s *Stack) candidateReference() (params.LocalReference, error) {
	if s.conn.active >= s.cfg.Connection.MaxConnections {
		return 0, fmt.Errorf("%w: connection capacity", ErrInvalidRequest)
	}
	limit := min(s.conn.referenceLimit, params.LocalReferenceReserved)
	if limit == 0 {
		return 0, fmt.Errorf("%w: %w", ErrInvalidRequest, ErrLocalReferenceExhausted)
	}
	ref := s.conn.next
	if ref >= limit {
		ref = 0
	}
	// At most occupied+1 candidates suffice when any reference is free.
	for attempts := 0; attempts < min(int(limit), len(s.conn.sections)+1); attempts++ {
		if _, used := s.conn.sections[ref]; !used {
			return ref, nil
		}
		ref++
		if ref >= limit {
			ref = 0
		}
	}
	return 0, fmt.Errorf("%w: %w", ErrInvalidRequest, ErrLocalReferenceExhausted)
}
func (s *Stack) registerConnection(c *connectionSection) {
	s.conn.sections[c.LocalReference] = c
	s.conn.active++
	s.conn.next = c.LocalReference + 1
}
func (s *Stack) requestConnection(ref params.LocalReference, states ...ConnectionState) (*connectionSection, error) {
	c := s.conn.sections[ref]
	if c == nil || c.State == ConnectionFrozen {
		return nil, fmt.Errorf("%w: %#x", ErrConnectionNotFound, ref)
	}
	if slices.Contains(states, c.State) {
		return c, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrConnectionState, c.State)
}

// Connect originates a class 2 CR and starts T(conn est).
// ITU-T Q.714 (05/01) §3.1.4.1, §3.1.3.1, §2.3.1.
//
// The error is ErrInvalidRequest, wrapping ErrLocalReferenceExhausted when no
// local reference is free; no connection section is then allocated.
func (s *Stack) Connect(req ConnectRequest) (params.LocalReference, Result, error) {
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	s.expire(now, &res)
	dpc := req.DPC
	if dpc == 0 {
		if !req.CalledPartyAddress.HasPointCode {
			return 0, res, fmt.Errorf("%w: no DPC or called point code", ErrInvalidRequest)
		}
		dpc = uint32(req.CalledPartyAddress.PointCode)
	}
	// ITU-T Q.704 (07/96) §§2.2.1-2.2.2: the DPC has 14 bits.
	if dpc > params.MaxPointCode {
		return 0, res, fmt.Errorf("%w: destination point code %d exceeds %d", ErrInvalidRequest, dpc, params.MaxPointCode)
	}
	if err := checkCalledAddress(req.CalledPartyAddress); err != nil {
		return 0, res, err
	}
	if err := checkImportance(req.Importance); err != nil {
		return 0, res, err
	}
	called := copyConnectionAddress(&req.CalledPartyAddress)
	m := &CR{ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: *copyConnectionAddress(called), CallingPartyAddress: copyConnectionAddress(req.CallingPartyAddress), Data: clone(req.Data), Importance: limitImportance(MsgTypeCR, req.Importance)}
	if !called.RouteOnSSN {
		m.HopCounter = new(s.cfg.HopCounter)
	}
	// Validate before reserving a reference or advancing SLS; Q.713 (03/01) Table 3.
	if _, err := (MarshalOptions{MaxMessageSize: s.cfg.MaxMessageSize}).Marshal(m); err != nil {
		return 0, res, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	ref, err := s.candidateReference()
	if err != nil {
		return 0, res, err
	}
	m.SourceLocalReference = ref
	label := RoutingLabel{OPC: s.cfg.PointCode, DPC: dpc, SLS: s.nextSLS, NI: s.cfg.NetworkIndicator}
	if err := s.transfer(&res, label, m); err != nil {
		return 0, res, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	s.sls(2, 0)
	c := &connectionSection{ConnectionInfo: ConnectionInfo{LocalReference: ref, State: ConnectionOutgoingPending, Remote: label, CalledPartyAddress: called, CallingPartyAddress: copyConnectionAddress(req.CallingPartyAddress)}}
	s.registerConnection(c)
	c.establishment = s.schedule(now.Add(s.cfg.Connection.EstablishmentTimeout), func(now time.Time, res *Result) {
		// ITU-T Q.714 (05/01) Figure C.2 sheets 2-3: failed setup freezes;
		// Q.711 (03/01) Figure 8: no indication after the user's disconnect.
		if c.disconnect == nil {
			res.Indications = append(res.Indications, &DisconnectIndication{LocalReference: ref, Originator: OriginatorProvider, Refused: true, RefusalCause: params.RefusalCauseExpirationOfTheConnectionEstablishmentTimer})
		}
		s.freezeConnection(now, c)
	})
	return ref, res, nil
}

// Accept confirms an incoming request with class 2, including a class 3 proposal.
// ITU-T Q.714 (05/01) §3.1.3.1, §3.1.6.2.
//
// The error is ErrConnectionNotFound, ErrConnectionState for a section that
// is not an incoming request, or ErrInvalidRequest for a response that
// cannot be sent. The request then stays pending, and the Result holds only
// what the timers that were due produced.
func (s *Stack) Accept(ref params.LocalReference, resp ConnectResponse) (Result, error) {
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	s.expire(now, &res)
	c, err := s.requestConnection(ref, ConnectionIncomingPending)
	if err != nil {
		return res, err
	}
	if err := checkImportance(resp.Importance); err != nil {
		return res, err
	}
	m := &CC{DestinationLocalReference: c.RemoteReference, SourceLocalReference: ref, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: copyConnectionAddress(resp.RespondingAddress), Data: clone(resp.Data), Importance: limitImportance(MsgTypeCC, resp.Importance)}
	if err := s.transfer(&res, c.Remote, m); err != nil {
		return res, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	c.State = ConnectionEstablished
	s.startConnectionInactivity(now, c)
	return res, nil
}

// SendData segments an NSDU into DT1 messages, with only the final M bit clear.
// ITU-T Q.714 (05/01) §3.5.1.1, §3.5.3; ITU-T Q.713 (03/01) Table 8.
//
// The error is ErrConnectionNotFound, ErrConnectionState for a section not in
// data transfer, or ErrInvalidRequest for an NSDU that is empty, longer than
// MaxDataLength or cannot be sent. No DT1 is then sent: the Result holds only
// what the timers that were due produced.
func (s *Stack) SendData(ref params.LocalReference, data []byte) (Result, error) {
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	s.expire(now, &res)
	c, err := s.requestConnection(ref, ConnectionEstablished)
	if err != nil {
		return res, err
	}
	if len(data) == 0 || len(data) > s.cfg.Connection.MaxDataLength {
		return res, fmt.Errorf("%w: NSDU length %d", ErrInvalidRequest, len(data))
	}
	// A DT1 has seven octets of fixed fields, pointer and length indicator.
	size := min(maxData, s.cfg.MaxMessageSize-7)
	if size <= 0 {
		return res, fmt.Errorf("%w: DT1 cannot fit", ErrInvalidRequest)
	}
	var out Result
	for off := 0; off < len(data); off += size {
		end := min(off+size, len(data))
		m := &DT1{DestinationLocalReference: c.RemoteReference, MoreData: end < len(data), Data: clone(data[off:end])}
		if err := s.transfer(&out, c.Remote, m); err != nil {
			return res, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
	}
	res.Transfers = append(res.Transfers, out.Transfers...)
	s.restartConnectionSend(now, c)
	return res, nil
}

// Disconnect refuses an incoming request or initiates release of a connection.
// ITU-T Q.714 (05/01) §3.1.4.2, §3.2.1, §3.3.3.1. For a connection request of
// its own still pending, the release waits for the CC; no N-DISCONNECT
// indication follows the request.
//
// The error is ErrConnectionNotFound, ErrConnectionState for a section that
// is already released, or ErrInvalidRequest for a request that cannot be
// sent. The section is then unchanged, and the Result holds only what the
// timers that were due produced.
func (s *Stack) Disconnect(ref params.LocalReference, req DisconnectRequest) (Result, error) {
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	s.expire(now, &res)
	c, err := s.requestConnection(ref, ConnectionIncomingPending, ConnectionOutgoingPending, ConnectionEstablished)
	if err != nil {
		return res, err
	}
	if err := checkImportance(req.Importance); err != nil {
		return res, err
	}
	req.Data = clone(req.Data)
	req.Importance = cloneImportance(req.Importance)
	if c.State == ConnectionIncomingPending {
		m := &CREF{DestinationLocalReference: c.RemoteReference, RefusalCause: req.RefusalCause, Data: req.Data, Importance: limitImportance(MsgTypeCREF, req.Importance)}
		if err := s.transfer(&res, c.Remote, m); err != nil {
			return res, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
		s.freezeConnection(now, c)
		return res, nil
	}
	m := s.connectionReleaseMessage(c, req)
	// Validate also a deferred disconnect before changing pending setup state.
	if _, err := (MarshalOptions{MaxMessageSize: s.cfg.MaxMessageSize}).Marshal(m); err != nil {
		return res, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	if c.State == ConnectionOutgoingPending {
		c.disconnect = &req
		return res, nil
	}
	s.beginConnectionRelease(now, c, m, false, &res)
	return res, nil
}

// Connection returns a detached snapshot.
// It does not run timers that are due; HandleTimeout, RunTimers and the other methods do.
func (s *Stack) Connection(ref params.LocalReference) (ConnectionInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.conn.sections[ref]
	if c == nil {
		return ConnectionInfo{}, false
	}
	info := c.ConnectionInfo
	info.CalledPartyAddress = copyConnectionAddress(info.CalledPartyAddress)
	info.CallingPartyAddress = copyConnectionAddress(info.CallingPartyAddress)
	return info, true
}

// Addresses are detached because GT digits belong to the section's identity.
// ITU-T Q.714 (05/01) §3.1.1, §3.1.2.
func copyConnectionAddress(a *params.PartyAddress) *params.PartyAddress {
	if a == nil {
		return nil
	}
	return new(a.Clone())
}

// ITU-T Q.713 (03/01) Table A.1: unqualified and unrecognized causes have no unique originator.
func connectionRefusalOriginator(cause params.RefusalCause) Originator {
	switch cause {
	case params.RefusalCauseEndUserOriginated, params.RefusalCauseEndUserCongestion, params.RefusalCauseEndUserFailure, params.RefusalCauseSCCPUserOriginated, params.RefusalCauseAccessFailure, params.RefusalCauseAccessCongestion, params.RefusalCauseSubsystemCongestion, params.RefusalCauseIncompatibleUserData:
		return OriginatorUser
	case params.RefusalCauseDestinationAddressUnknown, params.RefusalCauseDestinationInaccessible, params.RefusalCauseNetworkResourceQoSNotAvailableNonTransient, params.RefusalCauseNetworkResourceQoSNotAvailableTransient, params.RefusalCauseSubsystemFailure, params.RefusalCauseExpirationOfTheConnectionEstablishmentTimer, params.RefusalCauseHopCounterViolation, params.RefusalCauseSCCPFailure, params.RefusalCauseNoTranslationForAnAddressOfSuchNature, params.RefusalCauseUnequippedUser:
		return OriginatorProvider
	default:
		return OriginatorUndefined
	}
}

// ITU-T Q.713 (03/01) Table A.2: map the received release cause to NSU or NSP.
func connectionReleaseOriginator(cause params.ReleaseCause) Originator {
	switch cause {
	case params.ReleaseCauseEndUserOriginated, params.ReleaseCauseEndUserCongestion, params.ReleaseCauseEndUserFailure, params.ReleaseCauseSCCPUserOriginated, params.ReleaseCauseAccessFailure, params.ReleaseCauseAccessCongestion, params.ReleaseCauseSubsystemCongestion:
		return OriginatorUser
	case params.ReleaseCauseRemoteProcedureError, params.ReleaseCauseInconsistentConnectionData, params.ReleaseCauseSubsystemFailure, params.ReleaseCauseMTPFailure, params.ReleaseCauseNetworkCongestion, params.ReleaseCauseExpirationOfResetTimer, params.ReleaseCauseExpirationOfReceiveInactivityTimer, params.ReleaseCauseSCCPFailure:
		return OriginatorProvider
	default:
		return OriginatorUndefined
	}
}
