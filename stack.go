// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"hash/maphash"
	"sync"
	"time"

	"github.com/gomaja/go-pointcode"
	"github.com/gomaja/go-sccp/params"
)

// RoutingLabel is the MTP routing information of a message: the routing label
// (ITU-T Q.704 (07/96) §2.2) and the network indicator of the SIO (§14.2.2),
// as MTP-TRANSFER (ITU-T Q.711 (03/01) Table 18) and M3UA protocol data
// (RFC 4666 §3.3.1) carry them. Point codes are meaningful only in the network
// NI names (ITU-T Q.711 (03/01) §6.1.1.2.2).
type RoutingLabel struct {
	// OPC and DPC are 14-bit originating and destination point codes,
	// ITU-T Q.704 (07/96) §§2.2.2-2.2.3.
	OPC uint32
	DPC uint32
	// SLS is the 4-bit signalling link selection, ITU-T Q.704 (07/96) §2.2.4.
	SLS uint8
	// NI is the 2-bit network indicator, ITU-T Q.704 (07/96) §14.2.2.
	NI pointcode.NetworkIndicator
}

// TransferIndication is an MTP-TRANSFER indication, ITU-T Q.711 (03/01)
// §7.2.1: an SCCP message received from the MTP with its routing information.
type TransferIndication struct {
	RoutingLabel
	// Data is the encoded SCCP message. The stack does not retain it.
	Data []byte
}

// TransferRequest is an MTP-TRANSFER request, ITU-T Q.711 (03/01) §7.2.1: an
// SCCP message for the MTP to send with the given routing information.
type TransferRequest struct {
	RoutingLabel
	// Message is the message Data encodes.
	Message Message
	// Data is the encoded message, within the stack's maximum message size.
	Data []byte
}

// Indication is a primitive the stack delivers to an SCCP user, or an alert
// for the maintenance function (MaintenanceIndication). Its concrete type is
// one of the *Indication types of this package.
type Indication interface {
	indication()
}

// UnitdataIndication is an N-UNITDATA indication, ITU-T Q.711 (03/01) Table
// 12: user data received from the connectionless service, after reassembly of
// a segmented message.
type UnitdataIndication struct {
	// RoutingLabel is the label of the message, or of the first segment.
	RoutingLabel
	// MessageType is UDT, XUDT or LUDT.
	MessageType         MsgType
	CalledPartyAddress  params.PartyAddress
	CallingPartyAddress params.PartyAddress
	// ProtocolClass is 1 when the sender asked for in-sequence delivery. For a
	// reassembled message it is the in-sequence delivery option of its
	// segmentation parameter (ITU-T Q.714 (05/01) §4.1.1.2.2).
	ProtocolClass uint8
	ReturnOnError bool
	Importance    *params.Importance
	Data          []byte
	// Segments is the number of segments reassembled, 1 for a message that
	// was not segmented.
	Segments int
}

// NoticeIndication is an N-NOTICE indication, ITU-T Q.711 (03/01) Table 13:
// user data returned by the message return procedure (ITU-T Q.714 (05/01)
// §4.2).
type NoticeIndication struct {
	RoutingLabel
	// MessageType is the service message received: UDTS, XUDTS or LUDTS.
	MessageType         MsgType
	CalledPartyAddress  params.PartyAddress
	CallingPartyAddress params.PartyAddress
	ReasonForReturn     params.ReturnCause
	Importance          *params.Importance
	// Data is the returned user data. It may be incomplete: a segment, or a
	// truncated message (ITU-T Q.711 (03/01) §6.2.2.2).
	Data []byte
	// Segmentation is the segmentation parameter of a returned segment.
	Segmentation *params.Segmentation
}

// MaintenanceIndication alerts the maintenance function to an event the SCCP
// user is not told about: a message whose hop counter reached zero (ITU-T
// Q.714 (05/01) §2.3.1 item 3: "In addition, maintenance functions are
// alerted"), or a connection release that never completed (Figure C.2 sheet
// 6: "Inform maintenance").
type MaintenanceIndication struct {
	// RoutingLabel is the label of the message received, or for a release
	// the label of the connection section.
	RoutingLabel
	// MessageType is the message concerned: the one received, or the RLSD
	// that got no release complete.
	MessageType MsgType
	// LocalReference and RemoteReference identify the connection section of
	// a release, and are zero otherwise.
	LocalReference, RemoteReference params.LocalReference
	// Reason matches ErrHopCounterViolation or ErrReleaseIncomplete with
	// errors.Is.
	Reason error
}

