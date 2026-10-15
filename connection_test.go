package sccp

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gomaja/go-sccp/params"
)

type coTestClock struct {
	ns atomic.Int64
}

func (c *coTestClock) now() time.Time {
	return time.Unix(1000, c.ns.Load())
}

func (c *coTestClock) advance(d time.Duration) { c.ns.Add(int64(d)) }

func coTestConfig() ConnectionConfig {
	return ConnectionConfig{
		EstablishmentTimeout:     10 * time.Second,
		ReleaseTimeout:           3 * time.Second,
		RepeatReleaseTimeout:     2 * time.Second,
		ReleaseGiveUpTimeout:     7 * time.Second,
		InactivitySendTimeout:    30 * time.Second,
		InactivityReceiveTimeout: 60 * time.Second,
		ReferenceFreeze:          20 * time.Second,
		MaxConnections:           64,
		MaxDataLength:            65535,
	}
}

func coTestStack(t *testing.T, cfg StackConfig) (*Stack, *coTestClock) {
	t.Helper()
	c := new(coTestClock)
	cfg.Now = c.now
	cfg.PointCode = 1
	s, err := NewStack(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.RegisterSubsystem(6)
	return s, c
}

func coTestCounts(t *testing.T, res Result, transfers, indications, discards int) {
	t.Helper()
	if len(res.Transfers) != transfers || len(res.Indications) != indications || len(res.Discards) != discards {
		t.Fatalf("result counts = (%d,%d,%d), want (%d,%d,%d): %+v", len(res.Transfers), len(res.Indications), len(res.Discards), transfers, indications, discards, res)
	}
}

func coTestTransfer(t *testing.T, res Result, i int, label RoutingLabel, want Message) {
	t.Helper()
	if i >= len(res.Transfers) {
		t.Fatalf("missing transfer %d: %+v", i, res)
	}
	tr := res.Transfers[i]
	if tr.RoutingLabel != label {
		t.Fatalf("transfer label = %+v, want %+v", tr.RoutingLabel, label)
	}
	decoded, err := ParseMessage(tr.Data)
	if err != nil {
		t.Fatalf("transfer cannot be decoded: %v", err)
	}
	if !reflect.DeepEqual(decoded, want) || !reflect.DeepEqual(tr.Message, want) {
		t.Fatalf("transfer = %#v; decoded = %#v; want %#v", tr.Message, decoded, want)
	}
}

func coTestReceive(t *testing.T, s *Stack, label RoutingLabel, m Message) Result {
	t.Helper()
	b, err := m.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return s.HandleTransfer(TransferIndication{RoutingLabel: label, Data: b})
}

func coTestState(t *testing.T, s *Stack, ref params.LocalReference, want ConnectionState) {
	t.Helper()
	info, ok := s.Connection(ref)
	if !ok || info.State != want {
		t.Fatalf("connection %#x = %+v, %t; want state %s", ref, info, ok, want)
	}
}

func coTestOutgoing(t *testing.T, s *Stack) (params.LocalReference, RoutingLabel) {
	t.Helper()
	ref, res, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6)})
	if err != nil {
		t.Fatal(err)
	}
	coTestCounts(t, res, 1, 0, 0)
	label := res.Transfers[0].RoutingLabel
	coTestTransfer(t, res, 0, label, &CR{SourceLocalReference: ref, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: params.NewPointCodeAddress(2, 6)})
	if label.OPC != 1 || label.DPC != 2 || label.SLS > 15 || ref == params.LocalReferenceReserved {
		t.Fatalf("invalid allocated reference or label: %#x, %+v", ref, label)
	}
	coTestState(t, s, ref, ConnectionOutgoingPending)
	return ref, label
}

func coTestEstablished(t *testing.T, s *Stack) (params.LocalReference, RoutingLabel) {
	t.Helper()
	ref, label := coTestOutgoing(t, s)
	res := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, &CC{DestinationLocalReference: ref, SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}})
	coTestCounts(t, res, 0, 1, 0)
	if got, ok := res.Indications[0].(*ConnectConfirmation); !ok || got.LocalReference != ref || got.RespondingAddress != nil || len(got.Data) != 0 {
		t.Fatalf("confirmation = %#v", res.Indications[0])
	}
	coTestState(t, s, ref, ConnectionEstablished)
	return ref, label
}

func coTestIncoming(t *testing.T, s *Stack, class uint8) params.LocalReference {
	t.Helper()
	label := RoutingLabel{OPC: 2, DPC: 1, SLS: 7}
	res := coTestReceive(t, s, label, &CR{SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: class}, CalledPartyAddress: params.NewPointCodeAddress(1, 6)})
	coTestCounts(t, res, 0, 1, 0)
	ind, ok := res.Indications[0].(*ConnectIndication)
	if !ok || ind.RoutingLabel != label || ind.CalledPartyAddress != params.NewPointCodeAddress(1, 6) {
		t.Fatalf("connect indication = %#v", res.Indications[0])
	}
	coTestState(t, s, ind.LocalReference, ConnectionIncomingPending)
	return ind.LocalReference
}

// ITU-T Q.714 (05/01) §§3.1.4.1-3.1.4.2, §2.3.1: preserve CR parameters,
// assign an SLS, process CC and stop the establishment timer.
func TestConnectionOutgoingEstablishment(t *testing.T) {
	for _, onGT := range []bool{false, true} {
		t.Run(fmt.Sprintf("global_title_%t", onGT), func(t *testing.T) {
			s, clock := coTestStack(t, StackConfig{Connection: coTestConfig(), HopCounter: 9})
			called := params.NewPointCodeAddress(2, 6)
			var hop *params.HopCounter
			if onGT {
				gt, err := params.NewGlobalTitle1(params.NAIInternationalNumber, "1234")
				if err != nil {
					t.Fatal(err)
				}
				called.RouteOnSSN, called.GlobalTitle = false, gt
				h := params.HopCounter(9)
				hop = &h
			}
			calling := params.NewPointCodeAddress(1, 7)
			importance := params.Importance(3)
			data := bytes.Repeat([]byte{0x5a}, 128)
			ref, res, err := s.Connect(ConnectRequest{CalledPartyAddress: called, CallingPartyAddress: &calling, Data: data, Importance: &importance})
			if err != nil {
				t.Fatal(err)
			}
			coTestCounts(t, res, 1, 0, 0)
			label := res.Transfers[0].RoutingLabel
			coTestTransfer(t, res, 0, label, &CR{SourceLocalReference: ref, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: called, CallingPartyAddress: &calling, Data: data, Importance: &importance, HopCounter: hop})
			if label.OPC != 1 || label.DPC != 2 || label.SLS > 15 {
				t.Fatalf("label = %+v", label)
			}
			responding := params.NewPointCodeAddress(2, 8)
			res = coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, &CC{DestinationLocalReference: ref, SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: &responding, Data: []byte{0xaa}})
			coTestCounts(t, res, 0, 1, 0)
			want := &ConnectConfirmation{LocalReference: ref, RespondingAddress: &responding, Data: []byte{0xaa}}
			if !reflect.DeepEqual(res.Indications[0], want) {
				t.Fatalf("confirmation = %#v, want %#v", res.Indications[0], want)
			}
			info, ok := s.Connection(ref)
			if !ok || info.LocalReference != ref || info.RemoteReference != 0x102 || info.Remote != label || info.State != ConnectionEstablished {
				t.Fatalf("snapshot = %+v, %t", info, ok)
			}
			clock.advance(10 * time.Second)
			coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
		})
	}
}

