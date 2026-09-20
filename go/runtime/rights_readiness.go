package runtime

import (
	"context"
	"time"

	"github.com/openabstractions/abstraction-facade/go/resolution"
)

// rightsStateInterval is how often a served decision point's state is read for
// readiness (research/rights-defaults/DECISION.md §3: during an outage the
// rights candidate reports not ready).
var rightsStateInterval = time.Second

// watchRightsState reads the decision state every rightsStateInterval until ctx
// ends and sets the authorization and operator candidates ready only while the
// state reads.
func (h *Host) watchRightsState(ctx context.Context) {
	ticker := time.NewTicker(rightsStateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		h.refreshRights(h.rightsPolicy.CheckState(ctx) == nil)
	}
}

// refreshRights publishes the rights candidates' readiness. A stopped or closed
// decision host stays not ready whatever the state reads.
func (h *Host) refreshRights(readable bool) {
	h.mu.Lock()
	changed := false
	for _, index := range []int{h.rightsIndex, h.rightsOperatorIndex} {
		if index < 0 {
			continue
		}
		ready := readable && h.rights.Available()
		if index == h.rightsOperatorIndex {
			ready = ready && h.rights.OperatorAvailable()
		}
		if h.candidates[index].Ready != ready {
			h.candidates[index].Ready, changed = ready, true
		}
	}
	var err error
	if changed {
		var catalog *resolution.Catalog
		if catalog, err = resolution.New(h.candidates); err == nil {
			err = h.resolver.Update(catalog)
		}
	}
	h.mu.Unlock()
	if err != nil && h.onError != nil {
		h.onError(err)
	}
}