func (*UnitdataIndication) indication()    {}
func (*NoticeIndication) indication()      {}
func (*MaintenanceIndication) indication() {}

// Discard reports a received message the stack discarded, and why.
type Discard struct {
	RoutingLabel
	// MessageType is the type of the discarded message. For a message that
	// could not be decoded it is the first octet received, 0 when there was
	// none.
	MessageType MsgType
	// Reason matches one of the reasons below with errors.Is,
	// ErrInvalidRequest for invalid MTP routing information, or for the
	// connection-oriented procedures ErrConnectionNotFound,
	// ErrConnectionReference or ErrConnectionState.
	Reason error
}

// Result is what the stack produced for one event: the messages to transfer,
// in order, the indications for the SCCP users, in order, and the discards.
type Result struct {
	Transfers   []TransferRequest
	Indications []Indication
	Discards    []Discard
}

// Reasons for a Discard. They match with errors.Is.
var (
	// ErrSyntax: the message could not be decoded, or it carries values that
	// make it unusable (ITU-T Q.714 (05/01) §3.8.3.3; discarded by §3.8.4 and
	// §4.3).
	ErrSyntax = errors.New("sccp: syntax error")

	// ErrIncompleteAddress: a called party address routed on SSN with SSN 0
	// and no global title (ITU-T Q.714 (05/01) §2.2.2.1 item 4).
	ErrIncompleteAddress = errors.New("sccp: incomplete called party address")

	// ErrUndeliverable: the message could not be delivered (ITU-T Q.714
	// (05/01) §2.8), or the stack could not send its response. A UDT, XUDT or
	// LUDT that asked for it is returned as well (§4.2).
	ErrUndeliverable = errors.New("sccp: message undeliverable")

	// ErrReassembly: a segment could not be reassembled (ITU-T Q.714 (05/01)
	// §4.1.1.2).
	ErrReassembly = errors.New("sccp: reassembly failed")

	// ErrNotSupported: the stack does not run the procedure the message needs.
	ErrNotSupported = errors.New("sccp: procedure not supported")
)

// Reasons for a MaintenanceIndication. They match with errors.Is.
var (
	// ErrHopCounterViolation: a message routed on global title reached hop
	// counter zero, "an indication that an excessive routing could be
	// present" (ITU-T Q.714 (05/01) §2.8.6).
	ErrHopCounterViolation = errors.New("sccp: hop counter violation")

	// ErrReleaseIncomplete: a connection section was released without a
	// release complete, when T(int) expired (ITU-T Q.714 (05/01) §3.3.3.2).
	ErrReleaseIncomplete = errors.New("sccp: release incomplete")
)

// Errors returned for requests.
var (
	// ErrInvalidRequest reports a primitive whose parameters the stack refuses,
	// or a Discard for received MTP routing information outside its field widths.
	ErrInvalidRequest = errors.New("sccp: invalid request")

	// ErrSegmentation reports user data that cannot be segmented: longer than
	// 3952 octets, or needing more than 16 segments (ITU-T Q.714 (05/01)
	// §4.1.1.1).
	ErrSegmentation = errors.New("sccp: segmentation failed")
)

// minMessageSize is the smallest maximum message size: an IT, ITU-T Q.713
// (03/01) Table 18, the longest of the messages without user data that the
// connection-oriented procedures send (CC, CREF, RLSD, RLC, ERR, IT).
const minMessageSize = 11

// Default timer values and limits.
const (
	// DefaultReassemblyTimeout is T(reassembly), which ITU-T Q.714 (05/01)
	// Annex C.4 sets to 10 to 20 seconds.
	DefaultReassemblyTimeout = 15 * time.Second

	// DefaultSegmentationReferenceFreeze keeps a segmentation local reference
	// out of use after its last segment was sent, long enough for the far
	// end's T(reassembly) to end (ITU-T Q.714 (05/01) §4.1.1.1.2: the reference
	// "shall be frozen until the completion of the reassembly process").
	DefaultSegmentationReferenceFreeze = 20 * time.Second

	// DefaultMaxReassemblies bounds the reassembly processes in progress.
	DefaultMaxReassemblies = 1024

	// DefaultMaxReassemblyOctets bounds the user data buffered by them.
	DefaultMaxReassemblyOctets = 4 << 20

	// MinSegmentationThreshold is the smallest Z, ITU-T Q.714 (05/01)
	// §4.1.1.1.1 footnote 6: 160 <= Z <= Y.
	MinSegmentationThreshold = 160
)

