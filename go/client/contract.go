package client

// The generated contract types this package's API reaches, re-exported so an
// application names them through this package and never imports the generated
// one. scripts/idiom_check.py refuses a reachable type this file leaves out.

import (
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
)

type AcceptanceOutcome = api.AcceptanceOutcome

const (
	AcceptanceOutcomeAccepted              = api.AcceptanceOutcomeAccepted
	AcceptanceOutcomeDefinitelyNotAccepted = api.AcceptanceOutcomeDefinitelyNotAccepted
	AcceptanceOutcomeUnknown               = api.AcceptanceOutcomeUnknown
	AcceptanceOutcomeKeyConflict           = api.AcceptanceOutcomeKeyConflict
	AcceptanceOutcomeForbidden             = api.AcceptanceOutcomeForbidden
	AcceptanceOutcomeInvalid               = api.AcceptanceOutcomeInvalid
	AcceptanceOutcomeUnavailable           = api.AcceptanceOutcomeUnavailable
)

// AcceptanceOutcomeValues returns every member of AcceptanceOutcome in declaration order, in a new slice.
func AcceptanceOutcomeValues() []AcceptanceOutcome { return api.AcceptanceOutcomeValues() }

type AcceptanceResult = api.AcceptanceResult

type CancellationOutcome = api.CancellationOutcome

const (
	CancellationOutcomeRequested       = api.CancellationOutcomeRequested
	CancellationOutcomeAlreadyTerminal = api.CancellationOutcomeAlreadyTerminal
	CancellationOutcomeUnknown         = api.CancellationOutcomeUnknown
	CancellationOutcomeForbidden       = api.CancellationOutcomeForbidden
	CancellationOutcomeUnsupported     = api.CancellationOutcomeUnsupported
	CancellationOutcomeUnavailable     = api.CancellationOutcomeUnavailable
)

// CancellationOutcomeValues returns every member of CancellationOutcome in declaration order, in a new slice.
func CancellationOutcomeValues() []CancellationOutcome { return api.CancellationOutcomeValues() }

type CancellationResult = api.CancellationResult

type FailureCause = api.FailureCause

const (
	FailureCauseOther          = api.FailureCauseOther
	FailureCauseDigestMismatch = api.FailureCauseDigestMismatch
	FailureCauseOversize       = api.FailureCauseOversize
	FailureCauseShortTransfer  = api.FailureCauseShortTransfer
	FailureCauseUnauthorized   = api.FailureCauseUnauthorized
	FailureCauseNotFound       = api.FailureCauseNotFound
	FailureCauseRefused        = api.FailureCauseRefused
	FailureCauseServerError    = api.FailureCauseServerError
	FailureCauseTransport      = api.FailureCauseTransport
	FailureCauseResultLost     = api.FailureCauseResultLost
	FailureCauseCredential     = api.FailureCauseCredential
)

// FailureCauseValues returns every member of FailureCause in declaration order, in a new slice.
func FailureCauseValues() []FailureCause { return api.FailureCauseValues() }

type FailureClass = api.FailureClass

const (
	FailureClassRetryable = api.FailureClassRetryable
	FailureClassPermanent = api.FailureClassPermanent
	FailureClassUnknown   = api.FailureClassUnknown
)

// FailureClassValues returns every member of FailureClass in declaration order, in a new slice.
func FailureClassValues() []FailureClass { return api.FailureClassValues() }

type HistoryWindow = api.HistoryWindow

type InventoryOutcome = api.InventoryOutcome

const (
	InventoryOutcomePage        = api.InventoryOutcomePage
	InventoryOutcomeGap         = api.InventoryOutcomeGap
	InventoryOutcomeForbidden   = api.InventoryOutcomeForbidden
	InventoryOutcomeInvalid     = api.InventoryOutcomeInvalid
	InventoryOutcomeUnavailable = api.InventoryOutcomeUnavailable
)

