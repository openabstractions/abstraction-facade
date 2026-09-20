package runtime

import (
	"context"
	"errors"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
)

// ApplicationsContract is the provider applications profile a applications service publishes.
const ApplicationsContract = "abstraction.facade/applications@1"

// ApplicationsService is a separately composed abstraction.facade/applications@1 host
// over the runtime's application directory, already listening on
// Options.ApplicationsEndpoint. The runtime serves it, publishes the profile and
// closes it with the runtime. Its rights action goes in Options.RightsActions.
type ApplicationsService interface {
	Serve(context.Context) error
	Close() error
}

func validateApplications(options Options) error {
	if (options.Applications == nil) != (options.ApplicationsEndpoint == "") {
		return errors.New("runtime: a applications requires an explicit service and its endpoint")
	}
	return nil
}

func (h *Host) addApplicationsCandidate(options Options) {
	h.applicationsIndex = -1
	if options.Applications == nil {
		return
	}
	h.applications = options.Applications
	h.applicationsIndex = len(h.candidates)
	h.candidates = append(h.candidates, resolution.Candidate{Ready: true, Reference: wire.ServiceReference{
		Provider: "openabstractions.user-runtime", Capability: "abstraction.facade", Contract: ApplicationsContract,
		Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.ApplicationsEndpoint, Guarantees: []string{},
	}})
}
