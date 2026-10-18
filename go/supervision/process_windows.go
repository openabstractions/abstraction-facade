package supervision

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// PROC_THREAD_ATTRIBUTE_JOB_LIST is documented for Windows10/Server2016 onward.
// x/sys does not currently name it. Assignment is part of CreateProcess, before
// user code executes; unsupported hosts fail without a suspended-child fallback.
const attributeJobList = 0x0002000d

// PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES (winbase.h) starts the child
// inside the AppContainer its SECURITY_CAPABILITIES names (FAC-P3).
const attributeSecurityCapabilities = 0x00020009

// securityCapabilities is SECURITY_CAPABILITIES: the AppContainer's SID and
// its capabilities, here ContainerCapabilitySID alone.
type securityCapabilities struct {
	AppContainerSid *windows.SID
	Capabilities    *windows.SIDAndAttributes
	CapabilityCount uint32
	Reserved        uint32
}

// containerSID parses an AppContainer's SID: S-1-15-2 followed by seven
// sub-authorities. ALL APPLICATION PACKAGES (S-1-15-2-1) and ALL RESTRICTED
// APPLICATION PACKAGES (S-1-15-2-2) are groups, never a container.
func containerSID(s string, env []string) (*windows.SID, error) {
	sid, err := windows.StringToSid(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is no SID: %v", ErrContainer, s, err)
	}
	// Judged on the canonical text, S-1-15-2 and seven numbers: the SID lives
	// outside Go's heap, where x/sys's accessors trip the race detector's
	// pointer checks.
	if parts := strings.Split(sid.String(), "-"); len(parts) != 11 || parts[2] != "15" || parts[3] != "2" {
		return nil, fmt.Errorf("%w: %s is not one AppContainer's SID", ErrContainer, s)
	}
	if !slices.ContainsFunc(env, func(item string) bool { return strings.HasPrefix(item, "LOCALAPPDATA=") }) {
		return nil, fmt.Errorf("%w: the environment names no LOCALAPPDATA, which a contained start needs", ErrContainer)
	}
	return sid, nil
}

var queryJobProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("QueryInformationJobObject")
var setJobProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetInformationJobObject")
var isProcessInJobProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
var getHandleInformationProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetHandleInformation")
var getQueuedCompletionStatusProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetQueuedCompletionStatus")

// stopCode is the exit code Stop ends every process of the tree with:
// "OAST" in ASCII, outside the exception statuses (0xC0000000 and above)
// and outside the small codes programs exit with, so an engine that exits
// 1 by itself is never read as stopped.
const stopCode = 0x4F415354

// The job notifications this package reads from a job's completion port
// (winnt.h JOB_OBJECT_MSG_*). For each, the port's byte count is the
// message and the overlapped pointer carries a process id.
const (
	jobMsgActiveProcessZero   = 4
	jobMsgAbnormalExitProcess = 8
	jobMsgProcessMemoryLimit  = 9
	jobMsgJobMemoryLimit      = 10
)

// jobPortInformation is JOBOBJECT_ASSOCIATE_COMPLETION_PORT; x/sys names
// the class only.
type jobPortInformation struct {
	CompletionKey  uintptr
	CompletionPort windows.Handle
}

// jobMessage reads one notification from port within timeout. The
// overlapped value is a process id, never a pointer, so it is read into a
// uintptr rather than x/sys's **Overlapped, which the garbage collector
// would scan as a pointer.
func jobMessage(port windows.Handle, timeout uint32) (message uint32, pid uintptr, ok bool) {
	var key uintptr
	result, _, _ := getQueuedCompletionStatusProc.Call(uintptr(port), uintptr(unsafe.Pointer(&message)),
		uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&pid)), uintptr(timeout))
	return message, pid, result != 0
}

// handleFlags reads a handle's HANDLE_FLAG_* bits; x/sys names only the setter.
func handleFlags(h windows.Handle) (uint32, error) {
	var flags uint32
	result, _, err := getHandleInformationProc.Call(uintptr(h), uintptr(unsafe.Pointer(&flags)))
	if result == 0 {
		return 0, err
	}
	return flags, nil
}

