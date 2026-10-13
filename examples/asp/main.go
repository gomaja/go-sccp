// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

/*
Command sccp-asp sends SCCP connectionless user data from an M3UA ASP.

It hands one N-UNITDATA request per -count to a go-sccp Stack and sends every
message the stack produces — a UDT, or the XUDT segments of user data too long
for one message — as M3UA DATA through go-m3ua's MTP-TRANSFER. Messages coming
back from the SGP, such as a UDTS or XUDTS for undeliverable data, are passed
to the stack and printed as N-NOTICE indications.

The default user data is a TCAP Begin carrying a GSM MAP unstructuredSS-Request,
the network-initiated USSD of networkUnstructuredSsContext-v2.
*/
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gomaja/go-m3ua"
	m3params "github.com/gomaja/go-m3ua/messages/params"
	"github.com/gomaja/go-sctp"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

// ussdBegin is a TCAP Begin with otid 1, the dialogue portion for
// networkUnstructuredSsContext-v2 and an invoke of unstructuredSS-Request (60)
// with a 160-character GSM 7-bit USSD string and an MSISDN.
const ussdBegin = "6281df4804000000016b1e281c060700118605010101a011600f80020780a1090607040000010013026c81b6a181b302010102013c3081aa04010f04819bd9775d0e1287d961f7b80c4acf4131996b460315ab521728fbaecb4135d0510812d7dd647619242fbbcbf739e8ed06c540ceb7bddc1697e52e90b40c67e7413190f92d07bdcde6b27cce02c940f43788fe8683ea7016680632bfe520f93bdc4ebbcf207878bc9eb3403410fd0da287d96b10fd0daacf5d202a3aec5e83f2ef3ac8fc9683e6f4703eed3e83ee693a1a549f83e8e8f41c942f87e521800791447700091032"