// ITU-T Q.714 (05/01) §§3.1.3.1, 3.1.6.1-3.1.6.2: class 3 may be
// negotiated down to class 2, with no credit in the resulting CC.
func TestConnectionIncomingAcceptAndClassDowngrade(t *testing.T) {
	for _, class := range []uint8{2, 3} {
		t.Run(fmt.Sprint(class), func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
			calling := params.NewPointCodeAddress(2, 7)
			importance := params.Importance(4)
			label := RoutingLabel{OPC: 2, DPC: 1, SLS: 7}
			res := coTestReceive(t, s, label, &CR{SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: class}, CalledPartyAddress: params.NewPointCodeAddress(1, 6), CallingPartyAddress: &calling, Data: []byte{1, 2}, Importance: &importance})
			coTestCounts(t, res, 0, 1, 0)
			ind, ok := res.Indications[0].(*ConnectIndication)
			if !ok {
				t.Fatalf("indication = %#v", res.Indications[0])
			}
			want := &ConnectIndication{RoutingLabel: label, LocalReference: ind.LocalReference, CalledPartyAddress: params.NewPointCodeAddress(1, 6), CallingPartyAddress: &calling, Data: []byte{1, 2}, Importance: &importance}
			if !reflect.DeepEqual(ind, want) {
				t.Fatalf("indication = %#v, want %#v", ind, want)
			}
			responseAddress := params.NewPointCodeAddress(1, 8)
			res, err := s.Accept(ind.LocalReference, ConnectResponse{RespondingAddress: &responseAddress, Data: []byte{3, 4}, Importance: &importance})
			if err != nil {
				t.Fatal(err)
			}
			coTestCounts(t, res, 1, 0, 0)
			coTestTransfer(t, res, 0, RoutingLabel{OPC: 1, DPC: 2, SLS: 7}, &CC{DestinationLocalReference: 0x102, SourceLocalReference: ind.LocalReference, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: &responseAddress, Data: []byte{3, 4}, Importance: &importance})
			coTestState(t, s, ind.LocalReference, ConnectionEstablished)
		})
	}
}

// ITU-T Q.714 (05/01) §§2.8, 3.1.6.1, 3.2.1.1: failed routing or resource
// allocation must refuse the remote reference without allocating a section.
func TestConnectionIncomingRoutingRefusals(t *testing.T) {
	gt, err := params.NewGlobalTitle1(params.NAIInternationalNumber, "1234")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		called params.PartyAddress
		hop    *params.HopCounter
		setup  func(*Stack)
		cfg    StackConfig
		cause  params.RefusalCause
	}{
		{"unknown_title", params.NewGlobalTitleAddress(gt, 6), nil, nil, StackConfig{LocalGlobalTitle: func(params.PartyAddress) bool { return false }}, params.RefusalCauseDestinationAddressUnknown},
		{"translation_without_ssn", params.NewGlobalTitleAddress(gt, 0), nil, nil, StackConfig{}, params.RefusalCauseDestinationAddressUnknown},
		{"prohibited_subsystem", params.NewPointCodeAddress(1, 6), nil, func(s *Stack) { s.SetSubsystemAvailable(6, false) }, StackConfig{}, params.RefusalCauseSubsystemFailure},
		{"unequipped_subsystem", params.NewPointCodeAddress(1, 9), nil, nil, StackConfig{}, params.RefusalCauseUnequippedUser},
		{"hop_exhausted", params.NewGlobalTitleAddress(gt, 6), func() *params.HopCounter { h := params.HopCounter(1); return &h }(), nil, StackConfig{}, params.RefusalCauseHopCounterViolation},
		{"connection_limit", params.NewPointCodeAddress(1, 6), nil, func(s *Stack) { coTestOutgoing(t, s) }, StackConfig{Connection: ConnectionConfig{MaxConnections: 1}}, params.RefusalCauseNetworkResourceQoSNotAvailableTransient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := coTestStack(t, tc.cfg)
			if tc.setup != nil {
				tc.setup(s)
			}
			before := len(s.conn.sections)
			label := RoutingLabel{OPC: 2, DPC: 1, SLS: 9}
			res := coTestReceive(t, s, label, &CR{SourceLocalReference: 0x204, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: tc.called, HopCounter: tc.hop})
			if tc.cause == params.RefusalCauseHopCounterViolation {
				// ITU-T Q.714 (05/01) §2.3.1 item 3: maintenance is alerted too.
				coTestCounts(t, res, 1, 1, 0)
				if m, ok := res.Indications[0].(*MaintenanceIndication); !ok || m.RoutingLabel != label || m.MessageType != MsgTypeCR || !errors.Is(m.Reason, ErrHopCounterViolation) {
					t.Fatalf("indication %#v, want a maintenance alert", res.Indications[0])
				}
			} else {
				coTestCounts(t, res, 1, 0, 0)
			}
			coTestTransfer(t, res, 0, RoutingLabel{OPC: 1, DPC: 2, SLS: 9}, &CREF{DestinationLocalReference: 0x204, RefusalCause: tc.cause})
			if len(s.conn.sections) != before {
				t.Fatalf("refusal allocated a section: before %d, after %d", before, len(s.conn.sections))
			}
		})
	}
}

// ITU-T Q.714 (05/01) §3.8.3.3 b6, b7 and §3.8.4: a CR whose called party
// address routes on GT without a GT, or on SSN without an SSN, has a syntax
// error and is discarded without a CREF or a section.
func TestConnectionRequestWithIllConstructedAddressDiscarded(t *testing.T) {
	for _, called := range []params.PartyAddress{
		{HasSSN: true, SSN: 6},
		{RouteOnSSN: true, HasPointCode: true, PointCode: 1},
	} {
		s, _ := coTestStack(t, StackConfig{})
		before := len(s.conn.sections)
		res := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: 9}, &CR{SourceLocalReference: 0x204, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: called})
		coTestCounts(t, res, 0, 0, 1)
		if !errors.Is(res.Discards[0].Reason, ErrSyntax) {
			t.Fatalf("called %v: reason %v, want ErrSyntax", called, res.Discards[0].Reason)
		}
		if len(s.conn.sections) != before {
			t.Fatalf("called %v allocated a section", called)
		}
	}
}

// ITU-T Q.714 (05/01) §2.7 d): a CREF goes to the point code of the calling
// party address, the originating node, and not to the relay that sent the CR.
func TestConnectionRefusalRoutedToCallingPointCode(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{})
	calling := params.NewPointCodeAddress(0x0303, 7)
	res := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: 9}, &CR{SourceLocalReference: 0x204, ProtocolClass: params.ProtocolClass{Class: 2},
		CalledPartyAddress: params.NewPointCodeAddress(1, 9), CallingPartyAddress: &calling})
	coTestCounts(t, res, 1, 0, 0)
	coTestTransfer(t, res, 0, RoutingLabel{OPC: 1, DPC: 0x0303, SLS: 9}, &CREF{DestinationLocalReference: 0x204, RefusalCause: params.RefusalCauseUnequippedUser})
}

