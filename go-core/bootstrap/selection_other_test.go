//go:build !windows && !linux

package bootstrap

import (
	"context"
	"errors"
	"testing"
)

func TestTrustedInstallationUnsupported(t *testing.T) {
	if _, err := SelectInstalled(context.Background()); !errors.Is(err, ErrUnsupportedSelection) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SelectInstalled(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