// StackConfig configures a Stack. The zero value of every field selects its
// default.
type StackConfig struct {
	// PointCode is the signalling point code of this node: the OPC of the
	// messages it originates. It has 14 bits, ITU-T Q.704 (07/96) §2.2.3.
	PointCode uint32

	// NetworkIndicator is the network of messages originated for N-UNITDATA
	// and N-CONNECT requests. Zero is pointcode.NIInternational. The stack
	// answers a received message in the network it came from. A node
	// originating in several networks runs one Stack per network, each with
	// its point code there (ITU-T Q.711 (03/01) §6.1.1.2.2).
	NetworkIndicator pointcode.NetworkIndicator

	// MaxMessageSize is the largest SCCP message the stack sends, in octets.
	// Zero selects DefaultMaxMessageSize, the narrowband MTP-SAP of ITU-T
	// Q.711 (03/01) §7.1 a). Larger user data is segmented. It is at least
	// 11 octets, an IT (ITU-T Q.713 (03/01) Table 18), so that every message
	// the connection-oriented procedures must be able to send fits.
	MaxMessageSize int

	// HopCounter is the hop counter of the XUDT, XUDTS, LUDT and LUDTS the
	// stack originates. Zero selects params.MaxHopCounter (15).
	HopCounter params.HopCounter

	// UseXUDT sends user data that needs no segmentation in an XUDT instead
	// of a UDT. A request with an importance always uses an XUDT.
	UseXUDT bool

	// SegmentationThreshold is Z of ITU-T Q.714 (05/01) §4.1.1.1.1: user data
	// longer than Z octets is segmented into segments of at most Z octets even
	// when it would fit one message, and user data of Z octets or less is not
	// segmented unless it does not fit. Zero segments only what does not
	// fit; otherwise it must be at least MinSegmentationThreshold.
	SegmentationThreshold int

	// SegmentationReferenceFreeze is how long a segmentation local reference
	// stays out of use. Zero selects DefaultSegmentationReferenceFreeze.
	SegmentationReferenceFreeze time.Duration

	// ReassemblyTimeout is T(reassembly). Zero selects
	// DefaultReassemblyTimeout.
	ReassemblyTimeout time.Duration

	// MaxReassemblies bounds the reassembly processes, a failed one keeping
	// its place as the record of its message for T(reassembly) so that the
	// message is returned once; separately, it bounds the records of
	// messages that failed with no process. MaxReassemblyOctets bounds the
	// user data the processes hold. A first segment beyond either bound, or
	// a later segment beyond MaxReassemblyOctets, ends its reassembly with
	// the cause network congestion, ITU-T Q.714 (05/01) §4.1.1.2.3.4. A
	// message that fails with no process when no record is left is
	// discarded without a return. Zero selects the defaults.
	MaxReassemblies     int
	MaxReassemblyOctets int

	// MaxReassemblyOctetsPerPeer bounds the user data the processes of one
	// signalling point hold, identified in its network by the NI and OPC of
	// the routing information their segments arrive with, so that one peer
	// cannot take the whole of MaxReassemblyOctets. Beyond it, as beyond
	// MaxReassemblyOctets, the reassembly ends with the cause network
	// congestion. Zero sets no bound per peer: in a network routed on global
	// title, segments arrive from the relay that translated them, so a bound
	// per peer would bound most of the traffic.
	MaxReassemblyOctetsPerPeer int

	// LocalGlobalTitle reports whether a called party address routed on
	// global title addresses this node. The message is then delivered to the
	// subsystem number of the address, the result of the final translation
	// at this node (ITU-T Q.714 (05/01) §2.3.1 item 3 a) i)). Nil accepts
	// every global title. It is called without the stack locked, and may be
	// called for messages that are then discarded.
	LocalGlobalTitle func(called params.PartyAddress) bool

	// Connection configures the connection-oriented procedures.
	Connection ConnectionConfig

	// Now is the clock. Nil selects time.Now. It is called without the stack
	// locked, possibly from several goroutines at once.
	Now func() time.Time
}

