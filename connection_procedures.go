// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"fmt"
	"time"

	"github.com/gomaja/go-sccp/params"
)

// connectionReferences extracts the fields required by ITU-T Q.714 (05/01) Table B.1.
func connectionReferences(m Message) (destination, source params.LocalReference, hasSource bool) {
	switch m := m.(type) {
	case *CC:
		return m.DestinationLocalReference, m.SourceLocalReference, true
	case *CREF:
		return m.DestinationLocalReference, 0, false
	case *RLSD:
		return m.DestinationLocalReference, m.SourceLocalReference, true
	case *RLC:
		return m.DestinationLocalReference, m.SourceLocalReference, true
	case *DT1:
		return m.DestinationLocalReference, 0, false
	case *DT2:
		return m.DestinationLocalReference, 0, false
	case *AK:
		return m.DestinationLocalReference, 0, false
	case *ED:
		return m.DestinationLocalReference, 0, false
	case *EA:
		return m.DestinationLocalReference, 0, false
	case *RSR:
		return m.DestinationLocalReference, m.SourceLocalReference, true
	case *RSC:
		return m.DestinationLocalReference, m.SourceLocalReference, true
	case *ERR:
		return m.DestinationLocalReference, 0, false
	case *IT:
		return m.DestinationLocalReference, m.SourceLocalReference, true
	default:
		return 0, 0, false
	}
}

func (s *Stack) handleConnectionOriented(now time.Time, label RoutingLabel, m Message, local bool, res *Result) {
	// ITU-T Q.714 (05/01) Table B.1: a message type unknown to the
	// implemented protocol class (its Note), here the class 3 messages, is
	// discarded in any state; §1.1.4.1: unsupported message types "are
	// discarded with a report to OMAP ("syntax error")".
	switch m.MessageType() {
	case MsgTypeDT2, MsgTypeAK, MsgTypeED, MsgTypeEA, MsgTypeRSR, MsgTypeRSC:
		res.discard(label, m.MessageType(), fmt.Errorf("%w: %w: %s is a protocol class 3 message", ErrSyntax, ErrNotSupported, m.MessageType()))
		return
	}
	// ITU-T Q.714 (05/01) §§3.8.3.3 a2, 3.8.4: invalid classes only discard.
	// §3.8.4 permits liberal control data lengths; Q.713 (03/01) Table 8
	// requires a nonempty DT1 data parameter.
	invalid := false
	switch m := m.(type) {
	case *CC:
		invalid = m.ProtocolClass.Class != 2 && m.ProtocolClass.Class != 3
	case *IT:
		invalid = m.ProtocolClass.Class != 2 && m.ProtocolClass.Class != 3
	case *DT1:
		invalid = len(m.Data) == 0
	}
	if invalid {
		res.discard(label, m.MessageType(), fmt.Errorf("%w: invalid connection parameter", ErrSyntax))
		return
	}
	if cr, ok := m.(*CR); ok {
		s.receiveConnectionRequest(label, cr, local, res)
		return
	}
	dst, src, hasSource := connectionReferences(m)
	// ITU-T Q.714 (05/01) §3.8.4: all-ones references are reserved by Q.713 §§3.2-3.3.
	if dst >= params.LocalReferenceReserved || (hasSource && src >= params.LocalReferenceReserved) {
		res.discard(label, m.MessageType(), fmt.Errorf("%w: reserved local reference", ErrSyntax))
		return
	}
	c := s.conn.sections[dst]
	if c != nil && (c.State == ConnectionFrozen || c.State == ConnectionIdle) {
		c = nil
	}
	if c == nil {
		s.connectionMismatch(now, label, m, c, params.ErrorCauseUnassignedDestinationLRN, res)
		return
	}
	// CC/CREF have N/A in the source-reference and OPC columns, Table B.2.
	// ITU-T Q.714 (05/01) §3.1.4.2 items 1, 3: c3 has no associated peer yet.
	checkPeer := c.State != ConnectionOutgoingPending && m.MessageType() != MsgTypeCC && m.MessageType() != MsgTypeCREF
	if checkPeer && label.OPC != c.Remote.DPC {
		s.connectionMismatch(now, label, m, c, params.ErrorCausePointCodeMismatch, res)
		return
	}
	if checkPeer && hasSource && src != c.RemoteReference {
		s.connectionMismatch(now, label, m, c, params.ErrorCauseInconsistentSourceLRN, res)
		return
	}
	s.dispatchConnectionState(now, label, c, m, res)
}

