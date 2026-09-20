//go:build !windows && !linux && !darwin

package bootstrap

import (
	"context"
	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
	"os/exec"
)

func observeInstalled(context.Context) wire.BootstrapObservation {
	return statusEvidence(wire.BootstrapStateUnknown, "installation observation unsupported on this platform")
}
func hideStatusCommand(*exec.Cmd) {}