// ITU-T Q.714 (05/01) §3.2.1.1: the user may refuse an incoming request.
func TestConnectionUserRefusal(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
	ref := coTestIncoming(t, s, 2)
	importance := params.Importance(3)
	res, err := s.Disconnect(ref, DisconnectRequest{RefusalCause: params.RefusalCauseEndUserCongestion, Data: []byte{4}, Importance: &importance})
	if err != nil {
		t.Fatal(err)
	}
	coTestCounts(t, res, 1, 0, 0)
	coTestTransfer(t, res, 0, RoutingLabel{OPC: 1, DPC: 2, SLS: 7}, &CREF{DestinationLocalReference: 0x102, RefusalCause: params.RefusalCauseEndUserCongestion, Data: []byte{4}, Importance: &importance})
	coTestState(t, s, ref, ConnectionFrozen)
}

// ITU-T Q.713 (03/01) Table A.1: refusal causes determine the originator.
// ITU-T Q.714 (05/01) §3.2.3: CREF stops establishment and releases resources.
func TestConnectionReceivedRefusalOriginators(t *testing.T) {
	for _, tc := range []struct {
		cause      params.RefusalCause
		originator Originator
	}{
		{0, OriginatorUser}, {1, OriginatorUser}, {2, OriginatorUser}, {3, OriginatorUser},
		{4, OriginatorProvider}, {5, OriginatorProvider}, {6, OriginatorProvider}, {7, OriginatorProvider},
		{8, OriginatorUser}, {9, OriginatorUser}, {10, OriginatorProvider}, {11, OriginatorUser},
		{12, OriginatorProvider}, {13, OriginatorUser}, {15, OriginatorUndefined},
		{16, OriginatorProvider}, {17, OriginatorProvider}, {18, OriginatorProvider}, {19, OriginatorProvider},
	} {
		t.Run(fmt.Sprint(tc.cause), func(t *testing.T) {
			s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, label := coTestOutgoing(t, s)
			responding := params.NewPointCodeAddress(2, 8)
			res := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, &CREF{DestinationLocalReference: ref, RefusalCause: tc.cause, CalledPartyAddress: &responding, Data: []byte{0x55}})
			coTestCounts(t, res, 0, 1, 0)
			want := &DisconnectIndication{LocalReference: ref, Originator: tc.originator, Refused: true, RefusalCause: tc.cause, RespondingAddress: &responding, Data: []byte{0x55}}
			if !reflect.DeepEqual(res.Indications[0], want) {
				t.Fatalf("refusal indication = %#v, want %#v", res.Indications[0], want)
			}
			coTestState(t, s, ref, ConnectionFrozen)
			clock.advance(10 * time.Second)
			coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
		})
	}
}

// ITU-T Q.714 (05/01) §3.1.4.2: establishment expiry refuses and freezes.
func TestConnectionEstablishmentExpiry(t *testing.T) {
	s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
	ref, _ := coTestOutgoing(t, s)
	clock.advance(10*time.Second - time.Nanosecond)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	clock.advance(time.Nanosecond)
	res := s.HandleTimeout()
	coTestCounts(t, res, 0, 1, 0)
	want := &DisconnectIndication{LocalReference: ref, Originator: OriginatorProvider, Refused: true, RefusalCause: params.RefusalCauseExpirationOfTheConnectionEstablishmentTimer}
	if !reflect.DeepEqual(res.Indications[0], want) {
		t.Fatalf("expiry = %#v, want %#v", res.Indications[0], want)
	}
	coTestState(t, s, ref, ConnectionFrozen)
}

// ITU-T Q.714 (05/01) §3.8.3.3 a2 and §3.8.4: invalid CC classes
// discard as syntax errors without changing the section or T(conn est).
func TestConnectionInvalidConfirmationClassSyntaxDiscard(t *testing.T) {
	for class := range byte(16) {
		if class == 2 || class == 3 {
			continue
		}
		t.Run(fmt.Sprint(class), func(t *testing.T) {
			s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, label := coTestOutgoing(t, s)
			before, _ := s.Connection(ref)
			c := s.conn.sections[ref]
			timer := c.establishment
			deadline := timer.deadline
			b, err := (&CC{DestinationLocalReference: ref, SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}}).MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			b[7] = class
			clock.advance(5 * time.Second)
			res := s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, Data: b})
			coTestCounts(t, res, 0, 0, 1)
			if res.Discards[0].MessageType != MsgTypeCC || !errors.Is(res.Discards[0].Reason, ErrSyntax) {
				t.Fatalf("invalid CC discard = %+v", res.Discards[0])
			}
			after, ok := s.Connection(ref)
			if !ok || !reflect.DeepEqual(before, after) || c.establishment != timer || timer.index < 0 || !timer.deadline.Equal(deadline) || len(s.timers) != 1 {
				t.Fatal("syntax error changed the section or establishment timer")
			}
			clock.advance(5 * time.Second)
			coTestCounts(t, s.HandleTimeout(), 0, 1, 0)
			coTestState(t, s, ref, ConnectionFrozen)
		})
	}
}

// ITU-T Q.714 (05/01) §3.1.3.1: a class 2 proposal cannot be upgraded or
// established as class 3.
func TestConnectionConfirmationClassUpgradeReleases(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
	ref, label := coTestOutgoing(t, s)
	res := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, &CC{DestinationLocalReference: ref, SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 3}})
	coTestCounts(t, res, 1, 1, 1)
	if res.Discards[0].MessageType != MsgTypeCC || !errors.Is(res.Discards[0].Reason, ErrConnectionState) {
		t.Fatalf("class 3 CC discard = %+v", res.Discards[0])
	}
	coTestTransfer(t, res, 0, label, &RLSD{DestinationLocalReference: 0x102, SourceLocalReference: ref, ReleaseCause: params.ReleaseCauseRemoteProcedureError})
	if got, ok := res.Indications[0].(*DisconnectIndication); !ok || got.LocalReference != ref || got.Originator != OriginatorProvider {
		t.Fatalf("indication = %#v", res.Indications[0])
	}
	coTestState(t, s, ref, ConnectionReleasePending)
}

// ITU-T Q.714 (05/01) §§3.5.1, 3.5.3: every NSDU is carried by DT1; the
// M-bit identifies all but the final segment and input storage is not retained.
func TestConnectionSendDataSegmentation(t *testing.T) {
	for _, n := range []int{1, 255, 256, 510, 511} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, label := coTestEstablished(t, s)
			data := bytes.Repeat([]byte{0x76}, n)
			res, err := s.SendData(ref, data)
			if err != nil {
				t.Fatal(err)
			}
			coTestCounts(t, res, (n+254)/255, 0, 0)
			for i, tr := range res.Transfers {
				start, end := i*255, min((i+1)*255, n)
				coTestTransfer(t, res, i, label, &DT1{DestinationLocalReference: 0x102, MoreData: end < n, Data: bytes.Repeat([]byte{0x76}, end-start)})
				if len(tr.Data) > s.cfg.MaxMessageSize {
					t.Fatalf("oversized transfer: %d", len(tr.Data))
				}
			}
			data[0] = 0
			if res.Transfers[0].Message.(*DT1).Data[0] != 0x76 {
				t.Fatal("DT1 retains request storage")
			}
		})
	}
}

