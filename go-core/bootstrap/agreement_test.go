package bootstrap_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/openabstractions/abstraction-facade/go-core/resolution"
)

// The outside C++ consumer supplies independent implementation evidence for
// both the current-user convention and the explicit environment override.
func TestCppRuntimeBootstrapAgreement(t *testing.T) {
	probe := os.Getenv("OA_CPP_BOOTSTRAP_PROBE")
	if probe == "" {
		t.Skip("set OA_CPP_BOOTSTRAP_PROBE to the outside C++ bootstrap probe")
	}
	for _, override := range []string{"", "explicit-runtime-override"} {
		t.Run("endpoint="+override, func(t *testing.T) {
			t.Setenv("ABSTRACTION_RUNTIME_ENDPOINT", override)
			want, err := resolution.CheckedDefaultEndpoint()
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
