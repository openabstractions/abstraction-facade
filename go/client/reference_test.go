package client

import (
	"context"
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

// servedReference serves one resolved candidate and returns the resolver endpoint.
func servedReference(t *testing.T, ctx context.Context, reference wire.ServiceReference) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "oa-ref-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	endpoint := filepath.Join(dir, "resolver.sock")
	if runtime.GOOS == "windows" {
		endpoint = fmt.Sprintf(`\\.\pipe\oa-reference-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	}
	catalog, err := resolution.New([]resolution.Candidate{{Ready: true, Reference: reference}})
	if err != nil {
		t.Fatal(err)
	}
	host, err := resolution.Listen(endpoint, catalog, func(*identity.Peer, wire.ServiceReference) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- host.Serve(ctx) }()
	t.Cleanup(func() { host.Close(); <-done })
	return endpoint
}

func TestAResolvedBindingCarriesTheReferenceTheRuntimeReturned(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("a successful Program-bound connection remains unproven on Darwin")
	}
	served := wire.ServiceReference{
		Provider: "openabstractions.user-runtime", Capability: "abstraction.storage",
		Contract: "abstraction.storage/content-reader@1", Guarantees: []string{"abstraction.storage/local-only@1"},
		Scope: wire.ScopeLocal, Transport: "oa-framed-local@1", Endpoint: "content-reader-endpoint",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint := servedReference(t, ctx, served)

	binding, err := New(endpoint).ResolveService(ctx, "abstraction.storage/content-reader@1", Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	reference := binding.Reference()
	if reference.Provider != served.Provider {
		t.Fatalf("provider %q, want %q", reference.Provider, served.Provider)
	}
	if reference.Capability != served.Capability || reference.Contract != served.Contract ||
		reference.Scope != served.Scope || reference.Transport != served.Transport ||
		reference.Endpoint != served.Endpoint {
		t.Fatalf("reference %+v, want %+v", reference, served)
	}
	if len(reference.Guarantees) != 1 || reference.Guarantees[0] != served.Guarantees[0] {
		t.Fatalf("guarantees %v, want %v", reference.Guarantees, served.Guarantees)
	}
	if binding.Transport().Endpoint != served.Endpoint {
		t.Fatalf("bound endpoint %q, want %q", binding.Transport().Endpoint, served.Endpoint)
	}

	// Read-only: a caller editing its copy changes no binding.
	reference.Provider = "someone-else"
	reference.Guarantees[0] = "rewritten"
	again := binding.Reference()
	if again.Provider != served.Provider || again.Guarantees[0] != served.Guarantees[0] {
		t.Fatalf("the binding kept an editable reference: %+v", again)
	}
}

func TestResolveServiceRefusesAContractThatNamesNoCapability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, contract := range []string{"", "abstraction.storage", "/content-reader@1", "abstraction.storage/"} {
		if _, err := New("unused").ResolveService(ctx, contract, Requirements{}); err == nil {
			t.Fatalf("contract %q was accepted", contract)
		}
	}
}
