package bootstrap

import (
	"testing"

	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
)

func TestSystemdEvidence(t *testing.T) {
	for _, test := range []struct {
		input string
		want  wire.BootstrapState
	}{
		{"LoadState=loaded\nActiveState=active\nSubState=running", wire.BootstrapStateRunning},
		{"LoadState=loaded\nActiveState=activating\nSubState=auto-restart", wire.BootstrapStateStarting},
		{"LoadState=loaded\nActiveState=failed\nSubState=failed", wire.BootstrapStateInstalled},
		{"LoadState=loaded\nActiveState=inactive\nSubState=dead", wire.BootstrapStateInstalled},
		{"LoadState=not-found\nActiveState=inactive", wire.BootstrapStateUnknown},
		{"LoadState=loaded\nLoadState=not-found\nActiveState=active", wire.BootstrapStateUnknown},
	} {
		if got := observeSystemd(test.input); got.State != test.want {
			t.Fatal(got, test.want)
		}
	}
}
