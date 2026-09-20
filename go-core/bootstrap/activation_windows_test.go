package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-identity/listen"
	"golang.org/x/sys/windows"
)

// TestActivationHelperProcess is a stand-in `openabstractions.exe start`: a
// copy of this test binary under that name. It records its arguments and exits
// with the requested status.
func TestActivationHelperProcess(t *testing.T) {
	record := os.Getenv("OA_ACTIVATION_HELPER")
	if record == "" {
		return
	}
	os.WriteFile(record, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
	code, _ := strconv.Atoi(os.Getenv("OA_ACTIVATION_EXIT"))
	fmt.Println("activation helper output")
	os.Exit(code)
}

var fixedNow = time.Now()

type fakeActivation struct {
	elevated bool
	code     int
	runs     [][]string
}

func (f *fakeActivation) env(stat func(string) (os.FileInfo, error)) activationEnv {
	return activationEnv{
		elevated: func() (bool, error) { return f.elevated, nil },
		stat:     stat,
		run: func(_ context.Context, program string, args ...string) (string, int, error) {
			f.runs = append(f.runs, append([]string{program}, args...))
			return "start refused: an upgrade of this installation is in progress\n", f.code, nil
		},
		now: func() time.Time { return fixedNow },
	}
}

func installedSelection(t *testing.T) (Selection, string) {
	dir := filepath.Join(t.TempDir(), "tools")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "openabstractions.exe")
	if err := os.WriteFile(program, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return Selection{Endpoint: `\\.\pipe\oa-activation-test`, Server: listen.ServerExpectation{Program: program}}, program
}

func TestActivationRunsTheInstalledStartWithinTheCallersBudget(t *testing.T) {
	selection, program := installedSelection(t)
	fake := &fakeActivation{}
	ctx, cancel := context.WithDeadline(context.Background(), fixedNow.Add(7500*time.Millisecond))
	defer cancel()
	if err := activateWith(ctx, selection, fake.env(os.Stat)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fake.runs, [][]string{{program, "start", "--timeout", "7.5s"}}) {
		t.Fatalf("runs %v", fake.runs)
	}
	fake = &fakeActivation{}
	if err := activateWith(context.Background(), selection, fake.env(os.Stat)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fake.runs, [][]string{{program, "start", "--timeout", "20s"}}) {
		t.Fatalf("runs without a deadline %v", fake.runs)
	}
}

func TestActivationReportsAnUpgradeInProgress(t *testing.T) {
	selection, _ := installedSelection(t)
	fake := &fakeActivation{code: 3}
	err := activateWith(context.Background(), selection, fake.env(os.Stat))
	if !errors.Is(err, ErrUpgradeInProgress) || !strings.Contains(err.Error(), "upgrade of this installation is in progress") {
		t.Fatalf("exit 3: %v", err)
	}
	fake = &fakeActivation{code: 1}
	if err := activateWith(context.Background(), selection, fake.env(os.Stat)); err == nil || errors.Is(err, ErrUpgradeInProgress) {
		t.Fatalf("exit 1: %v", err)
	}
}

func TestActivationRefusesWithoutLaunching(t *testing.T) {
	selection, program := installedSelection(t)
	elevated := &fakeActivation{elevated: true}
	if err := activateWith(context.Background(), selection, elevated.env(os.Stat)); !errors.Is(err, ErrActivationRefused) || len(elevated.runs) != 0 {
		t.Fatalf("elevated caller: %v runs=%v", err, elevated.runs)
	}
	for name, change := range map[string]func(*Selection){
		"relative program":     func(s *Selection) { s.Server.Program = `tools\openabstractions.exe` },
		"another program":      func(s *Selection) { s.Server.Program = filepath.Join(filepath.Dir(program), "openabstractionsw.exe") },
		"no program":           func(s *Selection) { s.Server.Program = "" },
		"missing installation": func(s *Selection) { s.Server.Program = filepath.Join(t.TempDir(), "openabstractions.exe") },
	} {
		fake := &fakeActivation{}
		changed := selection
		change(&changed)
		if err := activateWith(context.Background(), changed, fake.env(os.Stat)); err == nil || len(fake.runs) != 0 {
			t.Fatalf("%s: %v runs=%v", name, err, fake.runs)
		}
	}
}

// The real runner against a copy of this binary named openabstractions.exe:
// its exit status 3 is the typed refusal, and the arguments reach it.
func TestActivationOfARealProgramCopy(t *testing.T) {
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("elevated: activation refuses this token before launching")
	}
	dir := filepath.Join(t.TempDir(), "tools")
	os.MkdirAll(dir, 0o700)
	program := filepath.Join(dir, "openabstractions.exe")
	source, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Create(program)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(target, source)
	source.Close()
	target.Close()
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	record := filepath.Join(t.TempDir(), "args")
	t.Setenv("OA_ACTIVATION_HELPER", record)
	selection := Selection{Server: listen.ServerExpectation{Program: program}}
	env := systemActivationEnv()
	run := env.run
	env.run = func(ctx context.Context, program string, args ...string) (string, int, error) {
		return run(ctx, program, append([]string{"-test.run=^TestActivationHelperProcess$", "--"}, args...)...)
	}
	for code, want := range map[string]error{"0": nil, "3": ErrUpgradeInProgress} {
		t.Setenv("OA_ACTIVATION_EXIT", code)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := activateWith(ctx, selection, env)
		cancel()
		if (want == nil && err != nil) || (want != nil && !errors.Is(err, want)) {
			t.Fatalf("exit %s: %v", code, err)
		}
		args, _ := os.ReadFile(record)
		if !strings.Contains(string(args), "start\n--timeout\n") {
			t.Fatalf("exit %s: arguments %q", code, args)
		}
	}
	if err := ActivateInstalled(context.Background(), Selection{Server: listen.ServerExpectation{Program: filepath.Join(t.TempDir(), "openabstractions.exe")}}); !errors.Is(err, ErrNoTrustedInstallation) {
		t.Fatalf("no installed program: %v", err)
	}
}
