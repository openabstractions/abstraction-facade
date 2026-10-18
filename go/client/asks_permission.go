package client

import (
	"context"
	asks "github.com/openabstractions/abstraction-asks/go/client"
)

const AsksPermissionContract = "abstraction.asks/permission@1"
const AsksPermissionOperatorContract = "abstraction.asks/permission-operator@1"

func (m *Machine) ResolveAsksPermission(ctx context.Context, need Requirements) (*asks.PermissionClient, error) {
	endpoint, err := m.resolve(ctx, "abstraction.asks", AsksPermissionContract, need)
	if err != nil {
		return nil, err
	}
	return asks.NewPermissionWithTransport(endpoint), nil
}
func (m *Machine) ResolveAsksPermissionOperator(ctx context.Context, need Requirements) (*asks.PermissionOperator, error) {
	endpoint, err := m.resolve(ctx, "abstraction.asks", AsksPermissionOperatorContract, need)
	if err != nil {
		return nil, err
	}
	return asks.NewPermissionOperatorWithTransport(endpoint), nil
}
