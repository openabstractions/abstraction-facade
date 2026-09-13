package client

import (
	"context"
	config "github.com/openabstractions/abstraction-config/go/client"
)

// ResolveConfigObserver requires the selected provider's notification contract.
func (m *Machine) ResolveConfigObserver(ctx context.Context, need Requirements) (*config.Observer, error) {
	ep, e := m.resolve(ctx, "abstraction.config", "abstraction.config/observer@1", need)
	if e != nil {
		return nil, e
	}
	return config.NewObserverWithTransport(ep), nil
}
