package resolution_test

import (
	canonical "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
	core "github.com/openabstractions/abstraction-facade/go-core/resolution"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	old "github.com/openabstractions/abstraction-facade/go/resolution"
	"testing"
)

func TestCompatibilityRetainsTypeIdentity(t *testing.T) {
	var request canonical.ResolveRequest = wire.ResolveRequest{Capability: "absent", Contracts: []string{"absent/v1"}, Scope: "local"}
	catalog, err := old.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	var retained *core.Catalog = catalog
	result, err := retained.ForCaller(func(wire.ServiceReference) bool { return true }).Resolve(request)
	if err != nil {
		t.Fatal(err)
	}
	var original wire.ResolveResult = result
	if original.Status != wire.ResolutionStatusUnavailable {
		t.Fatal(original)
	}
	if string(wire.Encode(&original)) != string(canonical.Encode(&result)) {
		t.Fatal("wire drift")
	}
}
