package client

import (
	"context"
	"errors"
	"time"

	config "github.com/openabstractions/abstraction-config/go/client"
	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	"github.com/openabstractions/abstraction-facade/go-core/resolution"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-identity/listen"
	logging "github.com/openabstractions/abstraction-logging/go/client"
	router "github.com/openabstractions/abstraction-router/go/client"
)

// Scope selects where a capability may execute.
type Scope = wire.Scope

const (
	ScopeAny    = wire.ScopeAny
	ScopeLocal  = wire.ScopeLocal
	ScopeRemote = wire.ScopeRemote
)

// ScopeValues returns the supported execution scopes.
func ScopeValues() []Scope { return wire.ScopeValues() }

// Requirements are demands, never fallback preferences. The zero scope means any.
type Requirements struct {
	Guarantees []string
	Scope      Scope
}

// ResolutionError is a resolve call that produced no usable service; see
// resolution.Error. Use errors.As to read its Status, capability, contract,
// what was looked for and, through Unwrap, the selection or transport cause.
type ResolutionError = resolution.Error

// ResolutionErrorStatus says why resolution produced no usable service.
type ResolutionErrorStatus = resolution.ErrorStatus

const (
	RuntimeUnavailable   = resolution.RuntimeUnavailable
	InvalidResolution    = resolution.InvalidResolution
	UnsupportedTransport = resolution.UnsupportedTransport
)

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
	resolver, _, _, err := m.resolverSelection(ctx)
	return resolver, err
}

// resolverSelection also names what it looked for, so a failure can say it.
func (m *Machine) resolverSelection(ctx context.Context) (*resolution.Client, *listen.ServerExpectation, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, "", err
	}
	if m.server != nil {
		return resolution.NewVerifiedClient(m.endpoint, 2*time.Second, *m.server), m.server, "the explicit endpoint " + m.endpoint, nil
	}
	if m.unverified {
		endpoint := m.endpoint
		if endpoint == "" {
			var err error
			endpoint, err = resolution.CheckedDefaultEndpoint()
			if err != nil {
				return nil, nil, "the installed runtime", err
			}
			return resolution.NewUnverifiedClient(endpoint, 2*time.Second), nil, "the installed runtime at " + endpoint, nil
		}
		return resolution.NewUnverifiedClient(endpoint, 2*time.Second), nil, "the explicit endpoint " + endpoint, nil
	}
	selectInstalled := m.selectInstalled
	if selectInstalled == nil {
		selectInstalled = bootstrap.SelectInstalled
	}
	selected, err := selectInstalled(ctx)
	if err != nil {
		return nil, nil, "the installed runtime", err
	}
	return resolution.NewVerifiedClient(selected.Endpoint, 2*time.Second, selected.Server), &selected.Server, "the installed runtime at " + selected.Endpoint, nil
}

func (m *Machine) resolve(ctx context.Context, capability, contract string, need Requirements) (listen.FrameClient, error) {
	endpoint, _, err := m.resolveReference(ctx, capability, contract, need)
	return endpoint, err
}

// resolveReference also returns the selected reference, for bindings whose
// callers persist the selection for restart.
//
// Every outcome that yields no service is a *ResolutionError: no runtime
// selected or reached (RuntimeUnavailable), the resolver's refusal, an invalid
// answer, or a reference this binding cannot use. The caller's cancellation,
// and a context already done on entry, stay the context error.
func (m *Machine) resolveReference(caller context.Context, capability, contract string, need Requirements) (listen.FrameClient, wire.ServiceReference, error) {
	if err := caller.Err(); err != nil {
		return listen.FrameClient{}, wire.ServiceReference{}, err
	}
	ctx, cancel := context.WithTimeout(caller, 2*time.Second)
	defer cancel()
	resolver, server, lookedFor, err := m.resolverSelection(ctx)
	if err != nil {
		return listen.FrameClient{}, wire.ServiceReference{}, resolution.Unreachable(caller, err, capability, contract, lookedFor)
	}

	if need.Scope == 0 {
		need.Scope = wire.ScopeAny
	}
	request := wire.ResolveRequest{Capability: capability, Contracts: []string{contract}, Guarantees: need.Guarantees, Scope: need.Scope}
	result, ctx, cancelActivated, err := m.resolveActivating(caller, ctx, resolver, server, request, capability, contract, lookedFor)
	defer cancelActivated()
	var activation *ResolutionError
	if errors.As(err, &activation) && activation.Status == UpgradeInProgress {
		return listen.FrameClient{}, wire.ServiceReference{}, err
	}
	if err != nil {
		return listen.FrameClient{}, wire.ServiceReference{}, resolution.Unreachable(caller, err, capability, contract, lookedFor)
	}
	if result.Status != wire.ResolutionStatusResolved {
		return listen.FrameClient{}, wire.ServiceReference{}, &ResolutionError{Status: ResolutionErrorStatus(result.Status.String()), Capability: capability, Contract: contract, LookedFor: lookedFor}
	}
	if (result.Reference.Scope != wire.ScopeLocal && result.Reference.Scope != wire.ScopeRemote) || result.Reference.Transport != resolution.LocalTransport {
		return listen.FrameClient{}, wire.ServiceReference{}, &ResolutionError{Status: UnsupportedTransport, Capability: capability, Contract: contract, LookedFor: lookedFor,
			Scope: result.Reference.Scope, Transport: result.Reference.Transport}
	}
	endpoint, err := resolution.BindLocal(ctx, *result.Reference, server, m.providerTrust)
	return endpoint, *result.Reference, err
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
