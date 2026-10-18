package runtime

import (
	"context"
	"errors"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
)

// RegistryContract is the provider registry profile a registry service publishes.
const RegistryContract = "abstraction.facade/registry@1"
const RegistryV2Contract = "abstraction.facade/registry@2"

// RegistryService is a separately composed registry@1 and registry@2 host
// over the runtime's provider declarations, already listening on
// Options.RegistryEndpoint. The runtime serves it, publishes the profile and
// closes it with the runtime. Its rights action goes in Options.RightsActions.
type RegistryService interface {
	Serve(context.Context) error
	Close() error
}

func validateRegistry(options Options) error {
	if (options.Registry == nil) != (options.RegistryEndpoint == "") {
		return errors.New("runtime: a registry requires an explicit service and its endpoint")
	}
	return nil
}

func (h *Host) addRegistryCandidate(options Options) {
	h.registryIndex = -1
	if options.Registry == nil {
		return
	}
	h.registry = options.Registry
	h.registryIndex = len(h.candidates)
	for _, contract := range []string{RegistryContract, RegistryV2Contract} {
		h.candidates = append(h.candidates, resolution.Candidate{Ready: true, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.facade", Contract: contract,
			Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.RegistryEndpoint, Guarantees: []string{},
		}})
	}
}
