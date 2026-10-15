// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package params_test

import (
	"testing"

	"github.com/gomaja/go-sccp/params"
)

func benchmarkAddress(b *testing.B) params.PartyAddress {
	b.Helper()
	gt, err := params.NewGlobalTitle4(0, params.NPISDNTelephony, params.NAIInternationalNumber, "447700900001")
	if err != nil {
		b.Fatal(err)
	}
	return params.NewGlobalTitleAddress(gt, 6)
}

func BenchmarkPartyAddress(b *testing.B) {
	address := benchmarkAddress(b)
	wire, err := address.MarshalBinary()
	if err != nil {
		b.Fatal(err)
	}
	b.Run("AppendBinary", func(b *testing.B) {
		buf := make([]byte, 0, len(wire))
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		for b.Loop() {
			var err error
			buf, err = address.AppendBinary(buf[:0])
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("UnmarshalBinary", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		var address params.PartyAddress
		for b.Loop() {
			if err := address.UnmarshalBinary(wire); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkBCD(b *testing.B) {
	const digits = "447700900001"
	wire, odd, err := params.EncodeBCD(digits)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("Encode12Digits", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(digits)))
		for b.Loop() {
			if _, _, err := params.EncodeBCD(digits); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Decode12Digits", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(wire)))
		for b.Loop() {
			if _, err := params.DecodeBCD(wire, odd); err != nil {
				b.Fatal(err)
			}
		}
	})
}
