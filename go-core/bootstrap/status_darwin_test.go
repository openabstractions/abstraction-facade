package bootstrap

import (
	"testing"

	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
)

func TestLaunchdEvidence(t *testing.T) {
	for _, test := range []struct {
		input string
		want  wire.BootstrapState
	}{
		{"service = {\n\tstate = running\n}", wire.BootstrapStateRunning},
		{"service = {\n\tstate = spawn scheduled\n}", wire.BootstrapStateStarting},
		{"service = {\n\tstate = waiting\n}", wire.BootstrapStateInstalled},
		{"service = {\n\t\tstate = running\n}", wire.BootstrapStateUnknown},
		{"garbled", wire.BootstrapStateUnknown},
	} {
		if got := observeLaunchd(test.input); got.State != test.want {
			t.Fatal(got, test.want)
		}
	}
}
