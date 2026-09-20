package resolution

import (
	"context"
	"errors"
	"fmt"

	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
)

// ErrorStatus says why a resolve call produced no usable service: one of the
// client-side statuses below, or the resolver's own refusal word.
type ErrorStatus string

const (
	// RuntimeUnavailable: no runtime could be selected or reached. The
	// selection or transport failure is the Error's cause. On a platform the
	// runtime declares unsupported, Platform names it.
	RuntimeUnavailable ErrorStatus = "runtime_unavailable"
	// InvalidResolution: the resolver answered, and its answer failed validation.
	InvalidResolution ErrorStatus = "invalid_resolution"
	// UnsupportedTransport: the resolver selected a reference whose scope or
	// transport this binding cannot use. Scope and Transport name it.
	UnsupportedTransport ErrorStatus = "unsupported_transport"
)

// Error is a resolve call that produced no usable service. It is distinct from
// the resolver's unavailable refusal, which means a reachable runtime serves
// no matching registration. The caller's own cancellation, and a context
// already done when resolution starts, are returned as the context error
// instead.
type Error struct {
	Status     ErrorStatus
	Capability string
	Contract   string
	// LookedFor names what resolution tried: "the installed runtime", "the
	// installed runtime at <endpoint>" or "the explicit endpoint <endpoint>".
	LookedFor string
	// Scope and Transport are the selected reference's, for UnsupportedTransport.
	Scope     wire.Scope
	Transport string
	// Platform names a platform the runtime declares unsupported, for
	// RuntimeUnavailable: "android" or "macos". Empty otherwise.
	Platform string
	// Err is the selection or transport failure for RuntimeUnavailable, and
	// the validation failure for InvalidResolution.
	Err error
}

func (e *Error) Error() string {
	message := "service resolution: " + string(e.Status)
	if e.Contract != "" {
		message += fmt.Sprintf(": %s (capability %s) at %s", e.Contract, e.Capability, e.LookedFor)
	}
	if e.Status == UnsupportedTransport {
		message += fmt.Sprintf(": %s reference over %s", e.Scope, e.Transport)
	}
	if e.Platform != "" {
		return message + ": no supported OpenAbstractions runtime exists for " + e.Platform
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *Error) Unwrap() error { return e.Err }

// Refusal returns the resolver's refusal when Status is one.
func (e *Error) Refusal() (wire.ResolutionStatus, bool) {
	status, ok := wire.ParseResolutionStatus(string(e.Status))
	return status, ok && status != wire.ResolutionStatusResolved
}

// Unreachable reports a failure of selection or of the resolver exchange as
// RuntimeUnavailable. The caller's cancellation stays the context error, and a
// resolver that answered with ServiceError, DispatchError or Refusal keeps that
// error; an invalid_resolution ServiceError becomes InvalidResolution.
func Unreachable(caller context.Context, err error, capability, contract, lookedFor string) error {
	if err == nil {
		return nil
	}
	if errors.Is(caller.Err(), context.Canceled) {
		return err
	}
	var service *wire.ServiceError
	if errors.As(err, &service) {
		if service.Code == "invalid_resolution" {
			return &Error{Status: InvalidResolution, Capability: capability, Contract: contract, LookedFor: lookedFor, Err: err}
		}
		return err
	}
	var dispatch wire.DispatchError
	var refusal *wire.Refusal
	if errors.As(err, &dispatch) || errors.As(err, &refusal) {
		return err
	}
	failure := &Error{Status: RuntimeUnavailable, Capability: capability, Contract: contract, LookedFor: lookedFor, Err: err}
	var platform *bootstrap.UnsupportedPlatformError
	if errors.As(err, &platform) {
		failure.Platform = platform.Platform
	}
	return failure
}
