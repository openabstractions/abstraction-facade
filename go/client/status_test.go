package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
	identity "github.com/openabstractions/abstraction-identity"
)

func TestObserveExpiredAndIndependentInputs(t *testing.T) {
	requests := DefaultStatusRequests()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	detail := "supervisor observation"
	report, err := New("unused").Observe(ctx, requests, wire.BootstrapObservation{State: wire.BootstrapStateRunning, Detail: detail})
	if !errors.Is(err, context.Canceled) || len(report.Capabilities) != 5 {
		t.Fatalf("%+v %v", report, err)
	}
	requests[0].Contracts[0] = "changed"
	detail = "changed"
	if report.Capabilities[0].Request.Contracts[0] == "changed" || report.Bootstrap.Detail == "changed" {
		t.Fatal("report aliases caller inputs")
	}
	for _, item := range report.Capabilities {
		if item.Result != nil {
			t.Fatal("canceled query reported a result")
		}
	}
	if DefaultStatusRequests()[0].Contracts[0] == "changed" {
		t.Fatal("defaults mutated")
	}
	report, err = New("unused").Observe(ctx, DefaultStatusRequests())
	if !errors.Is(err, context.Canceled) || report.Bootstrap.State != wire.BootstrapStateUnknown {
		t.Fatalf("absence inferred installation: %+v %v", report, err)
	}
}

func TestObservePartialFailureAndSharedBudget(t *testing.T) {
	report := wire.RuntimeObservation{Bootstrap: wire.BootstrapObservation{State: wire.BootstrapStateInstalled}}
	for _, request := range DefaultStatusRequests() {
		report.Capabilities = append(report.Capabilities, wire.CapabilityObservation{Request: request})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	result, err := observeQueries(ctx, report, func(got context.Context, request wire.ResolveRequest) (wire.ResolveResult, error) {
		if got != ctx {
			t.Fatal("caller context replaced")
		}
		calls++
		if calls == 1 {
			return wire.ResolveResult{Status: wire.ResolutionStatusForbidden}, nil
		}
		cancel()
		<-got.Done()
		return wire.ResolveResult{}, got.Err()
	})
	if !errors.Is(err, context.Canceled) || calls != 2 || result.Capabilities[0].Result.Status != wire.ResolutionStatusForbidden {
		t.Fatalf("%+v %v calls=%d", result, err, calls)
	}
	for _, item := range result.Capabilities[1:] {
		if item.Result != nil {
			t.Fatal("transport error converted to resolver state")
		}
	}
	if result.Bootstrap.State != wire.BootstrapStateInstalled {
		t.Fatal("bootstrap evidence replaced")
	}
}

func TestObserveAuthorizedStatuses(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Program-bound identity remains unproven on Darwin")
	}
	endpoint := filepath.Join(t.TempDir(), "status.sock")
	if runtime.GOOS == "windows" {
		endpoint = fmt.Sprintf(`\\.\pipe\oa-status-%d`, time.Now().UnixNano())
	}
	requests := DefaultStatusRequests()
	refs := []resolution.Candidate{}
	for i := 0; i < 2; i++ {
		refs = append(refs, resolution.Candidate{Ready: i == 0, Reference: wire.ServiceReference{Provider: "fixture", Capability: requests[i].Capability, Contract: requests[i].Contracts[0], Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: "fixture"}})
	}
	catalog, err := resolution.New(refs)
	if err != nil {
		t.Fatal(err)
	}
	host, err := resolution.Listen(endpoint, catalog, func(*identity.Peer, wire.ServiceReference) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	done := make(chan error, 1)
	go func() { done <- host.Serve(ctx) }()
	defer func() { cancel(); host.Close(); <-done }()
	machine := New(endpoint)
	report, err := machine.Observe(ctx, requests)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"resolved", "not_ready", "unavailable", "unavailable", "incompatible"}
	for i, item := range report.Capabilities {
		if item.Result == nil || item.Result.Status != expected[i] {
			t.Fatalf("item %d: %+v", i, item)
		}
	}
	if report.Bootstrap.State != wire.BootstrapStateUnknown {
		t.Fatal("resolver response invented platform evidence")
	}
	if probe := os.Getenv("OA_CPP_STATUS_PROBE"); probe != "" {
		output, err := exec.CommandContext(ctx, probe, "--observe", endpoint).CombinedOutput()
		if err != nil {
			t.Fatalf("C++ observation: %v %s", err, output)
		}
		var observed struct {
			Bootstrap    struct{ State string }
			Capabilities []struct{ Result *struct{ Status string } }
		}
		if err := json.Unmarshal(output, &observed); err != nil {
			t.Fatalf("C++ output: %v %s", err, output)
		}
		if observed.Bootstrap.State != "unknown" || len(observed.Capabilities) != len(expected) {
			t.Fatalf("C++ observation shape: %s", output)
		}
		for i, item := range observed.Capabilities {
			if item.Result == nil || item.Result.Status != expected[i] {
				t.Fatalf("C++ item %d: %s", i, output)
			}
		}
	}
	again, err := machine.Observe(ctx, requests)
	if err != nil || again.Capabilities[0].Result.Status != "resolved" {
		t.Fatalf("reuse: %+v %v", again, err)
	}
}
