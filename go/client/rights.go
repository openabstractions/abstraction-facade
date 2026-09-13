package client

import (
	"context"
	rights "github.com/openabstractions/abstraction-rights/go/client"
)

// ResolveRights binds one decision service. Resource providers enforce its
// decisions and require explicit authority to relay another caller's subject.
func (m *Machine) ResolveRights(ctx context.Context, need Requirements) (*rights.Client, error) {
	endpoint, err := m.resolve(ctx, "abstraction.rights", "abstraction.rights/authorization@1", need)
	if err != nil {
		return nil, err
	}
	return rights.NewWithTransport(endpoint), nil
}

// ResolveRightsOperator selects explicitly configured policy administration.
// Receiving operator authorization remains separate from service resolution.
func (m *Machine) ResolveRightsOperator(ctx context.Context, need Requirements) (*rights.Operator, error) {
	endpoint, err := m.resolve(ctx, "abstraction.rights", "abstraction.rights/operator@1", need)
	if err != nil {
		return nil, err
	}
	return rights.NewOperatorWithTransport(endpoint), nil
}
