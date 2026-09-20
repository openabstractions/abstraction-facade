package runtime

import (
	"context"
	"slices"

	"github.com/openabstractions/abstraction-facade/go/resolution"
)

// DeclaredProviders is the runtime's provider declarations: services outside
// the runtime process that a person registered, offered as resolver
// candidates after the runtime's own (research/inference-registration
// DECISION.md §3). The host serves and closes it with the runtime.
type DeclaredProviders interface {
	// Candidates is the current registrations and readiness, in policy order.
	// A candidate's Activate starts an on-demand provider.
	Candidates() []resolution.Candidate
	// Watch registers the function called after the candidates change.
	Watch(changed func())
	Serve(ctx context.Context) error
	Close() error
}

// catalogue is the runtime's own candidates followed by the declared ones.
// Callers hold h.mu.
func (h *Host) catalogue() (*resolution.Catalog, error) {
	candidates := slices.Clone(h.candidates)
	if h.providers != nil {
		candidates = append(candidates, h.providers.Candidates()...)
	}
	return resolution.New(candidates)
}

// refreshProviders republishes the catalogue after declared providers change.
func (h *Host) refreshProviders() {
	h.mu.Lock()
	catalog, err := h.catalogue()
	if err == nil && h.resolver != nil {
		err = h.resolver.Update(catalog)
	}
	h.mu.Unlock()
	if err != nil && h.onError != nil {
		h.onError(err)
	}
}
