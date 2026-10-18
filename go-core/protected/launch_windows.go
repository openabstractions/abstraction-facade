//go:build windows

package protected

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/openabstractions/abstraction-identity/go/trustedpath"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	seeMaskFlagNoUI       = 0x00000400
	swHide                = 0
)

// shellExecuteInfo is SHELLEXECUTEINFOW on 64-bit Windows (112 bytes).
type shellExecuteInfo struct {
	size       uint32
	mask       uint32
	window     windows.Handle
	verb       *uint16
	file       *uint16
	parameters *uint16
	directory  *uint16
	show       int32
	instance   windows.Handle
	idList     uintptr
	class      *uint16
	classKey   windows.Handle
	hotKey     uint32
	icon       windows.Handle
	process    windows.Handle
}

// launchParameters is the helper's command line as ShellExecuteExW takes it:
// the closed grammar has no space or quote inside an argument.
func launchParameters(operation, ticket string) (string, error) {
	args, err := HelperArguments(operation, ticket)
	if err != nil {
		return "", err
	}
	return strings.Join(args, " "), nil
}

// launchRequest is the ShellExecuteExW request: verb runas on the exact
// helper with its closed command line, hidden, owned by owner, keeping the
// process handle and reporting no error through the shell's own UI.
func launchRequest(owner uintptr, helper, parameters string) (shellExecuteInfo, error) {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return shellExecuteInfo{}, err
	}
	file, err := windows.UTF16PtrFromString(helper)
	if err != nil {
		return shellExecuteInfo{}, err
	}
	args, err := windows.UTF16PtrFromString(parameters)
	if err != nil {
		return shellExecuteInfo{}, err
	}
	info := shellExecuteInfo{mask: seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI, window: windows.Handle(owner), verb: verb, file: file, parameters: args, show: swHide}
	info.size = uint32(unsafe.Sizeof(info))
	return info, nil
}

// Launch raises the platform's administrator prompt for the exact installed
// helper and starts it with operation and ticket (FAC-H3). owner is the
// window of the prompt the person just answered, so the platform's prompt
// opens in front of it; zero names none. It returns once the person answered
// the prompt: ErrCancelled for no. On yes, wait reports the helper's exit
// status, waiting at most the given time, and releases the process handle;
// call it once.
func Launch(owner uintptr, helper, operation, ticket string) (wait func(time.Duration) (uint32, error), err error) {
	parameters, err := launchParameters(operation, ticket)
	if err != nil {
		return nil, err
	}
	if err := trustedpath.CheckExecutable(helper, trustedpath.MachineOwners()); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err == nil {
		defer windows.CoUninitialize()
	}
	info, err := launchRequest(owner, helper, parameters)
	if err != nil {
		return nil, err
	}
	ok, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return nil, ErrCancelled
		}
		return nil, fmt.Errorf("protected: ShellExecuteExW: %w", callErr)
	}
	if info.process == 0 {
		return nil, errors.New("protected: the helper started with no process handle")
	}
	process := info.process
	return func(limit time.Duration) (uint32, error) {
		defer windows.CloseHandle(process)
		event, err := windows.WaitForSingleObject(process, uint32(limit/time.Millisecond))
		if err != nil {
			return 0, err
		}
		if event != windows.WAIT_OBJECT_0 {
			return 0, fmt.Errorf("protected: the helper is still running after %s", limit)
		}
		var code uint32
		if err := windows.GetExitCodeProcess(process, &code); err != nil {
			return 0, err
		}
		return code, nil
	}, nil
}

// UACPolicy is what the machine's policy tells the platform prompt to do,
// recorded beside each launch (FAC-H3). Read is false when the values could
// not be read.
type UACPolicy struct {
	Read                       bool
	EnableLUA                  uint32
	ConsentPromptBehaviorAdmin uint32
	PromptOnSecureDesktop      uint32
}

// ReadUACPolicy reads the machine's UAC policy values.
func ReadUACPolicy() UACPolicy {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`, registry.QUERY_VALUE)
	if err != nil {
		return UACPolicy{}
	}
	defer key.Close()
	p := UACPolicy{Read: true}
	for name, field := range map[string]*uint32{"EnableLUA": &p.EnableLUA, "ConsentPromptBehaviorAdmin": &p.ConsentPromptBehaviorAdmin, "PromptOnSecureDesktop": &p.PromptOnSecureDesktop} {
		v, _, err := key.GetIntegerValue(name)
		if err != nil {
			return UACPolicy{}
		}
		*field = uint32(v)
	}
	return p
}

// String names the policy and whether the prompt checks the person's
// presence: ConsentPromptBehaviorAdmin 0 elevates an administrator without
// asking.
func (p UACPolicy) String() string {
	if !p.Read {
		return "UAC policy unread"
	}
	s := fmt.Sprintf("EnableLUA=%d ConsentPromptBehaviorAdmin=%d PromptOnSecureDesktop=%d", p.EnableLUA, p.ConsentPromptBehaviorAdmin, p.PromptOnSecureDesktop)
	if p.EnableLUA == 0 || p.ConsentPromptBehaviorAdmin == 0 {
		s += " (the platform elevates without asking; OA's yes is the only confirmation)"
	}
	return s
}