// ITU-T Q.714 (05/01) Table B.2: Note 2 preserves sections; Note 3 releases an inconsistent IT section.
func (s *Stack) connectionMismatch(now time.Time, label RoutingLabel, m Message, c *connectionSection, cause params.ErrorCause, res *Result) {
	dst, src, _ := connectionReferences(m)
	reason := ErrConnectionReference
	if cause == params.ErrorCauseUnassignedDestinationLRN {
		reason = ErrConnectionNotFound
	}
	res.discard(label, m.MessageType(), fmt.Errorf("%w: %s", reason, cause))
	send := func(m Message) {
		if err := s.transfer(res, replyLabel(label), m); err != nil {
			res.discard(label, m.MessageType(), fmt.Errorf("%w: mismatch response: %w", ErrUndeliverable, err))
		}
	}
	switch m.MessageType() {
	case MsgTypeCC:
		if cause == params.ErrorCauseUnassignedDestinationLRN {
			send(&ERR{DestinationLocalReference: src, ErrorCause: cause})
		}
	case MsgTypeRLSD:
		if cause == params.ErrorCauseUnassignedDestinationLRN {
			send(&RLC{DestinationLocalReference: src, SourceLocalReference: dst})
		} else {
			send(&ERR{DestinationLocalReference: src, ErrorCause: cause})
		}
	case MsgTypeIT:
		switch cause {
		case params.ErrorCauseUnassignedDestinationLRN:
			send(&ERR{DestinationLocalReference: src, ErrorCause: cause})
		case params.ErrorCauseInconsistentSourceLRN:
			send(&RLSD{DestinationLocalReference: src, SourceLocalReference: dst, ReleaseCause: params.ReleaseCauseInconsistentConnectionData})
			if c != nil {
				// ITU-T Q.714 (05/01) Table B.2 Note 3, §3.4 Table 3:
				// the second RLSD releases the stored section; c6 repeats its release.
				if c.State == ConnectionReleasePending {
					s.sendConnectionMessage(now, c, c.releaseMessage, res)
				} else {
					s.beginConnectionRelease(now, c, s.connectionReleaseMessage(c, DisconnectRequest{ReleaseCause: params.ReleaseCauseInconsistentConnectionData}), true, res)
				}
			}
		}
	}
}

