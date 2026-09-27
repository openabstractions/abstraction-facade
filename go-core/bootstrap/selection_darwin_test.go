package bootstrap

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

func TestDarwinSelectionIgnoresSpoofedHOME(t *testing.T) {
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(home, ".local", "bin", "openabstractions")
	agent := filepath.Join(home, "Library", "LaunchAgents", runtimeAgent+".plist")
	if err := os.MkdirAll(filepath.Dir(program), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(agent), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(program, []byte("runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent, []byte(validRuntimeAgent(program)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	uid := strconv.Itoa(os.Geteuid())
	selected, err := selectDarwinInstalled(context.Background(), func(requested string) (*user.User, error) {
		if requested != uid {
			t.Fatalf("lookup ID = %q, want %q", requested, uid)
		}
		return &user.User{Uid: uid, HomeDir: home}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Server.Program != program {
		t.Fatalf("program = %q, want account-home program %q", selected.Server.Program, program)
	}
}

func TestTrustedDarwinInstalledPath(t *testing.T) {
	home := t.TempDir()
	program := filepath.Join(home, ".local", "bin", "openabstractions")
	if err := os.MkdirAll(filepath.Dir(program), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(program, []byte("runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := trustedInstalledPath(home, program, os.Geteuid(), true); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(program, 0720); err != nil {
		t.Fatal(err)
	}
	if err := trustedInstalledPath(home, program, os.Geteuid(), true); !errors.Is(err, ErrNoTrustedInstallation) {
		t.Fatalf("group-writable runtime accepted: %v", err)
	}
	if err := os.Chmod(program, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".local", "linked")
	if err := os.Symlink(filepath.Dir(program), link); err != nil {
		t.Fatal(err)
	}
	if err := trustedInstalledPath(home, filepath.Join(link, "openabstractions"), os.Geteuid(), true); !errors.Is(err, ErrNoTrustedInstallation) {
		t.Fatalf("linked ancestor accepted: %v", err)
	}
}

func TestDarwinActivationRevalidatesSelection(t *testing.T) {
	selection := Selection{Endpoint: "xpc:com.openabstractions.runtime-v1", Server: listen.ServerExpectation{
		Principal: identity.User{Kind: "posix", UID: 501, GID: -1}, Program: "/Users/test/.local/bin/openabstractions",
	}}
	called := false
	err := activateDarwinWith(context.Background(), selection, func(context.Context) (Selection, error) { return selection, nil }, func(_ context.Context, args ...string) error {
		called = true
		if len(args) != 2 || args[0] != "kickstart" || args[1] != "gui/501/com.openabstractions.runtime" {
			t.Fatalf("launchctl args: %q", args)
		}
		return nil
	})
	if err != nil || !called {
		t.Fatal(err, called)
	}
	changed := selection
	changed.Server.Program = "/tmp/runtime"
	if err := activateDarwinWith(context.Background(), changed, func(context.Context) (Selection, error) { return selection, nil }, func(context.Context, ...string) error {
		t.Fatal("launchctl called for stale selection")
		return nil
	}); !errors.Is(err, ErrActivationRefused) {
		t.Fatalf("stale selection: %v", err)
	}
}
