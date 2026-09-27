package runtime

import (
	"context"
	"errors"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
)

// LendingContract is the profile a lending service publishes: placing a model
// file one store holds in an installed engine's own directory, by link.
const LendingContract = "abstraction.storage/lend@1"

// LendingService is a separately composed abstraction.storage/lend@1 host over
// whatever provider performs the lend, already listening on
// Options.LendingEndpoint. The runtime serves it, publishes the profile and
// closes it with the runtime; the service itself decides the caller's right
// and forwards. Its rights action goes in Options.RightsActions.
type LendingService interface {
	Serve(context.Context) error
	Close() error
}

func validateLending(options Options) error {
	if (options.Lending == nil) != (options.LendingEndpoint == "") {
		return errors.New("runtime: lending requires an explicit service and its endpoint")
	}
	return nil
}

func (h *Host) addLendingCandidate(options Options) {
	h.lendingIndex = -1
	if options.Lending == nil {
		return
	}
	h.lending = options.Lending
	h.lendingIndex = len(h.candidates)
	h.candidates = append(h.candidates, resolution.Candidate{Ready: true, Reference: wire.ServiceReference{
		Provider: "openabstractions.user-runtime", Capability: "abstraction.storage", Contract: LendingContract,
		Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.LendingEndpoint, Guarantees: []string{},
	}})
}
