package protected

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	"golang.org/x/sys/windows"
)

var endpointSerial atomic.Int64

// TestHelperServerExpectation is ID-H2 over a real pipe: the helper sends
// nothing to a server that is not the expected program, then completes the
// whole exchange with the one that is. This test process plays both ends;
// its stand-in Verify admits it, since it is not elevated.
func TestHelperServerExpectation(t *testing.T) {
	endpoint := listen.Endpoint(fmt.Sprintf("protected-test-%d-%d", os.Getpid(), endpointSerial.Add(1)))
	l, err := listen.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	f, b := newFake()
	b.now = time.Now
	f.answered[testTicket] = time.Now()
	var verified atomic.Int64
	b.verify = func(p *identity.Peer) (identity.Process, error) {
		verified.Add(1)
		path, err := p.Path.AtLeast(identity.ProofBound)
		if err != nil {
			return identity.Process{}, err
		}
		self, _ := os.Executable()
		if !identity.SameSubjectProgram(path, self) {
			return identity.Process{}, errors.New("another program")
		}
		return p.Process.AtLeast(identity.ProofKernel)
	}
	mustRegister(t, b, testTicket)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- b.Serve(ctx, l) }()
	defer func() {
		cancel()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	}()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	principal := identity.User{Kind: "windows", SID: u.Uid}
	servers := []listen.ServerExpectation{
		{Principal: principal, Program: filepath.Join(filepath.Dir(exe), "openabstractionsw.exe")},
		{Principal: identity.User{Kind: "windows", SID: "S-1-5-18"}, Program: exe},
		{Principal: principal, Program: exe},
	}
	var got []byte
	err = RunHelper(ctx, endpoint, servers, OperationSecurityLogPage, testTicket, func(_ context.Context, request []byte) ([]byte, string) {
		got = request
		return []byte("one page"), ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(testRequest) {
		t.Fatalf("the helper received %q", got)
	}
	if n := verified.Load(); n != 2 {
		t.Fatalf("the runtime verified %d frames, want the claim and the completion only", n)
	}
	state, result, _ := b.Take(testTicket, OperationSecurityLogPage, testSubject, testRequest)
	if state != StateDelivered || string(result) != "one page" {
		t.Fatalf("take: %s %q", state, result)
	}
	err = RunHelper(ctx, endpoint, servers[:2], OperationSecurityLogPage, testTicket, func(context.Context, []byte) ([]byte, string) {
		t.Fatal("acted for an untrusted server")
		return nil, ""
	})
	if !errors.Is(err, listen.ErrServerUntrusted) {
		t.Fatalf("untrusted servers: %v", err)
	}
	err = RunHelper(ctx, endpoint, servers[2:], OperationSecurityLogPage, testTicket, func(context.Context, []byte) ([]byte, string) {
		t.Fatal("acted on a delivered ticket")
		return nil, ""
	})
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("a delivered ticket: %v", err)
	}
}

// TestLaunchArguments is FAC-H3's command line and its refusals before any
// prompt: an untrusted helper path never reaches ShellExecuteExW.
func TestLaunchArguments(t *testing.T) {
	if got, err := launchParameters(OperationSecurityLogPage, testTicket); err != nil || got != "security-log-page --ticket "+testTicket {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := launchParameters("security-log-page --ticket x", testTicket); !errors.Is(err, ErrRefused) {
		t.Fatalf("a spliced operation: %v", err)
	}
	if size := unsafe.Sizeof(shellExecuteInfo{}); size != 112 {
		t.Fatalf("SHELLEXECUTEINFOW is %d bytes here, 112 on 64-bit Windows", size)
	}
	request, err := launchRequest(0x1234, `C:\Program Files\OpenAbstractions\tools\openabstractions-system.exe`, "security-log-page --ticket "+testTicket)
	if err != nil {
		t.Fatal(err)
	}
	if request.window != 0x1234 || windows.UTF16PtrToString(request.verb) != "runas" || request.size != 112 ||
		request.mask&seeMaskNoCloseProcess == 0 || request.show != swHide ||
		windows.UTF16PtrToString(request.parameters) != "security-log-page --ticket "+testTicket {
		t.Fatalf("the launch request: owner %#x verb %q mask %#x", request.window, windows.UTF16PtrToString(request.verb), request.mask)
	}
	helper := filepath.Join(t.TempDir(), HelperFile)
	if err := os.WriteFile(helper, []byte("MZ"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Launch(0, helper, OperationSecurityLogPage, testTicket); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a helper this account can change: %v", err)
	}
	if _, err := Launch(0, helper, "cmd", testTicket); !errors.Is(err, ErrRefused) {
		t.Fatalf("an operation outside the catalogue: %v", err)
	}
	if _, err := TrustedHelper(filepath.Join(filepath.Dir(helper), "openabstractions.exe")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a per-user layout: %v", err)
	}
	if _, err := TrustedHelper("openabstractions.exe"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a relative image: %v", err)
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ServerExpectations(helper, identity.User{Kind: "windows", SID: u.Uid}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no trusted runtime beside a per-user helper: %v", err)
	}
	if p := ReadUACPolicy(); p.Read && p.String() == "" {
		t.Fatal("an unnamed policy")
	}
}
