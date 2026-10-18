package supervision

import (
	"fmt"
	"time"
)

// Exit is what the supervisor observed of a tree that ended (FAC-P2): the
// parent's exit code, its run time, the job's memory peaks and whether a
// memory limit refused an allocation. Process.Exit reports it once Wait
// has returned.
type Exit struct {
	// Code is the parent's exit code. On Windows a crash ends a process
	// with its exception status, for example 0xC0000005 for an access
	// violation.
	Code uint32
	// Runtime is the parent's life, from its creation to its exit.
	Runtime time.Duration
	// Stopped is true when the supervisor's Stop ended the parent, and Code
	// is the termination's code. A parent already exiting with its own code
	// when the stop came, or one that ended before it, is not stopped.
	Stopped bool
	// PeakProcessMemory is the largest commit any process of the tree
	// held; PeakJobMemory the largest the whole tree held at once.
	PeakProcessMemory, PeakJobMemory uint64
	// ProcessMemoryLimit is the per-process limit the job carried, zero
	// for none.
	ProcessMemoryLimit uint64
	// MemoryLimitHit is true when a process of the tree was refused memory
	// by the job's per-process or job-wide limit.
	MemoryLimitHit bool
	// LimitEvidence says how MemoryLimitHit was decided: "job message"
	// when the job's completion port was read, "peak" when it is inferred
	// from the peak against the limit because no port could be associated.
	LimitEvidence string
	// AbnormalExits are the process ids of the tree's members the job
	// reported as ending abnormally, a crash's exception status among them.
	AbnormalExits []int
	// Unknown, when not empty, is why the parent's end could not be read
	// (waiting on it or reading its exit code failed); every other field
	// but ProcessMemoryLimit is then zero.
	Unknown string
}

// peakNearLimit is the inference used when no job message can be read: a
// peak within 1/64 of the limit counts as the limit hit. A refused large
// allocation can leave the peak well below the limit, so a miss here is
// not proof the limit was never reached.
func peakNearLimit(peak, limit uint64) bool {
	return limit > 0 && peak+limit/64 >= limit
}

// Crashed reports whether Code is an exception status a process ends with
// when an exception is not handled: severity error (0xC0000000 and above)
// or a breakpoint. The code of a supervisor's stop is not a crash.
func (e Exit) Crashed() bool {
	if e.Stopped || e.Unknown != "" {
		return false
	}
	return e.Code >= 0xC0000000 || e.Code == 0x80000003
}

// statusNames are the exception statuses an engine is known to end with.
var statusNames = map[uint32]string{
	0x80000003: "STATUS_BREAKPOINT",
	0xC0000005: "STATUS_ACCESS_VIOLATION",
	0xC0000017: "STATUS_NO_MEMORY",
	0xC000001D: "STATUS_ILLEGAL_INSTRUCTION",
	0xC00000FD: "STATUS_STACK_OVERFLOW",
	0xC0000135: "STATUS_DLL_NOT_FOUND",
	0xC0000139: "STATUS_ENTRYPOINT_NOT_FOUND",
	0xC000013A: "STATUS_CONTROL_C_EXIT",
	0xC0000142: "STATUS_DLL_INIT_FAILED",
	0xC0000374: "STATUS_HEAP_CORRUPTION",
	0xC0000409: "STATUS_STACK_BUFFER_OVERRUN",
	0xC0000420: "STATUS_ASSERTION_FAILURE",
}

// Status is Code as an exception status for a crash ("0xC0000005
// STATUS_ACCESS_VIOLATION", the hex alone for a status not named here)
// and as a decimal number otherwise.
func (e Exit) Status() string {
	if !e.Crashed() {
		return fmt.Sprintf("%d", e.Code)
	}
	if name, ok := statusNames[e.Code]; ok {
		return fmt.Sprintf("0x%08X %s", e.Code, name)
	}
	return fmt.Sprintf("0x%08X", e.Code)
}
