//go:build windows

package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/openabstractions/abstraction-identity/listen"
)

func TestUserNamespacesAndIdentityFailure(t *testing.T) {
	const firstSID = "S-1-5-21-100-200-300-1001"
	const secondSID = "S-1-5-21-100-200-300-1002"
	first, err := endpoint("runtime-v1", func() (string, error) { return firstSID, nil })
	if err != nil {
		t.Fatal(err)
	}
	second, err := endpoint("runtime-v1", func() (string, error) { return secondSID, nil })
	if err != nil {
		t.Fatal(err)
	}
	if first != `\\.\pipe\openabstractions-user-`+firstSID+`-runtime-v1` || first == second {
		t.Fatalf("namespaces: %q %q", first, second)
	}
	broken := errors.New("token unavailable")
	if value, err := endpoint("runtime-v1", func() (string, error) { return "", broken }); value != "" || !errors.Is(err, broken) {
		t.Fatalf("identity failure: %q %v", value, err)
	}
	for _, sid := range []string{"", `other\pipe`, "not-a-sid"} {
		if _, err := endpoint("runtime-v1", func() (string, error) { return sid, nil }); err == nil {
			t.Fatalf("accepted invalid SID %q", sid)
		}
	}
	if listen.Endpoint("runtime-v1") != `\\.\pipe\openabstractions-runtime-v1` {
		t.Fatal("legacy transport convention changed")
	}
}

func TestProcessSIDIgnoresUserEnvironment(t *testing.T) {
	before, err := Endpoint("runtime-v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"USER", "USERNAME", "USERDOMAIN", "SID", "USER_SID"} {
		t.Setenv(name, "untrusted-identity-claim")
	}
	after, err := Endpoint("runtime-v1")
	if err != nil || after != before {
		t.Fatalf("environment changed process namespace: %q %q %v", before, after, err)
	}
}

func TestBootstrapProcessChild(t *testing.T) {
	if os.Getenv("OA_BOOTSTRAP_CHILD") != "1" {
		return
	}
	value, err := Endpoint("runtime-v1")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("OA_BOOTSTRAP=" + value)
}

func TestCurrentUserBootstrapMatchesAcrossProcesses(t *testing.T) {
	expected, err := Endpoint("runtime-v1")
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestBootstrapProcessChild$")
	cmd.Env = append(os.Environ(), "OA_BOOTSTRAP_CHILD=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v %s", err, output)
	}
	if !strings.Contains(string(output), "OA_BOOTSTRAP="+expected+"\n") {
		t.Fatalf("child namespace differs: %s", output)
	}
}