// ITU-T Q.714 (05/01) §3.1.6.1, §2.3.1 and §2.8: route CR before allocating a section.
func (s *Stack) receiveConnectionRequest(label RoutingLabel, m *CR, local bool, res *Result) {
	if (m.ProtocolClass.Class != 2 && m.ProtocolClass.Class != 3) || m.SourceLocalReference >= params.LocalReferenceReserved {
		res.discard(label, MsgTypeCR, fmt.Errorf("%w: unusable connection request", ErrSyntax))
		return
	}
	hop := params.HopCounter(0)
	if m.HopCounter != nil {
		hop = *m.HopCounter
	}
	ssn, cause, verdict, reason := route(m.CalledPartyAddress, hop, m.HopCounter != nil, local)
	if verdict == routeDiscard {
		// ITU-T Q.714 (05/01) §2.2.2.1 item 4 and §3.8.3.3 b6, b7: an
		// incomplete or ill-constructed called address is discarded.
		res.discard(label, MsgTypeCR, reason)
		return
	}
	if verdict == routeDeliver {
		var available bool
		cause, available = s.subsystemCause(ssn)
		if !available {
			verdict = routeReturn
		}
	}
	// ITU-T Q.714 (05/01) §2.7 d): the point code of the calling party address
	// identifies the originating node of the incoming connection section, or
	// the OPC when there is none. Replies come from the point code the CR was
	// sent to.
	remote := replyLabel(label)
	if m.CallingPartyAddress != nil && m.CallingPartyAddress.HasPointCode {
		remote.DPC = uint32(m.CallingPartyAddress.PointCode)
	}
	refuse := func(cause params.RefusalCause) {
		// A refusal by the SCCP, not by a user primitive: the default
		// importance of ITU-T Q.714 (05/01) Table 2 applies (§2.6.2 a)).
		cref := &CREF{DestinationLocalReference: m.SourceLocalReference, RefusalCause: cause}
		if err := s.transfer(res, remote, cref); err != nil {
			res.discard(label, MsgTypeCR, fmt.Errorf("%w: CREF: %w", ErrUndeliverable, err))
		}
	}
	if verdict == routeReturn {
		refuse(connectionRoutingRefusal(cause))
		alertHopCounter(label, MsgTypeCR, cause, res)
		return
	}
	ref, err := s.candidateReference()
	if err != nil { // ITU-T Q.714 (05/01) §3.1.6.1, §3.2.1: transient resource refusal.
		refuse(params.RefusalCauseNetworkResourceQoSNotAvailableTransient)
		return
	}
	c := &connectionSection{ConnectionInfo: ConnectionInfo{LocalReference: ref, RemoteReference: m.SourceLocalReference, State: ConnectionIncomingPending, Remote: remote, CalledPartyAddress: copyConnectionAddress(&m.CalledPartyAddress), CallingPartyAddress: copyConnectionAddress(m.CallingPartyAddress)}}
	s.registerConnection(c)
	res.Indications = append(res.Indications, &ConnectIndication{RoutingLabel: label, LocalReference: ref, CalledPartyAddress: *copyConnectionAddress(&m.CalledPartyAddress), CallingPartyAddress: copyConnectionAddress(m.CallingPartyAddress), Data: clone(m.Data), Importance: cloneImportance(m.Importance)})
}

// ITU-T Q.714 (05/01) §2.8.1-§2.8.6: routing causes map to CREF causes.
func connectionRoutingRefusal(cause params.ReturnCause) params.RefusalCause {
	switch cause {
	case params.ReturnCauseNoTranslationForAnAddressOfSuchNature:
		return params.RefusalCauseNoTranslationForAnAddressOfSuchNature
	case params.ReturnCauseNoTranslationForThisSpecificAddress:
		return params.RefusalCauseDestinationAddressUnknown
	case params.ReturnCauseSubsystemFailure:
		return params.RefusalCauseSubsystemFailure
	case params.ReturnCauseUnequippedUser:
		return params.RefusalCauseUnequippedUser
	case params.ReturnCauseHopCounterViolation:
		return params.RefusalCauseHopCounterViolation
	case params.ReturnCauseMTPFailure:
		return params.RefusalCauseDestinationInaccessible
	case params.ReturnCauseSCCPFailure:
		return params.RefusalCauseSCCPFailure
	case params.ReturnCauseSubsystemCongestion:
		return params.RefusalCauseSubsystemCongestion
	case params.ReturnCauseNetworkCongestion:
		return params.RefusalCauseNetworkResourceQoSNotAvailableTransient
	default:
		return params.RefusalCauseUnqualified
	}
}

