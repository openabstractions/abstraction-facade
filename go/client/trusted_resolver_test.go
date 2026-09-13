//go:build windows || linux

package client

import (
	"context"
	"errors"
	"fmt"
	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

type countedResolverConn struct {
	listen.Conn
	count *atomic.Int64
}

func (c countedResolverConn) SetDeadline(at time.Time) error {
	return c.Conn.(interface{ SetDeadline(time.Time) error }).SetDeadline(at)
}
func (c countedResolverConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.count.Add(int64(n))
	return n, err
}

type selectionResolver struct{}

func (selectionResolver) Resolve(request wire.ResolveRequest) (wire.ResolveResult, error) {
	return wire.ResolveResult{Status: wire.ResolutionStatusResolved, Reference: &wire.ServiceReference{Provider: "catalogue-provider-is-not-server-identity", Capability: request.Capability, Contract: request.Contracts[0], Scope: wire.ScopeLocal, Transport: "oa-framed-local@1", Endpoint: "selected-provider"}}, nil
}

func TestDefaultResolverUsesInstallationExpectation(t *testing.T) {
	for _, mode := range []string{"correct", "wrong-installation", "wrong-principal", "explicit", "compatibility"} {
		t.Run(mode, func(t *testing.T) {
			endpoint := fmt.Sprintf(`\\.\pipe\oa-trusted-resolver-%d-%d`, os.Getpid(), time.Now().UnixNano())
			if runtime.GOOS == "linux" {
				dir, err := os.MkdirTemp("", "oa-trust-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.RemoveAll(dir); err != nil {
						t.Error(err)
					}
				})
				endpoint = filepath.Join(dir, "runtime.sock")
			}
			listener, err := listen.Listen(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var count atomic.Int64
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				call, err := listen.ReceiveFramed(ctx, countedResolverConn{conn, &count}, listen.Program, 1<<20)
				if err != nil {
					done <- err
					return
				}
				defer call.Close()
				dispatcher := wire.ResolverDispatcher{Handler: selectionResolver{}}
				reply, err := dispatcher.ExchangeFrame(call.Frame)
				if err == nil {
					err = call.Reply(reply)
				}
				done <- err
			}()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			principal, err := user.Current()
			if err != nil {
				t.Fatal(err)
			}
			selected := bootstrap.Selection{Endpoint: endpoint, Server: listen.ServerExpectation{Principal: identity.User{Kind: "windows", SID: principal.Uid}, Program: exe}}
			if runtime.GOOS == "linux" {
				selected.Server.Principal = identity.User{Kind: "posix", UID: os.Geteuid(), GID: -1}
			}
			switch mode {
			case "wrong-installation":
				selected.Server.Program = filepath.Join(t.TempDir(), "tools", "openabstractions.exe")
			case "wrong-principal":
				selected.Server.Principal.SID = "S-1-0-0"
				if runtime.GOOS == "linux" {
					selected.Server.Principal.UID++
				}
			}
			machine := Discover()
			selections := 0
			machine.selectInstalled = func(context.Context) (bootstrap.Selection, error) { selections++; return selected, nil }
			if mode == "explicit" {
				machine = NewVerified(endpoint, selected.Server)
			}
			if mode == "compatibility" {
				machine = NewUnverified(endpoint)
			}
			_, err = machine.ResolveLog(ctx, Requirements{})
			refused := mode == "wrong-installation" || mode == "wrong-principal"
			if refused {
				if !errors.Is(err, listen.ErrServerUntrusted) {
					t.Fatalf("expected trust refusal: %v", err)
				}
			} else if err != nil {
				select {
				case serverErr := <-done:
					t.Fatalf("client: %v; server: %v", err, serverErr)
				case <-ctx.Done():
					t.Fatal(err)
				}
			}
			select {
			case serverErr := <-done:
				if !refused && serverErr != nil {
					t.Fatal(serverErr)
				}
			case <-ctx.Done():
				t.Fatal("server did not release")
			}
			if refused && count.Load() != 0 {
				t.Fatalf("untrusted resolver received %d bytes", count.Load())
			}
			if !refused && count.Load() == 0 {
				t.Fatal("generated resolver was not called")
			}
			if mode != "explicit" && mode != "compatibility" && selections != 1 {
				t.Fatalf("installation selection calls: %d", selections)
			}
		})
	}
}

func TestDefaultResolverSelectionRefusals(t *testing.T) {
	for _, want := range []error{bootstrap.ErrNoTrustedInstallation, bootstrap.ErrAmbiguousInstallation, bootstrap.ErrUnsupportedSelection} {
		machine := Discover()
		machine.selectInstalled = func(context.Context) (bootstrap.Selection, error) { return bootstrap.Selection{}, want }
		if _, err := machine.ResolveLog(context.Background(), Requirements{}); !errors.Is(err, want) {
			t.Fatal(err)
		}
		observation, err := machine.Observe(context.Background(), DefaultStatusRequests())
		if !errors.Is(err, want) {
			t.Fatal(err)
		}
		if observation.Bootstrap.State != wire.BootstrapStateUnknown {
			t.Fatal("selection changed supplied bootstrap evidence")
		}
		for _, capability := range observation.Capabilities {
			if capability.Result != nil {
				t.Fatal("refused installation fabricated readiness")
			}
		}
	}
}