// Keep the pointer typed until LazyProc.Call's uintptr-escape boundary. The
// x/sys uintptr buffer signature can leave a movable Go stack address exposed.
func queryJob(job windows.Handle, class int32, data unsafe.Pointer, size uint32) error {
	result, _, err := queryJobProc.Call(uintptr(job), uintptr(class), uintptr(data), uintptr(size), 0)
	if result == 0 {
		return err
	}
	return nil
}

func setJob(job windows.Handle, class int32, data unsafe.Pointer, size uint32) error {
	result, _, err := setJobProc.Call(uintptr(job), uintptr(class), uintptr(data), uintptr(size))
	if result == 0 {
		return err
	}
	return nil
}

type Process struct {
	handle, job         windows.Handle
	container           string
	id                  int
	started             time.Time
	budget              time.Duration
	mu                  sync.Mutex
	waitOnce            sync.Once
	waitDone            chan struct{}
	exitCode            int
	waitErr             error
	closed              bool
	terminating         bool
	members             []windows.Handle
	terminationErr      error
	terminationDeadline time.Time
	stopAt              time.Time
	capturedTotal       uint32
	// port receives the job's notifications; zero when it could not be
	// associated. memoryLimit is the job's per-process limit.
	port        windows.Handle
	memoryLimit uint64
	// exit is what Wait observed of the ended tree; exitKnown is false
	// until Wait recorded it, known or not (FAC-P2).
	exit      Exit
	exitKnown bool
}

// Exit is what the supervisor observed once Wait has returned, and false
// before then. Every Wait records exactly one exit: when the parent's end
// could not be read, the record says so in Exit.Unknown (FAC-P2).
func (p *Process) Exit() (Exit, bool) {
	select {
	case <-p.waitDone:
	default:
		return Exit{}, false
	}
	return p.exit, p.exitKnown
}

func (p *Process) PID() int             { return p.id }
func (p *Process) StartTime() time.Time { return p.started }

// Contains verifies that the exact PID and creation-time identity is a member
// of this still-owned job. It does not infer ownership from a parent PID.
func (p *Process) Contains(pid int, started time.Time) (bool, error) {
	if pid <= 0 || started.IsZero() {
		return false, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false, errors.New("supervision: job already closed")
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(h)
	var creation, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exited, &kernel, &user); err != nil {
		return false, err
	}
	if !time.Unix(0, creation.Nanoseconds()).Equal(started) {
		return false, nil
	}
	var inJob uint32
	result, _, callErr := isProcessInJobProc.Call(uintptr(h), uintptr(p.job), uintptr(unsafe.Pointer(&inJob)))
	if result == 0 {
		return false, callErr
	}
	return inJob != 0, nil
}

// Lend duplicates h, a handle of this process, into the running child with
// exactly access, and returns the handle's value in the child (FAC-P4). The
// child's copy carries no more than access, whatever h itself may do, and is
// the child's to close; it ends with the child at the latest. The caller
// keeps h. A child whose tree has ended is refused.
func (p *Process) Lend(h uintptr, access uint32) (uintptr, error) {
	return p.LendTo(h, access, p.id)
}

