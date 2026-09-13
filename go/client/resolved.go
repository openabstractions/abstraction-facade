package client

import (
	"context"
	"time"

	config "github.com/openabstractions/abstraction-config/go/client"
	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	"github.com/openabstractions/abstraction-facade/go-core/resolution"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-identity/listen"
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

// New preserves endpoint-only compatibility for deliberately supplied providers.
// Deprecated: use NewVerified, Discover, or explicitly named NewUnverified.
func New(runtimeEndpoint string) *Machine { return NewUnverified(runtimeEndpoint) }

// NewUnverified selects endpoint-only compatibility. It authenticates no server.
// An empty endpoint uses the existing bootstrap location convention.
func NewUnverified(runtimeEndpoint string) *Machine {
	return &Machine{endpoint: runtimeEndpoint, unverified: true}
}

// NewVerified binds an explicit resolver endpoint to independent caller-supplied
// trust evidence. Providers inherit the same-runtime identity profile unless
// WithProviderTrust supplies an independent policy for a different host.
func NewVerified(runtimeEndpoint string, server listen.ServerExpectation) *Machine {
	if server.Process != nil {
		process := *server.Process
		server.Process = &process
	}
	return &Machine{endpoint: runtimeEndpoint, server: &server}
}

// WithProviderTrust returns a binding factory with independent provider policy.
// The default verified profile expects providers hosted by the selected runtime.
func (m *Machine) WithProviderTrust(policy resolution.ProviderTrust) *Machine {
	copy := *m
	copy.providerTrust = policy
	return &copy
}

func (m *Machine) resolver(ctx context.Context) (*resolution.Client, error) {
	resolver, _, err := m.resolverSelection(ctx)
	return resolver, err
}

func (m *Machine) resolverSelection(ctx context.Context) (*resolution.Client, *listen.ServerExpectation, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if m.server != nil {
		return resolution.NewVerifiedClient(m.endpoint, 2*time.Second, *m.server), m.server, nil
	}
	if m.unverified {
		endpoint := m.endpoint
		if endpoint == "" {
			var err error
			endpoint, err = resolution.CheckedDefaultEndpoint()
			if err != nil {
				return nil, nil, err
			}
		}
		return resolution.NewUnverifiedClient(endpoint, 2*time.Second), nil, nil
	}
	selectInstalled := m.selectInstalled
	if selectInstalled == nil {
		selectInstalled = bootstrap.SelectInstalled
	}
	selected, err := selectInstalled(ctx)
	if err != nil {
		return nil, nil, err
	}
	return resolution.NewVerifiedClient(selected.Endpoint, 2*time.Second, selected.Server), &selected.Server, nil
}

func (m *Machine) resolve(ctx context.Context, capability, contract string, need Requirements) (listen.FrameClient, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resolver, server, err := m.resolverSelection(ctx)
	if err != nil {
		return listen.FrameClient{}, err
	}

	if need.Scope == "" {
		need.Scope = wire.ScopeAny
	}
	request := wire.ResolveRequest{Capability: capability, Contracts: []string{contract}, Guarantees: need.Guarantees, Scope: need.Scope}
	result, err := resolver.Resolve(ctx, request)
	if err != nil {
		return listen.FrameClient{}, err
	}
	if result.Status != wire.ResolutionStatusResolved {
		return listen.FrameClient{}, &BindingError{Status: result.Status}
	}
	if result.Reference.Scope != wire.ScopeLocal || result.Reference.Transport != resolution.LocalTransport {
		return listen.FrameClient{}, &BindingError{Status: "unsupported_transport"}
	}
	return resolution.BindLocal(ctx, *result.Reference, server, m.providerTrust)
}

// ResolveLog binds only the selected compatible provider. A later call failure
// remains a failure of that binding; it never reroutes potentially accepted work.
func (m *Machine) ResolveLog(ctx context.Context, need Requirements) (*logging.Client, error) {
	ep, err := m.resolve(ctx, "abstraction.logging", "abstraction.logging/sink@1", need)
	if err != nil {
		return nil, err
	}
	return logging.NewWithTransport(ep), nil
}
func (m *Machine) ResolveConfig(ctx context.Context, need Requirements) (*config.Client, error) {
	ep, err := m.resolve(ctx, "abstraction.config", "abstraction.config/reader@1", need)
	if err != nil {
		return nil, err
	}
	return config.NewWithTransport(ep), nil
}

// ResolveConfigEditor binds user-setting mutations to the configuration service.
// The service owns persistence and checks the revision at the write boundary.
func (m *Machine) ResolveConfigEditor(ctx context.Context, need Requirements) (*config.Editor, error) {
	ep, err := m.resolve(ctx, "abstraction.config", "abstraction.config/editor@1", need)
	if err != nil {
		return nil, err
	}
	return config.NewEditorWithTransport(ep), nil
}
func (m *Machine) ResolveRouter(ctx context.Context, need Requirements) (*router.Client, error) {
	ep, err := m.resolve(ctx, "abstraction.router", "abstraction.router/router@1", need)
	if err != nil {
		return nil, err
	}
	return router.NewWithTransport(ep), nil
}

// ResolveLogReader selects a provider advertising retained history.
func (m *Machine) ResolveLogReader(ctx context.Context, need Requirements) (*logging.Reader, error) {
	ep, err := m.resolve(ctx, "abstraction.logging", "abstraction.logging/reader@1", need)
	if err != nil {
		return nil, err
	}
	return logging.NewReaderWithTransport(ep), nil
}

// ResolveLogObserver binds bounded long-poll history at the selected provider.
func (m *Machine) ResolveLogObserver(ctx context.Context, need Requirements) (*logging.Observer, error) {
	ep, err := m.resolve(ctx, "abstraction.logging", "abstraction.logging/observer@1", need)
	if err != nil {
		return nil, err
	}
	return logging.NewObserverWithTransport(ep), nil
}
