package client

import (
	"context"

	resource "github.com/openabstractions/abstraction-resource/go/client"
)

// ResolveResourceTable binds one reader of who holds a scarce resource on this
// machine. Every read remains subject to the selected service's
// abstraction.resource/table.read gate, which narrows a refused caller to its
// own program's rows rather than refusing the call.
func (m *Machine) ResolveResourceTable(ctx context.Context, need Requirements) (*resource.Client, error) {
	endpoint, err := m.resolve(ctx, "abstraction.resource", ResourceTableContract, need)
	if err != nil {
		return nil, err
	}
	return resource.NewWithTransport(endpoint), nil
}

// ResolveResourceLeases binds one holder of leases on a scarce resource: the
// writing side of the same service ResolveResourceTable reads. Every Acquire
// remains subject to abstraction.resource/hold on the resource asked for,
// which refuses a program with no rule rather than narrowing anything.
func (m *Machine) ResolveResourceLeases(ctx context.Context, need Requirements) (*resource.Client, error) {
	endpoint, err := m.resolve(ctx, "abstraction.resource", ResourceLeasesContract, need)
	if err != nil {
		return nil, err
	}
	return resource.NewWithTransport(endpoint), nil
}
