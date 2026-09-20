package bootstrap

import (
	"context"
	"os/exec"
	"strings"

	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
)

func observeInstalled(ctx context.Context) wire.BootstrapObservation {
	out, err := statusCommand(ctx, "/usr/bin/systemctl", "--user", "show", "abstraction-runtime.service", "--property=LoadState,ActiveState,SubState")
	if err != nil {
		return statusEvidence(wire.BootstrapStateUnknown, "user service manager observation failed: "+err.Error())
	}
	return observeSystemd(out)
}
func observeSystemd(out string) wire.BootstrapObservation {
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		_, duplicate := fields[key]
		if !ok || duplicate {
			return statusEvidence(wire.BootstrapStateUnknown, "ambiguous user service manager response")
		}
		fields[key] = value
	}
	if fields["LoadState"] != "loaded" {
		return statusEvidence(wire.BootstrapStateUnknown, "current user unit is not loaded; other installation scopes are unverified")
	}
	switch fields["ActiveState"] {
	case "activating":
		return statusEvidence(wire.BootstrapStateStarting, "user runtime unit is activating")
	case "active":
		return statusEvidence(wire.BootstrapStateRunning, "user runtime unit is active; capability readiness is separate")
	case "failed":
		return statusEvidence(wire.BootstrapStateInstalled, "installed user runtime unit reports failure")
	case "inactive", "deactivating":
		return statusEvidence(wire.BootstrapStateInstalled, "user runtime unit is loaded but not active")
	default:
		return statusEvidence(wire.BootstrapStateUnknown, "unrecognized user runtime unit state")
	}
}
func hideStatusCommand(*exec.Cmd) {}