// LendTo duplicates into one live member of this process's owned job. A
// launcher may create the engine that receives a protocol message; its PID
// is accepted only after the opened process handle proves job membership.
// The handle stays open through duplication, so PID reuse cannot redirect it.
func (p *Process) LendTo(h uintptr, access uint32, pid int) (uintptr, error) {
	if h == 0 || windows.Handle(h) == windows.InvalidHandle || access == 0 {
		return 0, errors.New("supervision: lend needs a handle and the access the child gets")
	}
	if pid <= 0 {
		return 0, errors.New("supervision: lend needs a process id")
	}
	// The lock keeps wait from closing the child's handle mid-duplication:
	// closeJob takes it before the handle is closed.
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, errors.New("supervision: the child has ended")
	}
	process, err := windows.OpenProcess(windows.PROCESS_DUP_HANDLE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return 0, fmt.Errorf("supervision: open the engine process %d for lending: %w", pid, err)
	}
	defer windows.CloseHandle(process)
	var inJob uint32
	result, _, callErr := isProcessInJobProc.Call(uintptr(process), uintptr(p.job), uintptr(unsafe.Pointer(&inJob)))
	if result == 0 {
		return 0, fmt.Errorf("supervision: prove engine process %d is in the owned job: %w", pid, callErr)
	}
	if inJob == 0 {
		return 0, fmt.Errorf("supervision: process %d is outside the owned job", pid)
	}
	status, err := windows.WaitForSingleObject(process, 0)
	if err != nil {
		return 0, fmt.Errorf("supervision: check engine process %d: %w", pid, err)
	}
	if status != uint32(windows.WAIT_TIMEOUT) {
		return 0, fmt.Errorf("supervision: engine process %d has ended", pid)
	}
	container, err := processContainerSID(process)
	if err != nil {
		return 0, fmt.Errorf("supervision: prove engine process %d's container: %w", pid, err)
	}
	if container != p.container {
		return 0, fmt.Errorf("supervision: process %d has another AppContainer identity", pid)
	}
	var target windows.Handle
	if err := windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(h), process, &target, access, false, 0); err != nil {
		return 0, fmt.Errorf("supervision: lend a handle to the child: %w", err)
	}
	return uintptr(target), nil
}

// processContainerSID reads the token of the same live process handle that
// LendTo uses for its job check and DuplicateHandle destination.
func processContainerSID(process windows.Handle) (string, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return "", err
	}
	defer token.Close()
	const tokenAppContainerSid = 31
	var size uint32
	_ = windows.GetTokenInformation(token, tokenAppContainerSid, nil, 0, &size)
	if size < uint32(unsafe.Sizeof(uintptr(0))) {
		return "", errors.New("the AppContainer token field is absent")
	}
	buf := make([]byte, size)
	if err := windows.GetTokenInformation(token, tokenAppContainerSid, &buf[0], size, &size); err != nil {
		return "", err
	}
	sid := *(**windows.SID)(unsafe.Pointer(&buf[0]))
	if sid == nil {
		return "", nil
	}
	return sid.String(), nil
}

func (p *Process) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	if !p.terminating {
		p.terminating = true
		p.stopAt = time.Now()
		p.terminationDeadline = p.stopAt.Add(p.budget)
		members, err := p.memberHandles()
		p.members = members
		p.terminationErr = errors.Join(err, windows.TerminateJobObject(p.job, stopCode))
	}
	return p.terminationErr
}

// Capture waitable handles before requesting termination: the active-process
// count may reach zero before process objects become signaled.
func (p *Process) memberHandles() ([]windows.Handle, error) {
	var result []windows.Handle
	var before jobAccounting
	if err := queryJob(p.job, windows.JobObjectBasicAccountingInformation, unsafe.Pointer(&before), uint32(unsafe.Sizeof(before))); err != nil {
		return nil, err
	}
	p.capturedTotal = before.TotalProcesses
	for capacity := 16; capacity <= 1<<20; capacity *= 2 {
		if time.Now().After(p.terminationDeadline) {
			return result, errors.New("supervision: member observation deadline")
		}
		data := make([]uintptr, capacity+2)
		err := queryJob(p.job, windows.JobObjectBasicProcessIdList, unsafe.Pointer(&data[0]), uint32(8+capacity*int(unsafe.Sizeof(uintptr(0)))))
		if errors.Is(err, windows.ERROR_MORE_DATA) {
			continue
		}
		if err != nil {
			return nil, err
		}
		counts := (*[2]uint32)(unsafe.Pointer(&data[0]))
		ids := unsafe.Slice((*uintptr)(unsafe.Add(unsafe.Pointer(&data[0]), 8)), int(counts[1]))
		for _, id := range ids {
			if time.Now().After(p.terminationDeadline) {
				return result, errors.New("supervision: member observation deadline")
			}
			h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(id))
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				continue
			} // Already exited and released.
			if err != nil {
				return result, err
			}
			result = append(result, h)
		}
		return result, nil
	}
	return nil, errors.New("supervision: job process list exceeds observation bound")
}
func (p *Process) closeJob() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		windows.CloseHandle(p.job)
		if p.port != 0 {
			windows.CloseHandle(p.port)
		}
		for _, h := range p.members {
			windows.CloseHandle(h)
		}
		p.closed = true
	}
}

