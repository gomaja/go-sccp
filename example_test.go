// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp_test

import (
	"errors"
	"fmt"
	"log"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

func ExampleUDT_MarshalBinary() {
	gt, err := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "447700900001")
	if err != nil {
		log.Fatal(err)
	}
	udt := &sccp.UDT{
		ProtocolClass:       params.ProtocolClass{Class: 0, ReturnOnError: true},
		CalledPartyAddress:  params.NewGlobalTitleAddress(gt, 6),
		CallingPartyAddress: params.NewSSNAddress(147),
		Data:                []byte{0x62, 0x03, 0x48, 0x01, 0x01},
	}
	wire, err := udt.MarshalBinary()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("% x\n", wire)

	m, err := sccp.ParseMessage(wire)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(m.(*sccp.UDT).CalledPartyAddress.GlobalTitle.Digits())

	// 256 octets of user data do not fit the one-octet length indicator.
	udt.Data = make([]byte, 256)
	_, err = udt.MarshalBinary()
	fmt.Println(errors.Is(err, sccp.ErrInvalidMessage))
	// Output:
	// 09 80 03 0e 10 0b 12 06 00 12 04 44 77 00 09 00 10 02 42 93 05 62 03 48 01 01
	// 447700900001 <nil>
	// true
}

func ExampleStack_Unitdata() {
	sender, err := sccp.NewStack(sccp.StackConfig{PointCode: 0x0101})
	if err != nil {
		log.Fatal(err)
	}
	receiver, err := sccp.NewStack(sccp.StackConfig{PointCode: 0x0202})
	if err != nil {
		log.Fatal(err)
	}
	receiver.RegisterSubsystem(6)

	res, err := sender.Unitdata(sccp.UnitdataRequest{
		CalledPartyAddress:  params.NewSSNAddress(6),
		CallingPartyAddress: params.NewSSNAddress(147),
		ProtocolClass:       1,
		ReturnOnError:       true,
		Data:                make([]byte, 600),
		DPC:                 0x0202,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, tr := range res.Transfers {
		fmt.Printf("%s of %d octets to %#x\n", tr.Message.MessageType(), len(tr.Data), tr.DPC)
		// The MTP carries the message to the receiving stack.
		in := receiver.HandleTransfer(sccp.TransferIndication{RoutingLabel: tr.RoutingLabel, Data: tr.Data})
		for _, ind := range in.Indications {
			u := ind.(*sccp.UnitdataIndication)
			fmt.Printf("delivered %d octets in %d segments to SSN %d\n", len(u.Data), u.Segments, u.CalledPartyAddress.SSN)
		}
	}
	// Output:
	// XUDT of 221 octets to 0x202
	// XUDT of 221 octets to 0x202
	// XUDT of 221 octets to 0x202
	// delivered 600 octets in 3 segments to SSN 6
}

func ExampleStack_Connect() {
	a, _ := sccp.NewStack(sccp.StackConfig{PointCode: 0x0101})
	b, _ := sccp.NewStack(sccp.StackConfig{PointCode: 0x0202})
	b.RegisterSubsystem(254)

	// deliver passes every message a stack sent to the other one, as the MTP
	// would, and prints what the receiving user is told.
	deliver := func(to *sccp.Stack, res sccp.Result) sccp.Result {
		var out sccp.Result
		for _, tr := range res.Transfers {
			fmt.Printf("  %s\n", tr.Message.MessageType())
			r := to.HandleTransfer(sccp.TransferIndication{RoutingLabel: tr.RoutingLabel, Data: tr.Data})
			out.Transfers = append(out.Transfers, r.Transfers...)
			for _, ind := range r.Indications {
				fmt.Printf("    %T\n", ind)
			}
			out.Indications = append(out.Indications, r.Indications...)
		}
		return out
	}

	ref, res, err := a.Connect(sccp.ConnectRequest{
		CalledPartyAddress:  params.NewPointCodeAddress(0x0202, 254),
		CallingPartyAddress: ptr(params.NewPointCodeAddress(0x0101, 254)),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("N-CONNECT request")
	in := deliver(b, res)
	remote := in.Indications[0].(*sccp.ConnectIndication).LocalReference

	fmt.Println("N-CONNECT response")
	res, _ = b.Accept(remote, sccp.ConnectResponse{})
	deliver(a, res)

	fmt.Println("N-DATA request of 300 octets")
	res, _ = a.SendData(ref, make([]byte, 300))
	deliver(b, res)

	fmt.Println("N-DISCONNECT request")
	res, _ = a.Disconnect(ref, sccp.DisconnectRequest{ReleaseCause: params.ReleaseCauseEndUserOriginated})
	deliver(a, deliver(b, res))
	// Output:
	// N-CONNECT request
	//   CR
	//     *sccp.ConnectIndication
	// N-CONNECT response
	//   CC
	//     *sccp.ConnectConfirmation
	// N-DATA request of 300 octets
	//   DT1
	//   DT1
	//     *sccp.DataIndication
	// N-DISCONNECT request
	//   RLSD
	//     *sccp.DisconnectIndication
	//   RLC
}
