//go:build windows || linux

package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type providerResolver struct{ endpoint string }

func (r providerResolver) Resolve(req wire.ResolveRequest) (wire.ResolveResult, error) {
	return wire.ResolveResult{Status: wire.ResolutionStatusResolved, Reference: &wire.ServiceReference{Provider: "provider-label-is-not-a-process", Capability: req.Capability, Contract: req.Contracts[0], Scope: wire.ScopeLocal, Transport: "oa-framed-local@1", Endpoint: r.endpoint}}, nil
}
func TestResolvedProviderRetainsIndependentTrust(t *testing.T) {
	for _, mode := range []string{"installed-profile", "explicit-policy", "wrong-program", "wrong-principal"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			endpoint := func(name string) string {
				if runtime.GOOS == "windows" {
					return fmt.Sprintf(`\\.\pipe\oa-provider-%s-%d-%d`, name, os.Getpid(), time.Now().UnixNano())
				}
				dir, err := os.MkdirTemp("", "oa-provider-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(dir) })
				return filepath.Join(dir, name)
			}
			resolverEndpoint, providerEndpoint := endpoint("resolver"), endpoint("provider")
			resolver, err := listen.Listen(resolverEndpoint)
			if err != nil {
				t.Fatal(err)
			}
			defer resolver.Close()
			provider, err := listen.Listen(providerEndpoint)
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			resolverDone := make(chan error, 1)
			go func() {
				conn, e := resolver.Accept()
				if e != nil {
					resolverDone <- e
					return
				}
				call, e := listen.ReceiveFramed(ctx, conn, listen.Program, 1<<20)
				if e != nil {
					resolverDone <- e
					return
				}
				defer call.Close()
				dispatcher := wire.ResolverDispatcher{Handler: providerResolver{providerEndpoint}}
				reply, e := dispatcher.ExchangeFrame(call.Frame)
				if e == nil {
					e = call.Reply(reply)
				}
				resolverDone <- e
			}()
			var bytes atomic.Int64
			providerDone := make(chan error, 1)
			go func() {
				conn, e := provider.Accept()
				if e != nil {
					providerDone <- e
					return
				}
				call, e := listen.ReceiveFramed(ctx, countedResolverConn{conn, &bytes}, listen.Program, 1<<20)
				if e == nil {
					call.Close()
				}
				providerDone <- e
			}()
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
			machine := Discover()
			selections := 0
			machine.selectInstalled = func(context.Context) (bootstrap.Selection, error) {
				selections++
				return bootstrap.Selection{Endpoint: resolverEndpoint, Server: expected}, nil
			}
			policyCalls := 0
			if mode != "installed-profile" {
				machine = machine.WithProviderTrust(func(ctx context.Context, ref wire.ServiceReference) (listen.ServerExpectation, error) {
					policyCalls++
					if _, ok := ctx.Deadline(); !ok {
						t.Error("resolution budget lost")
					}
					if ref.Endpoint != providerEndpoint {
						t.Error("wrong reference")
					}
					configured := expected
					if mode == "wrong-program" {
						configured.Program = filepath.Join(t.TempDir(), "wrong.exe")
					}
					if mode == "wrong-principal" {
						configured.Principal.SID = "S-1-0-0"
						configured.Principal.UID++
					}
					return configured, nil
				})
			}
			sink, err := machine.ResolveLog(ctx, Requirements{})
			if err != nil {
				t.Fatal(err)
			}
			err = sink.LogContext(ctx, 0, "verified provider", nil)
			refused := mode == "wrong-program" || mode == "wrong-principal"
			if refused && !errors.Is(err, listen.ErrServerUntrusted) {
				t.Fatalf("want trust refusal: %v", err)
			}
			if !refused && err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-resolverDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case err = <-providerDone:
				if !refused && err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if refused && bytes.Load() != 0 {
				t.Fatalf("untrusted provider received %d bytes", bytes.Load())
			}
			if !refused && bytes.Load() == 0 {
				t.Fatal("provider was not called")
			}
			if selections != 1 || mode != "installed-profile" && policyCalls != 1 {
				t.Fatalf("selection/policy repeated: %d/%d", selections, policyCalls)
			}
		})
	}
}

// The copied helper is an actual different executable at the returned endpoint.
// The normal installed-runtime profile must refuse it without policy overrides.
func TestDefaultProviderRejectsOtherImage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(t.TempDir(), "other-provider.exe")
	in, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(copied, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		in.Close()
		t.Fatal(err)
	}
	_, err = io.Copy(out, in)
	in.Close()
	closeErr := out.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	endpoint := fmt.Sprintf(`\\.\pipe\oa-other-provider-%d-%d`, os.Getpid(), time.Now().UnixNano())
	resolverEndpoint := endpoint + "-resolver"
	if runtime.GOOS == "linux" {
		dir, e := os.MkdirTemp("", "oa-other-")
		if e != nil {
			t.Fatal(e)
		}
		defer os.RemoveAll(dir)
		endpoint = filepath.Join(dir, "provider")
		resolverEndpoint = filepath.Join(dir, "resolver")
	}
	cmd := exec.CommandContext(ctx, copied, "-test.run=^TestProviderTrustChild$")
	cmd.Env = append(os.Environ(), "OA_PROVIDER_TRUST_CHILD="+endpoint)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "READY" {
		t.Fatal("provider child did not become ready", scanner.Text())
	}
	resolver, err := listen.Listen(resolverEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer resolver.Close()
	done := make(chan error, 1)
	go func() {
		conn, e := resolver.Accept()
		if e != nil {
			done <- e
			return
		}
		call, e := listen.ReceiveFramed(ctx, conn, listen.Program, 1<<20)
		if e != nil {
			done <- e
			return
		}
		defer call.Close()
		dispatcher := wire.ResolverDispatcher{Handler: providerResolver{endpoint}}
		reply, e := dispatcher.ExchangeFrame(call.Frame)
		if e == nil {
			e = call.Reply(reply)
		}
		done <- e
	}()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	expected := listen.ServerExpectation{Program: exe, Principal: identity.User{Kind: "windows", SID: current.Uid}}
	if runtime.GOOS == "linux" {
		expected.Principal = identity.User{Kind: "posix", UID: os.Geteuid(), GID: -1}
	}
	machine := Discover()
	machine.selectInstalled = func(context.Context) (bootstrap.Selection, error) {
		return bootstrap.Selection{Endpoint: resolverEndpoint, Server: expected}, nil
	}
	sink, err := machine.ResolveLog(ctx, Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	if err = sink.LogContext(ctx, 0, "must never arrive", nil); !errors.Is(err, listen.ErrServerUntrusted) {
		t.Fatal(err)
	}
	if !scanner.Scan() || scanner.Text() != "PAYLOAD=0" {
		t.Fatal("untrusted provider received payload", scanner.Text())
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func TestProviderTrustChild(t *testing.T) {
	endpoint := os.Getenv("OA_PROVIDER_TRUST_CHILD")
	if endpoint == "" {
		return
	}
	listener, err := listen.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	fmt.Println("READY")
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if deadline, ok := conn.(interface{ SetDeadline(time.Time) error }); ok {
		deadline.SetDeadline(time.Now().Add(4 * time.Second))
	}
	buffer := make([]byte, 4)
	n, _ := conn.Read(buffer)
	fmt.Printf("PAYLOAD=%d\n", n)
	if n != 0 {
		t.Fatal(strings.TrimSpace(string(buffer[:n])))
	}
}
