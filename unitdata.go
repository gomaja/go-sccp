// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"errors"
	"fmt"
	"hash/maphash"
	"slices"
	"time"

	"github.com/gomaja/go-sccp/params"
)

// MaxSegments is the largest number of segments of one message, ITU-T Q.714
// (05/01) §4.1.1.1.2: "A maximum of 16 segments can be sent for one
// N-UNITDATA request primitive."
const MaxSegments = params.MaxRemainingSegments + 1

// UnitdataRequest is an N-UNITDATA request, ITU-T Q.711 (03/01) Table 12.
type UnitdataRequest struct {
	CalledPartyAddress  params.PartyAddress
	CallingPartyAddress params.PartyAddress

	// ProtocolClass is 0, or 1 to have the messages with the same
	// SequenceControl delivered in sequence: they share one signalling link
	// selection (ITU-T Q.714 (05/01) §4.1).
	ProtocolClass   uint8
	SequenceControl uint8

	// ReturnOnError asks for the message return procedure (§4.2).
	ReturnOnError bool
	Importance    *params.Importance

	// Data is the user data, 1 to 3952 octets (§4.1.1.1.1).
	Data []byte

	// DPC is the point code of the SCCP node the message goes to. Zero
	// selects the point code of CalledPartyAddress.
	DPC uint32
}

// Unitdata runs an N-UNITDATA request.
//
// User data that fits one message within the maximum message size goes in a
// UDT, or an XUDT when the request has an importance or the stack is
// configured with UseXUDT. Longer user data is segmented into at most 16 XUDT
// messages, ITU-T Q.714 (05/01) §4.1.1.1.2: every segment is protocol class 1
// with the same called and calling party addresses, DPC and SLS, and carries
// the segmentation parameter with the first-segment bit on the first, the
// in-sequence delivery option of the request, the remaining segment count and
// one segmentation local reference. Segments are as equal in size as
// possible, the first being the largest, so that the total is at most the
// first segment's length times the number of segments. Return on error is
// requested in the first segment only (§4.1.1.1.3), so that a failure anywhere
// comes back as the first segment, which carries the SCCP user's own
// identification (ITU-T Q.715 (04/02) §8.3.1.6).
//
// The error is ErrInvalidRequest or ErrSegmentation; the Result then holds
// only what the timers that were due produced.
func (s *Stack) Unitdata(req UnitdataRequest) (Result, error) {
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	s.expire(now, &res)
	transfers, err := s.unitdata(now, req)
	if err != nil {
		return res, err
	}
	res.Transfers = append(res.Transfers, transfers...)
	return res, nil
}