// ITU-T Q.714 (05/01) §3.5.3: short M-bit fragments must accumulate until
// the first M=0 fragment, then the next NSDU starts with an empty buffer (F4).
func TestConnectionDT1ReassemblyAndBoundary(t *testing.T) {
	cfg := coTestConfig()
	cfg.MaxDataLength = 5
	s, _ := coTestStack(t, StackConfig{Connection: cfg})
	ref, label := coTestEstablished(t, s)
	peer := RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}
	coTestCounts(t, coTestReceive(t, s, peer, &DT1{DestinationLocalReference: ref, MoreData: true, Data: []byte{1}}), 0, 0, 0)
	coTestCounts(t, coTestReceive(t, s, peer, &DT1{DestinationLocalReference: ref, MoreData: true, Data: []byte{2, 3}}), 0, 0, 0)
	res := coTestReceive(t, s, peer, &DT1{DestinationLocalReference: ref, Data: []byte{4, 5}})
	coTestCounts(t, res, 0, 1, 0)
	if !reflect.DeepEqual(res.Indications[0], &DataIndication{LocalReference: ref, Data: []byte{1, 2, 3, 4, 5}}) {
		t.Fatalf("reassembly = %#v", res.Indications[0])
	}
	res.Indications[0].(*DataIndication).Data[0] = 99
	res = coTestReceive(t, s, peer, &DT1{DestinationLocalReference: ref, Data: []byte{6}})
	coTestCounts(t, res, 0, 1, 0)
	if !reflect.DeepEqual(res.Indications[0], &DataIndication{LocalReference: ref, Data: []byte{6}}) {
		t.Fatalf("next NSDU = %#v", res.Indications[0])
	}
}

// ITU-T Q.714 (05/01) §§3.5.3, 3.3.3.1: overflowing the reassembly limit
// initiates release rather than delivering a partial or oversized NSDU.
func TestConnectionReassemblyOverflowReleases(t *testing.T) {
	for _, segmented := range []bool{false, true} {
		t.Run(fmt.Sprint(segmented), func(t *testing.T) {
			cfg := coTestConfig()
			cfg.MaxDataLength = 3
			s, _ := coTestStack(t, StackConfig{Connection: cfg})
			ref, label := coTestEstablished(t, s)
			peer := RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}
			data := []byte{1, 2, 3, 4}
			if segmented {
				coTestCounts(t, coTestReceive(t, s, peer, &DT1{DestinationLocalReference: ref, MoreData: true, Data: []byte{1, 2, 3}}), 0, 0, 0)
				data = []byte{4}
			}
			res := coTestReceive(t, s, peer, &DT1{DestinationLocalReference: ref, Data: data})
			coTestCounts(t, res, 1, 1, 1)
			if res.Discards[0].MessageType != MsgTypeDT1 || !errors.Is(res.Discards[0].Reason, ErrReassembly) {
				t.Fatalf("overflow discard = %+v", res.Discards[0])
			}
			coTestTransfer(t, res, 0, label, &RLSD{DestinationLocalReference: 0x102, SourceLocalReference: ref, ReleaseCause: params.ReleaseCauseRemoteProcedureError})
			want := &DisconnectIndication{LocalReference: ref, Originator: OriginatorProvider, ReleaseCause: params.ReleaseCauseRemoteProcedureError}
			if !reflect.DeepEqual(res.Indications[0], want) {
				t.Fatalf("overflow indication = %#v", res.Indications[0])
			}
			coTestState(t, s, ref, ConnectionReleasePending)
		})
	}
}

// ITU-T Q.714 (05/01) §3.3.3.2: T(rel), T(repeat rel) and T(int) have
// distinct lifetimes; T(int) ends all retries even at a shared deadline.
func TestConnectionReleaseTimersAndFrozenExpiry(t *testing.T) {
	s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
	ref, label := coTestEstablished(t, s)
	importance := params.Importance(2)
	req := DisconnectRequest{ReleaseCause: params.ReleaseCauseEndUserOriginated, Data: []byte{8}, Importance: &importance}
	want := &RLSD{DestinationLocalReference: 0x102, SourceLocalReference: ref, ReleaseCause: params.ReleaseCauseEndUserOriginated, Data: []byte{8}, Importance: &importance}
	res, err := s.Disconnect(ref, req)
	if err != nil {
		t.Fatal(err)
	}
	coTestCounts(t, res, 1, 0, 0)
	coTestTransfer(t, res, 0, label, want)
	coTestState(t, s, ref, ConnectionReleasePending)
	clock.advance(3*time.Second - time.Nanosecond)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	clock.advance(time.Nanosecond)
	res = s.HandleTimeout()
	coTestCounts(t, res, 1, 0, 0)
	coTestTransfer(t, res, 0, label, want)
	for range 3 {
		clock.advance(2 * time.Second)
		res = s.HandleTimeout()
		coTestCounts(t, res, 1, 0, 0)
		coTestTransfer(t, res, 0, label, want)
	}
	clock.advance(time.Second)
	res = s.HandleTimeout()
	coTestCounts(t, res, 0, 1, 0)
	// ITU-T Q.714 (05/01) Figure C.2 sheet 6 and Annex C.4: alert maintenance.
	maintenance, ok := res.Indications[0].(*MaintenanceIndication)
	if !ok || maintenance.LocalReference != ref || maintenance.RemoteReference != 0x102 || maintenance.RoutingLabel != label || maintenance.MessageType != MsgTypeRLSD || !errors.Is(maintenance.Reason, ErrReleaseIncomplete) {
		t.Fatalf("interval expiry = %+v, want maintenance indication", res.Indications[0])
	}
	coTestState(t, s, ref, ConnectionFrozen)
	if _, err := s.SendData(ref, []byte{1}); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("frozen SendData error = %v", err)
	}
	clock.advance(20*time.Second - time.Nanosecond)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	coTestState(t, s, ref, ConnectionFrozen)
	clock.advance(time.Nanosecond)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	if _, ok := s.Connection(ref); ok || len(s.conn.sections) != 0 {
		t.Fatalf("expired freeze retained reference: %+v", s.conn.sections)
	}
	if _, ok := s.NextTimeout(); ok {
		t.Fatal("release or inactivity timer survived reference removal")
	}
}

// ITU-T Q.714 (05/01) §3.3.3.2: either RLC or collision with RLSD ends the
// local release and cancels every release timer.
func TestConnectionReleaseAcknowledgement(t *testing.T) {
	for _, crossing := range []bool{false, true} {
		for _, repeated := range []bool{false, true} {
			t.Run(fmt.Sprintf("crossing_%t_repeated_%t", crossing, repeated), func(t *testing.T) {
				s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
				ref, label := coTestEstablished(t, s)
				if _, err := s.Disconnect(ref, DisconnectRequest{}); err != nil {
					t.Fatal(err)
				}
				if repeated {
					clock.advance(3 * time.Second)
					coTestCounts(t, s.HandleTimeout(), 1, 0, 0)
				}
				var m Message = &RLC{DestinationLocalReference: ref, SourceLocalReference: 0x102}
				if crossing {
					m = &RLSD{DestinationLocalReference: ref, SourceLocalReference: 0x102, ReleaseCause: params.ReleaseCauseSCCPUserOriginated}
				}
				res := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, m)
				coTestCounts(t, res, 0, 0, 0)
				coTestState(t, s, ref, ConnectionFrozen)
				clock.advance(10 * time.Second)
				coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
			})
		}
	}
}

