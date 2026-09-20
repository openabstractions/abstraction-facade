package bootstrap

import (
	"context"
	"errors"
	"time"
)

// ErrUpgradeInProgress is an activation refused because an installer is
// replacing the selected installation. `openabstractions start` reports it with
// exit status 3; activation is possible again when the installation finishes.
var ErrUpgradeInProgress = errors.New("bootstrap: an upgrade of the installed runtime is in progress")

// ErrActivationRefused is an activation this process may not perform, such as
// one requested from an elevated token. Nothing is launched.
var ErrActivationRefused = errors.New("bootstrap: runtime activation refused")

// ErrActivationUnsupported is returned where installed runtimes are activated
// by the platform's own service manager and not by the SDK.
var ErrActivationUnsupported = errors.New("bootstrap: on-demand runtime activation is not implemented on this platform")

// DefaultActivationBudget bounds an activation whose caller set no deadline.
// It is `openabstractions start`'s own default.
const DefaultActivationBudget = 20 * time.Second

// ActivateInstalled starts the selected installation's runtime once, through
// its own `openabstractions start`, and waits for it to report readiness
// within ctx. It performs no installation and selects nothing: the Selection
// comes from SelectInstalled, so a machine with no installation never reaches
// it. On Windows it refuses an elevated caller without launching anything and
// reports an installer upgrade as ErrUpgradeInProgress. Other platforms return
// ErrActivationUnsupported.
func ActivateInstalled(ctx context.Context, selection Selection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return activateInstalled(ctx, selection)
}

// activationTimeout is the `start --timeout` for ctx: its remaining time, or
// the default without a deadline.
func activationTimeout(ctx context.Context, now time.Time) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return DefaultActivationBudget
	}
	remaining := deadline.Sub(now).Truncate(time.Millisecond)
	if remaining < time.Millisecond {
		return time.Millisecond
	}
	return remaining
}
