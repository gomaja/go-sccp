// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

/*
Command sccp-sgp receives SCCP connectionless user data at an M3UA SGP.

Every M3UA DATA message from an ASP goes to a go-sccp Stack, which reassembles
segmented XUDTs and prints each N-UNITDATA indication. Messages the stack
returns — a UDTS or XUDTS for data it cannot deliver, or for a reassembly that
fails — are sent back to the ASP on the same association. Connection requests
are refused: the stack waits for its user to answer them.
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
	"sync"
	"syscall"

	"github.com/gomaja/go-m3ua"
	m3params "github.com/gomaja/go-m3ua/messages/params"
	"github.com/gomaja/go-sctp"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

func main() {
	var (
		addr       = flag.String("addr", "127.0.0.1:2905", "Local SCTP address.")
		pc         = flag.Uint("pc", 0x0202, "Point code of this node (ITU, 14 bits).")
		ssn        = flag.Uint("ssn", 6, "Local subsystem that receives the user data.")
		rc         = flag.Uint("routing-context", 1, "M3UA Routing Context of the Application Server.")
		maxMessage = flag.Int("max-message-size", sccp.DefaultMaxMessageSize, "Largest SCCP message to send, in octets.")
	)
	flag.Parse()

	stack, err := sccp.NewStack(sccp.StackConfig{PointCode: uint32(*pc), MaxMessageSize: *maxMessage})
	if err != nil {
		log.Fatalf("Invalid SCCP configuration: %s", err)
	}
	stack.RegisterSubsystem(uint8(*ssn))

	config := m3ua.NewAssociationConfig().SetApplicationServers(m3ua.ASConfig{
		ASKey:       m3ua.ASKey{NetworkAppearance: 0, NetworkAppearanceSet: true, RoutingContext: uint32(*rc), RoutingContextSet: true},
		TrafficMode: m3params.TrafficModeLoadshare,
	})

	laddr, err := sctp.ResolveAddr("sctp", *addr)
	if err != nil {
		log.Fatalf("Failed to resolve the SCTP address: %s", err)
	}
	endpoint, err := m3ua.NewEndpoint(m3ua.EndpointConfig{Role: m3ua.RoleSGP})
	if err != nil {
		log.Fatalf("Failed to create the M3UA SGP endpoint: %s", err)
	}
	listener, err := endpoint.Listen("m3ua", laddr, m3ua.NewListenerConfig(config))
	if err != nil {
		log.Fatalf("Failed to listen: %s", err)
	}
	log.Printf("Waiting for an SCTP association on %s", listener.Addr())

	// An SGP has no withdrawal to run (RFC 4666 Section 4.9), so an interrupt
	// may close the associations directly.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Reassembly timer expiries return XUDTS messages; they go out on the
	// association the first segment came from.
	routes := newReplyRoutes()
	go stack.RunTimers(ctx, func(res sccp.Result) { deliver(ctx, stack, res, routes) })

	go func() {
		for {
			association, err := listener.Accept(ctx)
			if err != nil {
				var rejected *m3ua.AssociationEstablishmentError
				if errors.As(err, &rejected) {
					log.Printf("Rejected an M3UA association: %s", rejected)
					continue
				}
				log.Printf("Accept stopped: %s", err)
				cancel()
				return
			}
			log.Printf("Associated with %s", association.RemoteAddr())
			go serve(ctx, stack, association, routes)
		}
	}()
	<-ctx.Done()
	if err := endpoint.Close(); err != nil {
		log.Printf("Failed to close the M3UA endpoint: %s", err)
	}
}

func serve(ctx context.Context, stack *sccp.Stack, association *m3ua.Association, routes *replyRoutes) {
	defer func() { _ = association.Close() }()
	for {
		message, err := association.ReadData(ctx)
		if err != nil {
			if !errors.Is(err, m3ua.ErrNotEstablished) && ctx.Err() == nil {
				log.Printf("Reading from %s: %s", association.RemoteAddr(), err)
			}
			return
		}
		pd := message.ProtocolData
		routes.learn(pd.OriginatingPointCode, reply{association: association, as: message.AS, ni: pd.NetworkIndicator})
		deliver(ctx, stack, stack.HandleTransfer(sccp.TransferIndication{
			RoutingLabel: sccp.RoutingLabel{OPC: pd.OriginatingPointCode, DPC: pd.DestinationPointCode, SLS: pd.SignallingLinkSelection},
			Data:         pd.Data,
		}), routes)
	}
}

// deliver prints what the stack produced and sends its messages back to the
// peers they are addressed to. Cancelling ctx abandons the sends.
func deliver(ctx context.Context, stack *sccp.Stack, res sccp.Result, routes *replyRoutes) {
	for _, ind := range res.Indications {
		switch ind := ind.(type) {
		case *sccp.UnitdataIndication:
			log.Printf("N-UNITDATA indication: %s from %s to %s, %d octets in %d segment(s): %s",
				ind.MessageType, ind.CallingPartyAddress, ind.CalledPartyAddress, len(ind.Data), ind.Segments, hex.EncodeToString(ind.Data))
		case *sccp.ConnectIndication:
			// The connection waits for its user's answer with no timer:
			// this example serves no connections, so it refuses them.
			refusal, err := stack.Disconnect(ind.LocalReference, sccp.DisconnectRequest{RefusalCause: params.RefusalCauseEndUserOriginated})
			if err != nil {
				log.Printf("Refusing the connection from %d: %s", ind.OPC, err)
				continue
			}
			log.Printf("Refused a connection request from %d", ind.OPC)
			deliver(ctx, stack, refusal, routes)
		case *sccp.MaintenanceIndication:
			log.Printf("Maintenance: %s from %d: %s", ind.MessageType, ind.OPC, ind.Reason)
		}
	}
	for _, d := range res.Discards {
		log.Printf("Discarded %s from %d: %s", d.MessageType, d.OPC, d.Reason)
	}
	for _, tr := range res.Transfers {
		r, ok := routes.lookup(tr.DPC)
		if !ok {
			log.Printf("No association towards %d for %s", tr.DPC, tr.Message.MessageType())
			continue
		}
		_, err := r.association.WriteDataContext(ctx, m3ua.DataRequest{
			AS:           r.as,
			ProtocolData: *m3params.NewProtocolDataPayload(tr.OPC, tr.DPC, m3params.ServiceIndSCCP, r.ni, 0, tr.SLS, tr.Data),
		})
		if err != nil {
			log.Printf("Sending %s: %s", tr.Message.MessageType(), err)
			continue
		}
		log.Printf("Sent %s back to %d", tr.Message.MessageType(), tr.DPC)
	}
}

// reply is where messages for a peer point code go: the association and the
// Application Server its last DATA arrived on, and its network indicator.
type reply struct {
	association *m3ua.Association
	as          m3ua.ASKey
	ni          uint8
}

// replyRoutes maps a peer point code to the reply route it was last heard on.
type replyRoutes struct {
	mu     sync.Mutex
	routes map[uint32]reply
}

func newReplyRoutes() *replyRoutes { return &replyRoutes{routes: make(map[uint32]reply)} }

func (r *replyRoutes) learn(pc uint32, route reply) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[pc] = route
}

func (r *replyRoutes) lookup(pc uint32) (reply, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	route, ok := r.routes[pc]
	return route, ok
}