func (s *Stack) unitdata(now time.Time, req UnitdataRequest) ([]TransferRequest, error) {
	switch {
	case req.ProtocolClass > 1:
		return nil, fmt.Errorf("%w: protocol class %d is not connectionless", ErrInvalidRequest, req.ProtocolClass)
	case len(req.Data) == 0:
		return nil, fmt.Errorf("%w: no user data", ErrInvalidRequest)
	case len(req.Data) > MaxLongData:
		// §4.1.1.1.1: "If the length of the user data is greater than 3952
		// octets then the error treatment shall apply."
		return nil, fmt.Errorf("%w: %d octets of user data exceed %d", ErrSegmentation, len(req.Data), MaxLongData)
	}
	if err := checkCalledAddress(req.CalledPartyAddress); err != nil {
		return nil, err
	}
	if _, err := req.CallingPartyAddress.MarshalBinary(); err != nil {
		return nil, fmt.Errorf("%w: calling party address: %w", ErrInvalidRequest, err)
	}
	if !hasAddressElement(req.CallingPartyAddress) && !req.CallingPartyAddress.IndicatorOnly() {
		// ITU-T Q.713 (03/01) Tables 11 and 19, Note 2: an address carries an
		// element, or is the address indicator alone with bits 1-7 zero.
		return nil, fmt.Errorf("%w: calling party address without an address element", ErrInvalidRequest)
	}
	if err := checkImportance(req.Importance); err != nil {
		return nil, err
	}
	dpc := req.DPC
	if dpc == 0 {
		if !req.CalledPartyAddress.HasPointCode {
			return nil, fmt.Errorf("%w: no DPC and no point code in the called party address", ErrInvalidRequest)
		}
		dpc = uint32(req.CalledPartyAddress.PointCode)
	}
	label := RoutingLabel{OPC: s.cfg.PointCode, DPC: dpc, SLS: s.sls(req.ProtocolClass, req.SequenceControl)}
	limits := MarshalOptions{MaxMessageSize: s.cfg.MaxMessageSize}

	single := func() ([]TransferRequest, error) {
		m := s.singleMessage(req)
		b, err := limits.Marshal(m)
		if err != nil {
			return nil, err
		}
		return []TransferRequest{{RoutingLabel: label, Message: m, Data: b}}, nil
	}

	// §4.1.1.1.1: below Z the SCCP shall avoid segmentation.
	z := s.cfg.SegmentationThreshold
	if z == 0 || len(req.Data) < z {
		if transfers, err := single(); err == nil {
			return transfers, nil
		}
		// What remains possible is that the user data does not fit one
		// message: the parameters were checked above.
	}

	capacity := s.segmentCapacity(req)
	if z != 0 {
		capacity = min(capacity, z)
	}
	if capacity < 1 {
		return nil, fmt.Errorf("%w: the addresses leave no room for user data in a segment", ErrSegmentation)
	}
	n := (len(req.Data) + capacity - 1) / capacity
	if n > MaxSegments {
		return nil, fmt.Errorf("%w: %d octets need %d segments of at most %d octets, more than %d", ErrSegmentation, len(req.Data), n, capacity, MaxSegments)
	}
	if n == 1 {
		// Z asked for segmentation but one segment holds the user data: it
		// goes as one ordinary message of the requested class, since the
		// segmentation parameter "Should not be present in case of a single
		// XUDT message" (ITU-T Q.713 (03/01) Table 19 Note 2).
		return single()
	}
	ref, err := s.allocateSegmentationReference(now)
	if err != nil {
		return nil, err
	}

	transfers := make([]TransferRequest, 0, n)
	for i, chunk := range splitSegments(req.Data, n) {
		x := &XUDT{
			ProtocolClass:       params.ProtocolClass{Class: 1, ReturnOnError: req.ReturnOnError && i == 0},
			HopCounter:          s.cfg.HopCounter,
			CalledPartyAddress:  req.CalledPartyAddress.Clone(),
			CallingPartyAddress: req.CallingPartyAddress.Clone(),
			Data:                chunk,
			Segmentation: &params.Segmentation{
				FirstSegment:       i == 0,
				InSequenceDelivery: req.ProtocolClass == 1,
				RemainingSegments:  uint8(n - 1 - i),
				LocalReference:     ref,
			},
			Importance: limitImportance(MsgTypeXUDT, req.Importance),
		}
		b, err := limits.Marshal(x)
		if err != nil {
			// segmentCapacity encoded a segment of this size successfully.
			return nil, fmt.Errorf("%w: segment %d: %w", ErrSegmentation, i, err)
		}
		transfers = append(transfers, TransferRequest{RoutingLabel: label, Message: x, Data: b})
	}
	return transfers, nil
}

// singleMessage returns the unsegmented message for a request. It shares no
// memory with the request.
func (s *Stack) singleMessage(req UnitdataRequest) Message {
	class := params.ProtocolClass{Class: req.ProtocolClass, ReturnOnError: req.ReturnOnError}
	data := clone(req.Data)
	if !s.cfg.UseXUDT && req.Importance == nil {
		return &UDT{ProtocolClass: class, CalledPartyAddress: req.CalledPartyAddress.Clone(), CallingPartyAddress: req.CallingPartyAddress.Clone(), Data: data}
	}
	return &XUDT{ProtocolClass: class, HopCounter: s.cfg.HopCounter, CalledPartyAddress: req.CalledPartyAddress.Clone(),
		CallingPartyAddress: req.CallingPartyAddress.Clone(), Data: data, Importance: limitImportance(MsgTypeXUDT, req.Importance)}
}

