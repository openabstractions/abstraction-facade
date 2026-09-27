package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

func selectInstalled(ctx context.Context) (Selection, error) {
	return selectDarwinInstalled(ctx, user.LookupId)
}

func selectDarwinInstalled(ctx context.Context, lookup func(string) (*user.User, error)) (Selection, error) {
	endpoint, err := InstalledEndpoint("runtime-v1")
	if err != nil {
		return Selection{}, err
	}
	if err := listen.CanEver(endpoint, listen.Program); err != nil {
		return Selection{}, fmt.Errorf("%w: installed XPC transport cannot prove runtime identity: %v", ErrNoTrustedInstallation, err)
	}
	if os.Getuid() != os.Geteuid() {
		return Selection{}, fmt.Errorf("%w: select outside a changed effective identity", ErrNoTrustedInstallation)
	}
	uid := os.Geteuid()
	account, err := lookup(strconv.Itoa(uid))
	if err != nil {
		return Selection{}, fmt.Errorf("%w: current account unavailable: %v", ErrNoTrustedInstallation, err)
	}
	if account.Uid != strconv.Itoa(uid) || account.HomeDir == "" {
		return Selection{}, fmt.Errorf("%w: current account identity is inconsistent", ErrNoTrustedInstallation)
	}
	return selectDarwinInstalledAt(ctx, filepath.Clean(account.HomeDir), uid)
}

// selectDarwinInstalledAt keeps fixture paths injectable while production
// selection obtains the home directory from the account database for the
// process UID. HOME is not installation identity evidence.
func selectDarwinInstalledAt(ctx context.Context, home string, uid int) (Selection, error) {
	program := filepath.Join(home, ".local", "bin", "openabstractions")
	agent := filepath.Join(home, "Library", "LaunchAgents", runtimeAgent+".plist")
	if err := trustedInstalledPath(home, program, uid, true); err != nil {
		return Selection{}, err
	}
	if err := trustedInstalledPath(home, agent, uid, false); err != nil {
		return Selection{}, err
	}
	raw, err := readAgent(agent)
	if err != nil {
		return Selection{}, fmt.Errorf("%w: installed LaunchAgent: %v", ErrNoTrustedInstallation, err)
	}
	if err := ValidateRuntimeAgent(raw, program); err != nil {
		return Selection{}, fmt.Errorf("%w: %v", ErrNoTrustedInstallation, err)
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	endpoint, err := InstalledEndpoint("runtime-v1")
	if err != nil {
		return Selection{}, err
	}
	return Selection{Endpoint: endpoint, Server: listen.ServerExpectation{
		Principal: identity.User{Kind: "posix", UID: uid, GID: -1}, Program: program,
	}}, nil
}

// trustedInstalledPath rejects replacement through links or a different OS
// user. The executable remains a user-owned installation; this establishes no
// publisher identity. The XPC client derives its expected code identity from
// this independently selected path before it sends a capability request.
func trustedInstalledPath(home, path string, uid int, executable bool) error {
	refuse := func(detail string) error {
		return fmt.Errorf("%w: %s", ErrNoTrustedInstallation, detail)
	}
	if !filepath.IsAbs(home) || filepath.Clean(home) != home || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return refuse("installed path is not absolute and canonical")
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return refuse("installed path is outside the current user home")
	}
	current := home
	parts := append([]string{"."}, splitPath(rel)...)
	for i, part := range parts {
		if part != "." {
			current = filepath.Join(current, part)
		}
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("%w: inspect %s: %v", ErrNoTrustedInstallation, current, err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != uid {
			return refuse("installed path is not owned by the current user")
		}
		if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return refuse("installed path is linked or writable by another user")
		}
		last := i == len(parts)-1
		if !last && !info.IsDir() {
			return refuse("installed path ancestor is not a directory")
		}
		if last {
			if !info.Mode().IsRegular() {
				return refuse("installed file is not regular")
			}
			if executable && info.Mode().Perm()&0100 == 0 {
				return refuse("installed runtime is not executable by its owner")
			}
		}
	}
	return nil
}

func splitPath(path string) []string {
	var parts []string
	for path != "." && path != "" {
		dir, base := filepath.Split(path)
		parts = append([]string{base}, parts...)
		path = filepath.Clean(dir)
		if path == string(filepath.Separator) {
			break
		}
	}
	return parts
}
