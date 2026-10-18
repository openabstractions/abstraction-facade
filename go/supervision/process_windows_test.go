package supervision

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestInteractiveConsoleIsExplicitAndRetainsAtomicAttributes(t *testing.T) {
	if flags := creationFlags(Options{}); flags&windows.CREATE_NO_WINDOW == 0 || flags&windows.EXTENDED_STARTUPINFO_PRESENT == 0 {
		t.Fatalf("default flags = %#x", flags)
	}
	if flags := creationFlags(Options{InheritConsole: true}); flags&windows.CREATE_NO_WINDOW != 0 || flags&windows.CREATE_NEW_CONSOLE != 0 || flags&windows.EXTENDED_STARTUPINFO_PRESENT == 0 {
		t.Fatalf("interactive flags = %#x", flags)
	}
}

// cmd.exe parses its /c command itself. The quoted batch path and arguments
// must reach it without CommandLineToArgvW backslash escaping.
func TestBatchCommandLinePreservesQuotedPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "batch folder")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	batch := filepath.Join(root, "fixture launcher.bat")
	if err := os.WriteFile(batch, []byte("@echo off\r\necho first=[%~1]\r\necho second=[%~2]\r\nexit /b 7\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(root, "model folder", "model weights.safetensors")
	output, err := os.Create(filepath.Join(root, "output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	windowsDir, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		t.Fatal(err)
	}
	cmd := filepath.Join(windowsDir, "System32", "cmd.exe")
	command := fmt.Sprintf(`""%s" "alpha beta" "%s""`, batch, model)
	p, err := Start(context.Background(), Options{Executable: cmd, WindowsCmdLine: fmt.Sprintf(`"%s" /d /s /c %s`, cmd, command),
		Dir: root, Stdout: output, Stderr: output, ForceTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { p.Stop(); p.Wait() }()
	code, err := p.Wait()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("first=[alpha beta]\r\nsecond=[%s]\r\n", model)
	if code != 7 || string(data) != want {
		t.Fatalf("batch exit/output: code=%d want=7 output=%q want=%q", code, data, want)
	}
	if exit, known := p.Exit(); !known || exit.Code != 7 || exit.Stopped || exit.Unknown != "" {
		t.Fatalf("batch tree exit not confirmed: known=%v exit=%+v", known, exit)
	}
}

func TestWindowsCommandLineRejectsArguments(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p, err := Start(context.Background(), Options{Executable: exe, Args: []string{"unused"}, WindowsCmdLine: `"unused.exe"`, ForceTimeout: time.Second})
	if p != nil || err == nil || err.Error() != "supervision: Args and WindowsCmdLine are mutually exclusive" {
		t.Fatalf("ambiguous command line was not refused before start: process=%v error=%v", p, err)
	}
}

func TestDefaultArgumentsPreserveWindowsQuoting(t *testing.T) {
	want := []string{"alpha beta", `quote"value`, `C:\model folder\`, ""}
	if os.Getenv("OA_SUPERVISION_ARGV_CHILD") == "1" {
		if err := json.NewEncoder(os.Stdout).Encode(os.Args[len(os.Args)-len(want):]); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(filepath.Join(t.TempDir(), "argv.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	args := append([]string{"-test.run=^TestDefaultArgumentsPreserveWindowsQuoting$", "--"}, want...)
	p, err := Start(context.Background(), Options{Executable: exe, Args: args, Stdout: output, Stderr: output,
		Env: append(os.Environ(), "OA_SUPERVISION_ARGV_CHILD=1"), ForceTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { p.Stop(); p.Wait() }()
	code, err := p.Wait()
	if err != nil || code != 0 {
		t.Fatalf("argv child: code=%d error=%v", code, err)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("argv output: %v: %q", err, data)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default arguments: got=%q want=%q", got, want)
	}
}

// A configured process memory limit is set on the engine's job, and no
// limit is set without one.
func TestProcessMemoryLimitIsSetOnTheJob(t *testing.T) {
	if os.Getenv("OA_SUPERVISION_CHILD") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []uint64{0, 3 << 30} {
		p, err := Start(context.Background(), Options{Executable: exe, Args: []string{"-test.run=TestProcessMemoryLimitIsSetOnTheJob"},
			Env: append(os.Environ(), "OA_SUPERVISION_CHILD=1"), ForceTimeout: 3 * time.Second, ProcessMemoryLimit: limit})
		if err != nil {
			t.Fatal(err)
		}
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		queryErr := queryJob(p.job, windows.JobObjectExtendedLimitInformation, unsafe.Pointer(&info), uint32(unsafe.Sizeof(info)))
		p.Stop()
		p.Wait()
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		set := info.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY != 0
		if set != (limit > 0) || uint64(info.ProcessMemoryLimit) != limit {
			t.Fatalf("limit %d: flags %#x, limit %d", limit, info.BasicLimitInformation.LimitFlags, info.ProcessMemoryLimit)
		}
		if info.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE == 0 {
			t.Fatal("kill on close was lost")
		}
	}
}

func TestContainedIdentityAndStop(t *testing.T) {
	if os.Getenv("OA_SUPERVISION_CHILD") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p, err := Start(context.Background(), Options{Executable: exe, Args: []string{"-test.run=TestContainedIdentityAndStop"}, Env: append(os.Environ(), "OA_SUPERVISION_CHILD=1"), ForceTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { p.Stop(); p.Wait() }()
	if p.PID() <= 0 || p.StartTime().IsZero() {
		t.Fatal("missing exact process identity")
	}
	ok, err := p.Contains(p.PID(), p.StartTime())
	if err != nil || !ok {
		t.Fatalf("leader not in owned job: %v %v", ok, err)
	}
	ok, err = p.Contains(p.PID(), p.StartTime().Add(time.Nanosecond))
	if err != nil || ok {
		t.Fatalf("recycled identity accepted: %v %v", ok, err)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
}
