package supervision

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// viewOf wraps one mapped 4 KiB view, which lives outside the Go heap.
func viewOf(addr uintptr) []byte {
	return unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&addr))), 4096)
}

// marked is a small pagefile-backed section holding its own name, so a child
// can tell which object a handle value reaches.
func marked(t *testing.T, name string, inheritable bool) windows.Handle {
	t.Helper()
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{}))}
	if inheritable {
		sa.InheritHandle = 1
	}
	h, err := windows.CreateFileMapping(windows.InvalidHandle, sa, windows.PAGE_READWRITE, 0, 4096, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { windows.CloseHandle(h) })
	view, err := windows.MapViewOfFile(h, windows.FILE_MAP_WRITE, 0, 0, 4096)
	if err != nil {
		t.Fatal(err)
	}
	copy(viewOf(view), name)
	if err := windows.UnmapViewOfFile(view); err != nil {
		t.Fatal(err)
	}
	return h
}

// reaches is the child's view of one handle value: the name the section it
// maps holds, or "absent" when the value maps no section holding a name.
func reaches(value uintptr, name string) string {
	view, err := windows.MapViewOfFile(windows.Handle(value), windows.FILE_MAP_READ, 0, 0, 4096)
	if err != nil {
		return "absent"
	}
	defer windows.UnmapViewOfFile(view)
	if bytes.HasPrefix(viewOf(view), []byte(name)) {
		return name
	}
	return "other"
}

// TestInheritsExactlyTheGivenHandles: the child inherits the handles Inherit
// names and no other inheritable handle of the parent (FAC-P1). The decoy is
// inheritable too, so only the handle list keeps it out.
func TestInheritsExactlyTheGivenHandles(t *testing.T) {
	if spec := os.Getenv("OA_SUPERVISION_INHERIT"); spec != "" {
		var out []string
		for _, pair := range strings.Split(spec, ",") {
			name, value, _ := strings.Cut(pair, "=")
			v, _ := strconv.ParseUint(value, 10, 64)
			out = append(out, name+"="+reaches(uintptr(v), name))
		}
		fmt.Println(strings.Join(out, " "))
		os.Exit(0)
	}
	first, second, decoy := marked(t, "first", true), marked(t, "second", true), marked(t, "decoy", true)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	spec := fmt.Sprintf("first=%d,second=%d,decoy=%d", first, second, decoy)
	p, err := Start(context.Background(), Options{
		Executable: exe, Args: []string{"-test.run=^TestInheritsExactlyTheGivenHandles$"},
		Env:    append(os.Environ(), "OA_SUPERVISION_INHERIT="+spec),
		Stdout: w, Inherit: []uintptr{uintptr(first), uintptr(second)},
		ForceTimeout: 3 * time.Second,
	})
	w.Close()
	if err != nil {
		t.Fatal(err)
	}
	report, readErr := io.ReadAll(r)
	if code, err := p.Wait(); err != nil || code != 0 || readErr != nil {
		t.Fatalf("child: exit %d, %v, %v", code, err, readErr)
	}
	got := strings.TrimSpace(string(report))
	if got != "first=first second=second decoy=absent" {
		t.Fatalf("the child reached %q", got)
	}
}

// TestInheritRefusesWhatTheListCannotCarry: Start creates nothing for an
// Inherit list naming a handle that is not inheritable, not open, or named
// twice (FAC-P1).
func TestInheritRefusesWhatTheListCannotCarry(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	private := marked(t, "private", false)
	shared := marked(t, "shared", true)
	// A value that named a handle a moment ago: created and closed here,
	// never registered for a second close that could reach a reused value.
	closed, err := windows.CreateEvent(&windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	windows.CloseHandle(closed)
	for name, list := range map[string][]uintptr{
		"not inheritable": {uintptr(private)},
		"named twice":     {uintptr(shared), uintptr(shared)},
		"closed":          {uintptr(closed)},
		"zero":            {0},
	} {
		p, err := Start(context.Background(), Options{Executable: exe, Args: []string{"-test.run=^$"}, Inherit: list, ForceTimeout: time.Second})
		if err == nil {
			p.Stop()
			p.Wait()
		}
		// The refusal is the check's, before any job or process exists, and
		// never a CreateProcess failure that happens to follow.
		if !errors.Is(err, ErrInherit) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