// InventoryOutcomeValues returns every member of InventoryOutcome in declaration order, in a new slice.
func InventoryOutcomeValues() []InventoryOutcome { return api.InventoryOutcomeValues() }

type InventoryPage = api.InventoryPage

type OperatorCancellation = api.OperatorCancellation

type OperatorCancellationOutcome = api.OperatorCancellationOutcome

const (
	OperatorCancellationOutcomeRequested       = api.OperatorCancellationOutcomeRequested
	OperatorCancellationOutcomeAlreadyTerminal = api.OperatorCancellationOutcomeAlreadyTerminal
	OperatorCancellationOutcomeUnknown         = api.OperatorCancellationOutcomeUnknown
	OperatorCancellationOutcomeForbidden       = api.OperatorCancellationOutcomeForbidden
	OperatorCancellationOutcomeInvalid         = api.OperatorCancellationOutcomeInvalid
	OperatorCancellationOutcomeUnavailable     = api.OperatorCancellationOutcomeUnavailable
)

// OperatorCancellationOutcomeValues returns every member of OperatorCancellationOutcome in declaration order, in a new slice.
func OperatorCancellationOutcomeValues() []OperatorCancellationOutcome {
	return api.OperatorCancellationOutcomeValues()
}

type ObservationOutcome = api.ObservationOutcome

const (
	ObservationOutcomeObserved              = api.ObservationOutcomeObserved
	ObservationOutcomeUnknown               = api.ObservationOutcomeUnknown
	ObservationOutcomeForbidden             = api.ObservationOutcomeForbidden
	ObservationOutcomeInvalid               = api.ObservationOutcomeInvalid
	ObservationOutcomeDefinitelyNotAccepted = api.ObservationOutcomeDefinitelyNotAccepted
	ObservationOutcomeUnavailable           = api.ObservationOutcomeUnavailable
)

// ObservationOutcomeValues returns every member of ObservationOutcome in declaration order, in a new slice.
func ObservationOutcomeValues() []ObservationOutcome { return api.ObservationOutcomeValues() }

type ObservationResult = api.ObservationResult

type OperationSnapshot = api.OperationSnapshot

type Receipt = api.Receipt

type RequestIdentity = api.RequestIdentity

type ResultChunk = api.ResultChunk

type ResultOutcome = api.ResultOutcome

const (
	ResultOutcomeData        = api.ResultOutcomeData
	ResultOutcomeNotReady    = api.ResultOutcomeNotReady
	ResultOutcomeUnavailable = api.ResultOutcomeUnavailable
	ResultOutcomeUnsupported = api.ResultOutcomeUnsupported
	ResultOutcomeUnknown     = api.ResultOutcomeUnknown
	ResultOutcomeForbidden   = api.ResultOutcomeForbidden
	ResultOutcomeInvalid     = api.ResultOutcomeInvalid
)

// ResultOutcomeValues returns every member of ResultOutcome in declaration order, in a new slice.
func ResultOutcomeValues() []ResultOutcome { return api.ResultOutcomeValues() }

type ResultRead = api.ResultRead

type ServiceError = api.ServiceError

type ServiceErrorCode = api.ServiceErrorCode

const (
	ServiceErrorCodeHandlerError   = api.ServiceErrorCodeHandlerError
	ServiceErrorCodeInvalidResult  = api.ServiceErrorCodeInvalidResult
	ServiceErrorCodeUnknownVersion = api.ServiceErrorCodeUnknownVersion
	ServiceErrorCodeUnknownService = api.ServiceErrorCodeUnknownService
	ServiceErrorCodeUnknownMethod  = api.ServiceErrorCodeUnknownMethod
	ServiceErrorCodeWrongMode      = api.ServiceErrorCodeWrongMode
	ServiceErrorCodeForbidden      = api.ServiceErrorCodeForbidden
)

