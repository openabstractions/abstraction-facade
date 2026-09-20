package resolution

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

func TestCallerEchoIsTheBoundProgramEvidence(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("UNPROVEN on Darwin: the socket transport cannot meet Program proof, so no caller is echoed")
	}
	endpoint := fmt.Sprintf(`\\.\pipe\oa-caller-test-%d`, time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("", "oa-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
		endpoint = filepath.Join(dir, "caller.sock")
	}
	l, err := listen.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	candidate, _ := example()
	catalog, _ := New([]Candidate{candidate})
	done := make(chan error, 1)
	reached := make(chan struct{}, 1)
	go func() {
		conn, err := l.Accept()
		if err == nil {
			err = HandleConnection(ctx, conn, catalog, func(*identity.Peer, wire.ServiceReference) bool {
				reached <- struct{}{}
				return true
			})
		}
		done <- err
	}()
	got, err := NewClient(endpoint, 2*time.Second).ObserveCaller(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-reached:
		t.Fatal("caller echo consulted the resolution policy")
	default:
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	want := account.Uid
	if runtime.GOOS != "windows" {
		uid, _ := strconv.Atoi(account.Uid)
		want = strconv.Itoa(uid)
	}
	if got.Outcome != wire.CallerOutcomeObserved || got.Mechanism != "identity/"+runtime.GOOS || got.Account != want || got.PID != int64(os.Getpid()) {
		t.Fatalf("echo %+v, want account %s pid %d", got, want, os.Getpid())
	}
	if resolved, _ := filepath.EvalSymlinks(exe); !sameFile(got.Program, exe) && !sameFile(got.Program, resolved) {
		t.Fatalf("echoed program %q, want %q", got.Program, exe)
	}
	limits := identity.Ceiling()
	if got.Platform != limits.Platform || got.Transport != limits.Transport || got.Bindable != limits.Bindable || got.Stronger != limits.Stronger || len(got.Attributes) != 5 {
		t.Fatalf("echo ceiling %+v, want %+v", got, limits)
	}
	for i, name := range []string{"user", "process", "path", "package", "code"} {
		if got.Attributes[i].Attribute != name {
			t.Fatalf("attribute %d is %q, want %q", i, got.Attributes[i].Attribute, name)
		}
	}
	if got.Attributes[0].Proof != listen.Program.User.String() || got.Attributes[0].Ceiling != limits.Best.User.String() {
		t.Fatalf("user proof %+v", got.Attributes[0])
	}
	if refused := ObserveCaller(nil); refused.Outcome != wire.CallerOutcomeUnavailable || refused.Account != "" || refused.PID != -1 || len(refused.Attributes) != 0 {
		t.Fatalf("unbound caller echoed identity: %+v", refused)
	}
}

func sameFile(a, b string) bool {
	x, errX := os.Stat(a)
	y, errY := os.Stat(b)
	return errX == nil && errY == nil && os.SameFile(x, y)
}
