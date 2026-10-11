// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package params

import (
	"bytes"
	"fmt"
	"strings"
)

// MaxPointCode is the largest ITU signalling point code: 14 bits, ITU-T Q.713
// (03/01) §3.4.2.1.
const MaxPointCode = 0x3fff

// Address indicator bits, ITU-T Q.713 (03/01) §3.4.1, Figure 4.
const (
	aiPointCode   = 0x01
	aiSSN         = 0x02
	aiGTIShift    = 2
	aiGTIMask     = 0x0f
	aiRouteOnSSN  = 0x40
	aiNationalUse = 0x80
)

// PartyAddress is a called or calling party address, ITU-T Q.713 (03/01) §3.4
// and §3.5. The address indicator is derived from the fields on encoding.
type PartyAddress struct {
	// RouteOnSSN is the routing indicator, bit 7 of the address indicator:
	// route on the subsystem number when set, on the global title otherwise.
	RouteOnSSN bool

	// NationalUse is bit 8 of the address indicator, reserved for national
	// use and always zero on the international network.
	NationalUse bool

	// HasPointCode reports whether the address contains PointCode (bit 1).
	HasPointCode bool
	// PointCode is the 14-bit signalling point code, §3.4.2.1.
	PointCode uint16

	// HasSSN reports whether the address contains SSN (bit 2).
	HasSSN bool
	// SSN is the subsystem number, §3.4.2.2; 0 means not known.
	SSN uint8

	// GlobalTitle is the global title, nil when the address has none. Its
	// Indicator is the global title indicator of bits 3-6.
	GlobalTitle *GlobalTitle
}

// NewSSNAddress returns an address that routes on the subsystem number and
// contains nothing else.
func NewSSNAddress(ssn uint8) PartyAddress {
	return PartyAddress{RouteOnSSN: true, HasSSN: true, SSN: ssn}
}

// NewPointCodeAddress returns an address that routes on the subsystem number
// and contains the signalling point code and the subsystem number.
func NewPointCodeAddress(pc uint16, ssn uint8) PartyAddress {
	return PartyAddress{RouteOnSSN: true, HasPointCode: true, PointCode: pc, HasSSN: true, SSN: ssn}
}

// NewGlobalTitleAddress returns an address that routes on the global title and
// contains the subsystem number, which ITU-T Q.713 (03/01) §3.4.2 suggests
// every called party address carries; ssn 0 means not known.
func NewGlobalTitleAddress(gt *GlobalTitle, ssn uint8) PartyAddress {
	return PartyAddress{HasSSN: true, SSN: ssn, GlobalTitle: gt}
}

// AppendBinary appends the address contents — the address indicator followed
// by the address elements in the order point code, subsystem number, global
// title (§3.4.2) — to b. It does not append the length indicator.
func (a PartyAddress) AppendBinary(b []byte) ([]byte, error) {
	if !a.HasPointCode && a.PointCode != 0 {
		return b, fmt.Errorf("%w: point code %d set without HasPointCode", ErrInvalidValue, a.PointCode)
	}
	if a.PointCode > MaxPointCode {
		return b, fmt.Errorf("%w: point code %#x exceeds 14 bits", ErrInvalidValue, a.PointCode)
	}
	if !a.HasSSN && a.SSN != 0 {
		return b, fmt.Errorf("%w: subsystem number %d set without HasSSN", ErrInvalidValue, a.SSN)
	}

	var ai uint8
	if a.HasPointCode {
		ai |= aiPointCode
	}
	if a.HasSSN {
		ai |= aiSSN
	}
	if a.GlobalTitle != nil {
		if err := a.GlobalTitle.validate(); err != nil {
			return b, err
		}
		ai |= uint8(a.GlobalTitle.Indicator) << aiGTIShift
	}
	if a.RouteOnSSN {
		ai |= aiRouteOnSSN
	}
	if a.NationalUse {
		ai |= aiNationalUse
	}

	b = append(b, ai)
	if a.HasPointCode {
		b = append(b, byte(a.PointCode), byte(a.PointCode>>8))
	}
	if a.HasSSN {
		b = append(b, a.SSN)
	}
	if a.GlobalTitle != nil {
		b = a.GlobalTitle.appendContents(b)
	}
	return b, nil
}

// MarshalBinary returns the address contents.
func (a PartyAddress) MarshalBinary() ([]byte, error) { return a.AppendBinary(nil) }

