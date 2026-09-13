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
		return statusEvidence("unknown", "user service manager observation failed: "+err.Error())
	}
	return observeSystemd(out)
}
func observeSystemd(out string) wire.BootstrapObservation {
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		_, duplicate := fields[key]
		if !ok || duplicate {
			return statusEvidence("unknown", "ambiguous user service manager response")
		}
		fields[key] = value
	}
	if fields["LoadState"] != "loaded" {
		return statusEvidence("unknown", "current user unit is not loaded; other installation scopes are unverified")
	}
	switch fields["ActiveState"] {
	case "activating":
		return statusEvidence("starting", "user runtime unit is activating")
	case "active":
		return statusEvidence("running", "user runtime unit is active; capability readiness is separate")
	case "failed":
		return statusEvidence("installed", "installed user runtime unit reports failure")
	case "inactive", "deactivating":
		return statusEvidence("installed", "user runtime unit is loaded but not active")
	default:
		return statusEvidence("unknown", "unrecognized user runtime unit state")
	}
}
func hideStatusCommand(*exec.Cmd) {}
