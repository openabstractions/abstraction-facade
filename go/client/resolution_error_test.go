package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
	identity "github.com/openabstractions/abstraction-identity"
)

func unusedEndpoint(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`\\.\pipe\oa-%s-%d`, name, time.Now().UnixNano())
	}
	dir, err := os.MkdirTemp("", "oa-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, name+".sock")
}

func resolutionError(t *testing.T, err error, status ResolutionErrorStatus, lookedFor string) *ResolutionError {
	t.Helper()
	var refused *ResolutionError
	if !errors.As(err, &refused) {
		t.Fatalf("want *ResolutionError %s, got %T: %v", status, err, err)
	}
	if refused.Status != status || refused.Capability != "abstraction.logging" || refused.Contract != "abstraction.logging/sink@1" || refused.LookedFor != lookedFor {
		t.Fatalf("got %+v, want %s at %q", refused, status, lookedFor)
	}
	return refused
}

// VISION.md 2026-09-16: an adopted capability with no runtime fails with the
// facade's resolution error, visibly, and the application substitutes nothing.
// "A logging seam that quietly writes to stderr instead is the same defect"
// [LOG-S11]. ResolveLog returns no client, only the error.
func TestNoRuntimeInstalledIsAResolutionError(t *testing.T) {
	absent := errors.New("select installed runtime: no trusted runtime installation")
	m := Discover()
	m.selectInstalled = func(context.Context) (bootstrap.Selection, error) { return bootstrap.Selection{}, absent }
	sink, err := m.ResolveLog(context.Background(), Requirements{})
	if sink != nil {
		t.Fatalf("ResolveLog returned a client with no runtime: %v", sink)
	}
	refused := resolutionError(t, err, RuntimeUnavailable, "the installed runtime")
	if !errors.Is(err, absent) || refused.Unwrap() != absent {
		t.Fatalf("selection failure is not the cause: %v", err)
	}
	if _, ok := refused.Refusal(); ok {
		t.Fatal("runtime_unavailable reported as a resolver refusal")
	}
	if got := err.Error(); got != "service resolution: runtime_unavailable: abstraction.logging/sink@1 (capability abstraction.logging) at the installed runtime: "+absent.Error() {
		t.Fatalf("message %q", got)
	}
}

func TestUnsupportedPlatformNamesThePlatform(t *testing.T) {
	selection := fmt.Errorf("select installed runtime: %w", &bootstrap.UnsupportedPlatformError{Platform: "android"})
	m := Discover()
	m.selectInstalled = func(context.Context) (bootstrap.Selection, error) { return bootstrap.Selection{}, selection }
	_, err := m.ResolveLog(context.Background(), Requirements{})
	refused := resolutionError(t, err, RuntimeUnavailable, "the installed runtime")
	if refused.Platform != "android" || !errors.Is(err, bootstrap.ErrUnsupportedSelection) {
		t.Fatalf("platform or cause lost: %+v", refused)
	}
	if got := err.Error(); got != "service resolution: runtime_unavailable: abstraction.logging/sink@1 (capability abstraction.logging) at the installed runtime: no supported OpenAbstractions runtime exists for android" {
		t.Fatalf("message %q", got)
	}
}

func TestExplicitEndpointNobodyListensOnIsAResolutionError(t *testing.T) {
	endpoint := unusedEndpoint(t, "absent-resolver")
	_, err := NewUnverified(endpoint).ResolveLog(context.Background(), Requirements{})
	refused := resolutionError(t, err, RuntimeUnavailable, "the explicit endpoint "+endpoint)
	var service *wire.ServiceError
	if refused.Err == nil || errors.As(err, &service) {
		t.Fatalf("transport failure lost or reported as a service error: %v", err)
	}
}

func TestRuntimeWithoutTheServiceIsTheResolversRefusal(t *testing.T) {
	endpoint := unusedEndpoint(t, "empty-resolver")
	catalog, err := resolution.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := resolution.Listen(endpoint, catalog, func(*identity.Peer, wire.ServiceReference) bool { return true })
	if runtime.GOOS == "darwin" {
		if h != nil {
			h.Close()
			t.Fatal("insufficiently proven caller created a resolver")
		}
		if !errors.Is(err, identity.ErrNotProven) {
			t.Fatalf("expected startup proof refusal, got %v", err)
		}
		for _, path := range []string{endpoint, endpoint + ".lock"} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("proof refusal created endpoint artifact %q: %v", path, err)
			}
		}
		t.Skip("native Darwin sockets refuse Program at startup; resolver service refusal needs a running XPC host")
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	hostErrors := make(chan error, 4)
	h.OnError = func(err error) {
		select {
		case hostErrors <- err:
		default:
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx) }()
	defer func() { cancel(); h.Close(); <-done }()

	_, err = NewUnverified(endpoint).ResolveLog(ctx, Requirements{})
	refused := resolutionError(t, err, ResolutionErrorStatus(wire.ResolutionStatusUnavailable.String()), "the explicit endpoint "+endpoint)
	if status, ok := refused.Refusal(); !ok || status != wire.ResolutionStatusUnavailable || refused.Err != nil {
		t.Fatalf("resolver refusal %+v", refused)
	}

	// Success is unchanged: a registered local reference binds without a call.
	ref := wire.ServiceReference{Provider: "test", Capability: "abstraction.logging", Contract: "abstraction.logging/sink@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: unusedEndpoint(t, "sink")}
	if catalog, err = resolution.New([]resolution.Candidate{{Ready: true, Reference: ref}}); err != nil {
		t.Fatal(err)
	}
	if err = h.Update(catalog); err != nil {
		t.Fatal(err)
	}
	if client, err := NewUnverified(endpoint).ResolveLog(ctx, Requirements{}); err != nil || client == nil {
		t.Fatalf("registered service: %v", err)
	}
}

// An endpoint ABSTRACTION_RUNTIME_ENDPOINT named is the client's explicit
// choice, like NewVerified/NewUnverified: SDK activation never starts the
// selected installation to wait on it, and its absence is reported at once.
func TestEnvironmentNamedEndpointIsNeverActivated(t *testing.T) {
	endpoint := unusedEndpoint(t, "environment-named")
	m := Discover()
	m.selectInstalled = func(context.Context) (bootstrap.Selection, error) {
		return bootstrap.Selection{Endpoint: endpoint, EndpointFromEnvironment: true}, nil
	}
	m.activate = func(context.Context, bootstrap.Selection) error {
		t.Fatal("environment-named endpoint activated")
		return nil
	}
	_, err := m.ResolveLog(context.Background(), Requirements{})
	resolutionError(t, err, RuntimeUnavailable, "the installed runtime at "+endpoint)
}

func TestCallerCancellationStaysTheContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := Discover()
	m.selectInstalled = func(selection context.Context) (bootstrap.Selection, error) {
		cancel()
		<-selection.Done()
		return bootstrap.Selection{}, selection.Err()
	}
	_, err := m.ResolveLog(ctx, Requirements{})
	var refused *ResolutionError
	if !errors.Is(err, context.Canceled) || errors.As(err, &refused) {
		t.Fatalf("caller cancellation became %v", err)
	}
}
