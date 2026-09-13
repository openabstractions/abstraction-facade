package runtime

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	configwire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/client"
	"github.com/openabstractions/abstraction-facade/go/resolution"
	"github.com/openabstractions/abstraction-identity/listen"
)

func TestStartupCapabilityIsolation(t *testing.T) {
	for _, failure := range []string{"logging", "config", "jobs", "job-store", "nil-sink", "all"} {
		t.Run(failure, func(t *testing.T) {
			o := jobOptions(t)
			if failure == "nil-sink" {
				o.Sink = nil
			}
			var diagnostics []error
			o.OnError = func(err error) { diagnostics = append(diagnostics, err) }
			for _, entry := range []struct{ name, endpoint string }{{"logging", o.LogEndpoint}, {"config", o.ConfigEndpoint}, {"jobs", o.JobEndpoint}} {
				if failure != entry.name && failure != "all" {
					continue
				}
				occupied, err := listen.Listen(entry.endpoint)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { occupied.Close() })
			}
			if failure == "job-store" {
				if err := os.WriteFile(o.JobRoot, []byte("blocked store"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			h, err := Listen(o)
			if err != nil {
				t.Fatalf("independent startup failed: %v", err)
			}
			// Startup callbacks are synchronous; use a separate sink for concurrent errors.
			h.onError = nil
			if h.logging != nil {
				h.logging.OnError = nil
			}
			if h.config != nil {
				h.config.OnError = nil
			}
			if h.jobs != nil {
				h.jobs.onError = nil
			}
			h.resolver.OnError = nil
			wantErrors := 1
			if failure == "all" {
				wantErrors = 3
			}
			if len(diagnostics) != wantErrors {
				t.Fatalf("startup diagnostics: %v", diagnostics)
			}
			for _, err := range diagnostics {
				if !strings.Contains(err.Error(), " startup:") {
					t.Fatalf("unclassified startup error: %v", err)
				}
				if failure != "job-store" && failure != "nil-sink" && !errors.Is(err, listen.ErrTaken) {
					t.Fatalf("lost listener cause: %v", err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			done := make(chan error, 1)
			go func() { done <- h.Serve(ctx) }()
			defer func() {
				cancel()
				h.Close()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(5 * time.Second):
					t.Error("host did not stop")
				}
			}()
			if runtime.GOOS == "darwin" {
				t.Log("Program-bound IPC success remains unproven on Darwin")
				return
			}
			m := client.New(o.Endpoint)
			for _, entry := range []struct{ name, capability, contract string }{{"logging", "abstraction.logging", "abstraction.logging/sink@1"}, {"config", "abstraction.config", "abstraction.config/reader@1"}, {"jobs", "abstraction.job", "abstraction.job/acceptance@1"}, {"jobs", "abstraction.job", "abstraction.job/operations@1"}} {
				expected := wire.ResolutionStatusResolved
				if failure == entry.name || failure == "all" || failure == "job-store" && entry.name == "jobs" || failure == "nil-sink" && entry.name == "logging" {
					expected = wire.ResolutionStatusNotReady
					if entry.name == "jobs" {
						expected = wire.ResolutionStatusUnavailable
					}
				}
				result, err := resolution.NewClient(o.Endpoint, time.Second).Resolve(ctx, wire.ResolveRequest{Capability: entry.capability, Contracts: []string{entry.contract}, Scope: wire.ScopeLocal})
				if err != nil || result.Status != expected {
					t.Fatalf("%s: %+v %v", entry.contract, result, err)
				}
				if expected != wire.ResolutionStatusResolved && result.Reference != nil {
					t.Fatal("failed provider returned reference")
				}
			}
			if h.logging != nil {
				c, err := m.ResolveLog(ctx, client.Requirements{})
				if err != nil {
					t.Fatal(err)
				}
				if err = c.Log(20, "healthy sibling", nil); err != nil {
					t.Fatal(err)
				}
			}
			if h.config != nil {
				c, err := m.ResolveConfig(ctx, client.Requirements{})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = c.ReadWithOverrides(configwire.RunOverrides{}); err != nil {
					t.Fatal(err)
				}
			}
			if h.jobs != nil {
				c, err := m.ResolveJobs(ctx, client.Requirements{})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = c.GetHistoryWindow(ctx); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestResolverStartupFailureClosesHealthyProviders(t *testing.T) {
	o := jobOptions(t)
	occupied, err := listen.Listen(o.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	h, err := Listen(o)
	if !errors.Is(err, listen.ErrTaken) || h != nil {
		if h != nil {
			h.Close()
		}
		t.Fatalf("resolver failure: %v %v", h, err)
	}
	for _, endpoint := range []string{o.LogEndpoint, o.ConfigEndpoint, o.JobEndpoint} {
		l, err := listen.Listen(endpoint)
		if err != nil {
			t.Fatalf("provider leaked after fatal resolver startup: %v", err)
		}
		l.Close()
	}
}
