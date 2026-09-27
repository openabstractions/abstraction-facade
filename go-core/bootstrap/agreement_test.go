package bootstrap_test

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	"github.com/openabstractions/abstraction-facade/go-core/resolution"
)

// The outside C++ consumer supplies independent implementation evidence. On
// macOS its default client uses the fixed installed LaunchAgent endpoint. Other
// platforms share the generic convention and its explicit environment address.
func TestCppRuntimeBootstrapAgreement(t *testing.T) {
	probe := os.Getenv("OA_CPP_BOOTSTRAP_PROBE")
	if probe == "" {
		t.Skip("set OA_CPP_BOOTSTRAP_PROBE to the outside C++ bootstrap probe")
	}
	for _, override := range []string{"", "explicit-runtime-override"} {
		contract := "generic-default"
		if override != "" {
			contract = "generic-environment-override"
		}
		if runtime.GOOS == "darwin" {
			contract = "installed-default"
			if override != "" {
				contract = "installed-ignores-environment-override"
			}
		}
		t.Run(contract, func(t *testing.T) {
			t.Setenv("ABSTRACTION_RUNTIME_ENDPOINT", override)
			var want string
			var err error
			if runtime.GOOS == "darwin" {
				want, err = bootstrap.InstalledEndpoint("runtime-v1")
			} else {
				want, err = resolution.CheckedDefaultEndpoint()
			}
			if err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(probe, "--default-runtime-endpoint").CombinedOutput()
			if err != nil {
				t.Fatalf("C++ endpoint: %v %s", err, output)
			}
			if actual := strings.TrimSpace(string(output)); actual != want {
				t.Fatalf("C++ %q differs from Go %q", actual, want)
			}
		})
	}
}