// ServiceErrorCodeValues returns every member of ServiceErrorCode in declaration order, in a new slice.
func ServiceErrorCodeValues() []ServiceErrorCode { return api.ServiceErrorCodeValues() }

type Submission = api.Submission

type WorkFailure = api.WorkFailure

type WorkProgress = api.WorkProgress

type WorkState = api.WorkState

const (
	WorkStatePending     = api.WorkStatePending
	WorkStateRunning     = api.WorkStateRunning
	WorkStateTransferred = api.WorkStateTransferred
	WorkStateComplete    = api.WorkStateComplete
	WorkStateFailed      = api.WorkStateFailed
	WorkStateCancelled   = api.WorkStateCancelled
)

// WorkStateValues returns every member of WorkState in declaration order, in a new slice.
func WorkStateValues() []WorkState { return api.WorkStateValues() }

type BootstrapObservation = wire.BootstrapObservation

type ResolveRequest = wire.ResolveRequest

type RuntimeObservation = wire.RuntimeObservation

type Declaration = wire.Declaration

type DeclarationState = wire.DeclarationState

type DeclarationList = wire.DeclarationList

type DeclarationChange = wire.DeclarationChange

type DeclarationObservation = wire.DeclarationObservation

type RemoteTrust = wire.RemoteTrust

type Description = wire.Description

type ServiceState = wire.ServiceState

type ApplicationActivationOutcome = wire.ApplicationActivationOutcome

const (
	ApplicationActivationOutcomeReady           = wire.ApplicationActivationOutcomeReady
	ApplicationActivationOutcomeUnknown         = wire.ApplicationActivationOutcomeUnknown
	ApplicationActivationOutcomeDisabled        = wire.ApplicationActivationOutcomeDisabled
	ApplicationActivationOutcomeForbidden       = wire.ApplicationActivationOutcomeForbidden
	ApplicationActivationOutcomeInvalid         = wire.ApplicationActivationOutcomeInvalid
	ApplicationActivationOutcomeLaunchRefused   = wire.ApplicationActivationOutcomeLaunchRefused
	ApplicationActivationOutcomeIdentityRefused = wire.ApplicationActivationOutcomeIdentityRefused
	ApplicationActivationOutcomeNotReady        = wire.ApplicationActivationOutcomeNotReady
	ApplicationActivationOutcomeUnavailable     = wire.ApplicationActivationOutcomeUnavailable
)

func ApplicationActivationOutcomeValues() []ApplicationActivationOutcome {
	return wire.ApplicationActivationOutcomeValues()
}

type ApplicationActivationRecipe = wire.ApplicationActivationRecipe
type ApplicationActivationResult = wire.ApplicationActivationResult
type ApplicationChange = wire.ApplicationChange
type ApplicationContext = wire.ApplicationContext
type ApplicationDescriptor = wire.ApplicationDescriptor
type ApplicationEntry = wire.ApplicationEntry
type ApplicationInstance = wire.ApplicationInstance
type ApplicationInterface = wire.ApplicationInterface
type ApplicationOutcome = wire.ApplicationOutcome

const (
	ApplicationOutcomeApplied     = wire.ApplicationOutcomeApplied
	ApplicationOutcomePage        = wire.ApplicationOutcomePage
	ApplicationOutcomeUnknown     = wire.ApplicationOutcomeUnknown
	ApplicationOutcomeStale       = wire.ApplicationOutcomeStale
	ApplicationOutcomeConflict    = wire.ApplicationOutcomeConflict
	ApplicationOutcomeInvalid     = wire.ApplicationOutcomeInvalid
	ApplicationOutcomeForbidden   = wire.ApplicationOutcomeForbidden
	ApplicationOutcomeUnavailable = wire.ApplicationOutcomeUnavailable
)

func ApplicationOutcomeValues() []ApplicationOutcome { return wire.ApplicationOutcomeValues() }

type ApplicationPage = wire.ApplicationPage
type ApplicationPresence = wire.ApplicationPresence
