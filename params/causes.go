// Copyright 2019-2026 go-sccp authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package params

//go:generate stringer -type ParameterNameCode,ReleaseCause,ReturnCause,ResetCause,ErrorCause,RefusalCause,GlobalTitleIndicator,NatureOfAddressIndicator,NumberingPlan,EncodingScheme -linecomment -output constant_string.go

// The cause parameters are one octet each. A parameter encodes every value; in
// a message the encoder refuses 11111111, reserved in every cause table (ITU-T
// Q.713 (03/01) §3.11 to §3.15), and passes the other codes not listed here,
// spare or reserved, so that a cause received can be passed on.

// ReleaseCause is the release cause parameter, ITU-T Q.713 (03/01) §3.11.
type ReleaseCause uint8

// Release causes, ITU-T Q.713 (03/01) §3.11.
const (
	ReleaseCauseEndUserOriginated                  ReleaseCause = 0x00 // end user originated
	ReleaseCauseEndUserCongestion                  ReleaseCause = 0x01 // end user congestion
	ReleaseCauseEndUserFailure                     ReleaseCause = 0x02 // end user failure
	ReleaseCauseSCCPUserOriginated                 ReleaseCause = 0x03 // SCCP user originated
	ReleaseCauseRemoteProcedureError               ReleaseCause = 0x04 // remote procedure error
	ReleaseCauseInconsistentConnectionData         ReleaseCause = 0x05 // inconsistent connection data
	ReleaseCauseAccessFailure                      ReleaseCause = 0x06 // access failure
	ReleaseCauseAccessCongestion                   ReleaseCause = 0x07 // access congestion
	ReleaseCauseSubsystemFailure                   ReleaseCause = 0x08 // subsystem failure
	ReleaseCauseSubsystemCongestion                ReleaseCause = 0x09 // subsystem congestion
	ReleaseCauseMTPFailure                         ReleaseCause = 0x0a // MTP failure
	ReleaseCauseNetworkCongestion                  ReleaseCause = 0x0b // network congestion
	ReleaseCauseExpirationOfResetTimer             ReleaseCause = 0x0c // expiration of reset timer
	ReleaseCauseExpirationOfReceiveInactivityTimer ReleaseCause = 0x0d // expiration of receive inactivity timer
	ReleaseCauseUnqualified                        ReleaseCause = 0x0f // unqualified
	ReleaseCauseSCCPFailure                        ReleaseCause = 0x10 // SCCP failure
)

// ReturnCause is the return cause parameter, ITU-T Q.713 (03/01) §3.12, which
// maps one to one onto the reason for return of the N-NOTICE primitive (Annex
// A.5).
type ReturnCause uint8

// Return causes, ITU-T Q.713 (03/01) §3.12. Causes 0x08 to 0x0a apply to
// XUDT(S) only (Note to §3.12).
const (
	ReturnCauseNoTranslationForAnAddressOfSuchNature ReturnCause = 0x00 // no translation for an address of such nature
	ReturnCauseNoTranslationForThisSpecificAddress   ReturnCause = 0x01 // no translation for this specific address
	ReturnCauseSubsystemCongestion                   ReturnCause = 0x02 // subsystem congestion
	ReturnCauseSubsystemFailure                      ReturnCause = 0x03 // subsystem failure
	ReturnCauseUnequippedUser                        ReturnCause = 0x04 // unequipped user
	ReturnCauseMTPFailure                            ReturnCause = 0x05 // MTP failure
	ReturnCauseNetworkCongestion                     ReturnCause = 0x06 // network congestion
	ReturnCauseUnqualified                           ReturnCause = 0x07 // unqualified
	ReturnCauseErrorInMessageTransport               ReturnCause = 0x08 // error in message transport
	ReturnCauseErrorInLocalProcessing                ReturnCause = 0x09 // error in local processing
	ReturnCauseDestinationCannotPerformReassembly    ReturnCause = 0x0a // destination cannot perform reassembly
	ReturnCauseSCCPFailure                           ReturnCause = 0x0b // SCCP failure
	ReturnCauseHopCounterViolation                   ReturnCause = 0x0c // hop counter violation
	ReturnCauseSegmentationNotSupported              ReturnCause = 0x0d // segmentation not supported
	ReturnCauseSegmentationFailure                   ReturnCause = 0x0e // segmentation failure
)

// ResetCause is the reset cause parameter, ITU-T Q.713 (03/01) §3.13.
type ResetCause uint8