// ITU-T Q.714 (05/01) §3.3.5 and ITU-T Q.713 (03/01) Table A.2: incoming
// release sends RLC and one correctly classified disconnect indication.
func TestConnectionRemoteReleaseOriginators(t *testing.T) {
	for _, tc := range []struct {
		cause      params.ReleaseCause
		originator Originator
	}{
		{0, OriginatorUser}, {1, OriginatorUser}, {2, OriginatorUser}, {3, OriginatorUser},
		{4, OriginatorProvider}, {5, OriginatorProvider}, {6, OriginatorUser}, {7, OriginatorUser},
		{8, OriginatorProvider}, {9, OriginatorUser}, {10, OriginatorProvider}, {11, OriginatorProvider},
		{12, OriginatorProvider}, {13, OriginatorProvider}, {15, OriginatorUndefined}, {16, OriginatorProvider},
	} {
		t.Run(fmt.Sprint(tc.cause), func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, label := coTestEstablished(t, s)
			res := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, &RLSD{DestinationLocalReference: ref, SourceLocalReference: 0x102, ReleaseCause: tc.cause, Data: []byte{7}})
			coTestCounts(t, res, 1, 1, 0)
			coTestTransfer(t, res, 0, label, &RLC{DestinationLocalReference: 0x102, SourceLocalReference: ref})
			want := &DisconnectIndication{LocalReference: ref, Originator: tc.originator, ReleaseCause: tc.cause, Data: []byte{7}}
			if !reflect.DeepEqual(res.Indications[0], want) {
				t.Fatalf("release indication = %#v", res.Indications[0])
			}
			coTestState(t, s, ref, ConnectionFrozen)
		})
	}
}

// ITU-T Q.714 (05/01) §§3.1.2, 3.3.2 and ITU-T Q.713 (03/01) §§3.2-3.3:
// allocation skips active, frozen and reserved references, terminates on
// exhaustion and permits reuse after freeze. Regressions for F1, F2 and F10.
func TestConnectionAllocationExhaustionFreezeAndReuse(t *testing.T) {
	s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
	s.conn.referenceLimit = 4
	s.conn.next = 1
	used := make(map[params.LocalReference]bool)
	for range 4 {
		ref, res, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6)})
		if err != nil {
			t.Fatalf("allocation before exhausting four usable references: %v", err)
		}
		if used[ref] || ref >= 4 {
			t.Fatalf("allocated duplicate or outside test space: %#x", ref)
		}
		used[ref] = true
		coTestCounts(t, res, 1, 0, 0)
	}
	if len(used) != 4 {
		t.Fatalf("allocated references = %v", used)
	}
	// The watchdog protects the test process against F1; protocol timers use
	// the fake clock exclusively.
	done := make(chan error, 1)
	go func() {
		_, _, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6)})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("exhausted allocator succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exhausted allocator did not terminate promptly")
	}
	for ref := range used {
		coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1}, &CREF{DestinationLocalReference: ref, RefusalCause: params.RefusalCauseSCCPUserOriginated})
		coTestState(t, s, ref, ConnectionFrozen)
	}
	if _, _, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6)}); err == nil {
		t.Fatal("frozen references were reused")
	}
	clock.advance(20 * time.Second)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	if len(s.conn.sections) != 0 {
		t.Fatalf("freeze registry grew permanently: %d entries", len(s.conn.sections))
	}
	ref, _, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6)})
	if err != nil || !used[ref] {
		t.Fatalf("allocation after freeze = %#x, %v", ref, err)
	}
	s.conn.referenceLimit = params.LocalReferenceReserved
	s.conn.next = params.LocalReferenceReserved
	ref, res, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6)})
	if err != nil || ref == params.LocalReferenceReserved {
		t.Fatalf("reserved start = %#x, %v", ref, err)
	}
	coTestCounts(t, res, 1, 0, 0)
}

// ITU-T Q.714 (05/01) §3.8.6: ordinary ERR releases resources; service class
// mismatch initiates release. Regression for F9.
func TestConnectionReceivedERR(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprint(mismatch), func(t *testing.T) {
			s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, label := coTestEstablished(t, s)
			cause := params.ErrorCauseUnqualified
			if mismatch {
				cause = params.ErrorCauseServiceClassMismatch
			}
			res := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, &ERR{DestinationLocalReference: ref, ErrorCause: cause})
			wantTransfers := 0
			releaseCause := params.ReleaseCauseRemoteProcedureError
			if mismatch {
				wantTransfers = 1
				releaseCause = params.ReleaseCauseInconsistentConnectionData
			}
			coTestCounts(t, res, wantTransfers, 1, 0)
			want := &DisconnectIndication{LocalReference: ref, Originator: OriginatorProvider, ReleaseCause: releaseCause}
			if !reflect.DeepEqual(res.Indications[0], want) {
				t.Fatalf("ERR indication = %#v", res.Indications[0])
			}
			if mismatch {
				coTestTransfer(t, res, 0, label, &RLSD{DestinationLocalReference: 0x102, SourceLocalReference: ref, ReleaseCause: releaseCause})
				coTestState(t, s, ref, ConnectionReleasePending)
			} else {
				coTestState(t, s, ref, ConnectionFrozen)
				clock.advance(10 * time.Second)
				coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
			}
		})
	}
}

// ITU-T Q.714 (05/01) §3.4 and ITU-T Q.713 (03/01) Table 18: sending
// restarts T(ias), receiving restarts T(iar), and class 2 IT has zero sequence
// and credit fields. Every transfer retains the original connection SLS.
func TestConnectionInactivitySendAndReceiveRestart(t *testing.T) {
	cfg := coTestConfig()
	cfg.InactivitySendTimeout, cfg.InactivityReceiveTimeout = 5*time.Second, 12*time.Second
	s, clock := coTestStack(t, StackConfig{Connection: cfg})
	ref, label := coTestEstablished(t, s)
	clock.advance(4 * time.Second)
	res, err := s.SendData(ref, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	coTestCounts(t, res, 1, 0, 0)
	coTestTransfer(t, res, 0, label, &DT1{DestinationLocalReference: 0x102, Data: []byte{1}})
	clock.advance(time.Second)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	clock.advance(4 * time.Second)
	res = s.HandleTimeout()
	coTestCounts(t, res, 1, 0, 0)
	coTestTransfer(t, res, 0, label, &IT{DestinationLocalReference: 0x102, SourceLocalReference: ref, ProtocolClass: params.ProtocolClass{Class: 2}})
	clock.advance(2 * time.Second)
	coTestCounts(t, coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: 13}, &DT1{DestinationLocalReference: ref, Data: []byte{2}}), 0, 1, 0)
	clock.advance(time.Second)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	coTestState(t, s, ref, ConnectionEstablished)
	// Receiving an IT need not send an immediate reply and must ignore the
	// class 3 sequencing and credit fields for a class 2 connection.
	coTestCounts(t, coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: 14}, &IT{DestinationLocalReference: ref, SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}, SequencingSegmenting: params.SequencingSegmenting{SendSequenceNumber: 3, ReceiveSequenceNumber: 4, MoreData: true}, Credit: 9}), 0, 0, 0)
	clock.advance(2 * time.Second)
	res = s.HandleTimeout()
	coTestCounts(t, res, 1, 0, 0)
	coTestTransfer(t, res, 0, label, &IT{DestinationLocalReference: 0x102, SourceLocalReference: ref, ProtocolClass: params.ProtocolClass{Class: 2}})
	clock.advance(5 * time.Second)
	coTestCounts(t, s.HandleTimeout(), 1, 0, 0)
	clock.advance(5 * time.Second)
	res = s.HandleTimeout()
	coTestCounts(t, res, 1, 1, 0)
	coTestTransfer(t, res, 0, label, &RLSD{DestinationLocalReference: 0x102, SourceLocalReference: ref, ReleaseCause: params.ReleaseCauseExpirationOfReceiveInactivityTimer})
	want := &DisconnectIndication{LocalReference: ref, Originator: OriginatorProvider, ReleaseCause: params.ReleaseCauseExpirationOfReceiveInactivityTimer}
	if !reflect.DeepEqual(res.Indications[0], want) {
		t.Fatalf("inactivity indication = %#v", res.Indications[0])
	}
	coTestState(t, s, ref, ConnectionReleasePending)
}

