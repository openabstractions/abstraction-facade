package client

import (
	"context"
	asks "github.com/openabstractions/abstraction-asks/go/client"
)

// ResolveAsks binds caller-owned question admission and observation. Human
// answers are supplied by the provider's independently authorized operator.
func (m *Machine) ResolveAsks(ctx context.Context, need Requirements) (*asks.Client, error) {
	endpoint, err := m.resolve(ctx, "abstraction.asks", "abstraction.asks/application@1", need)
	if err != nil {
		return nil, err
	}
	return asks.NewWithTransport(endpoint), nil
}

// ResolveAsksOperator binds the explicitly authorized human-answering profile.
// The receiving service checks operator authority on every method call.
func (m *Machine) ResolveAsksOperator(ctx context.Context, need Requirements) (*asks.Operator, error) {
	endpoint, err := m.resolve(ctx, "abstraction.asks", "abstraction.asks/operator@1", need)
	if err != nil {
		return nil, err
	}
	return asks.NewOperatorWithTransport(endpoint), nil
}
