//go:build windows || linux

package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
)

// serveRestoredJobs answers generated acceptance frames on a real Program-bound
// endpoint owned by this test process.
func serveRestoredJobs(t *testing.T, ctx context.Context, owner string) string {
	t.Helper()
	endpoint := fmt.Sprintf(`\\.\pipe\oa-restore-jobs-%d-%d`, os.Getpid(), time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		endpoint = filepath.Join(t.TempDir(), "jobs")
	}
	listener, err := listen.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				call, err := listen.ReceiveFramed(ctx, conn, listen.Program, 1<<20)
				if err != nil {
					conn.Close()
					return
				}
				defer call.Close()
				dispatcher := api.RecoverableAcceptanceDispatcher{Handler: &jobHandler{owner: owner, result: receipt}}
				if reply, err := dispatcher.ExchangeFrame(call.Frame); err == nil {
					call.Reply(reply)
				}
			}()
		}
	}()
	return endpoint
}

func TestRestoreJobsAuthenticatesSavedServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint := serveRestoredJobs(t, ctx, "owner")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	expected := listen.ServerExpectation{Principal: identity.User{Kind: "windows", SID: current.Uid}, Program: exe}
	if runtime.GOOS == "linux" {
		expected.Principal = identity.User{Kind: "posix", UID: os.Geteuid(), GID: -1}
	}
	installed := func(server listen.ServerExpectation) *Machine {
		m := Discover()
		m.selectInstalled = func(context.Context) (bootstrap.Selection, error) {
			// The resolver endpoint is never contacted by restoration.
			return bootstrap.Selection{Endpoint: "resolver-not-contacted", Server: server}, nil
		}
		return m
	}
	saved := JobsBinding{Provider: "jobs-provider", Contract: "abstraction.job/operations@1", Endpoint: endpoint, LogicalOwner: "owner", RequiredGuarantees: []string{"bound"}}

	restored, err := installed(expected).RestoreJobs(ctx, saved)
	if err != nil {
		t.Fatal(err)
	}
	if got := restored.Binding(); !reflect.DeepEqual(got, saved) {
		t.Fatalf("binding %+v, saved %+v", got, saved)
	}
	id := api.RequestIdentity{Key: "saved-key", HistoryEpoch: "epoch"}
	if v, err := restored.Reconcile(ctx, id); err != nil || v.Receipt == nil || v.Receipt.LogicalOwner != "owner" {
		t.Fatalf("reconcile after restore: %+v %v", v, err)
	}

	wrongProgram := expected
	wrongProgram.Program = filepath.Join(t.TempDir(), "impostor.exe")
	if _, err := installed(wrongProgram).RestoreJobs(ctx, saved); !errors.Is(err, listen.ErrServerUntrusted) {
		t.Fatalf("untrusted server restored: %v", err)
	}
	policy := installed(expected).WithProviderTrust(func(context.Context, wire.ServiceReference) (listen.ServerExpectation, error) {
		return wrongProgram, nil
	})
	if _, err := policy.RestoreJobs(ctx, saved); !errors.Is(err, listen.ErrServerUntrusted) {
		t.Fatalf("provider trust policy ignored: %v", err)
	}
	changed := saved
	changed.LogicalOwner = "another-owner"
	if _, err := installed(expected).RestoreJobs(ctx, changed); serviceCode(err) != "invalid_acceptance" {
		t.Fatalf("changed owner restored: %v", err)
	}
	for _, bad := range []JobsBinding{
		{},
		{Endpoint: endpoint, Contract: saved.Contract},
		{Endpoint: endpoint, LogicalOwner: "owner", Contract: "abstraction.logging/sink@1"},
	} {
		if _, err := installed(expected).RestoreJobs(ctx, bad); serviceCode(err) != "invalid_binding" {
			t.Fatalf("invalid binding %+v: %v", bad, err)
		}
	}
	missing := Discover()
	want := errors.New("installation evidence unavailable")
	missing.selectInstalled = func(context.Context) (bootstrap.Selection, error) { return bootstrap.Selection{}, want }
	if _, err := missing.RestoreJobs(ctx, saved); !errors.Is(err, want) {
		t.Fatalf("missing installation: %v", err)
	}
}