// Reset causes, ITU-T Q.713 (03/01) §3.13.
const (
	ResetCauseEndUserOriginated                                     ResetCause = 0x00 // end user originated
	ResetCauseSCCPUserOriginated                                    ResetCause = 0x01 // SCCP user originated
	ResetCauseMessageOutOfOrderIncorrectSendSequenceNumber          ResetCause = 0x02 // message out of order - incorrect P(S)
	ResetCauseMessageOutOfOrderIncorrectReceiveSequenceNumber       ResetCause = 0x03 // message out of order - incorrect P(R)
	ResetCauseRemoteProcedureErrorMessageOutOfWindow                ResetCause = 0x04 // remote procedure error - message out of window
	ResetCauseRemoteProcedureErrorIncorrectSendSequenceNumberReinit ResetCause = 0x05 // remote procedure error - incorrect P(S) after (re)initialization
	ResetCauseRemoteProcedureErrorGeneral                           ResetCause = 0x06 // remote procedure error - general
	ResetCauseRemoteEndUserOperational                              ResetCause = 0x07 // remote end user operational
	ResetCauseNetworkOperational                                    ResetCause = 0x08 // network operational
	ResetCauseAccessOperational                                     ResetCause = 0x09 // access operational
	ResetCauseNetworkCongestion                                     ResetCause = 0x0a // network congestion
	ResetCauseUnqualified                                           ResetCause = 0x0c // unqualified
)

// ErrorCause is the error cause parameter, ITU-T Q.713 (03/01) §3.14.
type ErrorCause uint8

// Error causes, ITU-T Q.713 (03/01) §3.14.
const (
	ErrorCauseUnassignedDestinationLRN ErrorCause = 0x00 // local reference number (LRN) mismatch - unassigned destination LRN
	ErrorCauseInconsistentSourceLRN    ErrorCause = 0x01 // local reference number (LRN) mismatch - inconsistent source LRN
	ErrorCausePointCodeMismatch        ErrorCause = 0x02 // point code mismatch
	ErrorCauseServiceClassMismatch     ErrorCause = 0x03 // service class mismatch
	ErrorCauseUnqualified              ErrorCause = 0x04 // unqualified
)

// RefusalCause is the refusal cause parameter, ITU-T Q.713 (03/01) §3.15.
type RefusalCause uint8

// Refusal causes, ITU-T Q.713 (03/01) §3.15.
const (
	RefusalCauseEndUserOriginated                           RefusalCause = 0x00 // end user originated
	RefusalCauseEndUserCongestion                           RefusalCause = 0x01 // end user congestion
	RefusalCauseEndUserFailure                              RefusalCause = 0x02 // end user failure
	RefusalCauseSCCPUserOriginated                          RefusalCause = 0x03 // SCCP user originated
	RefusalCauseDestinationAddressUnknown                   RefusalCause = 0x04 // destination address unknown
	RefusalCauseDestinationInaccessible                     RefusalCause = 0x05 // destination inaccessible
	RefusalCauseNetworkResourceQoSNotAvailableNonTransient  RefusalCause = 0x06 // network resource - QoS not available/non-transient
	RefusalCauseNetworkResourceQoSNotAvailableTransient     RefusalCause = 0x07 // network resource - QoS not available/transient
	RefusalCauseAccessFailure                               RefusalCause = 0x08 // access failure
	RefusalCauseAccessCongestion                            RefusalCause = 0x09 // access congestion
	RefusalCauseSubsystemFailure                            RefusalCause = 0x0a // subsystem failure
	RefusalCauseSubsystemCongestion                         RefusalCause = 0x0b // subsystem congestion
	RefusalCauseExpirationOfTheConnectionEstablishmentTimer RefusalCause = 0x0c // expiration of the connection establishment timer
	RefusalCauseIncompatibleUserData                        RefusalCause = 0x0d // incompatible user data
	RefusalCauseUnqualified                                 RefusalCause = 0x0f // unqualified
	RefusalCauseHopCounterViolation                         RefusalCause = 0x10 // hop counter violation
	RefusalCauseSCCPFailure                                 RefusalCause = 0x11 // SCCP failure
	RefusalCauseNoTranslationForAnAddressOfSuchNature       RefusalCause = 0x12 // no translation for an address of such nature
	RefusalCauseUnequippedUser                              RefusalCause = 0x13 // unequipped user
)
