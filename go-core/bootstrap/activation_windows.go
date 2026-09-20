package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// exitUpgradeInProgress is `openabstractions start`'s refusal while an
// installer holds the upgrade exclusion for its installation.
const exitUpgradeInProgress = 3

// exitVirtualizedProfile is `openabstractions start`'s refusal from a caller
// that sees a packaged app's private copy of AppData.
const exitVirtualizedProfile = 4

type activationEnv struct {
	elevated func() (bool, error)
	stat     func(string) (os.FileInfo, error)
	// run executes program with args and returns its combined bounded output and
	// exit status; err reports a failure to run or wait, not a nonzero exit.
	run func(ctx context.Context, program string, args ...string) (string, int, error)
	now func() time.Time
}

func systemActivationEnv() activationEnv {
	return activationEnv{
		elevated: func() (bool, error) { return windows.GetCurrentProcessToken().IsElevated(), nil },
		stat:     os.Stat,
		run:      runActivationCommand,
		now:      time.Now,
	}
}

func activateInstalled(ctx context.Context, selection Selection) error {
	return activateWith(ctx, selection, systemActivationEnv())
}

func activateWith(ctx context.Context, selection Selection, env activationEnv) error {
	program := selection.Server.Program
	if !filepath.IsAbs(program) || !strings.EqualFold(filepath.Base(program), "openabstractions.exe") {
		return fmt.Errorf("%w: the selected installation names no openabstractions.exe", ErrActivationRefused)
	}
	info, err := env.stat(program)
	if err != nil {
		return fmt.Errorf("%w: installed %s: %v", ErrNoTrustedInstallation, program, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: installed %s is not a regular file", ErrNoTrustedInstallation, program)
	}
	elevated, err := env.elevated()
	if err != nil {
		return fmt.Errorf("%w: the caller's token elevation is unknown: %v", ErrActivationRefused, err)
	}
	if elevated {
		return fmt.Errorf("%w: the caller's token is elevated, and the installed runtime starts only in the user's unelevated session", ErrActivationRefused)
	}
	output, code, err := env.run(ctx, program, "start", "--timeout", activationTimeout(ctx, env.now()).String())
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return fmt.Errorf("bootstrap: run %s start: %w", program, err)
	}
	detail := strings.Join(strings.Fields(output), " ")
	switch code {
	case 0:
		return nil
	case exitUpgradeInProgress:
		return fmt.Errorf("%w: %s", ErrUpgradeInProgress, detail)
	case exitVirtualizedProfile:
		return fmt.Errorf("%w: %s", ErrActivationRefused, detail)
	}
	return fmt.Errorf("bootstrap: %s start exited %d: %s", program, code, detail)
}

func runActivationCommand(ctx context.Context, program string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = filepath.Dir(program)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.WaitDelay = 100 * time.Millisecond
	var output boundedStatusOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return output.String(), exit.ExitCode(), nil
	}
	if err != nil {
		return output.String(), -1, err
	}
	return output.String(), 0, nil
}
