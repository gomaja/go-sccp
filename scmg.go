// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package sccp

import (
	"fmt"
	"io"

	"github.com/gomaja/go-sccp/params"
)

// SCMGType is the SCMG format identifier, ITU-T Q.713 (03/01) §5.1.1, Table 23.
type SCMGType uint8

// SCMG format identifiers, ITU-T Q.713 (03/01) Table 23.
const (
	SCMGTypeSSA SCMGType = 0x01 // SSA
	SCMGTypeSSP SCMGType = 0x02 // SSP
	SCMGTypeSST SCMGType = 0x03 // SST
	SCMGTypeSOR SCMGType = 0x04 // SOR
	SCMGTypeSOG SCMGType = 0x05 // SOG
	SCMGTypeSSC SCMGType = 0x06 // SSC
)

// SCMGSubsystemNumber is the subsystem number of SCCP management, ITU-T Q.713
// (03/01) §3.4.2.2 and §5.1.
const SCMGSubsystemNumber = 1

// SCCPService is the "Affected SCCP service" of an SSC congestion level, bits
// 6-5 of its octet: the Implementors' Guide (03/2002) for ITU-T Q.713 (03/01),
// modifications 1 and 2 to Figure 13 and §5.2.4, so that the connectionless
// and connection-oriented services report congestion separately.
type SCCPService uint8

// Affected SCCP services. The code 11 is reserved.
const (
	SCCPServiceBoth               SCCPService = 0 // connectionless and connection-oriented
	SCCPServiceConnectionless     SCCPService = 1 // connectionless
	SCCPServiceConnectionOriented SCCPService = 2 // connection-oriented
)

// SCMG is an SCCP management message, carried in the data of a UDT, XUDT or
// LUDT between the SCMG subsystems, ITU-T Q.713 (03/01) clause 5, Tables 24
// and 25.
type SCMG struct {
	Type SCMGType

	// AffectedSSN is the affected subsystem number (§5.2.1).
	AffectedSSN uint8

	// AffectedPointCode is the 14-bit affected signalling point code, coded as
	// in the called party address (§5.2.2, §3.4.2.1).
	AffectedPointCode uint16

	// SubsystemMultiplicityIndicator is the 2-bit SMI (§5.2.3).
	SubsystemMultiplicityIndicator uint8

	// CongestionLevel is the SCCP congestion level of SSC, 1 (least) to 8
	// (most congested), in bits 1-4 (§5.2.4). It is absent from the other
	// messages and must be zero for them.
	CongestionLevel uint8

	// AffectedService is the SCCP service the congestion level of SSC
	// applies to, in bits 6-5 of its octet; zero, both services, for the
	// other messages.
	AffectedService SCCPService
}

// AppendBinary appends the SCMG message to b. On error b is returned
// unchanged.
func (s SCMG) AppendBinary(b []byte) ([]byte, error) {
	switch {
	case s.Type < SCMGTypeSSA || s.Type > SCMGTypeSSC:
		return b, fmt.Errorf("%w: SCMG format identifier %d", ErrInvalidMessage, uint8(s.Type))
	case s.AffectedPointCode > params.MaxPointCode:
		return b, fmt.Errorf("%w: SCMG affected point code %#x exceeds 14 bits", ErrInvalidMessage, s.AffectedPointCode)
	case s.SubsystemMultiplicityIndicator > 3:
		return b, fmt.Errorf("%w: SCMG subsystem multiplicity indicator %d exceeds 2 bits", ErrInvalidMessage, s.SubsystemMultiplicityIndicator)
	case s.Type == SCMGTypeSSC && (s.CongestionLevel < 1 || s.CongestionLevel > 8):
		return b, fmt.Errorf("%w: SCCP congestion level %d, want 1 to 8", ErrInvalidMessage, s.CongestionLevel)
	case s.Type != SCMGTypeSSC && (s.CongestionLevel != 0 || s.AffectedService != SCCPServiceBoth):
		return b, fmt.Errorf("%w: %s carries no SCCP congestion level", ErrInvalidMessage, s.Type)
	case s.AffectedService > SCCPServiceConnectionOriented:
		return b, fmt.Errorf("%w: affected SCCP service %d is reserved", ErrInvalidMessage, uint8(s.AffectedService))
	}
	b = append(b, byte(s.Type), s.AffectedSSN, byte(s.AffectedPointCode), byte(s.AffectedPointCode>>8), s.SubsystemMultiplicityIndicator)
	if s.Type == SCMGTypeSSC {
		b = append(b, s.CongestionLevel|byte(s.AffectedService)<<4)
	}
	return b, nil
}

// MarshalBinary encodes the SCMG message.
func (s SCMG) MarshalBinary() ([]byte, error) { return s.AppendBinary(nil) }

// UnmarshalBinary decodes an SCMG message. Spare bits are not examined (ITU-T
// Q.714 (05/01) §1.1.4.4), the reserved affected SCCP service decodes as both
// services, the default (§1.1.4.3: other values "may be reset to default
// values"), and octets after the message are ignored. On error the receiver
// is left unchanged.
func (s *SCMG) UnmarshalBinary(b []byte) error {
	if len(b) < 5 {
		return fmt.Errorf("%w: SCMG: %w", ErrMalformedMessage, io.ErrUnexpectedEOF)
	}
	v := SCMG{
		Type:                           SCMGType(b[0]),
		AffectedSSN:                    b[1],
		AffectedPointCode:              (uint16(b[2]) | uint16(b[3])<<8) & params.MaxPointCode,
		SubsystemMultiplicityIndicator: b[4] & 0x03,
	}
	switch v.Type {
	case SCMGTypeSSA, SCMGTypeSSP, SCMGTypeSST, SCMGTypeSOR, SCMGTypeSOG:
	case SCMGTypeSSC:
		if len(b) < 6 {
			return fmt.Errorf("%w: SSC congestion level: %w", ErrMalformedMessage, io.ErrUnexpectedEOF)
		}
		v.CongestionLevel = b[5] & 0x0f
		if service := SCCPService(b[5] >> 4 & 0x03); service <= SCCPServiceConnectionOriented {
			v.AffectedService = service
		}
	default:
		return fmt.Errorf("%w: unknown SCMG format identifier %#02x", ErrMalformedMessage, b[0])
	}
	*s = v
	return nil
}

// String returns the message in a human readable form.
func (s SCMG) String() string {
	str := fmt.Sprintf("%s{AffectedSSN: %d, AffectedPointCode: %d, SMI: %d", s.Type, s.AffectedSSN, s.AffectedPointCode, s.SubsystemMultiplicityIndicator)
	if s.Type == SCMGTypeSSC {
		str += fmt.Sprintf(", CongestionLevel: %d, AffectedService: %s", s.CongestionLevel, s.AffectedService)
	}
	return str + "}"
}
