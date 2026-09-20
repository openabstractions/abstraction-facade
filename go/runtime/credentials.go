package runtime

import (
	"context"
	"errors"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
)

// CredentialsService is a separately composed credentials host, for example
// abstraction-credentials/go/service.Host, already listening on
// Options.CredentialsEndpoint. The runtime serves it, publishes
// abstraction.credentials/holder@1 and, when an enforcer designation is
// configured, abstraction.credentials/applier@1, and closes it with the runtime.
// Its decision function and rights actions (Options.RightsActions) belong to
// the composition.
type CredentialsService interface {
	Serve(context.Context) error
	Close() error
	ApplierAvailable() bool
}

func validateCredentials(options Options) error {
	if (options.Credentials == nil) != (options.CredentialsEndpoint == "") {
		return errors.New("runtime: credentials require an explicit service and its endpoint")
	}
	return nil
}

func (h *Host) addCredentialCandidates(options Options) {
	if options.Credentials == nil {
		return
	}
	h.credentials = options.Credentials
	h.credentialsIndex = len(h.candidates)
	contracts := []string{"abstraction.credentials/holder@1"}
	if options.Credentials.ApplierAvailable() {
		contracts = append(contracts, "abstraction.credentials/applier@1")
	}
	for _, contract := range contracts {
		h.candidates = append(h.candidates, resolution.Candidate{Ready: true, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.credentials", Contract: contract, Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.CredentialsEndpoint, Guarantees: []string{},
		}})
	}
}
