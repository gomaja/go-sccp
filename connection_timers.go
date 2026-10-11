// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"fmt"
	"time"

	"github.com/gomaja/go-sccp/params"
)

// ITU-T Q.714 (05/01) §3.3.2: release resources, retain only the frozen reference.
func (s *Stack) freezeConnection(now time.Time, c *connectionSection) {
	s.stopConnectionTimers(c)
	if c.State == ConnectionFrozen {
		return
	}
	s.conn.active--
	frozen := &connectionSection{ConnectionInfo: ConnectionInfo{LocalReference: c.LocalReference, RemoteReference: c.RemoteReference, State: ConnectionFrozen, Remote: c.Remote}}
	s.conn.sections[c.LocalReference] = frozen
	s.schedule(now.Add(s.cfg.Connection.ReferenceFreeze), func(_ time.Time, _ *Result) {
		// ITU-T Q.714 (05/01) §3.1.2: a reference becomes available after freezing ends.
		if s.conn.sections[frozen.LocalReference] == frozen {
			delete(s.conn.sections, frozen.LocalReference)
		}
	})
}
func (s *Stack) stopConnectionTimers(c *connectionSection) {
	for _, t := range []*timer{c.establishment, c.releaseTimer, c.repeatRelease, c.giveUp, c.sendInactivity, c.receiveInactivity} {
		s.stop(t)
	}
	c.establishment = nil
	c.releaseTimer = nil
	c.repeatRelease = nil
	c.giveUp = nil
	c.sendInactivity = nil
	c.receiveInactivity = nil
	s.dropConnectionReassembly(c)
	c.disconnect = nil
}

// ITU-T Q.714 (05/01) §3.1.4.2, §3.1.6.2: start inactivity control upon confirmation.
func (s *Stack) startConnectionInactivity(now time.Time, c *connectionSection) {
	s.restartConnectionSend(now, c)
	s.restartConnectionReceive(now, c)
}

// ITU-T Q.714 (05/01) §3.4: reset T(ias) whenever a section sends a message.
func (s *Stack) restartConnectionSend(now time.Time, c *connectionSection) {
	s.stop(c.sendInactivity)
	if c.State != ConnectionEstablished {
		c.sendInactivity = nil
		return
	}
	c.sendInactivity = s.schedule(now.Add(s.cfg.Connection.InactivitySendTimeout), func(now time.Time, res *Result) {
		// ITU-T Q.714 (05/01) Figure C.2 sheet 4: T(ias) only sends in Active.
		if c.State != ConnectionEstablished {
			return
		}
		// ITU-T Q.714 (05/01) §3.4; ITU-T Q.713 (03/01) Table 18: class 2 has zero sequencing and credit.
		m := &IT{DestinationLocalReference: c.RemoteReference, SourceLocalReference: c.LocalReference, ProtocolClass: params.ProtocolClass{Class: 2}}
		s.sendConnectionMessage(now, c, m, res)
		// Even a failed encode must not permanently disable the send timer.
		if c.sendInactivity == nil || c.sendInactivity.index < 0 {
			s.restartConnectionSend(now, c)
		}
	})
}

// ITU-T Q.714 (05/01) §3.4: reset T(iar) whenever a valid section message arrives.
func (s *Stack) restartConnectionReceive(now time.Time, c *connectionSection) {
	s.stop(c.receiveInactivity)
	if c.State != ConnectionEstablished {
		c.receiveInactivity = nil
		return
	}
	c.receiveInactivity = s.schedule(now.Add(s.cfg.Connection.InactivityReceiveTimeout), func(now time.Time, res *Result) {
		s.beginConnectionRelease(now, c, s.connectionReleaseMessage(c, DisconnectRequest{ReleaseCause: params.ReleaseCauseExpirationOfReceiveInactivityTimer}), true, res)
	})
}

// Protocol responses use the connection's fixed label, ITU-T Q.714 (05/01) §3.1.4.1.
func (s *Stack) sendConnectionMessage(now time.Time, c *connectionSection, m Message, res *Result) {
	// ITU-T Q.714 (05/01) §3.3.3.2: keep retransmission data private from returned transfers.
	if release, ok := m.(*RLSD); ok {
		v := *release
		v.Data = clone(release.Data)
		v.Importance = cloneImportance(release.Importance)
		m = &v
	}
	if err := s.transfer(res, c.Remote, m); err != nil {
		res.discard(c.Remote, m.MessageType(), fmt.Errorf("%w: cannot send connection message: %w", ErrUndeliverable, err))
		return
	}
	s.restartConnectionSend(now, c)
}
func (s *Stack) connectionReleaseMessage(c *connectionSection, req DisconnectRequest) *RLSD {
	return &RLSD{DestinationLocalReference: c.RemoteReference, SourceLocalReference: c.LocalReference, ReleaseCause: req.ReleaseCause, Data: clone(req.Data), Importance: limitImportance(MsgTypeRLSD, req.Importance)}
}

// ITU-T Q.714 (05/01) §3.3.3.1: send RLSD, start T(rel), stop inactivity, optionally notify.
func (s *Stack) beginConnectionRelease(now time.Time, c *connectionSection, m *RLSD, notify bool, res *Result) {
	s.stopConnectionTimers(c)
	c.State = ConnectionReleasePending
	c.releaseMessage = m
	s.sendConnectionMessage(now, c, m, res)
	if notify {
		res.Indications = append(res.Indications, &DisconnectIndication{LocalReference: c.LocalReference, Originator: OriginatorProvider, ReleaseCause: m.ReleaseCause})
	}
	c.releaseTimer = s.schedule(now.Add(s.cfg.Connection.ReleaseTimeout), func(now time.Time, res *Result) {
		// ITU-T Q.714 (05/01) §3.3.3.2: first expiry starts T(int) and periodic repetition.
		s.sendConnectionMessage(now, c, c.releaseMessage, res)
		c.giveUp = s.schedule(now.Add(s.cfg.Connection.ReleaseGiveUpTimeout), func(now time.Time, res *Result) {
			// ITU-T Q.714 (05/01) Figure C.2 sheet 6, Annex C.4:
			// T(int) alerts maintenance, without another N-DISCONNECT.
			res.Indications = append(res.Indications, &MaintenanceIndication{RoutingLabel: c.Remote, MessageType: MsgTypeRLSD, LocalReference: c.LocalReference,
				RemoteReference: c.RemoteReference, Reason: fmt.Errorf("%w: T(int) expired", ErrReleaseIncomplete)})
			s.freezeConnection(now, c)
		})
		s.scheduleConnectionRepeat(now, c)
	})
}
func (s *Stack) scheduleConnectionRepeat(now time.Time, c *connectionSection) {
	deadline := now.Add(s.cfg.Connection.RepeatReleaseTimeout)
	// ITU-T Q.714 (05/01) §3.3.3.2: repeat only during T(int), never at its endpoint.
	if c.giveUp == nil || !deadline.Before(c.giveUp.deadline) {
		return
	}
	c.repeatRelease = s.schedule(deadline, func(now time.Time, res *Result) {
		s.sendConnectionMessage(now, c, c.releaseMessage, res)
		s.scheduleConnectionRepeat(now, c)
	})
}
