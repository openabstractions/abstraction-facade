// Package client supplies service bindings selected by the runtime resolver.
// Service absence is reported by the requested capability. Discovery performs
// no activation or provider initialization.
package client

import (
	"context"
	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	"github.com/openabstractions/abstraction-facade/go-core/resolution"
	"github.com/openabstractions/abstraction-identity/listen"
)

type Machine struct {
	endpoint      string
	server        *listen.ServerExpectation
	unverified    bool
	providerTrust resolution.ProviderTrust
	// Private seam for isolated installation-selection fixtures.
	selectInstalled func(context.Context) (bootstrap.Selection, error)
}

// Discover selects registered installation evidence before contacting the resolver.
// Unsupported installation proof is returned explicitly. No unverified fallback.
// Resolve* methods query actual registrations and preserve typed refusals.
func Discover() *Machine { return &Machine{} }
