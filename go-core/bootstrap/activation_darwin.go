package bootstrap

import (
	"context"
	"fmt"
)

func activateInstalled(ctx context.Context, selection Selection) error {
	return activateDarwinWith(ctx, selection, selectInstalled, func(ctx context.Context, args ...string) error {
		_, err := statusCommand(ctx, "/bin/launchctl", args...)
		return err
	})
}

// activateDarwinWith is the explicit activation route. XPC clients do not call
// it during discovery because looking up a declared Mach service activates its
// launchd job. Revalidation keeps direct callers bound to the installation they
// selected before requesting a kickstart.
func activateDarwinWith(ctx context.Context, selection Selection, selectCurrent func(context.Context) (Selection, error), launchctl func(context.Context, ...string) error) error {
	current, err := selectCurrent(ctx)
	if err != nil {
		return err
	}
	if selection.Endpoint != current.Endpoint || selection.Server.Principal.Kind != current.Server.Principal.Kind ||
		selection.Server.Principal.UID != current.Server.Principal.UID || selection.Server.Principal.GID != current.Server.Principal.GID ||
		selection.Server.Principal.SID != current.Server.Principal.SID || selection.Server.Program != current.Server.Program || selection.Server.Process != nil {
		return fmt.Errorf("%w: selection no longer matches the installed LaunchAgent", ErrActivationRefused)
	}
	err = launchctl(ctx, "kickstart", "gui/"+fmt.Sprint(current.Server.Principal.UID)+"/"+runtimeAgent)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("bootstrap: activate installed LaunchAgent: %w", err)
	}
	return nil
}