// ITU-T Q.714 (05/01) Table B.3: abnormal messages are handled by state before procedure dispatch.
func (s *Stack) dispatchConnectionState(now time.Time, label RoutingLabel, c *connectionSection, m Message, res *Result) {
	discard := func() {
		res.discard(label, m.MessageType(), fmt.Errorf("%w: %s in %s", ErrConnectionState, m.MessageType(), c.State))
	}
	switch c.State {
	case ConnectionIncomingPending: // c2: no received message completes a local user's pending response.
		discard()
		return
	case ConnectionOutgoingPending: // c3: CC/CREF are normal; other known messages are ERROR 3.
		switch m.(type) {
		case *CC, *CREF:
			s.receiveConnectionSetup(now, label, c, m, res)
		default:
			discard()
			// ITU-T Q.714 (05/01) Figure C.2 sheets 2-3: Released
			// is answered to its sender before freeing the unassociated section.
			if released, ok := m.(*RLSD); ok {
				if err := s.transfer(res, replyLabel(label), &RLC{DestinationLocalReference: released.SourceLocalReference, SourceLocalReference: released.DestinationLocalReference}); err != nil {
					res.discard(label, MsgTypeRLSD, fmt.Errorf("%w: RLC: %w", ErrUndeliverable, err))
				}
			}
			// ITU-T Q.711 (03/01) Figure 8; Q.714 Figure C.2 sheet 3.
			if c.disconnect == nil {
				res.Indications = append(res.Indications, &DisconnectIndication{LocalReference: c.LocalReference, Originator: OriginatorProvider, Refused: true, RefusalCause: params.RefusalCauseUnqualified})
			}
			s.freezeConnection(now, c)
		}
	case ConnectionReleasePending: // c6: RLSD/RLC complete release; ERR is ERROR 3.
		switch m.(type) {
		case *RLSD, *RLC:
			s.freezeConnection(now, c)
		case *ERR:
			discard()
			// ITU-T Q.714 (05/01) Figure C.2 sheet 6, Figure C.3 sheet 5:
			// Released/error completes pending release without another user indication.
			s.freezeConnection(now, c)
		default:
			discard()
		}
	case ConnectionEstablished:
		// ITU-T Q.714 (05/01) §3.4: every received section message resets T(iar).
		s.restartConnectionReceive(now, c)
		// CC, CREF and RLC in c4 are DISCARD, not procedure errors (Table B.3).
		switch m.(type) {
		case *CC, *CREF, *RLC:
			discard()
			return
		}
		s.receiveEstablishedConnection(now, label, c, m, res)
	default:
		discard()
	}
}

// ITU-T Q.714 (05/01) §3.1.4.2, §3.1.3.1, §3.2.3: complete or refuse outgoing setup.
func (s *Stack) receiveConnectionSetup(now time.Time, label RoutingLabel, c *connectionSection, m Message, res *Result) {
	switch m := m.(type) {
	case *CREF:
		// ITU-T Q.714 (05/01) Figure C.2 sheets 2-3; Q.711 Figure 8.
		if c.disconnect == nil {
			res.Indications = append(res.Indications, &DisconnectIndication{LocalReference: c.LocalReference, Originator: connectionRefusalOriginator(m.RefusalCause), Refused: true, RefusalCause: m.RefusalCause, RespondingAddress: copyConnectionAddress(m.CalledPartyAddress), Data: clone(m.Data)})
		}
		s.freezeConnection(now, c)
	case *CC:
		s.stop(c.establishment)
		c.establishment = nil
		c.RemoteReference = m.SourceLocalReference
		c.Remote.DPC = label.OPC // §3.1.4.2 item 3: associate the confirming signalling point.
		if m.ProtocolClass.Class != 2 {
			res.discard(label, MsgTypeCC, fmt.Errorf("%w: class %d confirmation", ErrConnectionState, m.ProtocolClass.Class))
		}
		// ITU-T Q.714 (05/01) Figure C.2 sheet 3: honour the deferred
		// user release even when the confirmation attempts a class 3 upgrade.
		if req := c.disconnect; req != nil {
			c.disconnect = nil
			s.beginConnectionRelease(now, c, s.connectionReleaseMessage(c, *req), false, res)
			return
		}
		if m.ProtocolClass.Class != 2 {
			s.beginConnectionRelease(now, c, s.connectionReleaseMessage(c, DisconnectRequest{ReleaseCause: params.ReleaseCauseRemoteProcedureError}), true, res)
			return
		}
		c.State = ConnectionEstablished
		s.startConnectionInactivity(now, c)
		res.Indications = append(res.Indications, &ConnectConfirmation{LocalReference: c.LocalReference, RespondingAddress: copyConnectionAddress(m.CalledPartyAddress), Data: clone(m.Data)})
	}
}

