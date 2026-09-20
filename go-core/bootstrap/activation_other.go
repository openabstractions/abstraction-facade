//go:build !windows

package bootstrap

import "context"

// systemd and launchd start and restart the installed runtime; the SDK does not.
func activateInstalled(context.Context, Selection) error { return ErrActivationUnsupported }
