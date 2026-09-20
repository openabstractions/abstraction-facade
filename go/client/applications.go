package client

import (
	"context"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-identity/listen"
	"time"
)

// ApplicationsContract identifies the experimental application presence directory.
const ApplicationsContract = "abstraction.facade/applications@1"

// Applications uses the shared authenticated local transport. Directory metadata grants no invocation authority.
type Applications struct{ transport listen.FrameClient }

func (m *Machine) ResolveApplications(ctx context.Context, need Requirements) (*Applications, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if need.Scope != 0 && need.Scope != wire.ScopeAny && need.Scope != wire.ScopeLocal {
		return nil, &ResolutionError{Status: ResolutionErrorStatus(wire.ResolutionStatusUnmetRequirements.String()), Capability: "abstraction.facade", Contract: ApplicationsContract, Scope: need.Scope}
	}
	need.Scope = wire.ScopeLocal
	endpoint, err := m.resolve(ctx, "abstraction.facade", ApplicationsContract, need)
	if err != nil {
		return nil, err
	}
	return &Applications{transport: endpoint.WithDefaults(35*time.Second, 1<<20)}, nil
}
func (a *Applications) client(ctx context.Context) (*wire.ApplicationsClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return wire.NewApplicationsClient(a.transport.WithContext(ctx)), nil
}
func (a *Applications) Register(ctx context.Context, descriptor wire.ApplicationDescriptor) (wire.ApplicationChange, error) {
	c, err := a.client(ctx)
	if err != nil {
		return wire.ApplicationChange{}, err
	}
	return c.Register(descriptor)
}
func (a *Applications) Remove(ctx context.Context, application string) (wire.ApplicationChange, error) {
	c, err := a.client(ctx)
	if err != nil {
		return wire.ApplicationChange{}, err
	}
	return c.Remove(application)
}
func (a *Applications) Announce(ctx context.Context, presence wire.ApplicationPresence) (wire.ApplicationChange, error) {
	c, err := a.client(ctx)
	if err != nil {
		return wire.ApplicationChange{}, err
	}
	return c.Announce(presence)
}
func (a *Applications) Withdraw(ctx context.Context, application, instance string) (wire.ApplicationChange, error) {
	c, err := a.client(ctx)
	if err != nil {
		return wire.ApplicationChange{}, err
	}
	return c.Withdraw(application, instance)
}
func (a *Applications) Observe(ctx context.Context, cursor string, waitMs int64) (wire.ApplicationPage, error) {
	c, err := a.client(ctx)
	if err != nil {
		return wire.ApplicationPage{}, err
	}
	return c.Observe(cursor, waitMs)
}
func (a *Applications) Activate(ctx context.Context, application string) (wire.ApplicationActivationResult, error) {
	c, err := a.client(ctx)
	if err != nil {
		return wire.ApplicationActivationResult{}, err
	}
	return c.Activate(application)
}