// UnmarshalBinary decodes address contents (without the length indicator).
//
// The spare bits 7-8 of the point code are not examined. Octets that follow an
// address without a global title are ignored. The special calling party
// address that is only an address indicator with bits 1-7 zero is accepted
// (ITU-T Q.713 (03/01) §3.5).
//
// On error the receiver is left unchanged.
func (a *PartyAddress) UnmarshalBinary(b []byte) error {
	if len(b) < 1 {
		return truncated("party address")
	}
	ai := b[0]
	v := PartyAddress{
		RouteOnSSN:  ai&aiRouteOnSSN != 0,
		NationalUse: ai&aiNationalUse != 0,
	}
	off := 1
	if ai&aiPointCode != 0 {
		if len(b) < off+2 {
			return truncated("party address point code")
		}
		v.HasPointCode = true
		v.PointCode = (uint16(b[off]) | uint16(b[off+1])<<8) & MaxPointCode
		off += 2
	}
	if ai&aiSSN != 0 {
		if len(b) < off+1 {
			return truncated("party address subsystem number")
		}
		v.HasSSN = true
		v.SSN = b[off]
		off++
	}
	if gti := GlobalTitleIndicator(ai >> aiGTIShift & aiGTIMask); gti != GTINoGT {
		gt := &GlobalTitle{Indicator: gti}
		if err := gt.unmarshalContents(b[off:]); err != nil {
			return err
		}
		v.GlobalTitle = gt
	}
	*a = v
	return nil
}

// Clone returns a deep copy of the address: the copy shares no memory with a.
func (a PartyAddress) Clone() PartyAddress {
	if a.GlobalTitle != nil {
		gt := *a.GlobalTitle
		gt.AddressInformation = bytes.Clone(gt.AddressInformation)
		a.GlobalTitle = &gt
	}
	return a
}

// IndicatorOnly reports whether the address is only an address indicator with
// bits 1-7 zero, the calling party address ITU-T Q.713 (03/01) §3.5 keeps
// receivable "for compatibility reasons with earlier versions".
func (a PartyAddress) IndicatorOnly() bool {
	return !a.RouteOnSSN && !a.HasPointCode && !a.HasSSN && a.GlobalTitle == nil
}

// String returns the address in a human readable form.
func (a PartyAddress) String() string {
	var s strings.Builder
	s.WriteString("{")
	if a.RouteOnSSN {
		s.WriteString("RouteOnSSN")
	} else {
		s.WriteString("RouteOnGT")
	}
	if a.NationalUse {
		s.WriteString(", NationalUse")
	}
	if a.HasPointCode {
		fmt.Fprintf(&s, ", PC: %d", a.PointCode)
	}
	if a.HasSSN {
		fmt.Fprintf(&s, ", SSN: %d", a.SSN)
	}
	if a.GlobalTitle != nil {
		fmt.Fprintf(&s, ", GT: %s", a.GlobalTitle)
	}
	s.WriteString("}")
	return s.String()
}

// GlobalTitleIndicator is the global title indicator, ITU-T Q.713 (03/01)
// §3.4.1, bits 3-6 of the address indicator.
type GlobalTitleIndicator uint8

// Global title indicators, ITU-T Q.713 (03/01) §3.4.1. Values 5 to 7 are spare
// international, 8 to 14 spare national and 15 is reserved for extension.
const (
	GTINoGT      GlobalTitleIndicator = 0 // no global title
	GTINAIOnly   GlobalTitleIndicator = 1 // nature of address indicator only
	GTITTOnly    GlobalTitleIndicator = 2 // translation type only
	GTITTNPES    GlobalTitleIndicator = 3 // translation type, numbering plan and encoding scheme
	GTITTNPESNAI GlobalTitleIndicator = 4 // translation type, numbering plan, encoding scheme and nature of address indicator
)

// NatureOfAddressIndicator is the 7-bit nature of address indicator, ITU-T
// Q.713 (03/01) §3.4.2.3.1.
type NatureOfAddressIndicator uint8

// Nature of address indicators, ITU-T Q.713 (03/01) §3.4.2.3.1.
const (
	NAIUnknown                   NatureOfAddressIndicator = 0x00 // unknown
	NAISubscriberNumber          NatureOfAddressIndicator = 0x01 // subscriber number
	NAINationalUse               NatureOfAddressIndicator = 0x02 // reserved for national use
	NAINationalSignificantNumber NatureOfAddressIndicator = 0x03 // national significant number
	NAIInternationalNumber       NatureOfAddressIndicator = 0x04 // international number
)

