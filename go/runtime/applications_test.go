package runtime

import (
	"context"
	"testing"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
)

type discoveryApplications struct{}

func (*discoveryApplications) Serve(context.Context) error { return nil }
func (*discoveryApplications) Close() error                { return nil }

func TestApplicationsDiscoverySeparatesClosedRecordVersion(t *testing.T) {
	h := &Host{}
	h.addApplicationsCandidate(Options{Applications: &discoveryApplications{}, ApplicationsEndpoint: "applications-endpoint"})
	catalog, err := resolution.New(h.candidates)
	if err != nil {
		t.Fatal(err)
	}
	resolver := wire.NewResolverClient(&wire.ResolverDispatcher{Handler: catalog.ForCaller(func(wire.ServiceReference) bool { return true })})
	for _, test := range []struct {
		contract string
		found    bool
	}{
		{"abstraction.facade/applications@1", false},
		{"abstraction.facade/applications@2", true},
	} {
		result, err := resolver.Resolve(wire.ResolveRequest{Capability: "abstraction.facade", Contracts: []string{test.contract}, Guarantees: []string{}, Scope: wire.ScopeLocal})
		if err != nil {
			t.Fatal(err)
		}
		if test.found {
			if result.Reference == nil || result.Reference.Contract != test.contract {
				t.Fatalf("version 2 resolution: %+v", result)
			}
		} else if result.Reference != nil || result.Status != wire.ResolutionStatusIncompatible {
			t.Fatalf("published closed record admitted: %+v", result)
		}
	}
}
