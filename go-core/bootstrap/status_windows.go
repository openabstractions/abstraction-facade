package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"unsafe"

	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
	"golang.org/x/sys/windows"
)

// SupervisorName uses the current token's authentication session, matching the
// installed per-user service instance used by explicit activation.
func SupervisorName() (string, error) {
	var data [64]byte
	var size uint32
	if err := windows.GetTokenInformation(windows.GetCurrentProcessToken(), windows.TokenStatistics, &data[0], uint32(len(data)), &size); err != nil {
		return "", err
	}
	if size < 16 {
		return "", errors.New("incomplete process token statistics")
	}
	luid := *(*windows.LUID)(unsafe.Pointer(&data[8]))
	return fmt.Sprintf("OpenAbstractionsSupervisor_%x", uint64(uint32(luid.HighPart))<<32|uint64(luid.LowPart)), nil
}

func observeInstalled(ctx context.Context) wire.BootstrapObservation {
	name, err := SupervisorName()
	if err != nil {
		return statusEvidence("unknown", "process session identity unavailable")
	}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return statusEvidence("unknown", "system command directory unavailable")
	}
	return observeWindows(ctx, name, filepath.Join(system, "sc.exe"), statusCommand, os.Executable, os.Stat)
}

var serviceState = regexp.MustCompile(`(?m)^\s*STATE\s*:\s*([1-7])\s`)

func observeWindows(ctx context.Context, name, sc string, run func(context.Context, string, ...string) (string, error), executable func() (string, error), stat func(string) (os.FileInfo, error)) wire.BootstrapObservation {
	out, err := run(ctx, sc, "query", name)
	if err == nil {
		matches := serviceState.FindAllStringSubmatch(out, -1)
		if len(matches) != 1 {
			return statusEvidence("unknown", "unrecognized service state response")
		}
		m := matches[0]
		switch m[1] {
		case "2":
			return statusEvidence("starting", "current-session supervisor is start-pending")
		case "4":
			return statusEvidence("running", "current-session supervisor is running; capability readiness is separate")
		default:
			return statusEvidence("installed", "current-session supervisor is registered but not running")
		}
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1060 {
		return statusEvidence("unknown", "supervisor query failed: "+err.Error())
	}
	self, err := executable()
	if err != nil {
		return statusEvidence("unknown", "current executable path unavailable")
	}
	// A sibling is payload evidence only. Missing siblings say nothing about
	// installations elsewhere or a compatible runtime owned by another host.
	for _, file := range []string{"openabstractions.exe", "jobdw.exe"} {
		path := filepath.Join(filepath.Dir(self), file)
		info, err := stat(path)
		if err != nil || !filepath.IsAbs(path) || !info.Mode().IsRegular() {
			return statusEvidence("unknown", "no verified installation in the current executable directory")
		}
	}
	return statusEvidence("installed", "runtime and supervisor sibling payloads are present; active supervision is unverified")
}
func hideStatusCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