// ITU-T Q.714 (05/01) Annex C.4: zero configuration selects the contracted
// defaults, observable through their actual expiration times.
func TestConnectionDefaultEstablishmentTimer(t *testing.T) {
	s, clock := coTestStack(t, StackConfig{})
	ref, _ := coTestOutgoing(t, s)
	clock.advance(90*time.Second - time.Nanosecond)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	clock.advance(time.Nanosecond)
	coTestCounts(t, s.HandleTimeout(), 0, 1, 0)
	coTestState(t, s, ref, ConnectionFrozen)
	clock.advance(2 * time.Minute)
	coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
	if _, ok := s.Connection(ref); ok {
		t.Fatal("default freeze did not expire")
	}
}

// ITU-T Q.714 (05/01) §3.4: T(iar) must exceed T(ias); all timer and limit
// values must be nonnegative under the public configuration contract.
func TestConnectionConfigurationValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  ConnectionConfig
	}{
		{"establishment", ConnectionConfig{EstablishmentTimeout: -1}},
		{"release", ConnectionConfig{ReleaseTimeout: -1}},
		{"repeat", ConnectionConfig{RepeatReleaseTimeout: -1}},
		{"give_up", ConnectionConfig{ReleaseGiveUpTimeout: -1}},
		{"send_inactivity", ConnectionConfig{InactivitySendTimeout: -1}},
		{"receive_inactivity", ConnectionConfig{InactivityReceiveTimeout: -1}},
		{"freeze", ConnectionConfig{ReferenceFreeze: -1}},
		{"connections", ConnectionConfig{MaxConnections: -1}},
		{"data", ConnectionConfig{MaxDataLength: -1}},
		{"equal_inactivity", ConnectionConfig{InactivitySendTimeout: time.Second, InactivityReceiveTimeout: time.Second}},
		{"shorter_receive", ConnectionConfig{InactivitySendTimeout: 2 * time.Second, InactivityReceiveTimeout: time.Second}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewStack(StackConfig{Connection: tc.cfg})
			if s != nil || !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("NewStack = %v, %v", s, err)
			}
		})
	}
}

// ITU-T Q.713 (03/01) Tables 3-6 and §3.4: invalid primitive parameters
// cannot allocate references, emit messages or modify a valid connection.
func TestConnectionRequestValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  ConnectRequest
	}{
		{"missing_destination", ConnectRequest{CalledPartyAddress: params.NewSSNAddress(6)}},
		{"long_data", ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6), Data: make([]byte, 129)}},
		{"invalid_address", ConnectRequest{CalledPartyAddress: params.PartyAddress{RouteOnSSN: true, HasPointCode: true, PointCode: 0x4000, HasSSN: true, SSN: 6}}},
		{"importance", ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6), Importance: func() *params.Importance { x := params.Importance(8); return &x }()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
			_, res, err := s.Connect(tc.req)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("Connect error = %v", err)
			}
			coTestCounts(t, res, 0, 0, 0)
			if len(s.conn.sections) != 0 {
				t.Fatal("invalid request allocated a section")
			}
		})
	}
	s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
	ref, _ := coTestEstablished(t, s)
	for _, data := range [][]byte{nil, {}, make([]byte, 65536)} {
		res, err := s.SendData(ref, data)
		if !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("SendData length %d error = %v", len(data), err)
		}
		coTestCounts(t, res, 0, 0, 0)
		coTestState(t, s, ref, ConnectionEstablished)
	}
	res, err := s.Disconnect(ref, DisconnectRequest{Data: make([]byte, 129)})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Disconnect error = %v", err)
	}
	coTestCounts(t, res, 0, 0, 0)
	coTestState(t, s, ref, ConnectionEstablished)
	incoming := coTestIncoming(t, s, 2)
	res, err = s.Accept(incoming, ConnectResponse{Data: make([]byte, 129)})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Accept error = %v", err)
	}
	coTestCounts(t, res, 0, 0, 0)
	coTestState(t, s, incoming, ConnectionIncomingPending)
}

// ITU-T Q.714 (05/01) §3.1.4.1: the public DPC override determines the
// destination label when the called party address does not supply a point code.
func TestConnectionExplicitDestination(t *testing.T) {
	for _, called := range []params.PartyAddress{params.NewSSNAddress(6), params.NewPointCodeAddress(2, 6)} {
		s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
		ref, res, err := s.Connect(ConnectRequest{CalledPartyAddress: called, DPC: 3})
		if err != nil {
			t.Fatal(err)
		}
		coTestCounts(t, res, 1, 0, 0)
		label := res.Transfers[0].RoutingLabel
		if label.OPC != 1 || label.DPC != 3 || label.SLS > 15 {
			t.Fatalf("explicit destination label = %+v", label)
		}
		coTestTransfer(t, res, 0, label, &CR{SourceLocalReference: ref, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: called})
	}
}

// ITU-T Q.714 (05/01) §3.1.4.2: disconnect during outgoing setup is deferred
// until CC, CREF or establishment expiry, and request storage is detached.
// Q.711 (03/01) Figure 8 and Q.714 Figure C.2 sheet 3: after the user
// disconnects, setup failures do not produce another user indication.
func TestConnectionDeferredOutgoingDisconnect(t *testing.T) {
	for _, outcome := range []string{"confirmed", "refused", "expired"} {
		t.Run(outcome, func(t *testing.T) {
			s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, label := coTestOutgoing(t, s)
			data := []byte{8}
			importance := params.Importance(2)
			res, err := s.Disconnect(ref, DisconnectRequest{ReleaseCause: params.ReleaseCauseEndUserOriginated, Data: data, Importance: &importance})
			if err != nil {
				t.Fatal(err)
			}
			coTestCounts(t, res, 0, 0, 0)
			coTestState(t, s, ref, ConnectionOutgoingPending)
			data[0], importance = 0, 7
			peer := RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}
			switch outcome {
			case "confirmed":
				res = coTestReceive(t, s, peer, &CC{DestinationLocalReference: ref, SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}})
				coTestCounts(t, res, 1, 0, 0)
				wantImportance := params.Importance(2)
				coTestTransfer(t, res, 0, label, &RLSD{DestinationLocalReference: 0x102, SourceLocalReference: ref, ReleaseCause: params.ReleaseCauseEndUserOriginated, Data: []byte{8}, Importance: &wantImportance})
				coTestState(t, s, ref, ConnectionReleasePending)
			case "refused":
				res = coTestReceive(t, s, peer, &CREF{DestinationLocalReference: ref, RefusalCause: params.RefusalCauseSCCPUserOriginated})
				coTestCounts(t, res, 0, 0, 0)
				coTestState(t, s, ref, ConnectionFrozen)
			case "expired":
				clock.advance(10 * time.Second)
				coTestCounts(t, s.HandleTimeout(), 0, 0, 0)
				coTestState(t, s, ref, ConnectionFrozen)
			}
		})
	}
}

