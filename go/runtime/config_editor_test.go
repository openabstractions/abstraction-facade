package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	configwire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-facade/go/client"
)

func TestResolvedConfigEditorSharesServiceOwnership(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("successful Program-bound IPC remains unproven on macOS")
	}
	home := t.TempDir()
	for _, key := range []string{"HOME", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(key, home)
	}
	t.Setenv("ProgramData", filepath.Join(home, "machine"))
	t.Setenv("ABSTRACTION_STORE", "host-run-value")
	o := jobOptions(t)
	h, _ := runJobRuntime(t, o)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	machine := client.New(o.Endpoint)
	editor, err := machine.ResolveConfigEditor(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := editor.ReadUserContext(ctx)
	if err != nil || initial.Values.Store != "" {
		t.Fatalf("user rung includes host override: %+v %v", initial, err)
	}
	values := initial.Values
	values.Off["paused-provider"] = "user choice"
	applied, err := editor.ReplaceUserContext(ctx, initial.Revision, values)
	if err != nil || applied.Outcome != configwire.UserReplaceOutcomeApplied {
		t.Fatalf("selected editor did not apply: %+v %v", applied, err)
	}
	other, err := machine.ResolveConfigEditor(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	conflict, err := other.ReplaceUserContext(ctx, initial.Revision, initial.Values)
	if err != nil || conflict.Outcome != configwire.UserReplaceOutcomeConflict {
		t.Fatalf("stale editor overwrote newer state: %+v %v", conflict, err)
	}
	reader, err := machine.ResolveConfig(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	effective, err := reader.ReadWithOverridesContext(ctx, configwire.RunOverrides{})
	if err != nil || effective.Off["paused-provider"] != "user choice" || effective.Store != "" {
		t.Fatalf("reader disagrees with service edit: %+v %v", effective, err)
	}
	h.config.Close()
	until := time.Now().Add(time.Second)
	for {
		_, err = machine.ResolveConfigEditor(ctx, client.Requirements{})
		var refusal *client.BindingError
		if errors.As(err, &refusal) && refusal.Status == "not_ready" {
			break
		}
		if time.Now().After(until) {
			t.Fatalf("stopped editor remained ready: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := editor.ReadUserContext(ctx); err == nil {
		t.Fatal("stale binding still called stopped config host")
	}
	if _, err := machine.ResolveConfig(ctx, client.Requirements{}); err == nil {
		t.Fatal("reader outlived shared config listener")
	}
	if _, err := machine.ResolveJobs(ctx, client.Requirements{}); err != nil {
		t.Fatalf("config shutdown disabled jobs: %v", err)
	}
}