// NumberingPlan is the numbering plan, ITU-T Q.713 (03/01) §3.4.2.3.3.
type NumberingPlan uint8

// Numbering plans, ITU-T Q.713 (03/01) §3.4.2.3.3.
const (
	NPUnknown        NumberingPlan = 0x0 // unknown
	NPISDNTelephony  NumberingPlan = 0x1 // ISDN/telephony (E.163 and E.164)
	NPGeneric        NumberingPlan = 0x2 // generic
	NPData           NumberingPlan = 0x3 // data (X.121)
	NPTelex          NumberingPlan = 0x4 // telex (F.69)
	NPMaritimeMobile NumberingPlan = 0x5 // maritime mobile (E.210, E.211)
	NPLandMobile     NumberingPlan = 0x6 // land mobile (E.212)
	NPISDNMobile     NumberingPlan = 0x7 // ISDN/mobile (E.214)
	NPPrivate        NumberingPlan = 0xe // private network or network-specific
)

// EncodingScheme is the encoding scheme, ITU-T Q.713 (03/01) §3.4.2.3.3.
type EncodingScheme uint8

// Encoding schemes, ITU-T Q.713 (03/01) §3.4.2.3.3.
const (
	ESUnknown          EncodingScheme = 0x0 // unknown
	ESBCDOdd           EncodingScheme = 0x1 // BCD, odd number of digits
	ESBCDEven          EncodingScheme = 0x2 // BCD, even number of digits
	ESNationalSpecific EncodingScheme = 0x3 // national specific
)

// GlobalTitle is the global title of a party address, ITU-T Q.713 (03/01)
// §3.4.2.3. Which fields exist depends on Indicator; the encoder refuses a
// non-zero field that the indicator's format does not carry.
type GlobalTitle struct {
	// Indicator is the global title indicator. Indicators 1 to 4 select the
	// formats of §3.4.2.3.1 to §3.4.2.3.4. The spare and reserved indicators
	// 5 to 15 have no defined format: their contents are carried in
	// AddressInformation only.
	Indicator GlobalTitleIndicator

	// TranslationType exists for indicators 2, 3 and 4.
	TranslationType uint8

	// NumberingPlan and EncodingScheme exist for indicators 3 and 4.
	NumberingPlan  NumberingPlan
	EncodingScheme EncodingScheme

	// NatureOfAddress exists for indicators 1 and 4.
	NatureOfAddress NatureOfAddressIndicator

	// OddDigits is the odd/even indicator of indicator 1 (bit 8 of octet 1).
	// Indicators 3 and 4 signal it with EncodingScheme instead.
	OddDigits bool

	// AddressInformation is the global title address information as
	// transmitted: BCD address signals (Figure 8/Q.713) when the encoding
	// scheme is BCD, otherwise opaque.
	AddressInformation []byte
}

// NewGlobalTitle1 returns a global title with indicator 1, the nature of
// address indicator only, ITU-T Q.713 (03/01) §3.4.2.3.1. digits are encoded
// as BCD address signals; see EncodeBCD.
func NewGlobalTitle1(nai NatureOfAddressIndicator, digits string) (*GlobalTitle, error) {
	addr, odd, err := EncodeBCD(digits)
	if err != nil {
		return nil, err
	}
	gt := &GlobalTitle{Indicator: GTINAIOnly, NatureOfAddress: nai, OddDigits: odd, AddressInformation: addr}
	return gt, gt.validate()
}

// NewGlobalTitle3 returns a global title with indicator 3, translation type,
// numbering plan and encoding scheme, ITU-T Q.713 (03/01) §3.4.2.3.3. digits
// are encoded as BCD address signals and the encoding scheme is set to BCD
// odd or even accordingly.
func NewGlobalTitle3(tt uint8, np NumberingPlan, digits string) (*GlobalTitle, error) {
	addr, odd, err := EncodeBCD(digits)
	if err != nil {
		return nil, err
	}
	gt := &GlobalTitle{Indicator: GTITTNPES, TranslationType: tt, NumberingPlan: np, EncodingScheme: bcdScheme(odd), AddressInformation: addr}
	return gt, gt.validate()
}

