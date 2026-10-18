package supervision

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// exitChild is what the test binary does when started as a child: hold
// committed memory, then end the way OA_EXIT_CHILD names.
func exitChild() {
	switch os.Getenv("OA_EXIT_CHILD") {
	case "code":
		hold(64 << 20)
		time.Sleep(200 * time.Millisecond)
		os.Exit(7)
	case "crash":
		// The status a native fault ends a process with, without raising
		// one: the supervisor sees only the exit code.
		windows.TerminateProcess(windows.CurrentProcess(), 0xC0000005)
	case "limit":
		// The job's limit refuses this commit; the child reports that it
		// was refused by its exit code.
		if _, err := windows.VirtualAlloc(0, 512<<20, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	case "sleep":
		time.Sleep(time.Hour)
	}
	os.Exit(99)
}

var held []byte

func hold(n int) {
	held = make([]byte, n)
	for i := 0; i < n; i += 4096 {
		held[i] = 1
	}
}

func startExitChild(t *testing.T, mode string, limit uint64) *Process {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p, err := Start(context.Background(), Options{Executable: exe, Args: []string{"-test.run=TestExitIsObserved"},
		Env: append(os.Environ(), "OA_EXIT_CHILD="+mode), ForceTimeout: 5 * time.Second, ProcessMemoryLimit: limit})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// An ended tree's exit is observed with its code, run time and memory
// peak; a crash status reads as a named crash and the job reports the
// member's abnormal exit; a refused allocation is reported by the job's
// own message; a supervisor's stop is not a crash (FAC-P2).
func TestExitIsObserved(t *testing.T) {
	if os.Getenv("OA_EXIT_CHILD") != "" {
		exitChild()
	}
	t.Run("code", func(t *testing.T) {
		p := startExitChild(t, "code", 0)
		if _, ok := p.Exit(); ok {
			t.Fatal("an exit was reported before Wait returned")
		}
		code, err := p.Wait()
		if err != nil || code != 7 {
			t.Fatalf("Wait = %d, %v", code, err)
		}
		e, ok := p.Exit()
		if !ok || e.Code != 7 || e.Crashed() || e.Status() != "7" || e.Stopped {
			t.Fatalf("exit %+v ok %v", e, ok)
		}
		if e.Runtime < 200*time.Millisecond || e.Runtime > time.Minute {
			t.Fatalf("runtime %v", e.Runtime)
		}
		if e.PeakProcessMemory < 64<<20 || e.PeakJobMemory < e.PeakProcessMemory {
			t.Fatalf("peaks %d %d", e.PeakProcessMemory, e.PeakJobMemory)
		}
		if e.MemoryLimitHit || e.LimitEvidence != "job message" || len(e.AbnormalExits) != 0 {
			t.Fatalf("limit %v %q abnormal %v", e.MemoryLimitHit, e.LimitEvidence, e.AbnormalExits)
		}
	})
	t.Run("crash", func(t *testing.T) {
		p := startExitChild(t, "crash", 0)
		pid := p.PID()
		if _, err := p.Wait(); err != nil {
			t.Fatal(err)
		}
		e, ok := p.Exit()
		if !ok || !e.Crashed() || e.Status() != "0xC0000005 STATUS_ACCESS_VIOLATION" {
			t.Fatalf("exit %+v ok %v status %q", e, ok, e.Status())
		}
		if len(e.AbnormalExits) != 1 || e.AbnormalExits[0] != pid {
			t.Fatalf("abnormal exits %v, want [%d]", e.AbnormalExits, pid)
		}
	})
	t.Run("limit", func(t *testing.T) {
		p := startExitChild(t, "limit", 256<<20)
		code, err := p.Wait()
		if err != nil || code != 3 {
			t.Fatalf("Wait = %d, %v: the limit did not refuse the commit", code, err)
		}
		e, _ := p.Exit()
		if !e.MemoryLimitHit || e.LimitEvidence != "job message" || e.ProcessMemoryLimit != 256<<20 {
			t.Fatalf("exit %+v", e)
		}
	})
	t.Run("stop after its own exit", func(t *testing.T) {
		// A provider that sees the engine's output end stops the tree
		// before the parent's exit is read; the code is still the
		// engine's own.
		p := startExitChild(t, "crash", 0)
		windows.WaitForSingleObject(p.handle, windows.INFINITE)
		if err := p.Stop(); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Wait(); err != nil {
			t.Fatal(err)
		}
		if e, _ := p.Exit(); e.Stopped || !e.Crashed() {
			t.Fatalf("exit %+v", e)
		}
	})
	t.Run("stopped", func(t *testing.T) {
		p := startExitChild(t, "sleep", 0)
		time.Sleep(100 * time.Millisecond)
		if err := p.Stop(); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Wait(); err != nil {
			t.Fatal(err)
		}
		e, ok := p.Exit()
		if !ok || !e.Stopped || e.Crashed() || e.Code != stopCode {
			t.Fatalf("exit %+v ok %v", e, ok)
		}
	})
}

// Without a port the limit is inferred from the peak, and no limit is
// never hit.
func TestLimitIsInferredFromThePeakWithoutAPort(t *testing.T) {
	for _, c := range []struct {
		peak, limit uint64
		hit         bool
	}{{10 << 20, 64 << 20, false}, {63<<20 + 512<<10, 64 << 20, true}, {64 << 20, 64 << 20, true}, {1 << 30, 0, false}} {
		if got := peakNearLimit(c.peak, c.limit); got != c.hit {
			t.Fatalf("peak %s limit %s: hit %v", strconv.FormatUint(c.peak, 10), strconv.FormatUint(c.limit, 10), got)
		}
	}
}

// FAC-P2: a stop is read from the stop's own code. A parent that exits 1
// (or any code of its own) is not stopped though the stop was asked for
// before its exit time, as when it was already exiting; the stop's code
// before the stop was asked for is not a stop either.
func TestAStopIsReadFromTheStopsOwnCode(t *testing.T) {
	stopAt := time.Now()
	after, before := stopAt.Add(time.Millisecond), stopAt.Add(-time.Millisecond)
	for _, c := range []struct {
		code      uint32
		asked     bool
		exitedAt  time.Time
		stopped   bool
		situation string
	}{
		{stopCode, true, after, true, "the stop ended it"},
		{1, true, after, false, "it exited 1 by itself while the stop came"},
		{7, true, after, false, "it exited 7 by itself while the stop came"},
		{stopCode, true, before, false, "it ended before the stop was asked for"},
		{stopCode, false, after, false, "no stop was asked for"},
	} {
		if got := stoppedBy(c.code, c.asked, c.exitedAt, stopAt); got != c.stopped {
			t.Fatalf("%s: stopped %v", c.situation, got)
		}
	}
	if stopCode == 1 || stopCode >= 0xC0000000 || stopCode < 0x10000 {
		t.Fatalf("stop code %#x is an ordinary exit code or an exception status", stopCode)
	}
}

// FAC-P2: every Wait records exactly one exit; a parent whose end cannot be
// read is recorded as unknown, with the reason.
func TestAnUnreadableExitIsRecordedAsUnknown(t *testing.T) {
	p := &Process{waitDone: make(chan struct{}), memoryLimit: 1 << 30}
	if _, err := p.Wait(); err == nil {
		t.Fatal("waiting on no process succeeded")
	}
	e, ok := p.Exit()
	if !ok || e.Unknown == "" || e.Crashed() || e.Stopped || e.ProcessMemoryLimit != 1<<30 {
		t.Fatalf("exit %+v ok %v", e, ok)
	}
}