func main() {
	var (
		addr        = flag.String("addr", "127.0.0.1:2905", "Remote SGP SCTP address.")
		data        = flag.String("data", ussdBegin, "SCCP user data in hexadecimal.")
		count       = flag.Int("count", 1, "Number of N-UNITDATA requests to send.")
		interval    = flag.Duration("interval", time.Second, "Time between requests.")
		wait        = flag.Duration("wait", 2*time.Second, "Time to wait for returned messages after the last request.")
		opc         = flag.Uint("opc", 0x0101, "Point code of this node (ITU, 14 bits).")
		dpc         = flag.Uint("dpc", 0x0202, "Point code of the destination SCCP node.")
		ni          = flag.Uint("ni", 0, "MTP network indicator.")
		rc          = flag.Uint("routing-context", 1, "M3UA Routing Context.")
		calledGT    = flag.String("called-gt", "447700900001", "Called party global title digits (E.164); empty routes on SSN.")
		calledSSN   = flag.Uint("called-ssn", 6, "Called party subsystem number.")
		callingGT   = flag.String("calling-gt", "447700900099", "Calling party global title digits (E.164); empty for an SSN-only address.")
		callingSSN  = flag.Uint("calling-ssn", 147, "Calling party subsystem number.")
		class       = flag.Uint("class", 1, "SCCP protocol class, 0 or 1.")
		maxMessage  = flag.Int("max-message-size", sccp.DefaultMaxMessageSize, "Largest SCCP message to send, in octets.")
		segmentAtZ  = flag.Int("segmentation-threshold", 0, "Z of ITU-T Q.714 §4.1.1.1.1: segment user data longer than this into segments of at most this length; 0 segments only what does not fit.")
		returnOnErr = flag.Bool("return-on-error", true, "Request the message return procedure.")
	)
	flag.Parse()

	payload, err := hex.DecodeString(*data)
	if err != nil {
		log.Fatalf("Invalid -data: %s", err)
	}
	called, err := address(*calledGT, uint8(*calledSSN))
	if err != nil {
		log.Fatalf("Invalid called address: %s", err)
	}
	calling, err := address(*callingGT, uint8(*callingSSN))
	if err != nil {
		log.Fatalf("Invalid calling address: %s", err)
	}

	stack, err := sccp.NewStack(sccp.StackConfig{
		PointCode:             uint32(*opc),
		MaxMessageSize:        *maxMessage,
		SegmentationThreshold: *segmentAtZ,
	})
	if err != nil {
		log.Fatalf("Invalid SCCP configuration: %s", err)
	}
	// The calling subsystem receives the N-NOTICE indications of returned
	// messages.
	stack.RegisterSubsystem(uint8(*callingSSN))

	asKey := m3ua.ASKey{NetworkAppearance: 0, NetworkAppearanceSet: true, RoutingContext: uint32(*rc), RoutingContextSet: true}
	peer := m3ua.SGPIdentity{SignallingGateway: "sg", SignallingGatewayProcess: "sgp"}
	associationConfig := m3ua.NewAssociationConfig()
	associationConfig.
		SetASPIdentifier(1).
		SetApplicationServers(m3ua.ASConfig{ASKey: asKey, TrafficMode: m3params.TrafficModeLoadshare})
	associationConfig.PeerSGP = &peer

	endpoint, err := m3ua.NewEndpoint(m3ua.EndpointConfig{
		Role: m3ua.RoleASP,
		ASP: &m3ua.ASPConfig{
			SignallingGateways: []m3ua.SignallingGatewayConfig{{
				ID: peer.SignallingGateway,
				SGPs: []m3ua.SignallingGatewayProcessConfig{{
					ID:                 peer.SignallingGatewayProcess,
					ApplicationServers: []m3ua.RemoteASConfig{{ID: "as", ASKey: &asKey}},
				}},
			}},
			Routing: &m3ua.ASPRoutingConfig{
				SignallingGatewaySelection:        m3ua.RouteSelectionPrimaryBackup,
				SignallingGatewayProcessSelection: map[m3ua.SignallingGatewayID]m3ua.RouteSelectionMode{peer.SignallingGateway: m3ua.RouteSelectionPrimaryBackup},
				Paths:                             []m3ua.MTPRoutePath{{ID: "via-sg", SignallingGateway: peer.SignallingGateway, ApplicationServers: []m3ua.RemoteASID{"as"}}},
				MTPRoutes: []m3ua.MTPRouteConfig{{
					ID:                    "sccp",
					DestinationPointCode:  uint32(*dpc),
					ServiceIndicators:     []uint8{m3params.ServiceIndSCCP},
					OriginatingPointCodes: []uint32{uint32(*opc)},
					Paths:                 []m3ua.MTPRoutePathID{"via-sg"},
				}},
				// The example has no destination discovery of its own.
				AllowUnknownDestinations: true,
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to create the M3UA ASP endpoint: %s", err)
	}
	defer func() { _ = endpoint.Close() }()

	remote, err := sctp.ResolveAddr("sctp", *addr)
	if err != nil {
		log.Fatalf("Failed to resolve the SGP address: %s", err)
	}
	signals, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	// The association has its own lifetime, so that the withdrawal at the end
	// still runs after an interrupt (see the go-m3ua ASP example).
	lifetime, closeAssociation := context.WithCancel(context.Background())
	defer closeAssociation()
	association, err := endpoint.Dial(lifetime, "m3ua", nil, remote, associationConfig)
	if err != nil {
		log.Fatalf("Failed to establish the M3UA association: %s", err)
	}

	send := func(res sccp.Result) {
		report(res)
		for _, tr := range res.Transfers {
			transfer(signals, endpoint, tr, uint8(*ni))
		}
	}
	go stack.RunTimers(signals, send)
	go func() {
		for {
			message, err := association.ReadData(lifetime)
			if err != nil {
				return
			}
			pd := message.ProtocolData
			send(stack.HandleTransfer(sccp.TransferIndication{
				RoutingLabel: sccp.RoutingLabel{OPC: pd.OriginatingPointCode, DPC: pd.DestinationPointCode, SLS: pd.SignallingLinkSelection},
				Data:         pd.Data,
			}))
		}
	}()

	for i := range *count {
		if signals.Err() != nil {
			break
		}
		res, err := stack.Unitdata(sccp.UnitdataRequest{
			CalledPartyAddress:  called,
			CallingPartyAddress: calling,
			ProtocolClass:       uint8(*class),
			SequenceControl:     uint8(i),
			ReturnOnError:       *returnOnErr,
			Data:                payload,
			DPC:                 uint32(*dpc),
		})
		if err != nil {
			log.Fatalf("N-UNITDATA request refused: %s", err)
		}
		log.Printf("N-UNITDATA request %d: %d octets of user data in %d message(s)", i+1, len(payload), len(res.Transfers))
		send(res)
		if i+1 < *count {
			select {
			case <-time.After(*interval):
			case <-signals.Done():
			}
		}
	}
	select {
	case <-time.After(*wait):
	case <-signals.Done():
	}

	// RFC 4666 Section 4.9 option (a): ASP Inactive and ASP Down before the
	// association goes.
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := association.ShutdownContext(shutdown); err != nil {
		log.Printf("Graceful withdrawal did not complete: %s", err)
	}
}

// address returns a global title address routed on GT, or an SSN-only
// address routed on SSN when digits is empty.
func address(digits string, ssn uint8) (params.PartyAddress, error) {
	if digits == "" {
		return params.NewSSNAddress(ssn), nil
	}
	gt, err := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, digits)
	if err != nil {
		return params.PartyAddress{}, err
	}
	return params.NewGlobalTitleAddress(gt, ssn), nil
}

// transfer sends one SCCP message as M3UA DATA, retrying while the
// association is still being activated. Cancelling ctx abandons the send.
func transfer(ctx context.Context, endpoint *m3ua.Endpoint, tr sccp.TransferRequest, ni uint8) {
	request := m3ua.MTPTransferRequest{
		MTPRoute: "sccp",
		ProtocolData: m3params.NewProtocolDataPayload(
			tr.OPC, tr.DPC, m3params.ServiceIndSCCP, ni, 0, tr.SLS, tr.Data),
	}
	for attempt := 0; ; attempt++ {
		_, err := endpoint.MTPTransferContext(ctx, request)
		if err == nil {
			log.Printf("Sent %s: %d octets, SLS %d", tr.Message.MessageType(), len(tr.Data), tr.SLS)
			return
		}
		if ctx.Err() != nil {
			log.Printf("MTP-TRANSFER of %s abandoned: %s", tr.Message.MessageType(), err)
			return
		}
		var selection *m3ua.MTPSelectionError
		if attempt == 50 || !errors.As(err, &selection) {
			log.Printf("MTP-TRANSFER of %s failed: %s", tr.Message.MessageType(), err)
			return
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
}

func report(res sccp.Result) {
	for _, ind := range res.Indications {
		switch ind := ind.(type) {
		case *sccp.NoticeIndication:
			log.Printf("N-NOTICE: %s returned %d octets: %s", ind.MessageType, len(ind.Data), ind.ReasonForReturn)
		case *sccp.UnitdataIndication:
			log.Printf("N-UNITDATA indication: %d octets from %s", len(ind.Data), ind.CallingPartyAddress)
		case *sccp.MaintenanceIndication:
			log.Printf("Maintenance: %s from %d: %s", ind.MessageType, ind.OPC, ind.Reason)
		}
	}
	for _, d := range res.Discards {
		log.Printf("Discarded %s: %s", d.MessageType, d.Reason)
	}
}
