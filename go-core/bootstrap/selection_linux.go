package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

func selectInstalled(ctx context.Context) (Selection, error) {
	if os.Getuid() != os.Geteuid() {
		return Selection{}, fmt.Errorf("%w: select outside a changed effective identity", ErrNoTrustedInstallation)
	}
	out, err := statusCommand(ctx, "/usr/bin/systemctl", "--user", "show", "abstraction-runtime.service", "--property=LoadState,ExecStart,User", "--no-pager")
	if err != nil {
		return Selection{}, fmt.Errorf("%w: user manager query: %w", ErrNoTrustedInstallation, err)
	}
	program, err := selectedSystemdProgram(out)
	if err != nil {
		return Selection{}, err
	}
	// Kernel image identity names the executable after symlink resolution.
	program, err = filepath.EvalSymlinks(program)
	if err != nil {
		return Selection{}, fmt.Errorf("%w: installed executable: %w", ErrNoTrustedInstallation, err)
	}
	endpoint := os.Getenv("ABSTRACTION_RUNTIME_ENDPOINT")
	if endpoint == "" {
		endpoint, err = Endpoint("runtime-v1")
		if err != nil {
			return Selection{}, err
		}
	}
	return Selection{Endpoint: endpoint, Server: listen.ServerExpectation{
		Principal: identity.User{Kind: "posix", UID: os.Geteuid(), GID: -1}, Program: program,
	}}, nil
}

// systemctl show renders each ExecStart entry as one structured block. Only one
// configured executable and the user manager's own principal are supported here.
// Reject ambiguous output instead of interpreting a command line or running it.
func selectedSystemdProgram(out string) (string, error) {
	refuse := func(detail string) (string, error) {
		return "", fmt.Errorf("%w: %s", ErrNoTrustedInstallation, detail)
	}
	fields := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || (key != "LoadState" && key != "ExecStart" && key != "User") {
			return refuse("unexpected user manager output")
		}
		if _, duplicate := fields[key]; duplicate {
			return refuse("duplicate user manager property")
		}
		fields[key] = value
	}
	if len(fields) != 3 || fields["LoadState"] != "loaded" || fields["User"] != "" {
		return refuse("loaded runtime under the user manager's principal required")
	}
	entry := fields["ExecStart"]
	if !strings.HasPrefix(entry, "{ path=") || !strings.HasSuffix(entry, " }") || strings.Count(entry, "{ path=") != 1 {
		return refuse("one runtime executable required")
	}
	path, rest, ok := strings.Cut(strings.TrimPrefix(entry, "{ path="), " ; argv[]=")
	if !ok || !strings.Contains(rest, " ; ignore_errors=no ; ") {
		return refuse("unrecognized executable registration")
	}
	if strings.Contains(path, `\`) {
		decoded, err := strconv.Unquote(`"` + strings.ReplaceAll(path, `"`, `\"`) + `"`)
		if err != nil {
			return refuse("invalid executable path escape")
		}
		path = decoded
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
		return refuse("absolute canonical executable required")
	}
	return path, nil
}
