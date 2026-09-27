//go:build !windows && !darwin

package bootstrap

import "context"

// The platform has no supported installed-runtime activation route.
func activateInstalled(context.Context, Selection) error { return ErrActivationUnsupported }
