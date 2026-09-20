package client

import (
	"context"
	"errors"
	"io/fs"
	"runtime"
	"time"

	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	"github.com/openabstractions/abstraction-facade/go-core/resolution"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-identity/listen"
)

// UpgradeInProgress is a resolution that found the installed runtime stopped
// while an installer replaces it; see resolution.UpgradeInProgress.
const UpgradeInProgress = resolution.UpgradeInProgress

// resolveActivating is the resolver call of default discovery. On Windows,
// when an installation was selected and its resolver endpoint does not exist,
// it runs that installation's `openabstractions start` once within the
// caller's budget and asks again. An explicit endpoint, a missing installation,
// a refused connection and an untrusted server are reported as they are. The
// returned context bounds the rest of the resolution.
func (m *Machine) resolveActivating(caller, ctx context.Context, resolver *resolution.Client, server *listen.ServerExpectation,
	request wire.ResolveRequest, capability, contract, lookedFor string) (wire.ResolveResult, context.Context, context.CancelFunc, error) {
	result, err := resolver.Resolve(ctx, request)
	if err == nil || !m.activates(caller, server, err) {
		return result, ctx, func() {}, err
	}
	activate := m.activate
	if activate == nil {
		activate = bootstrap.ActivateInstalled
	}
	budget, cancelBudget := caller, context.CancelFunc(func() {})
	if _, ok := caller.Deadline(); !ok {
		budget, cancelBudget = context.WithTimeout(caller, bootstrap.DefaultActivationBudget)
	}
	activation := activate(budget, bootstrap.Selection{Server: *server})
	cancelBudget()
	if errors.Is(activation, bootstrap.ErrUpgradeInProgress) {
		return wire.ResolveResult{}, ctx, func() {}, &ResolutionError{Status: UpgradeInProgress, Capability: capability, Contract: contract, LookedFor: lookedFor, Err: activation}
	}
	if activation != nil {
		return wire.ResolveResult{}, ctx, func() {}, errors.Join(err, activation)
	}
	retry, cancel := context.WithTimeout(caller, 2*time.Second)
	result, err = resolver.Resolve(retry, request)
	return result, retry, cancel, err
}

// activates reports an absent resolver endpoint of a selected installation, on
// the platform whose SDK starts the installed runtime.
func (m *Machine) activates(caller context.Context, server *listen.ServerExpectation, err error) bool {
	if m.server != nil || m.unverified || server == nil || caller.Err() != nil {
		return false
	}
	if m.activate == nil && runtime.GOOS != "windows" {
		return false
	}
	return errors.Is(err, fs.ErrNotExist) && !errors.Is(err, listen.ErrServerUntrusted)
}