// Stack runs the SCCP procedures of an end node: connectionless data transfer
// with segmentation and reassembly, the message return procedure and the
// connection-oriented procedures. It performs no I/O: the caller passes the
// messages the MTP delivered to HandleTransfer, sends the TransferRequests of
// every Result, delivers its Indications to the SCCP users, and calls
// HandleTimeout when NextTimeout says a timer is due (RunTimers does that).
//
// A Stack is safe for concurrent use.
type Stack struct {
	mu  sync.Mutex
	cfg StackConfig

	subsystems map[uint8]bool // registered subsystems and their availability

	timers timerHeap
	wake   chan struct{}

	nextSLS uint8

	// segmentation
	nextSegmentationReference uint32
	frozenSegmentation        map[uint32]time.Time
	frozenSegmentationOrder   []frozenReference

	// reassembly: processes in progress and records of failed messages. A
	// slot holds a process, then the record of its message if it fails; the
	// records of messages that failed with no process are counted apart.
	reassemblies     map[reassemblyKey]*reassembly
	reassemblyOctets int
	peerOctets       map[reassemblyPeer]int // reassemblyOctets by (NI, OPC)
	reassemblySlots  int
	orphanRecords    int
	segmentSeed      maphash.Seed

	conn connectionState
}

// NewStack returns a Stack for the configuration.
func NewStack(cfg StackConfig) (*Stack, error) {
	if cfg.MaxMessageSize == 0 {
		cfg.MaxMessageSize = DefaultMaxMessageSize
	}
	if cfg.HopCounter == 0 {
		cfg.HopCounter = params.MaxHopCounter
	}
	if cfg.SegmentationReferenceFreeze == 0 {
		cfg.SegmentationReferenceFreeze = DefaultSegmentationReferenceFreeze
	}
	if cfg.ReassemblyTimeout == 0 {
		cfg.ReassemblyTimeout = DefaultReassemblyTimeout
	}
	if cfg.MaxReassemblies == 0 {
		cfg.MaxReassemblies = DefaultMaxReassemblies
	}
	if cfg.MaxReassemblyOctets == 0 {
		cfg.MaxReassemblyOctets = DefaultMaxReassemblyOctets
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	switch {
	case cfg.PointCode > params.MaxPointCode:
		return nil, fmt.Errorf("%w: point code %d exceeds %d", ErrInvalidRequest, cfg.PointCode, params.MaxPointCode)
	case !cfg.NetworkIndicator.Valid():
		return nil, fmt.Errorf("%w: network indicator %d, want 0 to 3", ErrInvalidRequest, cfg.NetworkIndicator)
	case cfg.MaxMessageSize < minMessageSize || cfg.MaxMessageSize > MaxBroadbandMessageSize:
		return nil, fmt.Errorf("%w: maximum message size %d, want %d to %d", ErrInvalidRequest, cfg.MaxMessageSize, minMessageSize, MaxBroadbandMessageSize)
	case cfg.HopCounter > params.MaxHopCounter:
		return nil, fmt.Errorf("%w: hop counter %d exceeds %d", ErrInvalidRequest, cfg.HopCounter, params.MaxHopCounter)
	case cfg.SegmentationThreshold != 0 && cfg.SegmentationThreshold < MinSegmentationThreshold:
		return nil, fmt.Errorf("%w: segmentation threshold %d below %d", ErrInvalidRequest, cfg.SegmentationThreshold, MinSegmentationThreshold)
	case cfg.SegmentationReferenceFreeze < 0, cfg.ReassemblyTimeout < 0, cfg.MaxReassemblies < 0, cfg.MaxReassemblyOctets < 0, cfg.MaxReassemblyOctetsPerPeer < 0:
		return nil, fmt.Errorf("%w: negative timer or limit", ErrInvalidRequest)
	}
	s := &Stack{
		cfg:                       cfg,
		subsystems:                make(map[uint8]bool),
		wake:                      make(chan struct{}, 1),
		nextSegmentationReference: 1,
		frozenSegmentation:        make(map[uint32]time.Time),
		reassemblies:              make(map[reassemblyKey]*reassembly),
		peerOctets:                make(map[reassemblyPeer]int),
		segmentSeed:               maphash.MakeSeed(),
	}
	if err := s.initConnections(); err != nil {
		return nil, err
	}
	return s, nil
}

// RegisterSubsystem equips a local subsystem and marks it available.
func (s *Stack) RegisterSubsystem(ssn uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subsystems[ssn] = true
}

// UnregisterSubsystem removes a local subsystem: messages for it are returned
// with the cause unequipped user (ITU-T Q.714 (05/01) §2.8.5).
func (s *Stack) UnregisterSubsystem(ssn uint8) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subsystems, ssn)
}