// NewGlobalTitle4 returns a global title with indicator 4, translation type,
// numbering plan, encoding scheme and nature of address indicator, ITU-T Q.713
// (03/01) §3.4.2.3.4, the format used on the international network. digits
// are encoded as BCD address signals and the encoding scheme is set to BCD
// odd or even accordingly.
func NewGlobalTitle4(tt uint8, np NumberingPlan, nai NatureOfAddressIndicator, digits string) (*GlobalTitle, error) {
	addr, odd, err := EncodeBCD(digits)
	if err != nil {
		return nil, err
	}
	gt := &GlobalTitle{Indicator: GTITTNPESNAI, TranslationType: tt, NumberingPlan: np, EncodingScheme: bcdScheme(odd), NatureOfAddress: nai, AddressInformation: addr}
	return gt, gt.validate()
}

func bcdScheme(odd bool) EncodingScheme {
	if odd {
		return ESBCDOdd
	}
	return ESBCDEven
}

// Digits decodes AddressInformation as BCD address signals. It fails for
// global titles that do not identify the address as BCD: indicator 2, whose
// encoding the translation type implies (§3.4.2.3.2), indicators 3 and 4 with a
// non-BCD encoding scheme, and the spare indicators.
func (g *GlobalTitle) Digits() (string, error) {
	var odd bool
	switch g.Indicator {
	case GTINAIOnly:
		odd = g.OddDigits
	case GTITTNPES, GTITTNPESNAI:
		switch g.EncodingScheme {
		case ESBCDOdd:
			odd = true
		case ESBCDEven:
		default:
			return "", fmt.Errorf("%w: encoding scheme %s is not BCD", ErrMalformed, g.EncodingScheme)
		}
	default:
		return "", fmt.Errorf("%w: global title indicator %d does not identify a BCD address", ErrMalformed, uint8(g.Indicator))
	}
	return DecodeBCD(g.AddressInformation, odd)
}

func (g *GlobalTitle) validate() error {
	if g.Indicator == GTINoGT || g.Indicator > aiGTIMask {
		return fmt.Errorf("%w: global title indicator %d, want 1 to 15", ErrInvalidValue, uint8(g.Indicator))
	}
	hasTT, hasNPES, hasNAI, hasOE := false, false, false, false
	switch g.Indicator {
	case GTINAIOnly:
		hasNAI, hasOE = true, true
	case GTITTOnly:
		hasTT = true
	case GTITTNPES:
		hasTT, hasNPES = true, true
	case GTITTNPESNAI:
		hasTT, hasNPES, hasNAI = true, true, true
	}
	switch {
	case !hasTT && g.TranslationType != 0:
		return fmt.Errorf("%w: global title indicator %d carries no translation type", ErrInvalidValue, uint8(g.Indicator))
	case !hasNPES && (g.NumberingPlan != 0 || g.EncodingScheme != 0):
		return fmt.Errorf("%w: global title indicator %d carries no numbering plan or encoding scheme", ErrInvalidValue, uint8(g.Indicator))
	case !hasNAI && g.NatureOfAddress != 0:
		return fmt.Errorf("%w: global title indicator %d carries no nature of address indicator", ErrInvalidValue, uint8(g.Indicator))
	case !hasOE && g.OddDigits:
		return fmt.Errorf("%w: global title indicator %d carries no odd/even indicator", ErrInvalidValue, uint8(g.Indicator))
	case g.NumberingPlan > 0x0f:
		return fmt.Errorf("%w: numbering plan %d exceeds 4 bits", ErrInvalidValue, uint8(g.NumberingPlan))
	case g.EncodingScheme > 0x0f:
		return fmt.Errorf("%w: encoding scheme %d exceeds 4 bits", ErrInvalidValue, uint8(g.EncodingScheme))
	case g.NatureOfAddress > 0x7f:
		return fmt.Errorf("%w: nature of address indicator %d exceeds 7 bits", ErrInvalidValue, uint8(g.NatureOfAddress))
	}
	return nil
}

// appendContents appends the global title of a validated g.
func (g *GlobalTitle) appendContents(b []byte) []byte {
	switch g.Indicator {
	case GTINAIOnly:
		nai := uint8(g.NatureOfAddress)
		if g.OddDigits {
			nai |= 0x80
		}
		b = append(b, nai)
	case GTITTOnly:
		b = append(b, g.TranslationType)
	case GTITTNPES:
		b = append(b, g.TranslationType, uint8(g.NumberingPlan)<<4|uint8(g.EncodingScheme))
	case GTITTNPESNAI:
		b = append(b, g.TranslationType, uint8(g.NumberingPlan)<<4|uint8(g.EncodingScheme), uint8(g.NatureOfAddress))
	}
	return append(b, g.AddressInformation...)
}

