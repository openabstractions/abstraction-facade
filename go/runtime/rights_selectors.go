package runtime

import (
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
)

func (h *Host) addSelectorRightsCandidates(options Options, readable bool) {
	if h.rights == nil || !h.rights.SelectorAvailable() {
		return
	}
	contracts := []string{"abstraction.rights/authorization@2"}
	if options.RightsOperator != nil {
		contracts = append(contracts, "abstraction.rights/operator@2")
	}
	for _, contract := range contracts {
		ready := readable && h.rights.SelectorAvailable()
		if contract == "abstraction.rights/operator@2" {
			ready = ready && h.rights.SelectorOperatorAvailable()
		}
		h.candidates = append(h.candidates, resolution.Candidate{Ready: ready, Reference: wire.ServiceReference{Provider: "openabstractions.user-runtime", Capability: "abstraction.rights", Contract: contract, Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.RightsEndpoint, Guarantees: []string{}}})
	}
}