// ITU-T Q.713 (03/01) Table 8 and ITU-T Q.714 (05/01) §3.5.3: segmentation
// also respects a configured message budget below the maximum DT1 wire size.
func TestConnectionSmallMessageBudget(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{Connection: coTestConfig(), MaxMessageSize: 15})
	_, res, err := s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6), Data: make([]byte, 128)})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("oversized CR error = %v", err)
	}
	coTestCounts(t, res, 0, 0, 0)
	if len(s.conn.sections) != 0 {
		t.Fatal("failed CR consumed a connection")
	}
	ref, label := coTestEstablished(t, s)
	res, err = s.SendData(ref, bytes.Repeat([]byte{0x21}, 10))
	if err != nil {
		t.Fatal(err)
	}
	coTestCounts(t, res, 2, 0, 0)
	coTestTransfer(t, res, 0, label, &DT1{DestinationLocalReference: 0x102, MoreData: true, Data: bytes.Repeat([]byte{0x21}, 8)})
	coTestTransfer(t, res, 1, label, &DT1{DestinationLocalReference: 0x102, Data: bytes.Repeat([]byte{0x21}, 2)})
	for _, tr := range res.Transfers {
		if len(tr.Data) > 15 {
			t.Fatalf("message budget exceeded: %d", len(tr.Data))
		}
	}
	incoming := coTestIncoming(t, s, 2)
	res, err = s.Accept(incoming, ConnectResponse{Data: make([]byte, 128)})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("oversized CC error = %v", err)
	}
	coTestCounts(t, res, 0, 0, 0)
	coTestState(t, s, incoming, ConnectionIncomingPending)
	res, err = s.Accept(incoming, ConnectResponse{})
	if err != nil {
		t.Fatal(err)
	}
	coTestCounts(t, res, 1, 0, 0)
}

// ITU-T Q.714 (05/01) §§3.1, 3.3: simultaneous setup and release primitives
// serialize allocation and transitions, including active capacity reclaimed
// while references remain frozen.
func TestConnectionConcurrentLifecycles(t *testing.T) {
	cfg := coTestConfig()
	cfg.MaxConnections = 8
	s, _ := coTestStack(t, StackConfig{Connection: cfg})
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for worker := range 8 {
		wg.Go(func() {
			fail := func(format string, args ...any) { errCh <- fmt.Errorf(format, args...) }
			remote := params.LocalReference(0x200 + worker)
			for range 20 {
				var ref params.LocalReference
				var label RoutingLabel
				if worker%2 == 0 {
					var res Result
					var err error
					ref, res, err = s.Connect(ConnectRequest{CalledPartyAddress: params.NewPointCodeAddress(2, 6)})
					if err != nil || len(res.Transfers) != 1 {
						fail("Connect = %+v, %v", res, err)
						return
					}
					label = res.Transfers[0].RoutingLabel
					b, err := (&CC{DestinationLocalReference: ref, SourceLocalReference: remote, ProtocolClass: params.ProtocolClass{Class: 2}}).MarshalBinary()
					if err != nil {
						fail("encode CC: %v", err)
						return
					}
					res = s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, Data: b})
					if len(res.Transfers) != 0 || len(res.Indications) != 1 || len(res.Discards) != 0 {
						fail("CC = %+v", res)
						return
					}
				} else {
					label = RoutingLabel{OPC: 1, DPC: 2, SLS: uint8(worker)}
					b, err := (&CR{SourceLocalReference: remote, ProtocolClass: params.ProtocolClass{Class: 2}, CalledPartyAddress: params.NewPointCodeAddress(1, 6)}).MarshalBinary()
					if err != nil {
						fail("encode CR: %v", err)
						return
					}
					res := s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1, SLS: uint8(worker)}, Data: b})
					if len(res.Transfers) != 0 || len(res.Indications) != 1 || len(res.Discards) != 0 {
						fail("CR = %+v", res)
						return
					}
					ind, ok := res.Indications[0].(*ConnectIndication)
					if !ok {
						fail("CR indication = %#v", res.Indications[0])
						return
					}
					ref = ind.LocalReference
					res, err = s.Accept(ref, ConnectResponse{})
					if err != nil || len(res.Transfers) != 1 || res.Transfers[0].RoutingLabel != label {
						fail("Accept = %+v, %v", res, err)
						return
					}
				}
				res, err := s.Disconnect(ref, DisconnectRequest{})
				if err != nil || len(res.Transfers) != 1 || res.Transfers[0].RoutingLabel != label {
					fail("Disconnect = %+v, %v", res, err)
					return
				}
				b, err := (&RLC{DestinationLocalReference: ref, SourceLocalReference: remote}).MarshalBinary()
				if err != nil {
					fail("encode RLC: %v", err)
					return
				}
				res = s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, Data: b})
				if !res.empty() {
					fail("RLC = %+v", res)
					return
				}
				info, ok := s.Connection(ref)
				if !ok || info.State != ConnectionFrozen {
					fail("completed connection = %+v, %t", info, ok)
					return
				}
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

// ITU-T Q.714 (05/01) §3.1.4.2 item 3: associate the signalling point that
// actually confirms the connection while retaining the originally assigned SLS.
func TestConnectionConfirmationAssociatesActualPeer(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
	ref, label := coTestOutgoing(t, s)
	res := coTestReceive(t, s, RoutingLabel{OPC: 3, DPC: 1, SLS: 13}, &CC{DestinationLocalReference: ref, SourceLocalReference: 0x102, ProtocolClass: params.ProtocolClass{Class: 2}})
	coTestCounts(t, res, 0, 1, 0)
	label.DPC = 3
	info, ok := s.Connection(ref)
	if !ok || info.Remote != label {
		t.Fatalf("confirming peer snapshot = %+v, %t", info, ok)
	}
	res, err := s.SendData(ref, []byte{8})
	if err != nil {
		t.Fatal(err)
	}
	coTestCounts(t, res, 1, 0, 0)
	coTestTransfer(t, res, 0, label, &DT1{DestinationLocalReference: 0x102, Data: []byte{8}})
}

// ITU-T Q.714 (05/01) §§3.1.2, 3.3.2: allocation must skip a deliberately
// selected occupied candidate, including a frozen candidate with free capacity.
func TestConnectionAllocationSkipsActiveAndFrozen(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
	first, label := coTestOutgoing(t, s)
	s.conn.next = first
	second, _ := coTestOutgoing(t, s)
	if second == first {
		t.Fatal("allocated an active reference")
	}
	res := coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, &CREF{DestinationLocalReference: first, RefusalCause: params.RefusalCauseSCCPUserOriginated})
	coTestCounts(t, res, 0, 1, 0)
	s.conn.next = first
	third, _ := coTestOutgoing(t, s)
	if third == first || third == second {
		t.Fatalf("allocated an active or frozen reference: %#x", third)
	}
	coTestState(t, s, first, ConnectionFrozen)
	coTestState(t, s, second, ConnectionOutgoingPending)
}

