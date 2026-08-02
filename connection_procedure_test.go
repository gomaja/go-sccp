// Copyright 2019-2024 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp_test

import (
	"errors"
	"testing"

	"github.com/gomaja/go-sccp"
	"github.com/gomaja/go-sccp/params"
)

func TestConnectionSectionConfirmEstablishesOriginatingSection(t *testing.T) {
	section := sccp.NewPendingConnectionSection(0x010203, 3, 9)
	confirm := sccp.NewCC(0x010203, 0x040506, 3, false, params.NewCreditOptional(5))

	if err := section.HandleConnectionConfirm(confirm); err != nil {
		t.Fatalf("HandleConnectionConfirm() error = %v", err)
	}
	if got, want := section.State, sccp.ConnectionStateEstablished; got != want {
		t.Fatalf("State = %v, want %v", got, want)
	}
	if got, want := section.LocalReference, uint32(0x010203); got != want {
		t.Fatalf("LocalReference = %#06x, want %#06x", got, want)
	}
	if got, want := section.RemoteReference, uint32(0x040506); got != want {
		t.Fatalf("RemoteReference = %#06x, want %#06x", got, want)
	}
	if got, want := section.ProtocolClass, 3; got != want {
		t.Fatalf("ProtocolClass = %d, want %d", got, want)
	}
	if got, want := section.Credit, uint8(5); got != want {
		t.Fatalf("Credit = %d, want %d", got, want)
	}
}

func TestConnectionSectionConfirmCanDowngradeProtocolClass(t *testing.T) {
	section := sccp.NewPendingConnectionSection(0x010203, 3, 9)
	confirm := sccp.NewCC(0x010203, 0x040506, 2, false)

	if err := section.HandleConnectionConfirm(confirm); err != nil {
		t.Fatalf("HandleConnectionConfirm() error = %v", err)
	}
	if got, want := section.ProtocolClass, 2; got != want {
		t.Fatalf("ProtocolClass = %d, want %d", got, want)
	}
}

func TestConnectionSectionConfirmRequiresMatchingLocalReference(t *testing.T) {
	section := sccp.NewPendingConnectionSection(0x010203, 3, 9)

	err := section.HandleConnectionConfirm(sccp.NewCC(0x070809, 0x040506, 2, false))
	if !errors.Is(err, sccp.ErrReferenceMismatch) {
		t.Fatalf("HandleConnectionConfirm() error = %v, want %v", err, sccp.ErrReferenceMismatch)
	}
	if got, want := section.State, sccp.ConnectionStatePending; got != want {
		t.Fatalf("State = %v, want %v", got, want)
	}
	if got := section.RemoteReference; got != 0 {
		t.Fatalf("RemoteReference = %#06x, want zero", got)
	}
}

func TestConnectionSectionRefusalFreezesPendingReference(t *testing.T) {
	section := sccp.NewPendingConnectionSection(0x010203, 3, 9)
	refusal := sccp.NewCREF(0x010203, params.RefusalCauseSCCPUserOriginated)

	if err := section.HandleConnectionRefused(refusal); err != nil {
		t.Fatalf("HandleConnectionRefused() error = %v", err)
	}
	if got, want := section.State, sccp.ConnectionStateFrozen; got != want {
		t.Fatalf("State = %v, want %v", got, want)
	}
}

func TestConnectionSectionReleaseSendsReleasedMessage(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 2, 0)

	released, err := section.Release(params.ReleaseCauseSCCPUserOriginated)
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if got, want := section.State, sccp.ConnectionStateReleasePending; got != want {
		t.Fatalf("State = %v, want %v", got, want)
	}
	if got, want := released.DestinationLocalReference.Uint32(), uint32(0x040506); got != want {
		t.Fatalf("RLSD destination reference = %#06x, want %#06x", got, want)
	}
	if got, want := released.SourceLocalReference.Uint32(), uint32(0x010203); got != want {
		t.Fatalf("RLSD source reference = %#06x, want %#06x", got, want)
	}
	if got, want := released.ReleaseCause.Value(), params.ReleaseCauseSCCPUserOriginated; got != want {
		t.Fatalf("RLSD release cause = %v, want %v", got, want)
	}
}

func TestConnectionSectionReleaseCompleteFreezesReference(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 2, 0)
	if _, err := section.Release(params.ReleaseCauseSCCPUserOriginated); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	if err := section.HandleReleaseComplete(sccp.NewRLC(0x010203, 0x040506)); err != nil {
		t.Fatalf("HandleReleaseComplete() error = %v", err)
	}
	if got, want := section.State, sccp.ConnectionStateFrozen; got != want {
		t.Fatalf("State = %v, want %v", got, want)
	}
}

func TestConnectionSectionReleasedRepliesWithReleaseComplete(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 2, 0)
	released := sccp.NewRLSD(0x010203, 0x040506, params.ReleaseCauseSCCPUserOriginated)

	complete, err := section.HandleReleased(released)
	if err != nil {
		t.Fatalf("HandleReleased() error = %v", err)
	}
	if got, want := section.State, sccp.ConnectionStateFrozen; got != want {
		t.Fatalf("State = %v, want %v", got, want)
	}
	if got, want := complete.DestinationLocalReference.Uint32(), uint32(0x040506); got != want {
		t.Fatalf("RLC destination reference = %#06x, want %#06x", got, want)
	}
	if got, want := complete.SourceLocalReference.Uint32(), uint32(0x010203); got != want {
		t.Fatalf("RLC source reference = %#06x, want %#06x", got, want)
	}
}

func TestConnectionSectionReleasedRequiresKnownReferences(t *testing.T) {
	section := sccp.NewEstablishedConnectionSection(0x010203, 0x040506, 2, 0)

	_, err := section.HandleReleased(sccp.NewRLSD(0x010203, 0x070809, params.ReleaseCauseSCCPUserOriginated))
	if !errors.Is(err, sccp.ErrReferenceMismatch) {
		t.Fatalf("HandleReleased() error = %v, want %v", err, sccp.ErrReferenceMismatch)
	}
	if got, want := section.State, sccp.ConnectionStateEstablished; got != want {
		t.Fatalf("State = %v, want %v", got, want)
	}
}
