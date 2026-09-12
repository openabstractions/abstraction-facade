package client

import (
	"context"
	"time"

	config "github.com/openabstractions/abstraction-config/go/client"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
	logging "github.com/openabstractions/abstraction-logging/go/client"
	router "github.com/openabstractions/abstraction-router/go/client"
)

// Requirements are demands, never fallback preferences. Empty scope means any.
type Requirements struct {
	Guarantees []string
	Scope      string
}

// BindingError preserves the resolver's typed refusal for callers.
type BindingError struct{ Status string }

func (e *BindingError) Error() string { return "facade binding: " + e.Status }

// New selects the trusted bootstrap location supplied by the installer or host.
// It does not open a store, activate a process or test availability.
func New(runtimeEndpoint string) *Machine { return &Machine{endpoint: runtimeEndpoint} }

func (m *Machine) resolve(ctx context.Context, capability, contract string, need Requirements) (string, error) {
	endpoint := m.endpoint
	if endpoint == "" {
		var err error
		endpoint, err = resolution.CheckedDefaultEndpoint()
		if err != nil {
			return "", err
		}
	}
	if need.Scope == "" {
		need.Scope = wire.ScopeAny
	}
	request := wire.ResolveRequest{Capability: capability, Contracts: []string{contract}, Guarantees: need.Guarantees, Scope: need.Scope}
	result, err := resolution.NewClient(endpoint, 2*time.Second).Resolve(ctx, request)
	if err != nil {
		return "", err
	}
	if result.Status != wire.ResolutionStatusResolved {
		return "", &BindingError{Status: result.Status}
	}
	if result.Reference.Scope != wire.ScopeLocal || result.Reference.Transport != resolution.LocalTransport {
		return "", &BindingError{Status: "unsupported_transport"}
	}
	return result.Reference.Endpoint, nil
}

// ResolveLog binds only the selected compatible provider. A later call failure
// remains a failure of that binding; it never reroutes potentially accepted work.
func (m *Machine) ResolveLog(ctx context.Context, need Requirements) (*logging.Client, error) {
	ep, err := m.resolve(ctx, "abstraction.logging", "abstraction.logging/sink@1", need)
	if err != nil {
		return nil, err
	}
	return logging.New(ep), nil
}
func (m *Machine) ResolveConfig(ctx context.Context, need Requirements) (*config.Client, error) {
	ep, err := m.resolve(ctx, "abstraction.config", "abstraction.config/reader@1", need)
	if err != nil {
		return nil, err
	}
	return config.New(ep), nil
}
func (m *Machine) ResolveRouter(ctx context.Context, need Requirements) (*router.Client, error) {
	ep, err := m.resolve(ctx, "abstraction.router", "abstraction.router/router@1", need)
	if err != nil {
		return nil, err
	}
	return router.New(ep), nil
}
