package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSystemdTrustedExecutableSelection(t *testing.T) {
	unit := "LoadState=loaded\nExecStart={ path=/home/app/.local/bin/openabstractions ; argv[]=/home/app/.local/bin/openabstractions serve runtime ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }\nUser=\n"
	for _, path := range []string{"/home/app/.local/bin/openabstractions", "/home/space name/.local/bin/openabstractions", `/home/space\x20name/.local/bin/openabstractions`} {
		input := strings.Replace(unit, "/home/app/.local/bin/openabstractions", path, 1)
		got, err := selectedSystemdProgram(input)
		want := strings.ReplaceAll(path, `\x20`, " ")
		if err != nil || got != want {
			t.Fatalf("%q: %q %v", path, got, err)
		}
	}
	for _, invalid := range []string{
		strings.Replace(unit, "LoadState=loaded", "LoadState=not-found", 1),
		strings.Replace(unit, "User=\n", "User=other\n", 1),
		unit + "User=\n",
		strings.Replace(unit, " ; argv[]=", " ; other[]=", 1),
		strings.Replace(unit, "ignore_errors=no", "ignore_errors=yes", 1),
		strings.Replace(unit, "path=/home/app/.local/bin/openabstractions", "path=relative", 1),
		strings.Replace(unit, "path=/home/app/.local/bin/openabstractions", "path=/home/app/../other", 1),
		strings.Replace(unit, "path=/home/app/.local/bin/openabstractions", `path=/home/app/\qbad`, 1),
		strings.Replace(unit, "User=\n", "Unknown=\n", 1),
		strings.Replace(unit, " ; argv[]=", " } { path=/second ; argv[]=", 1),
	} {
		if _, err := selectedSystemdProgram(invalid); !errors.Is(err, ErrNoTrustedInstallation) {
			t.Fatalf("ambiguous registration accepted: %q %v", invalid, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SelectInstalled(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