// SetSubsystemAvailable marks a registered subsystem available or prohibited.
// Messages for a prohibited subsystem are returned with the cause subsystem
// failure (ITU-T Q.714 (05/01) §2.8.3). It reports false for a subsystem that
// is not registered.
func (s *Stack) SetSubsystemAvailable(ssn uint8, available bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subsystems[ssn]; !ok {
		return false
	}
	s.subsystems[ssn] = available
	return true
}

// SubsystemStatus reports whether a subsystem is registered and available.
func (s *Stack) SubsystemStatus(ssn uint8) (registered, available bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	available, registered = s.subsystems[ssn]
	return registered, available
}

// HandleTransfer runs the procedures for a message received from the MTP.
//
// LocalGlobalTitle is called before the stack is locked, so it may call the
// Stack's methods.
func (s *Stack) HandleTransfer(ind TransferIndication) Result {
	// ITU-T Q.704 (07/96) §§2.2.1-2.2.4, §14.2.2: refuse values
	// outside the standard routing label and network indicator fields.
	var routingErr error
	switch {
	case !ind.NI.Valid():
		routingErr = fmt.Errorf("%w: network indicator %d, want 0 to 3", ErrInvalidRequest, ind.NI)
	case ind.OPC > params.MaxPointCode:
		routingErr = fmt.Errorf("%w: originating point code %d exceeds %d", ErrInvalidRequest, ind.OPC, params.MaxPointCode)
	case ind.DPC > params.MaxPointCode:
		routingErr = fmt.Errorf("%w: destination point code %d exceeds %d", ErrInvalidRequest, ind.DPC, params.MaxPointCode)
	case ind.SLS > 15:
		routingErr = fmt.Errorf("%w: signalling link selection %d, want 0 to 15", ErrInvalidRequest, ind.SLS)
	}
	var m Message
	var err error
	if routingErr == nil {
		m, err = ParseMessage(ind.Data)
	}
	local := true
	if routingErr == nil && err == nil && s.cfg.LocalGlobalTitle != nil {
		if called, ok := calledAddress(m); ok && called.GlobalTitle != nil {
			// A copy: the callback must not reach the message the stack keeps.
			local = s.cfg.LocalGlobalTitle(called.Clone())
		}
	}

	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	s.expire(now, &res)

	if routingErr != nil {
		t := MsgType(0)
		if len(ind.Data) > 0 {
			t = MsgType(ind.Data[0])
		}
		res.discard(ind.RoutingLabel, t, routingErr)
		return res
	}

	if err != nil {
		// ITU-T Q.714 (05/01) §1.1.4.2: a message with an unrecognized type
		// is discarded; §4.3 and §3.8.4: so is one with a syntax error.
		t := MsgType(0)
		if len(ind.Data) > 0 {
			t = MsgType(ind.Data[0])
		}
		res.discard(ind.RoutingLabel, t, fmt.Errorf("%w: %w", ErrSyntax, err))
		return res
	}
	switch m := m.(type) {
	case *UDT, *XUDT, *LUDT:
		s.handleUnitdata(now, ind.RoutingLabel, m, local, &res)
	case *UDTS, *XUDTS, *LUDTS:
		s.handleService(ind.RoutingLabel, m, local, &res)
	default:
		s.handleConnectionOriented(now, ind.RoutingLabel, m, local, &res)
	}
	return res
}

// calledAddress returns the called party address of a message that SCCP
// routing examines: the connectionless messages and CR.
func calledAddress(m Message) (params.PartyAddress, bool) {
	switch m := m.(type) {
	case *UDT:
		return m.CalledPartyAddress, true
	case *XUDT:
		return m.CalledPartyAddress, true
	case *LUDT:
		return m.CalledPartyAddress, true
	case *UDTS:
		return m.CalledPartyAddress, true
	case *XUDTS:
		return m.CalledPartyAddress, true
	case *LUDTS:
		return m.CalledPartyAddress, true
	case *CR:
		return m.CalledPartyAddress, true
	}
	return params.PartyAddress{}, false
}

// HandleTimeout runs the timers that are due.
func (s *Stack) HandleTimeout() Result {
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	s.expire(now, &res)
	return res
}

// NextTimeout returns when the earliest timer is due, and false when no timer
// runs.
func (s *Stack) NextTimeout() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.timers) == 0 {
		return time.Time{}, false
	}
	return s.timers[0].deadline, true
}

