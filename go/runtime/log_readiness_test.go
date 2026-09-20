package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	client "github.com/openabstractions/abstraction-facade/go/client"
	host "github.com/openabstractions/abstraction-facade/go/runtime"
	"github.com/openabstractions/abstraction-identity/listen"
)

// A composition with no logging sink reports it: a startup error kept on the
// host and given to OnError, and every logging contract resolving not_ready
// while the independent configuration provider resolves.
func TestMissingLogSinkIsReported(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	for _, withCallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("OnError=%v", withCallback), func(t *testing.T) {
			root := t.TempDir()
			for _, key := range []string{"HOME", "APPDATA", "XDG_CONFIG_HOME", "ProgramData"} {
				t.Setenv(key, root)
			}
			suffix := fmt.Sprintf("%d-%v", os.Getpid(), withCallback)
			options := host.Options{Endpoint: listen.Endpoint("ls-r-" + suffix), LogEndpoint: listen.Endpoint("ls-l-" + suffix), ConfigEndpoint: listen.Endpoint("ls-c-" + suffix)}
			var mu sync.Mutex
			var reported []error
			if withCallback {
				options.OnError = func(err error) { mu.Lock(); reported = append(reported, err); mu.Unlock() }
			}
			h, err := host.Listen(options)
			if err != nil {
				t.Fatal(err)
			}
			startup := h.StartupErrors()
			if len(startup) != 1 || !errors.Is(startup[0], host.ErrNoLogSink) || !strings.Contains(startup[0].Error(), "no logging sink") {
				t.Fatalf("startup errors: %v", startup)
			}
			startup[0] = nil
			if h.StartupErrors()[0] == nil {
				t.Fatal("StartupErrors returned the host's own slice")
			}
			mu.Lock()
			if withCallback && (len(reported) != 1 || !errors.Is(reported[0], host.ErrNoLogSink)) {
				t.Fatalf("OnError: %v", reported)
			}
			mu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- h.Serve(ctx) }()
			defer func() {
				cancel()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("runtime did not stop")
				}
			}()
			contracts := []string{"abstraction.logging/sink@1", "abstraction.logging/reader@1", "abstraction.logging/observer@1", "abstraction.config/reader@1"}
			requests := make([]wire.ResolveRequest, len(contracts))
			for i, contract := range contracts {
				capability, _, _ := strings.Cut(contract, "/")
				requests[i] = wire.ResolveRequest{Capability: capability, Contracts: []string{contract}, Guarantees: []string{}, Scope: wire.ScopeLocal}
			}
			var report wire.RuntimeObservation
			for {
				report, err = client.New(options.Endpoint).Observe(ctx, requests)
				if err == nil {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(err)
				case <-time.After(20 * time.Millisecond):
				}
			}
			want := []wire.ResolutionStatus{wire.ResolutionStatusNotReady, wire.ResolutionStatusNotReady, wire.ResolutionStatusNotReady, wire.ResolutionStatusResolved}
			for i, observation := range report.Capabilities {
				if observation.Result == nil || observation.Result.Status != want[i] {
					t.Fatalf("%s: %+v, want %s", contracts[i], observation.Result, want[i])
				}
			}
		})
	}
}
