package bootstrap

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWindowsSupervisorEvidence(t *testing.T) {
	for _, test := range []struct {
		output, state string
		err           error
	}{
		{"STATE : 2 START_PENDING", "starting", nil},
		{"STATE : 4 RUNNING", "running", nil},
		{"STATE : 1 STOPPED", "installed", nil},
		{"garbled", "unknown", nil},
		{"STATE : 4 RUNNING\nSTATE : 1 STOPPED", "unknown", nil},
		{"", "unknown", errors.New("access denied")},
	} {
		got := observeWindows(context.Background(), "own-session", "sc", func(_ context.Context, path string, args ...string) (string, error) {
			if path != "sc" || len(args) != 2 || args[0] != "query" || args[1] != "own-session" {
				t.Fatal(path, args)
			}
			return test.output, test.err
		}, func() (string, error) { t.Fatal("unexpected fallback"); return "", nil }, os.Stat)
		if got.State != test.state {
			t.Fatal(got, test.state)
		}
	}
}

func TestMissingSessionServiceDoesNotInventUninstalled(t *testing.T) {
	t.Setenv("OA_BOOTSTRAP_STATUS_HELPER", "absent-service")
	missing := exec.Command(os.Args[0], "-test.run=^TestStatusHelperProcess$").Run()
	var exit *exec.ExitError
	if !errors.As(missing, &exit) || exit.ExitCode() != 1060 {
		t.Fatal(missing)
	}
	dir := t.TempDir()
	probe := func() string {
		return observeWindows(context.Background(), "own-session", "sc", func(context.Context, string, ...string) (string, error) { return "", missing }, func() (string, error) { return filepath.Join(dir, "panel.exe"), nil }, os.Stat).State
	}
	if got := probe(); got != "unknown" {
		t.Fatal(got)
	}
	for _, name := range []string{"openabstractions.exe", "jobdw.exe"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("inert payload witness"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got := probe(); got != "installed" {
		t.Fatal(got)
	}
}