// RunTimers runs the stack's timers until ctx is done, calling deliver with
// every non-empty Result they produce. It needs the default clock: it waits in
// real time.
func (s *Stack) RunTimers(ctx context.Context, deliver func(Result)) {
	t := time.NewTimer(time.Hour)
	defer t.Stop()
	for {
		wait := time.Hour
		if next, ok := s.NextTimeout(); ok {
			wait = max(time.Until(next), 0)
		}
		t.Reset(wait)
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-t.C:
			if res := s.HandleTimeout(); !res.empty() {
				deliver(res)
			}
		}
	}
}

func (r *Result) empty() bool {
	return len(r.Transfers) == 0 && len(r.Indications) == 0 && len(r.Discards) == 0
}

func (r *Result) discard(label RoutingLabel, t MsgType, reason error) {
	r.Discards = append(r.Discards, Discard{RoutingLabel: label, MessageType: t, Reason: reason})
}

// transfer encodes m within the maximum message size and appends it to the
// result.
func (s *Stack) transfer(r *Result, label RoutingLabel, m Message) error {
	b, err := MarshalOptions{MaxMessageSize: s.cfg.MaxMessageSize}.Marshal(m)
	if err != nil {
		return err
	}
	r.Transfers = append(r.Transfers, TransferRequest{RoutingLabel: label, Message: m, Data: b})
	return nil
}

// replyLabel routes a message back to the originator of a received one: to
// its OPC, from the point code it was sent to, on the same link selection
// and in the same network.
func replyLabel(received RoutingLabel) RoutingLabel {
	return RoutingLabel{OPC: received.DPC, DPC: received.OPC, SLS: received.SLS, NI: received.NI}
}

// originLabel routes a message to the SCCP node that originated a received
// one. ITU-T Q.714 (05/01) §2.7.5.1 c): the point code of a calling party
// address routed on SSN identifies that node; without one, the OPC of the
// routing label does. A calling party address routed on global title would
// need a translation this end node does not make (ITU-T Q.715 (04/02)
// §8.3.1.1), so the message goes to the OPC, the node that can translate it.
func originLabel(received RoutingLabel, calling params.PartyAddress) RoutingLabel {
	l := replyLabel(received)
	if calling.RouteOnSSN && calling.HasPointCode {
		l.DPC = uint32(calling.PointCode)
	}
	return l
}

// sls returns the signalling link selection of an originated message: the
// sequence control of class 1 (ITU-T Q.714 (05/01) §4.1: the same SLS keeps
// the sequence), otherwise a rotating value that load-shares class 0.
func (s *Stack) sls(class, sequenceControl uint8) uint8 {
	if class == 1 {
		return sequenceControl & 0x0f
	}
	v := s.nextSLS
	s.nextSLS = (s.nextSLS + 1) & 0x0f
	return v
}

// timer is a stack timer. fire runs with the stack lock held.
type timer struct {
	deadline time.Time
	fire     func(now time.Time, res *Result)
	index    int // position in the heap, -1 when stopped or fired
}

// timerHeap orders the running timers by deadline, as a container/heap.
type timerHeap []*timer

func (h timerHeap) Len() int           { return len(h) }
func (h timerHeap) Less(i, j int) bool { return h[i].deadline.Before(h[j].deadline) }

func (h timerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}

func (h *timerHeap) Push(x any) {
	t := x.(*timer)
	t.index = len(*h)
	*h = append(*h, t)
}

func (h *timerHeap) Pop() any {
	old := *h
	t := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	t.index = -1
	return t
}

// schedule starts a timer that calls fire at the deadline.
func (s *Stack) schedule(deadline time.Time, fire func(now time.Time, res *Result)) *timer {
	t := &timer{deadline: deadline, fire: fire}
	heap.Push(&s.timers, t)
	if t.index == 0 {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	return t
}

// stop stops a timer; stopping a stopped timer does nothing.
func (s *Stack) stop(t *timer) {
	if t == nil || t.index < 0 {
		return
	}
	heap.Remove(&s.timers, t.index)
}

// expire fires every timer due at now, in deadline order.
func (s *Stack) expire(now time.Time, res *Result) {
	for len(s.timers) > 0 && !s.timers[0].deadline.After(now) {
		heap.Pop(&s.timers).(*timer).fire(now, res)
	}
}