// unmarshalContents decodes the global title of indicator g.Indicator from b,
// which extends to the end of the party address.
func (g *GlobalTitle) unmarshalContents(b []byte) error {
	header := 0
	switch g.Indicator {
	case GTINAIOnly, GTITTOnly:
		header = 1
	case GTITTNPES:
		header = 2
	case GTITTNPESNAI:
		header = 3
	}
	if len(b) < header {
		return truncated("global title")
	}
	switch g.Indicator {
	case GTINAIOnly:
		g.NatureOfAddress = NatureOfAddressIndicator(b[0] & 0x7f)
		g.OddDigits = b[0]&0x80 != 0
	case GTITTOnly:
		g.TranslationType = b[0]
	case GTITTNPES:
		g.TranslationType = b[0]
		g.NumberingPlan = NumberingPlan(b[1] >> 4)
		g.EncodingScheme = EncodingScheme(b[1] & 0x0f)
	case GTITTNPESNAI:
		g.TranslationType = b[0]
		g.NumberingPlan = NumberingPlan(b[1] >> 4)
		g.EncodingScheme = EncodingScheme(b[1] & 0x0f)
		// Bit 8 of octet 3 is spare, Figure 11/Q.713.
		g.NatureOfAddress = NatureOfAddressIndicator(b[2] & 0x7f)
	}
	g.AddressInformation = append([]byte(nil), b[header:]...)
	return nil
}

// String returns the global title in a human readable form.
func (g *GlobalTitle) String() string {
	var s strings.Builder
	fmt.Fprintf(&s, "{GTI: %d", uint8(g.Indicator))
	switch g.Indicator {
	case GTINAIOnly:
		fmt.Fprintf(&s, ", NAI: %s, OddDigits: %t", g.NatureOfAddress, g.OddDigits)
	case GTITTOnly:
		fmt.Fprintf(&s, ", TT: %d", g.TranslationType)
	case GTITTNPES:
		fmt.Fprintf(&s, ", TT: %d, NP: %s, ES: %s", g.TranslationType, g.NumberingPlan, g.EncodingScheme)
	case GTITTNPESNAI:
		fmt.Fprintf(&s, ", TT: %d, NP: %s, ES: %s, NAI: %s", g.TranslationType, g.NumberingPlan, g.EncodingScheme, g.NatureOfAddress)
	}
	if digits, err := g.Digits(); err == nil {
		fmt.Fprintf(&s, ", Digits: %s}", digits)
	} else {
		fmt.Fprintf(&s, ", AddressInformation: %x}", g.AddressInformation)
	}
	return s.String()
}

// bcdSignals maps a BCD address signal to its character, Figure 8/Q.713. The
// spare codes 1010, 1101 and 1110 decode as A, D and E.
const bcdSignals = "0123456789ABCDEF"

// EncodeBCD encodes address signals as Figure 8/Q.713: two signals per octet,
// the first in bits 1-4, and a filler code 0000 after the last signal of an odd
// count (ITU-T Q.713 (03/01) §3.4.2.3.1). It reports whether the count is odd.
//
// Signals are the digits 0-9, B for code 11, C for code 12 and F for ST, in
// either case. The spare codes A, D and E are refused.
func EncodeBCD(digits string) (addr []byte, odd bool, err error) {
	addr = make([]byte, (len(digits)+1)/2)
	for i := 0; i < len(digits); i++ {
		v, ok := bcdValue(digits[i])
		if !ok {
			return nil, false, fmt.Errorf("%w: %q is not a BCD address signal", ErrInvalidValue, digits[i])
		}
		if i%2 == 0 {
			addr[i/2] = v
		} else {
			addr[i/2] |= v << 4
		}
	}
	return addr, len(digits)%2 == 1, nil
}

func bcdValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c == 'B' || c == 'b':
		return 0xb, true
	case c == 'C' || c == 'c':
		return 0xc, true
	case c == 'F' || c == 'f':
		return 0xf, true
	}
	return 0, false
}

// DecodeBCD decodes BCD address signals, Figure 8/Q.713. With odd set, the
// filler in the high nibble of the last octet is dropped without being
// examined.
func DecodeBCD(addr []byte, odd bool) (string, error) {
	if odd && len(addr) == 0 {
		return "", fmt.Errorf("%w: odd number of address signals in an empty address", ErrMalformed)
	}
	n := 2 * len(addr)
	if odd {
		n--
	}
	s := make([]byte, n)
	for i := range n {
		v := addr[i/2]
		if i%2 == 1 {
			v >>= 4
		}
		s[i] = bcdSignals[v&0x0f]
	}
	return string(s), nil
}
