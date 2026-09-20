package client

import (
	"context"
	"time"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-identity/listen"
)

// RegistryContract is the runtime's provider registry profile.
const RegistryContract = "abstraction.facade/registry@1"

// Registry calls abstraction.facade/registry@1: the provider declarations a
// person placed in the runtime and the runtime's reading of each. It is an
// operator tool; applications resolve and never read it. Each call is a rights
// decision the runtime makes for this program (provider.manage).
type Registry struct{ transport listen.FrameClient }

// ResolveRegistry binds the selected runtime's provider registry.
func (m *Machine) ResolveRegistry(ctx context.Context, need Requirements) (*Registry, error) {
	endpoint, err := m.resolve(ctx, "abstraction.facade", RegistryContract, need)
	if err != nil {
		return nil, err
	}
	// Observe waits up to 30 seconds inside one call.
	return &Registry{transport: endpoint.WithDefaults(35*time.Second, 1<<20)}, nil
}

func (r *Registry) client(ctx context.Context) (*wire.RegistryClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return wire.NewRegistryClient(r.transport.WithContext(ctx)), nil
}

// Declarations reads every declaration, its reading and the revision edits take.
func (r *Registry) Declarations(ctx context.Context) (DeclarationList, error) {
	c, err := r.client(ctx)
	if err != nil {
		return DeclarationList{}, err
	}
	return c.Declarations()
}

// Declare adds one declaration at the listed revision.
func (r *Registry) Declare(ctx context.Context, expectedRevision string, declaration Declaration) (DeclarationChange, error) {
	c, err := r.client(ctx)
	if err != nil {
		return DeclarationChange{}, err
	}
	return c.Declare(expectedRevision, declaration)
}

// Withdraw removes one declaration at the listed revision.
func (r *Registry) Withdraw(ctx context.Context, expectedRevision, name string) (DeclarationChange, error) {
	c, err := r.client(ctx)
	if err != nil {
		return DeclarationChange{}, err
	}
	return c.Withdraw(expectedRevision, name)
}

// Observe waits up to wait for the declarations or their readings to differ
// from cursor; an empty cursor reads at once.
func (r *Registry) Observe(ctx context.Context, cursor string, wait time.Duration) (DeclarationObservation, error) {
	c, err := r.client(ctx)
	if err != nil {
		return DeclarationObservation{}, err
	}
	return c.Observe(cursor, wait.Milliseconds())
}

// DescribeEndpoint calls abstraction.facade/endpoint@1 Describe on a local
// endpoint: the services it hosts and each one's readiness. It is an operator
// diagnostic. The endpoint is a platform endpoint path, and the connection
// requires no particular server program; a registry binds the declared
// program itself.
func (m *Machine) DescribeEndpoint(ctx context.Context, endpoint string) (Description, error) {
	if err := ctx.Err(); err != nil {
		return Description{}, err
	}
	transport := listen.FrameClient{Endpoint: endpoint, Timeout: 5 * time.Second, MaxFrame: 1 << 20}
	return wire.NewEndpointClient(transport.WithContext(ctx)).Describe()
}