// Native JOBOBJECT_BASIC_ACCOUNTING_INFORMATION has four LARGE_INTEGERs and
// four DWORDs. Querying ActiveProcesses observes descendant termination.
type jobAccounting struct {
	UserTime, KernelTime, UserPeriod, KernelPeriod                   int64
	PageFaults, TotalProcesses, ActiveProcesses, TerminatedProcesses uint32
}

// Wait returns only after the parent and all observed job members have exited.
// A nonzero exit code is returned separately from containment errors.
// Concurrent callers observe the same result.
func (p *Process) Wait() (int, error) {
	p.waitOnce.Do(func() {
		p.exitCode, p.waitErr = p.wait()
		close(p.waitDone)
	})
	<-p.waitDone
	return p.exitCode, p.waitErr
}

func (p *Process) wait() (int, error) {
	defer windows.CloseHandle(p.handle)
	defer p.closeJob()
	code, exited, stopped, err := p.waitTree()
	if exited {
		p.observe(code, stopped)
	} else {
		// The parent's end could not be read: still one record, saying so.
		p.exit, p.exitKnown = Exit{Unknown: err.Error(), ProcessMemoryLimit: p.memoryLimit}, true
	}
	return int(code), err
}

// stoppedBy decides whether Stop ended the parent: a stop was asked for, the
// parent ended with stopCode, and it ended no earlier than the stop. A
// parent with its own code was already exiting when the stop came.
func stoppedBy(code uint32, stopAsked bool, exitedAt, stopAt time.Time) bool {
	return stopAsked && code == stopCode && !exitedAt.Before(stopAt)
}

// observe records what the job saw of the ended tree (FAC-P2): the
// parent's run time from its process times, the job's memory peaks, and
// the job's notifications, read until the tree's last process is reported
// gone or a second passes. Without a port, a limit counts as hit when the
// peak came within 1/64 of it.
func (p *Process) observe(code uint32, stopped bool) {
	e := Exit{Code: code, ProcessMemoryLimit: p.memoryLimit}
	// Without the exit time, a stop asked for counts when the code is the
	// stop's own.
	exitedAt := p.stopAt
	var creation, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(p.handle, &creation, &exited, &kernel, &user) == nil {
		e.Runtime = time.Duration(exited.Nanoseconds() - creation.Nanoseconds())
		exitedAt = time.Unix(0, exited.Nanoseconds())
	}
	e.Stopped = stoppedBy(code, stopped, exitedAt, p.stopAt)
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if queryJob(p.job, windows.JobObjectExtendedLimitInformation, unsafe.Pointer(&info), uint32(unsafe.Sizeof(info))) == nil {
		e.PeakProcessMemory, e.PeakJobMemory = uint64(info.PeakProcessMemoryUsed), uint64(info.PeakJobMemoryUsed)
	}
	switch {
	case p.port != 0:
		e.LimitEvidence = "job message"
		deadline := time.Now().Add(time.Second)
		for zero := false; ; {
			var wait uint32
			if remaining := time.Until(deadline); !zero && remaining > 0 {
				wait = uint32(remaining / time.Millisecond)
			}
			message, pid, ok := jobMessage(p.port, wait)
			if !ok {
				break
			}
			switch message {
			case jobMsgAbnormalExitProcess:
				e.AbnormalExits = append(e.AbnormalExits, int(pid))
			case jobMsgProcessMemoryLimit, jobMsgJobMemoryLimit:
				e.MemoryLimitHit = true
			case jobMsgActiveProcessZero:
				// Every earlier notification is already queued; read
				// what is left without waiting.
				zero = true
			}
		}
	case p.memoryLimit > 0:
		e.LimitEvidence = "peak"
		e.MemoryLimitHit = peakNearLimit(e.PeakProcessMemory, p.memoryLimit)
	}
	p.exit, p.exitKnown = e, true
}