func (s *Stack) receiveEstablishedConnection(now time.Time, label RoutingLabel, c *connectionSection, m Message, res *Result) {
	switch m := m.(type) {
	case *DT1:
		// ITU-T Q.714 (05/01) §3.5.3: accumulate M=1 segments until a final M=0.
		if len(m.Data) > s.cfg.Connection.MaxDataLength-len(c.reassembly) {
			res.discard(label, MsgTypeDT1, fmt.Errorf("%w: %w: NSDU exceeds data limit", ErrConnectionState, ErrReassembly))
			s.beginConnectionRelease(now, c, s.connectionReleaseMessage(c, DisconnectRequest{ReleaseCause: params.ReleaseCauseRemoteProcedureError}), true, res)
			return
		}
		if !s.appendConnectionData(c, m.Data) {
			// ITU-T Q.714 (05/01) §3.5.3; §4.1.1.2.3.4 maps no buffer
			// space to network congestion, applied here to M-bit reassembly.
			res.discard(label, MsgTypeDT1, fmt.Errorf("%w: connection buffer capacity", ErrReassembly))
			s.beginConnectionRelease(now, c, s.connectionReleaseMessage(c, DisconnectRequest{ReleaseCause: params.ReleaseCauseNetworkCongestion}), true, res)
			return
		}
		if !m.MoreData {
			res.Indications = append(res.Indications, &DataIndication{LocalReference: c.LocalReference, Data: c.reassembly})
			s.dropConnectionReassembly(c)
		}
	case *RLSD:
		// ITU-T Q.714 (05/01) §3.3.5: answer release, indicate, free and freeze.
		s.sendConnectionMessage(now, c, &RLC{DestinationLocalReference: c.RemoteReference, SourceLocalReference: c.LocalReference}, res)
		res.Indications = append(res.Indications, &DisconnectIndication{LocalReference: c.LocalReference, Originator: connectionReleaseOriginator(m.ReleaseCause), ReleaseCause: m.ReleaseCause, Data: clone(m.Data)})
		s.freezeConnection(now, c)
	case *ERR:
		// ITU-T Q.714 (05/01) §3.8.6: service class mismatch starts release; other ERR frees immediately.
		if m.ErrorCause == params.ErrorCauseServiceClassMismatch {
			s.beginConnectionRelease(now, c, s.connectionReleaseMessage(c, DisconnectRequest{ReleaseCause: params.ReleaseCauseInconsistentConnectionData}), true, res)
		} else {
			res.Indications = append(res.Indications, &DisconnectIndication{LocalReference: c.LocalReference, Originator: OriginatorProvider, ReleaseCause: params.ReleaseCauseRemoteProcedureError})
			s.freezeConnection(now, c)
		}
	case *IT:
		// ITU-T Q.714 (05/01) §3.4 Table 3: class discrepancy releases; class 2 ignores sequencing/credit.
		if m.ProtocolClass.Class != 2 {
			res.discard(label, MsgTypeIT, fmt.Errorf("%w: inactivity class mismatch", ErrConnectionState))
			s.beginConnectionRelease(now, c, s.connectionReleaseMessage(c, DisconnectRequest{ReleaseCause: params.ReleaseCauseInconsistentConnectionData}), true, res)
		}
	default:
		res.discard(label, m.MessageType(), fmt.Errorf("%w: unexpected class 2 message", ErrUndeliverable))
	}
}

// ITU-T Q.714 (05/01) §3.5.3: bound retained M-bit buffer capacity across sections.
func (s *Stack) appendConnectionData(c *connectionSection, data []byte) bool {
	length := len(c.reassembly) + len(data) // The per-section limit was checked before entry.
	oldCap := cap(c.reassembly)
	if length > oldCap {
		// Amortised doubling capped at MaxDataLength, without integer overflow.
		newCap := s.cfg.Connection.MaxDataLength
		if oldCap <= newCap/2 {
			newCap = max(length, 2*oldCap)
		}
		available := s.cfg.Connection.MaxReassemblyOctets - s.conn.reassemblyOctets
		if newCap-oldCap > available {
			// Near the bound, grow only to what the data needs: a DT1 is
			// refused only when the octets themselves do not fit.
			newCap = length
		}
		if newCap-oldCap > available {
			return false
		}
		buffer := make([]byte, len(c.reassembly), newCap)
		copy(buffer, c.reassembly)
		c.reassembly = buffer
		s.conn.reassemblyOctets += newCap - oldCap
	}
	c.reassembly = append(c.reassembly, data...)
	return true
}

func (s *Stack) dropConnectionReassembly(c *connectionSection) {
	s.conn.reassemblyOctets -= cap(c.reassembly)
	c.reassembly = nil
}
