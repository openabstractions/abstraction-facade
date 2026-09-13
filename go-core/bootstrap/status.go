package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"

	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
)

// ObserveInstalled reads current-user installation/supervision evidence. It
// performs no activation. Running supervision never establishes capability
// readiness; callers combine this with the facade's service observations.
func ObserveInstalled(ctx context.Context) wire.BootstrapObservation {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return statusEvidence("unknown", err.Error())
	}
	return observeInstalled(ctx)
}

func statusEvidence(state, detail string) wire.BootstrapObservation {
	return wire.BootstrapObservation{State: state, Detail: detail}
}

type boundedStatusOutput struct{ bytes.Buffer }

func (b *boundedStatusOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		return 0, errors.New("supervisor observation output limit exceeded")
	}
	return b.Buffer.Write(p)
}

func statusCommand(ctx context.Context, path string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	hideStatusCommand(cmd)
	cmd.WaitDelay = 100 * time.Millisecond
	var output boundedStatusOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return output.String(), err
}
