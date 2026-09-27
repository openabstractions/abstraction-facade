// Package bootstrap preserves the original imports through the pure core.
package bootstrap

import (
	"context"
	core "github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
)

func Endpoint(service string) (string, error) { return core.Endpoint(service) }
func InstalledEndpoint(service string) (string, error) {
	return core.InstalledEndpoint(service)
}

type Selection = core.Selection

func SelectInstalled(ctx context.Context) (Selection, error) { return core.SelectInstalled(ctx) }

func ObserveInstalled(ctx context.Context) wire.BootstrapObservation {
	return core.ObserveInstalled(ctx)
}

// ProfileView is how this process sees the account's profile folders.
type ProfileView = core.ProfileView

// CurrentProfileView probes this process's profile view once.
func CurrentProfileView() (ProfileView, error) { return core.CurrentProfileView() }