// segmentCapacity returns the most user data one segment of the request can
// carry: Y of ITU-T Q.713 (03/01) Table 19 Note 1 for these addresses, at most
// 247 octets with three-octet addresses and the segmentation parameter. It is
// bounded by the data length indicator, the maximum message size and the
// one-octet pointer to the optional part.
func (s *Stack) segmentCapacity(req UnitdataRequest) int {
	limits := MarshalOptions{MaxMessageSize: s.cfg.MaxMessageSize}
	probe := &XUDT{
		ProtocolClass:       params.ProtocolClass{Class: 1, ReturnOnError: req.ReturnOnError},
		HopCounter:          s.cfg.HopCounter,
		CalledPartyAddress:  req.CalledPartyAddress,
		CallingPartyAddress: req.CallingPartyAddress,
		Segmentation:        &params.Segmentation{FirstSegment: true, RemainingSegments: params.MaxRemainingSegments},
		Importance:          req.Importance,
	}
	fits := func(n int) bool {
		probe.Data = make([]byte, n)
		_, err := limits.Marshal(probe)
		return err == nil
	}
	// Fitting is monotonic in the data length: find the largest n that fits.
	lo, hi := 0, maxData
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// splitSegments splits data into n segments as equal as possible, the longer
// ones first.
func splitSegments(data []byte, n int) [][]byte {
	q, r := len(data)/n, len(data)%n
	out := make([][]byte, n)
	off := 0
	for i := range out {
		size := q
		if i < r {
			size++
		}
		out[i] = clone(data[off : off+size])
		off += size
	}
	return out
}

func cloneImportance(i *params.Importance) *params.Importance {
	if i == nil {
		return nil
	}
	return new(*i)
}

// checkCalledAddress refuses a called party address the originating node must
// not send, ITU-T Q.714 (05/01) §2.2.2.1: routed on SSN, it carries a subsystem
// number other than 0 (item 2; §3.8.3.3 b6); with SSN 0 or none it carries a
// global title and is routed on GT (item 3: "the routing indicator shall be
// set to "Route on GT""), SSN 0 without a global title being incomplete (item
// 5); routed on GT, it carries a global title (§3.8.3.3 b7).
func checkCalledAddress(a params.PartyAddress) error {
	if _, err := a.MarshalBinary(); err != nil {
		return fmt.Errorf("%w: called party address: %w", ErrInvalidRequest, err)
	}
	switch {
	case a.RouteOnSSN && (!a.HasSSN || a.SSN == 0):
		return fmt.Errorf("%w: called party address routed on SSN without a subsystem number", ErrInvalidRequest)
	case !a.RouteOnSSN && a.GlobalTitle == nil:
		return fmt.Errorf("%w: called party address routed on GT without GT", ErrInvalidRequest)
	}
	return nil
}

// checkImportance refuses an importance the parameter cannot carry, ITU-T
// Q.713 (03/01) §3.19.
func checkImportance(i *params.Importance) error {
	if i == nil {
		return nil
	}
	if _, err := i.MarshalBinary(); err != nil {
		return fmt.Errorf("%w: importance: %w", ErrInvalidRequest, err)
	}
	return nil
}

// limitImportance returns the importance of a message produced by an SCCP
// user's primitive: the requested one, at most the maximum of ITU-T Q.714
// (05/01) Table 2 for the message type (§2.6.2 a)). Without a request the
// parameter stays absent and the default of Table 2 applies. The result is a
// copy.
func limitImportance(t MsgType, i *params.Importance) *params.Importance {
	if i == nil {
		return nil
	}
	limit := params.Importance(6) // DT1, RLSD, UDT, XUDT, LUDT
	switch t {
	case MsgTypeCR, MsgTypeCC, MsgTypeCREF:
		limit = 4
	}
	return new(min(*i, limit))
}

type frozenReference struct {
	ref   uint32
	until time.Time
}

// allocateSegmentationReference returns a 24-bit segmentation local reference
// that is not frozen, and freezes it (ITU-T Q.714 (05/01) §4.1.1.1.2; the
// mechanism is implementation dependent, footnote 7).
func (s *Stack) allocateSegmentationReference(now time.Time) (uint32, error) {
	i := 0
	for i < len(s.frozenSegmentationOrder) && !s.frozenSegmentationOrder[i].until.After(now) {
		f := s.frozenSegmentationOrder[i]
		if until, ok := s.frozenSegmentation[f.ref]; ok && until.Equal(f.until) {
			delete(s.frozenSegmentation, f.ref)
		}
		i++
	}
	s.frozenSegmentationOrder = slices.Delete(s.frozenSegmentationOrder, 0, i)

	if len(s.frozenSegmentation) > params.MaxSegmentationLocalReference {
		return 0, fmt.Errorf("%w: every segmentation local reference is frozen", ErrSegmentation)
	}
	for {
		ref := s.nextSegmentationReference & params.MaxSegmentationLocalReference
		s.nextSegmentationReference = (ref + 1) & params.MaxSegmentationLocalReference
		if _, frozen := s.frozenSegmentation[ref]; frozen {
			continue
		}
		until := now.Add(s.cfg.SegmentationReferenceFreeze)
		s.frozenSegmentation[ref] = until
		s.frozenSegmentationOrder = append(s.frozenSegmentationOrder, frozenReference{ref, until})
		return ref, nil
	}
}

// unitdata holds the fields the connectionless procedures use from a UDT,
// XUDT or LUDT.
type unitdata struct {
	label        RoutingLabel
	typ          MsgType
	class        params.ProtocolClass
	hop          params.HopCounter
	hasHop       bool
	called       params.PartyAddress
	calling      params.PartyAddress
	data         []byte
	segmentation *params.Segmentation
	importance   *params.Importance
}

func unitdataOf(label RoutingLabel, m Message) unitdata {
	switch m := m.(type) {
	case *UDT:
		return unitdata{label: label, typ: MsgTypeUDT, class: m.ProtocolClass, called: m.CalledPartyAddress, calling: m.CallingPartyAddress, data: m.Data}
	case *XUDT:
		return unitdata{label: label, typ: MsgTypeXUDT, class: m.ProtocolClass, hop: m.HopCounter, hasHop: true, called: m.CalledPartyAddress,
			calling: m.CallingPartyAddress, data: m.Data, segmentation: m.Segmentation, importance: m.Importance}
	case *LUDT:
		return unitdata{label: label, typ: MsgTypeLUDT, class: m.ProtocolClass, hop: m.HopCounter, hasHop: true, called: m.CalledPartyAddress,
			calling: m.CallingPartyAddress, data: m.Data, segmentation: m.Segmentation, importance: m.Importance}
	}
	panic(fmt.Sprintf("sccp: unitdataOf(%T)", m))
}

// routing outcomes
type routeVerdict int

const (
	routeDeliver routeVerdict = iota
	routeReturn
	routeDiscard
)

// route applies ITU-T Q.714 (05/01) §2.3.1 to a message the MTP delivered to
// this node. local is the final translation of a global title at this node
// (StackConfig.LocalGlobalTitle). The reason accompanies routeDiscard.
func route(called params.PartyAddress, hop params.HopCounter, hasHop, local bool) (uint8, params.ReturnCause, routeVerdict, error) {
	if called.RouteOnSSN {
		switch {
		case !called.HasSSN:
			// §3.8.3.3 b6: "no SSN is included although the routing
			// indicator indicates 'Route on SSN'", a syntax error.
			return 0, 0, routeDiscard, fmt.Errorf("%w: called party address routed on SSN without SSN", ErrSyntax)
		case called.SSN != 0:
			return called.SSN, 0, routeDeliver, nil
		case called.GlobalTitle == nil:
			// §2.2.2.1 item 4: SSN 0 and no GT, incomplete address.
			return 0, 0, routeDiscard, ErrIncompleteAddress
		}
		// §2.2.2.1 item 2: a global title and SSN 0 go to translation.
	} else if called.GlobalTitle == nil {
		// §3.8.3.3 b7: "no GT is included although the routing indicator
		// indicates 'Route on GT'", a syntax error.
		return 0, 0, routeDiscard, fmt.Errorf("%w: called party address routed on GT without GT", ErrSyntax)
	}
	// §2.3.1 item 3: the hop counter is decremented and a value of zero is a
	// hop counter violation (§2.8.6).
	if hasHop && hop <= 1 {
		return 0, params.ReturnCauseHopCounterViolation, routeReturn, nil
	}
	if !local || !called.HasSSN || called.SSN == 0 {
		// §2.8.2: no translation for this address, or a translation to route
		// on SSN while no SSN is present.
		return 0, params.ReturnCauseNoTranslationForThisSpecificAddress, routeReturn, nil
	}
	return called.SSN, 0, routeDeliver, nil
}

// subsystemCause reports whether a local subsystem can receive a message, and
// the return cause when it cannot.
func (s *Stack) subsystemCause(ssn uint8) (params.ReturnCause, bool) {
	available, registered := s.subsystems[ssn]
	switch {
	case !registered:
		return params.ReturnCauseUnequippedUser, false // §2.8.5
	case !available:
		return params.ReturnCauseSubsystemFailure, false // §2.8.3 item 3)
	}
	return 0, true
}

func (s *Stack) handleUnitdata(now time.Time, label RoutingLabel, m Message, local bool, res *Result) {
	u := unitdataOf(label, m)
	switch {
	case u.class.Class > 1:
		res.discard(label, u.typ, fmt.Errorf("%w: protocol class %d in a connectionless message", ErrSyntax, u.class.Class))
		return
	case len(u.data) == 0:
		// The data parameter carries at least one octet (ITU-T Q.713 (03/01)
		// Tables 11, 19 and 21); an empty one is a construction error (Q.714
		// (05/01) §3.8.3.3 b1), discarded (§4.3), with no user data to deliver.
		res.discard(label, u.typ, fmt.Errorf("%w: no user data", ErrSyntax))
		return
	}
	ssn, cause, verdict, reason := route(u.called, u.hop, u.hasHop, local)
	switch verdict {
	case routeDiscard:
		res.discard(label, u.typ, reason)
		return
	case routeReturn:
		s.reject(&u, u.class.ReturnOnError, cause, undeliverable(cause), res)
		alertHopCounter(label, u.typ, cause, res)
		return
	}

	seg := u.segmentation
	if seg != nil && (!seg.FirstSegment || seg.RemainingSegments != 0) {
		s.reassemble(now, &u, ssn, res)
		return
	}
	if cause, ok := s.subsystemCause(ssn); !ok {
		s.reject(&u, u.class.ReturnOnError, cause, undeliverable(cause), res)
		return
	}
	res.Indications = append(res.Indications, &UnitdataIndication{
		RoutingLabel:        label,
		MessageType:         u.typ,
		CalledPartyAddress:  u.called,
		CallingPartyAddress: u.calling,
		ProtocolClass:       u.class.Class,
		ReturnOnError:       u.class.ReturnOnError,
		Importance:          u.importance,
		Data:                u.data,
		Segments:            1,
	})
}

// alertHopCounter alerts maintenance to a hop counter violation, ITU-T Q.714
// (05/01) §2.3.1 item 3: "In addition, maintenance functions are alerted".
func alertHopCounter(label RoutingLabel, t MsgType, cause params.ReturnCause, res *Result) {
	if cause == params.ReturnCauseHopCounterViolation {
		res.Indications = append(res.Indications, &MaintenanceIndication{RoutingLabel: label, MessageType: t, Reason: ErrHopCounterViolation})
	}
}

func undeliverable(cause params.ReturnCause) error {
	return fmt.Errorf("%w: %s", ErrUndeliverable, cause)
}

// reject records an undeliverable message as discarded and runs the message
// return procedure for it when return on error was requested.
func (s *Stack) reject(u *unitdata, returnOnError bool, cause params.ReturnCause, reason error, res *Result) {
	res.discard(u.label, u.typ, reason)
	if returnOnError {
		s.returnMessage(u, cause, res)
	}
}

// returnMessage runs the message return procedure, ITU-T Q.714 (05/01) §4.2,
// for an undeliverable UDT, XUDT or LUDT that requested it: a UDTS, XUDTS or
// LUDTS goes back to the originating point with the addresses swapped, a
// fresh hop counter and the user data, truncated when the service message
// would exceed the maximum message size (§4.2, last paragraph; §4.1.2). A
// returned segment keeps its segmentation parameter (§4.1.1.2.3: "The
// reassembly function will never change the segment number of the segments
// to be returned").
func (s *Stack) returnMessage(u *unitdata, cause params.ReturnCause, res *Result) {
	var seg *params.Segmentation
	if u.segmentation != nil {
		seg = new(*u.segmentation)
	}
	xudts := func(data []byte) Message {
		return &XUDTS{ReturnCause: cause, HopCounter: s.cfg.HopCounter, CalledPartyAddress: u.calling, CallingPartyAddress: u.called, Data: data, Segmentation: seg}
	}
	var build func([]byte) Message
	switch u.typ {
	case MsgTypeUDT:
		build = func(data []byte) Message {
			return &UDTS{ReturnCause: cause, CalledPartyAddress: u.calling, CallingPartyAddress: u.called, Data: data}
		}
	case MsgTypeXUDT:
		build = xudts
	case MsgTypeLUDT:
		build = func(data []byte) Message {
			return &LUDTS{ReturnCause: cause, HopCounter: s.cfg.HopCounter, CalledPartyAddress: u.calling, CallingPartyAddress: u.called, Data: data, Segmentation: seg}
		}
	}
	// An LUDT comes back as an LUDTS (§4.2: "an LUDTS message should be used
	// in response to an LUDT message"), truncated to a short LUDTS when it
	// does not fit (ITU-T Q.715 (04/02) §8.3.1.6). Only when no LUDTS can be
	// built does it come back as an XUDTS, truncated the same way (§4.2: "the
	// user data will be truncated to fit into one XUDTS message").
	m, b, err := s.fitReturn(build, u.data)
	if err != nil && u.typ == MsgTypeLUDT {
		m, b, err = s.fitReturn(xudts, u.data)
	}
	if err != nil {
		res.discard(u.label, u.typ, fmt.Errorf("%w: %s, and the service message cannot be built: %w", ErrUndeliverable, cause, err))
		return
	}
	res.Transfers = append(res.Transfers, TransferRequest{RoutingLabel: originLabel(u.label, u.calling), Message: m, Data: b})
}

// fitReturn builds a service message with as much of data as fits the maximum
// message size.
func (s *Stack) fitReturn(build func([]byte) Message, data []byte) (Message, []byte, error) {
	limits := MarshalOptions{MaxMessageSize: s.cfg.MaxMessageSize}
	if len(data) == 0 {
		// An empty data parameter cannot be encoded; nothing to return.
		return nil, nil, fmt.Errorf("%w: no user data to return", ErrInvalidMessage)
	}
	m := build(clone(data))
	b, err := limits.Marshal(m)
	if err == nil {
		return m, b, nil
	}
	if !errors.Is(err, ErrMessageTooLong) && !errors.Is(err, ErrInvalidMessage) {
		return nil, nil, err
	}
	// The longest prefix that encodes: fitting is monotonic in the length.
	lo, hi := 0, min(len(data), MaxLongData)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if _, err := limits.Marshal(build(data[:mid])); err == nil {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return nil, nil, err
	}
	m = build(clone(data[:lo]))
	b, err = limits.Marshal(m)
	return m, b, err
}

// handleService delivers a received UDTS, XUDTS or LUDTS to the local user as
// an N-NOTICE indication (ITU-T Q.714 (05/01) §4.2). A service message is
// never returned: when it cannot be delivered it is discarded. Returned
// segments are not reassembled, which §4.2 leaves to the implementation.
func (s *Stack) handleService(label RoutingLabel, m Message, local bool, res *Result) {
	n := &NoticeIndication{RoutingLabel: label, MessageType: m.MessageType()}
	var hop params.HopCounter
	switch m := m.(type) {
	case *UDTS:
		n.ReasonForReturn, n.CalledPartyAddress, n.CallingPartyAddress, n.Data = m.ReturnCause, m.CalledPartyAddress, m.CallingPartyAddress, m.Data
	case *XUDTS:
		n.ReasonForReturn, n.CalledPartyAddress, n.CallingPartyAddress, n.Data = m.ReturnCause, m.CalledPartyAddress, m.CallingPartyAddress, m.Data
		n.Importance, n.Segmentation, hop = m.Importance, m.Segmentation, m.HopCounter
	case *LUDTS:
		n.ReasonForReturn, n.CalledPartyAddress, n.CallingPartyAddress, n.Data = m.ReturnCause, m.CalledPartyAddress, m.CallingPartyAddress, m.Data
		n.Importance, n.Segmentation, hop = m.Importance, m.Segmentation, m.HopCounter
	}
	ssn, cause, verdict, reason := route(n.CalledPartyAddress, hop, n.MessageType != MsgTypeUDTS, local)
	switch verdict {
	case routeDiscard:
		res.discard(label, n.MessageType, reason)
		return
	case routeReturn:
		res.discard(label, n.MessageType, undeliverable(cause))
		alertHopCounter(label, n.MessageType, cause, res)
		return
	}
	// §2.3.1 item 2 b): a message for an unavailable subsystem goes to the
	// return procedure, which discards a service message (§4.2 b)).
	if cause, ok := s.subsystemCause(ssn); !ok {
		res.discard(label, n.MessageType, undeliverable(cause))
		return
	}
	res.Indications = append(res.Indications, n)
}

// reassemblyKey identifies a reassembly process by "the unique combination of
// the Calling Party Address, MTP routing information, and the Segmentation
// Local Reference" (ITU-T Q.714 (05/01) §4.1.1.2.2). The MTP routing
// information is the whole routing label: §4.1.1.1.2 sends every segment with
// "identical MTP routing information (DPC, SLS)".
type reassemblyKey struct {
	label     RoutingLabel
	calling   string
	reference uint32
}

type reassembly struct {
	key   reassemblyKey
	first unitdata // the first segment, with its data at data[:firstLen]
	ssn   uint8

	expected      uint8 // remaining segment count of the next segment
	segments      int
	bound         int // first segment length x number of segments
	firstLen      int
	data          []byte // the user data so far, the only copy kept
	returnOnError bool
	timer         *timer

	// failed marks the record of a message whose reassembly failed: its
	// other segments are discarded, since "only one XUDTS/LUDTS message will
	// be sent" for a reassembly error (§4.2). The record holds no data and
	// lasts T(reassembly). orphan marks the record of a message that had no
	// process: its first segment was refused or never arrived. firstKnown
	// and firstHash identify the first segment of the failed message, so
	// that a duplicate of it is told from the first segment of a new message
	// with the same reference.
	failed, orphan, firstKnown bool
	firstHash                  uint64
}

// segmentHash identifies a segment by its user data and remaining segment
// count, with a seed of the stack's own.
func (s *Stack) segmentHash(u *unitdata) uint64 {
	var h maphash.Hash
	h.SetSeed(s.segmentSeed)
	_, _ = h.Write(u.data)
	_ = h.WriteByte(u.segmentation.RemainingSegments)
	return h.Sum64()
}

// firstSegment returns the first segment as received, for the message return
// procedure.
func (p *reassembly) firstSegment() *unitdata {
	u := p.first
	u.data = p.data[:p.firstLen]
	return &u
}

// reassemble runs ITU-T Q.714 (05/01) §4.1.1.2 for a segment. The subsystem
// is checked for every segment (§2.3.1 item 2 b)): a segment for an
// unavailable subsystem cannot be delivered, and neither can the message it
// belongs to.
func (s *Stack) reassemble(now time.Time, u *unitdata, ssn uint8, res *Result) {
	calling, err := u.calling.MarshalBinary()
	if err != nil {
		res.discard(u.label, u.typ, fmt.Errorf("%w: calling party address: %w", ErrReassembly, err))
		return
	}
	seg := u.segmentation
	key := reassemblyKey{label: u.label, calling: string(calling), reference: seg.LocalReference}
	p := s.reassemblies[key]

	if p != nil && p.failed {
		if !seg.FirstSegment || !p.firstKnown || p.firstHash == s.segmentHash(u) {
			// The rest of a failed message, duplicates included, and its
			// first segment when that was never seen: discarded without
			// another return.
			res.discard(u.label, u.typ, fmt.Errorf("%w: segment of a failed message, reference %#06x", ErrReassembly, key.reference))
			return
		}
		// Another first segment starts a new message with the reference.
		s.endReassembly(p)
		p = nil
	}

	if seg.FirstSegment && p != nil {
		// §4.1.1.2.1: a first segment for a busy reference discards the
		// segments received so far and returns the new message.
		s.endReassembly(p)
		res.discard(p.first.label, p.first.typ, fmt.Errorf("%w: a new first segment arrived for reference %#06x", ErrReassembly, key.reference))
		cause, reason := params.ReturnCauseErrorInMessageTransport, fmt.Errorf("%w: first segment for reference %#06x, which is busy", ErrReassembly, key.reference)
		if c, ok := s.subsystemCause(ssn); !ok {
			cause, reason = c, undeliverable(c)
		}
		s.rejectSegment(now, key, u, cause, reason, res)
		return
	}
	if cause, ok := s.subsystemCause(ssn); !ok {
		if p == nil {
			s.rejectSegment(now, key, u, cause, undeliverable(cause), res)
			return
		}
		// The process returns its first segment, once, when any of its
		// segments asked for it (§4.1.1.2.3).
		p.returnOnError = p.returnOnError || u.class.ReturnOnError
		s.failReassembly(now, p, cause, undeliverable(cause), res)
		res.discard(u.label, u.typ, undeliverable(cause))
		return
	}

	if seg.FirstSegment {
		if s.reassemblySlots >= s.cfg.MaxReassemblies || !s.roomFor(key.label.OPC, len(u.data)) {
			// §4.1.1.2.3.4: no buffer space to perform reassembly.
			s.rejectSegment(now, key, u, params.ReturnCauseNetworkCongestion,
				fmt.Errorf("%w: no buffer space for reference %#06x", ErrReassembly, key.reference), res)
			return
		}
		segments := int(seg.RemainingSegments) + 1
		// Buffers hold exactly the octets received, so that the accounting
		// is of the memory held.
		buffer := make([]byte, len(u.data))
		copy(buffer, u.data)
		p = &reassembly{
			key:           key,
			first:         *u,
			ssn:           ssn,
			expected:      seg.RemainingSegments - 1,
			segments:      segments,
			bound:         len(u.data) * segments, // §4.1.1.2.1
			firstLen:      len(u.data),
			data:          buffer,
			returnOnError: u.class.ReturnOnError,
		}
		p.first.data = nil
		s.reassemblies[key] = p
		s.reassemblySlots++
		s.hold(key.label.OPC, len(u.data))
		p.timer = s.schedule(now.Add(s.cfg.ReassemblyTimeout), func(now time.Time, res *Result) {
			// §4.1.1.2.1 and §4.1.1.2.3.2: T(reassembly) expiry discards the
			// message and is an error in message transport.
			s.failReassembly(now, p, params.ReturnCauseErrorInMessageTransport,
				fmt.Errorf("%w: T(reassembly) expired with %d of %d segments", ErrReassembly, p.segments-int(p.expected)-1, p.segments), res)
		})
		return
	}

	if p == nil {
		// §4.1.1.2.2: no association is possible, the segment is discarded.
		// §4.1.1.2.3.2 names it an error in message transport, returned when
		// the segment asks for it.
		s.rejectSegment(now, key, u, params.ReturnCauseErrorInMessageTransport,
			fmt.Errorf("%w: segment for reference %#06x with no reassembly in progress", ErrReassembly, key.reference), res)
		return
	}
	p.returnOnError = p.returnOnError || u.class.ReturnOnError
	total := len(p.data) + len(u.data)
	switch {
	case seg.RemainingSegments != p.expected:
		// §4.1.1.2.2: out of sequence or duplicate: return, discard.
		s.failReassembly(now, p, params.ReturnCauseErrorInMessageTransport,
			fmt.Errorf("%w: segment with %d remaining, want %d", ErrReassembly, seg.RemainingSegments, p.expected), res)
		res.discard(u.label, u.typ, fmt.Errorf("%w: out of sequence", ErrReassembly))
		return
	case total > p.bound:
		// §4.1.1.2.3.2: "the message length received exceeds the number of
		// segments times the length of the first segment".
		s.failReassembly(now, p, params.ReturnCauseErrorInMessageTransport,
			fmt.Errorf("%w: %d octets exceed %d segments of %d octets", ErrReassembly, total, p.segments, p.firstLen), res)
		res.discard(u.label, u.typ, fmt.Errorf("%w: segment too long", ErrReassembly))
		return
	case total > MaxLongData:
		// §4.1.1.2.3.1: reassembly of more than the maximum supported data
		// length, 3952 octets (clause 4, §4.1.1.1.1).
		s.failReassembly(now, p, params.ReturnCauseDestinationCannotPerformReassembly,
			fmt.Errorf("%w: %d octets exceed %d", ErrReassembly, total, MaxLongData), res)
		res.discard(u.label, u.typ, fmt.Errorf("%w: message too long", ErrReassembly))
		return
	case !s.roomFor(key.label.OPC, len(u.data)):
		// §4.1.1.2.3.4: no buffer space to perform reassembly.
		s.failReassembly(now, p, params.ReturnCauseNetworkCongestion,
			fmt.Errorf("%w: no buffer space for reference %#06x", ErrReassembly, key.reference), res)
		res.discard(u.label, u.typ, fmt.Errorf("%w: no buffer space for reference %#06x", ErrReassembly, key.reference))
		return
	}
	// The buffer grows to the exact length, so that MaxReassemblyOctets bounds
	// the memory held and not only the octets received.
	data := make([]byte, total)
	copy(data[copy(data, p.data):], u.data)
	p.data = data
	s.hold(key.label.OPC, len(u.data))
	if seg.RemainingSegments > 0 {
		p.expected--
		return
	}

	// §4.1.1.2.2: the last segment completes the message.
	s.endReassembly(p)
	class := uint8(0)
	if p.first.segmentation.InSequenceDelivery {
		class = 1
	}
	res.Indications = append(res.Indications, &UnitdataIndication{
		RoutingLabel:        p.first.label,
		MessageType:         p.first.typ,
		CalledPartyAddress:  p.first.called,
		CallingPartyAddress: p.first.calling,
		ProtocolClass:       class,
		ReturnOnError:       p.returnOnError,
		Importance:          p.first.importance,
		Data:                p.data,
		Segments:            p.segments,
	})
}

// The records of failed messages keep their other segments, duplicates
// included, from another return for T(reassembly) (ITU-T Q.714 (05/01) §4.2:
// "only one XUDTS/LUDTS message will be sent"). A record is never ended early
// to make room: that would let its message be returned twice.

// rejectSegment runs the message return procedure for a segment of a message
// that has no process, its first segment refused or never received, and
// records the message as failed. When no record is left, the segment is
// discarded without a return.
func (s *Stack) rejectSegment(now time.Time, key reassemblyKey, u *unitdata, cause params.ReturnCause, reason error, res *Result) {
	if s.orphanRecords >= s.cfg.MaxReassemblies {
		res.discard(u.label, u.typ, fmt.Errorf("%w, not returned: no room to record the failed message", reason))
		return
	}
	s.reject(u, u.class.ReturnOnError, cause, reason, res)
	p := &reassembly{key: key, failed: true, orphan: true, firstKnown: u.segmentation.FirstSegment}
	if p.firstKnown {
		p.firstHash = s.segmentHash(u)
	}
	s.reassemblies[key] = p
	s.orphanRecords++
	p.timer = s.schedule(now.Add(s.cfg.ReassemblyTimeout), func(time.Time, *Result) { s.endReassembly(p) })
}

// failReassembly ends a process: it returns the first segment, with cause,
// when any of its segments asked for it (ITU-T Q.714 (05/01) §4.1.1.2.3), and
// the process becomes the record of its message in its own slot, so that
// every message admitted to reassembly is recorded.
func (s *Stack) failReassembly(now time.Time, p *reassembly, cause params.ReturnCause, reason error, res *Result) {
	first := p.firstSegment()
	s.reject(first, p.returnOnError, cause, reason, res)
	s.hold(p.key.label.OPC, -len(p.data))
	*p = reassembly{key: p.key, failed: true, firstKnown: true, firstHash: s.segmentHash(first), timer: p.timer}
	s.stop(p.timer)
	p.timer = s.schedule(now.Add(s.cfg.ReassemblyTimeout), func(time.Time, *Result) { s.endReassembly(p) })
}

func (s *Stack) endReassembly(p *reassembly) {
	if s.reassemblies[p.key] != p {
		return
	}
	delete(s.reassemblies, p.key)
	if p.orphan {
		s.orphanRecords--
	} else {
		s.reassemblySlots--
		s.hold(p.key.label.OPC, -len(p.data))
	}
	s.stop(p.timer)
}

// roomFor reports whether the processes can hold n more octets of user data
// from the signalling point opc: within MaxReassemblyOctets and
// MaxReassemblyOctetsPerPeer (ITU-T Q.714 (05/01) §4.1.1.2.3.4).
func (s *Stack) roomFor(opc uint32, n int) bool {
	perPeer := s.cfg.MaxReassemblyOctetsPerPeer
	return s.reassemblyOctets+n <= s.cfg.MaxReassemblyOctets && (perPeer == 0 || s.peerOctets[opc]+n <= perPeer)
}

// hold accounts for n more octets of user data held for the signalling point
// opc, or fewer when n is negative.
func (s *Stack) hold(opc uint32, n int) {
	s.reassemblyOctets += n
	if v := s.peerOctets[opc] + n; v != 0 {
		s.peerOctets[opc] = v
	} else {
		delete(s.peerOctets, opc)
	}
}