// The public primitive contract reports unknown and frozen references as
// lookup failures and rejects requests in every incompatible live state.
func TestConnectionRequestStateAndReferenceErrors(t *testing.T) {
	for _, state := range []string{"unknown", "incoming", "outgoing", "established", "release", "frozen"} {
		t.Run(state, func(t *testing.T) {
			s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref := params.LocalReference(99)
			switch state {
			case "incoming":
				ref = coTestIncoming(t, s, 2)
			case "outgoing":
				ref, _ = coTestOutgoing(t, s)
			case "established", "release", "frozen":
				var label RoutingLabel
				ref, label = coTestEstablished(t, s)
				if state != "established" {
					if _, err := s.Disconnect(ref, DisconnectRequest{}); err != nil {
						t.Fatal(err)
					}
				}
				if state == "frozen" {
					coTestReceive(t, s, RoutingLabel{OPC: 2, DPC: 1, SLS: label.SLS}, &RLC{DestinationLocalReference: ref, SourceLocalReference: 0x102})
				}
			}
			wantErr := ErrConnectionState
			if state == "unknown" || state == "frozen" {
				wantErr = ErrConnectionNotFound
			}
			for _, method := range []string{"Accept", "SendData", "Disconnect"} {
				if method == "Accept" && state == "incoming" || method == "SendData" && state == "established" || method == "Disconnect" && (state == "incoming" || state == "outgoing" || state == "established") {
					continue
				}
				var res Result
				var err error
				switch method {
				case "Accept":
					res, err = s.Accept(ref, ConnectResponse{})
				case "SendData":
					res, err = s.SendData(ref, []byte{1})
				case "Disconnect":
					res, err = s.Disconnect(ref, DisconnectRequest{})
				}
				if !errors.Is(err, wantErr) {
					t.Fatalf("%s error = %v, want %v", method, err, wantErr)
				}
				coTestCounts(t, res, 0, 0, 0)
			}
		})
	}
}

// The public primitive contract distinguishes lookup failures from invalid
// states; failed requests still return the results of every already-due timer.
func TestConnectionRequestErrorsExpireDueTimers(t *testing.T) {
	for _, method := range []string{"Connect", "Accept", "SendData", "Disconnect"} {
		t.Run(method, func(t *testing.T) {
			s, clock := coTestStack(t, StackConfig{Connection: coTestConfig()})
			ref, _ := coTestOutgoing(t, s)
			clock.advance(10 * time.Second)
			var res Result
			var err error
			wantErr := ErrConnectionNotFound
			switch method {
			case "Connect":
				_, res, err = s.Connect(ConnectRequest{})
				wantErr = ErrInvalidRequest
			case "Accept":
				res, err = s.Accept(99, ConnectResponse{})
			case "SendData":
				res, err = s.SendData(99, []byte{1})
			case "Disconnect":
				res, err = s.Disconnect(99, DisconnectRequest{})
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want %v", err, wantErr)
			}
			coTestCounts(t, res, 0, 1, 0)
			got, ok := res.Indications[0].(*DisconnectIndication)
			if !ok || got.LocalReference != ref || got.RefusalCause != params.RefusalCauseExpirationOfTheConnectionEstablishmentTimer {
				t.Fatalf("due timer result = %#v", res.Indications[0])
			}
		})
	}
	s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
	ref, _ := coTestOutgoing(t, s)
	res, err := s.Accept(ref, ConnectResponse{})
	if !errors.Is(err, ErrConnectionState) {
		t.Fatalf("Accept outgoing error = %v", err)
	}
	coTestCounts(t, res, 0, 0, 0)
	res, err = s.SendData(ref, []byte{1})
	if !errors.Is(err, ErrConnectionState) {
		t.Fatalf("SendData outgoing error = %v", err)
	}
	coTestCounts(t, res, 0, 0, 0)
}

// Connection is a snapshot; neither request addresses nor returned address
// trees may share mutable storage with a connection owned by the stack.
func TestConnectionSnapshotsAndRequestStorageAreIndependent(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
	gt, err := params.NewGlobalTitle1(params.NAIInternationalNumber, "1234")
	if err != nil {
		t.Fatal(err)
	}
	called := params.NewPointCodeAddress(2, 6)
	called.GlobalTitle = gt
	calling := called
	ref, res, err := s.Connect(ConnectRequest{CalledPartyAddress: called, CallingPartyAddress: &calling, Data: []byte{1}})
	if err != nil {
		t.Fatal(err)
	}
	gt.AddressInformation[0] = 0
	calling.SSN = 99
	info, ok := s.Connection(ref)
	if !ok || info.CalledPartyAddress.GlobalTitle.AddressInformation[0] != 0x21 || info.CallingPartyAddress.SSN != 6 {
		t.Fatalf("request storage mutated snapshot: %+v", info)
	}
	info.CalledPartyAddress.SSN = 90
	info.CalledPartyAddress.GlobalTitle.AddressInformation[0] = 0
	info.CallingPartyAddress.GlobalTitle.AddressInformation[0] = 0
	res.Transfers[0].Message.(*CR).CalledPartyAddress.GlobalTitle.AddressInformation[0] = 0
	info, _ = s.Connection(ref)
	if info.CalledPartyAddress.SSN != 6 || info.CalledPartyAddress.GlobalTitle.AddressInformation[0] != 0x21 || info.CallingPartyAddress.GlobalTitle.AddressInformation[0] != 0x21 {
		t.Fatalf("snapshot leaked stack storage: %+v", info)
	}
}

// The public stack methods serialize access to sections. Concurrent snapshot,
// send, receive and timeout calls must retain a single connection and label.
func TestConnectionConcurrentPublicMethods(t *testing.T) {
	s, _ := coTestStack(t, StackConfig{Connection: coTestConfig()})
	ref, label := coTestEstablished(t, s)
	wire, err := (&DT1{DestinationLocalReference: ref, Data: []byte{1}}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for worker := range 8 {
		wg.Go(func() {
			for range 30 {
				switch worker % 4 {
				case 0:
					res, err := s.SendData(ref, []byte{2})
					if err != nil || len(res.Transfers) != 1 || res.Transfers[0].RoutingLabel != label {
						errCh <- fmt.Errorf("send result %+v, %v", res, err)
						return
					}
				case 1:
					info, ok := s.Connection(ref)
					if !ok || info.State != ConnectionEstablished || info.Remote != label {
						errCh <- fmt.Errorf("snapshot %+v, %t", info, ok)
						return
					}
					info.CalledPartyAddress.SSN = 99
				case 2:
					res := s.HandleTransfer(TransferIndication{RoutingLabel: RoutingLabel{OPC: 2, DPC: 1, SLS: 13}, Data: wire})
					if len(res.Transfers) != 0 || len(res.Indications) != 1 || len(res.Discards) != 0 {
						errCh <- fmt.Errorf("receive result %+v", res)
						return
					}
				case 3:
					if res := s.HandleTimeout(); !res.empty() {
						errCh <- fmt.Errorf("premature timer result %+v", res)
						return
					}
				}
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	coTestState(t, s, ref, ConnectionEstablished)
	info, _ := s.Connection(ref)
	if info.CalledPartyAddress.SSN != 6 {
		t.Fatal("concurrent snapshot modified section")
	}
	if ConnectionEstablished.String() == "" || OriginatorProvider.String() == "" {
		t.Fatal("state or originator has no string representation")
	}
}
