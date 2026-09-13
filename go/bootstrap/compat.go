// Package bootstrap preserves the original imports through the pure core.
package bootstrap

import (
	"context"
	core "github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
)

func Endpoint(service string) (string, error) { return core.Endpoint(service) }
func ObserveInstalled(ctx context.Context) wire.BootstrapObservation {
	return core.ObserveInstalled(ctx)
}
