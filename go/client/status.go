package client

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
)

// DefaultStatusRequests returns independent requests for the default runtime's
// five contracts. Individual applications may observe only their own contracts.
func DefaultStatusRequests() []wire.ResolveRequest {
	requests := make([]wire.ResolveRequest, len(wire.DefaultRuntimeContracts))
	for i, contract := range wire.DefaultRuntimeContracts {
		capability, _, _ := strings.Cut(contract, "/")
		requests[i] = wire.ResolveRequest{Capability: capability, Contracts: []string{contract}, Guarantees: []string{}, Scope: wire.ScopeLocal}
	}
	return requests
}

// Observe composes explicit platform evidence with authorized resolver queries.
// Omitted evidence defaults to unknown; no process is started or inspected here.
// At most one observation may be supplied. Its source must establish its state.
// One caller deadline covers all queries; without one the total budget is two
// seconds. The first error stops querying and is returned alongside partial
// observations. All requests remain present; absent results mean unobserved.
// A resolved candidate does not prove a subsequent provider call will succeed.
func (m *Machine) Observe(ctx context.Context, requests []wire.ResolveRequest, evidence ...wire.BootstrapObservation) (wire.RuntimeObservation, error) {
	report := wire.RuntimeObservation{Bootstrap: wire.BootstrapObservation{State: wire.BootstrapStateUnknown}, Capabilities: make([]wire.CapabilityObservation, len(requests))}
	for i, request := range requests {
		request.Contracts = slices.Clone(request.Contracts)
		request.Guarantees = slices.Clone(request.Guarantees)
		report.Capabilities[i].Request = request
	}
	if len(evidence) > 1 {
		return report, errors.New("facade observation: at most one bootstrap observation")
	}
	if len(evidence) == 1 {
		report.Bootstrap = evidence[0]
		switch report.Bootstrap.State {
		case wire.BootstrapStateUnknown, wire.BootstrapStateInstalled, wire.BootstrapStateStarting, wire.BootstrapStateRunning, wire.BootstrapStateUnavailable:
		default:
			return report, errors.New("facade observation: invalid bootstrap state")
		}
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	resolver, err := m.resolver(ctx)
	if err != nil {
		return report, err
	}
	return observeQueries(ctx, report, resolver.Resolve)
}

func observeQueries(ctx context.Context, report wire.RuntimeObservation, resolve func(context.Context, wire.ResolveRequest) (wire.ResolveResult, error)) (wire.RuntimeObservation, error) {
	for i := range report.Capabilities {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		result, err := resolve(ctx, report.Capabilities[i].Request)
		if err != nil {
			return report, err
		}
		report.Capabilities[i].Result = &result
	}
	return report, nil
}
