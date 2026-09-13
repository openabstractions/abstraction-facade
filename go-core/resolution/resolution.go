// Package resolution selects service references from a trusted runtime catalogue.
// It owns no files, performs no calls to providers and never accepts work.
package resolution

import (
	"fmt"
	"slices"
	"strings"

	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
)

// Candidate is runtime-owned registration and readiness, not client input.
type Candidate struct {
	Reference wire.ServiceReference
	Ready     bool
}

// Catalog is an immutable snapshot in administrator/runtime policy order.
// Refreshing a snapshot does not transfer previously accepted work.
type Catalog struct{ candidates []Candidate }

func New(candidates []Candidate) (*Catalog, error) {
	c := &Catalog{}
	seen := map[[3]string]bool{}
	for _, candidate := range candidates {
		r := candidate.Reference
		if r.Provider == "" || r.Capability == "" || r.Contract == "" ||
			r.Transport == "" || r.Endpoint == "" || strings.ContainsRune(r.Endpoint, 0) ||
			(r.Scope != wire.ScopeLocal && r.Scope != wire.ScopeRemote) || !distinct(r.Guarantees) {
			return nil, fmt.Errorf("resolution: invalid registration")
		}
		// Each provider/capability/contract has one binding in this snapshot.
		// A tuple avoids aliases when an identifier itself contains a separator.
		key := [3]string{r.Provider, r.Capability, r.Contract}
		if seen[key] {
			return nil, fmt.Errorf("resolution: duplicate registration")
		}
		seen[key] = true
		candidate.Reference = clone(r)
		c.candidates = append(c.candidates, candidate)
	}
	return c, nil
}

// Authorize is supplied by the receiving boundary for its authenticated caller.
// A nil function denies access. It must not trust identity supplied in arguments.
type Authorize func(wire.ServiceReference) bool

// ForCaller binds authorization to a request receiver, not to catalogue state.
func (c *Catalog) ForCaller(authorize Authorize) *Resolver {
	return &Resolver{catalog: c, authorize: authorize}
}

type Resolver struct {
	catalog   *Catalog
	authorize Authorize
}

func (r *Resolver) Resolve(request wire.ResolveRequest) (wire.ResolveResult, error) {
	result := wire.ResolveResult{Status: wire.ResolutionStatusInvalidRequest}
	if request.Capability == "" || len(request.Contracts) == 0 || !distinct(request.Contracts) ||
		!distinct(request.Guarantees) ||
		(request.Scope != wire.ScopeAny && request.Scope != wire.ScopeLocal && request.Scope != wire.ScopeRemote) {
		return result, nil
	}
	if r == nil || r.catalog == nil || r.authorize == nil {
		result.Status = wire.ResolutionStatusForbidden
		return result, nil
	}
	var present, allowed, compatible, sufficient bool
	for _, candidate := range r.catalog.candidates {
		ref := candidate.Reference
		if ref.Capability != request.Capability || (request.Scope != wire.ScopeAny && request.Scope != ref.Scope) {
			continue
		}
		present = true
		if !r.authorize(clone(ref)) {
			continue
		}
		allowed = true
		if !slices.Contains(request.Contracts, ref.Contract) {
			continue
		}
		compatible = true
		if !containsAll(ref.Guarantees, request.Guarantees) {
			continue
		}
		sufficient = true
		if !candidate.Ready {
			continue
		}
		selected := clone(ref)
		return wire.ResolveResult{Status: wire.ResolutionStatusResolved, Reference: &selected}, nil
	}
	switch {
	case !present:
		result.Status = wire.ResolutionStatusUnavailable
	case !allowed:
		result.Status = wire.ResolutionStatusForbidden
	case !compatible:
		result.Status = wire.ResolutionStatusIncompatible
	case !sufficient:
		result.Status = wire.ResolutionStatusUnmetRequirements
	default:
		result.Status = wire.ResolutionStatusNotReady
	}
	return result, nil
}

func distinct(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func containsAll(available, required []string) bool {
	for _, value := range required {
		if !slices.Contains(available, value) {
			return false
		}
	}
	return true
}

func clone(r wire.ServiceReference) wire.ServiceReference {
	r.Guarantees = slices.Clone(r.Guarantees)
	return r
}