// waitTree waits for the parent, then for every member of its tree; exited
// is true once the parent's exit code was read, and stopped when Stop came
// before the parent's exit.
func (p *Process) waitTree() (code uint32, exited, stopped bool, _ error) {
	if _, err := windows.WaitForSingleObject(p.handle, windows.INFINITE); err != nil {
		return 0, false, false, err
	}
	if err := windows.GetExitCodeProcess(p.handle, &code); err != nil {
		return 0, false, false, err
	}
	p.mu.Lock()
	stopped = p.terminating
	p.mu.Unlock()
	exited = true
	if err := p.Stop(); err != nil {
		return code, true, stopped, fmt.Errorf("supervision: terminate remaining descendants: %w", err)
	}
	deadline := p.terminationDeadline
	for {
		var accounting jobAccounting
		if err := queryJob(p.job, windows.JobObjectBasicAccountingInformation, unsafe.Pointer(&accounting), uint32(unsafe.Sizeof(accounting))); err != nil {
			return code, true, stopped, err
		}
		if accounting.TotalProcesses != p.capturedTotal {
			return code, true, stopped, fmt.Errorf("supervision: job membership changed during termination (%d -> %d); complete signaling unproven", p.capturedTotal, accounting.TotalProcesses)
		}
		if accounting.ActiveProcesses == 0 {
			break
		}
		if !time.Now().Before(deadline) {
			return code, true, stopped, fmt.Errorf("supervision: descendants remain after force deadline")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, member := range p.members {
		remaining := time.Until(deadline)
		if remaining < 0 {
			return code, true, stopped, errors.New("supervision: process signals exceeded force deadline")
		}
		ms := uint32((remaining + time.Millisecond - 1) / time.Millisecond)
		status, err := windows.WaitForSingleObject(member, ms)
		if err != nil {
			return code, true, stopped, err
		}
		if status != windows.WAIT_OBJECT_0 {
			return code, true, stopped, errors.New("supervision: process termination not signaled within force deadline")
		}
	}
	return code, true, stopped, nil
}

// checkInherit refuses an Inherit list the handle list cannot carry as given:
// a zero or pseudo handle, one named twice, one that is not open in this
// process, and one not marked inheritable. Start never changes a caller's
// handle, so marking one inheritable is the caller's act (FAC-P1).
func checkInherit(inherit []uintptr) error {
	seen := make(map[uintptr]bool, len(inherit))
	for _, h := range inherit {
		if h == 0 || windows.Handle(h) == windows.InvalidHandle {
			return fmt.Errorf("%w: %#x is not a handle", ErrInherit, h)
		}
		if seen[h] {
			return fmt.Errorf("%w: %#x named twice", ErrInherit, h)
		}
		seen[h] = true
		flags, err := handleFlags(windows.Handle(h))
		if err != nil {
			return fmt.Errorf("%w: %#x: %w", ErrInherit, h, err)
		}
		if flags&windows.HANDLE_FLAG_INHERIT == 0 {
			return fmt.Errorf("%w: %#x is not inheritable", ErrInherit, h)
		}
	}
	return nil
}

func Start(ctx context.Context, o Options) (_ *Process, err error) {
	if !filepath.IsAbs(o.Executable) || strings.IndexByte(o.Executable, 0) >= 0 {
		return nil, errors.New("supervision: executable must be an absolute path without NUL")
	}
	if o.WindowsCmdLine != "" && len(o.Args) != 0 {
		return nil, errors.New("supervision: Args and WindowsCmdLine are mutually exclusive")
	}
	if strings.IndexByte(o.WindowsCmdLine, 0) >= 0 {
		return nil, errors.New("supervision: NUL in Windows command line")
	}
	if o.ForceTimeout <= 0 {
		return nil, errors.New("supervision: positive force timeout required")
	}
	if strings.IndexByte(o.Dir, 0) >= 0 {
		return nil, errors.New("supervision: invalid working directory")
	}
	if o.Dir != "" && !filepath.IsAbs(o.Dir) {
		return nil, errors.New("supervision: working directory must be absolute")
	}
	for _, arg := range o.Args {
		if strings.IndexByte(arg, 0) >= 0 {
			return nil, errors.New("supervision: NUL in argument")
		}
	}
	for _, env := range o.Env {
		if strings.IndexByte(env, 0) >= 0 {
			return nil, errors.New("supervision: NUL in environment")
		}
	}
	if err := checkInherit(o.Inherit); err != nil {
		return nil, err
	}
	var container *windows.SID
	if o.Container != "" {
		var err error
		if container, err = containerSID(o.Container, o.Env); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var opened []*os.File
	defer func() {
		for _, f := range opened {
			f.Close()
		}
	}()
	standard := [3]*os.File{o.Stdin, o.Stdout, o.Stderr}
	for i, f := range standard {
		if f == nil {
			mode := os.O_RDONLY
			if i != 0 {
				mode = os.O_WRONLY
			}
			var openErr error
			f, openErr = os.OpenFile(os.DevNull, mode, 0)
			if openErr != nil {
				return nil, openErr
			}
			opened = append(opened, f)
			standard[i] = f
		}
		if _, statErr := f.Stat(); statErr != nil {
			return nil, fmt.Errorf("supervision: standard handle: %w", statErr)
		}
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			windows.CloseHandle(job)
		}
	}()
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if o.ProcessMemoryLimit > 0 {
		limits.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY
		limits.ProcessMemoryLimit = uintptr(o.ProcessMemoryLimit)
	}
	if err = setJob(job, windows.JobObjectExtendedLimitInformation, unsafe.Pointer(&limits), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, err
	}
	// The job's notifications go to a port of its own before any process
	// joins it, so none is missed: a refused allocation and an abnormal
	// member exit among them (FAC-P2). A port that cannot be associated
	// leaves the limit inferred from the peak.
	port, portErr := windows.CreateIoCompletionPort(windows.InvalidHandle, 0, 0, 1)
	if portErr == nil {
		association := jobPortInformation{CompletionPort: port}
		if setJob(job, windows.JobObjectAssociateCompletionPortInformation, unsafe.Pointer(&association), uint32(unsafe.Sizeof(association))) != nil {
			windows.CloseHandle(port)
			port = 0
		}
	} else {
		port = 0
	}
	defer func() {
		if !success && port != 0 {
			windows.CloseHandle(port)
		}
	}()
	// Duplicate all standard handles; never change inheritance on caller-owned files.
	handles := make([]windows.Handle, 3)
	defer func() {
		for _, h := range handles {
			if h != 0 {
				windows.CloseHandle(h)
			}
		}
	}()
	for i, f := range standard {
		if err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(f.Fd()), windows.CurrentProcess(), &handles[i], 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			return nil, err
		}
	}
	// The handle list names everything the child inherits: its standard
	// handles and the caller's Inherit handles, which stay the caller's.
	list := slices.Clone(handles)
	for _, h := range o.Inherit {
		list = append(list, windows.Handle(h))
	}
	attrs, err := windows.NewProcThreadAttributeList(3)
	if err != nil {
		return nil, err
	}
	defer attrs.Delete()
	if err = attrs.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&list[0]), uintptr(len(list))*unsafe.Sizeof(list[0])); err != nil {
		return nil, err
	}
	if err = attrs.Update(attributeJobList, unsafe.Pointer(&job), unsafe.Sizeof(job)); err != nil {
		return nil, fmt.Errorf("supervision: atomic job assignment: %w", err)
	}
	// The AppContainer is part of the same CreateProcess: the child's first
	// instruction already runs contained, with OA's one capability and no
	// other (FAC-P3).
	var capabilities securityCapabilities
	var granted []windows.SIDAndAttributes
	if container != nil {
		capability, sidErr := windows.StringToSid(ContainerCapabilitySID)
		if sidErr != nil {
			return nil, fmt.Errorf("%w: the OA capability: %v", ErrContainer, sidErr)
		}
		granted = []windows.SIDAndAttributes{{Sid: capability, Attributes: windows.SE_GROUP_ENABLED}}
		capabilities.AppContainerSid = container
		capabilities.Capabilities, capabilities.CapabilityCount = &granted[0], uint32(len(granted))
		if err = attrs.Update(attributeSecurityCapabilities, unsafe.Pointer(&capabilities), unsafe.Sizeof(capabilities)); err != nil {
			return nil, fmt.Errorf("%w: the AppContainer attribute: %v", ErrContainer, err)
		}
	}
	si := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESTDHANDLES, StdInput: handles[0], StdOutput: handles[1], StdErr: handles[2]}, ProcThreadAttributeList: attrs.List()}
	exe, err := windows.UTF16PtrFromString(o.Executable)
	if err != nil {
		return nil, err
	}
	commandLine := o.WindowsCmdLine
	if commandLine == "" {
		args := append([]string{o.Executable}, o.Args...)
		commandLine = windows.ComposeCommandLine(args)
	}
	command, err := windows.UTF16PtrFromString(commandLine)
	if err != nil {
		return nil, err
	}
	var dir *uint16
	if o.Dir != "" {
		dir, err = windows.UTF16PtrFromString(o.Dir)
		if err != nil {
			return nil, err
		}
	}
	var envPtr *uint16
	flags := creationFlags(o)
	var envBlock []uint16
	if o.Env != nil {
		for _, item := range o.Env {
			part, encodeErr := windows.UTF16FromString(item)
			if encodeErr != nil {
				return nil, encodeErr
			}
			envBlock = append(envBlock, part...)
		}
		envBlock = append(envBlock, 0)
		if len(envBlock) == 1 {
			envBlock = append(envBlock, 0)
		}
		envPtr = &envBlock[0]
		flags |= windows.CREATE_UNICODE_ENVIRONMENT
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	var pi windows.ProcessInformation
	err = windows.CreateProcess(exe, command, nil, nil, true, flags, envPtr, dir, &si.StartupInfo, &pi)
	runtime.KeepAlive(handles)
	runtime.KeepAlive(list)
	runtime.KeepAlive(job)
	runtime.KeepAlive(envBlock)
	runtime.KeepAlive(capabilities)
	runtime.KeepAlive(granted)
	runtime.KeepAlive(container)
	if err != nil {
		if container != nil {
			return nil, fmt.Errorf("%w: CreateProcess in the AppContainer %s: %v", ErrContainer, o.Container, err)
		}
		return nil, fmt.Errorf("supervision: atomic contained creation: %w", err)
	}
	windows.CloseHandle(pi.Thread)
	var creation, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(pi.Process, &creation, &exited, &kernel, &user); err != nil {
		windows.TerminateJobObject(job, 1)
		windows.CloseHandle(pi.Process)
		return nil, fmt.Errorf("supervision: process creation time: %w", err)
	}
	success = true
	containerName := ""
	if container != nil {
		containerName = container.String()
	}
	return &Process{handle: pi.Process, job: job, container: containerName, id: int(pi.ProcessId), started: time.Unix(0, creation.Nanoseconds()), budget: o.ForceTimeout,
		waitDone: make(chan struct{}), port: port, memoryLimit: o.ProcessMemoryLimit}, nil
}

func creationFlags(o Options) uint32 {
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT)
	if !o.InheritConsole {
		flags |= windows.CREATE_NO_WINDOW
	}
	return flags
}
