package client

import (
	"context"

	credentials "github.com/openabstractions/abstraction-credentials/go/client"
)

// ResolveCredentials binds the credential holder of the caller's account:
// register, rotate, revoke, list and audit credentials by name. Every call is a
// rights decision on the bound caller, and no call returns secret bytes.
func (m *Machine) ResolveCredentials(ctx context.Context, need Requirements) (*credentials.Holder, error) {
	endpoint, err := m.resolve(ctx, "abstraction.credentials", "abstraction.credentials/holder@1", need)
	if err != nil {
		return nil, err
	}
	return credentials.NewHolderWithTransport(endpoint), nil
}

// ResolveCredentialsApplier binds the applier for a consuming service the
// receiving host designated as an enforcer. Any other program reads forbidden.
func (m *Machine) ResolveCredentialsApplier(ctx context.Context, need Requirements) (*credentials.Applier, error) {
	endpoint, err := m.resolve(ctx, "abstraction.credentials", "abstraction.credentials/applier@1", need)
	if err != nil {
		return nil, err
	}
	return credentials.NewApplierWithTransport(endpoint), nil
}
