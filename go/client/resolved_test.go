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

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
	identity "github.com/openabstractions/abstraction-identity"
)

func TestBindingDoesNotSubstituteTransport(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("successful Program-bound connection remains unproven on Darwin; resolution tests verify refusal")
	}
	dir, err := os.MkdirTemp("", "oa-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	endpoint := filepath.Join(dir, "resolver.sock")
	if runtime.GOOS == "windows" {
		endpoint = fmt.Sprintf(`\\.\pipe\oa-bind-test-%d`, time.Now().UnixNano())
	}
	ref := wire.ServiceReference{Provider: "test", Capability: "abstraction.logging", Contract: "abstraction.logging/sink@1", Scope: wire.ScopeRemote, Transport: "https", Endpoint: "https://invalid.example"}
	catalog, err := resolution.New([]resolution.Candidate{{Ready: true, Reference: ref}})
	if err != nil {
		t.Fatal(err)
	}
	h, err := resolution.Listen(endpoint, catalog, func(*identity.Peer, wire.ServiceReference) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx) }()
	defer func() { cancel(); h.Close(); <-done }()
	_, err = New(endpoint).ResolveLog(ctx, Requirements{})
	var binding *BindingError
	if !errors.As(err, &binding) || binding.Status != "unsupported_transport" {
		t.Fatalf("remote substituted: %v", err)
	}
	ref.Scope = wire.ScopeLocal
	ref.Transport = "unknown-local"
	catalog, err = resolution.New([]resolution.Candidate{{Ready: true, Reference: ref}})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Update(catalog); err != nil {
		t.Fatal(err)
	}
	_, err = New(endpoint).ResolveLog(ctx, Requirements{})
	if !errors.As(err, &binding) || binding.Status != "unsupported_transport" {
		t.Fatalf("unknown transport substituted: %v", err)
	}
	canceled, cancelCall := context.WithCancel(ctx)
	cancelCall()
	_, err = New(endpoint).ResolveLog(canceled, Requirements{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost caller cancellation: %v", err)
	}
}
