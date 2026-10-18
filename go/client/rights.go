package client

import (
	"context"
	rights "github.com/openabstractions/abstraction-rights/go/client"
)

// ResolveRights binds one decision service. Resource providers enforce its
// decisions and require explicit authority to relay another caller's subject.
func (m *Machine) ResolveRights(ctx context.Context, need Requirements) (*rights.Client, error) {
	endpoint, err := m.resolve(ctx, "abstraction.rights", RightsContract, need)
	if err != nil {
		return nil, err
	}
	return rights.NewWithTransport(endpoint), nil
}

// ResolveRightsOperator selects explicitly configured policy administration.
// Receiving operator authorization remains separate from service resolution.
func (m *Machine) ResolveRightsOperator(ctx context.Context, need Requirements) (*rights.Operator, error) {
	endpoint, err := m.resolve(ctx, "abstraction.rights", RightsOperatorContract, need)
	if err != nil {
		return nil, err
	}
	return rights.NewOperatorWithTransport(endpoint), nil
}

// ResolveRightsSelector selects the typed service explicitly. Absence refuses;
// no typed request is sent to authorization@1.
func (m *Machine) ResolveRightsSelector(ctx context.Context, need Requirements) (*rights.Client, error) {
	endpoint, err := m.resolve(ctx, "abstraction.rights", RightsSelectorContract, need)
	if err != nil {
		return nil, err
	}
	return rights.NewWithTransport(endpoint), nil
}
func (m *Machine) ResolveRightsSelectorOperator(ctx context.Context, need Requirements) (*rights.SelectorOperator, error) {
	endpoint, err := m.resolve(ctx, "abstraction.rights", RightsSelectorOperatorContract, need)
	if err != nil {
		return nil, err
	}
	return rights.NewSelectorOperatorWithTransport(endpoint), nil
}
