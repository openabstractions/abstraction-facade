package resolution

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
)

func TestResolutionCorpus(t *testing.T) {
	data, err := os.ReadFile("../../testdata/resolution.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name       string
		Request    wire.ResolveRequest
		Candidates []Candidate
		Allowed    []string
		Want       string
		Provider   string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			catalog, err := New(tc.Candidates)
			if err != nil {
				t.Fatal(err)
			}
			handler := catalog.ForCaller(func(ref wire.ServiceReference) bool {
				return slices.Contains(tc.Allowed, ref.Provider)
			})
			// Exercise generated request validation, dispatch, reply encoding and
			// typed client validation as well as the selection implementation.
			client := wire.NewResolverClient(&wire.ResolverDispatcher{Handler: handler})
			result, err := client.Resolve(tc.Request)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != tc.Want {
				t.Fatalf("got %s, want %s", result.Status, tc.Want)
			}
			if tc.Provider == "" {
				if result.Reference != nil {
					t.Fatalf("refusal leaked provider: %+v", result.Reference)
				}
			} else if result.Reference == nil || result.Reference.Provider != tc.Provider {
				t.Fatalf("got reference %+v, want provider %s", result.Reference, tc.Provider)
			}
		})
	}
}

func example() (Candidate, wire.ResolveRequest) {
	return Candidate{Reference: wire.ServiceReference{Provider: "p", Capability: "work",
			Contract: "example.work/runner@1", Guarantees: []string{"durable"}, Scope: wire.ScopeLocal,
			Transport: "ipc", Endpoint: "opaque"}, Ready: true},
		wire.ResolveRequest{Capability: "work", Contracts: []string{"example.work/runner@1"},
			Guarantees: []string{"durable"}, Scope: wire.ScopeAny}
}

func TestSnapshotAndPolicyCannotMutateGuarantees(t *testing.T) {
	candidate, request := example()
	catalog, err := New([]Candidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	candidate.Reference.Guarantees[0] = "weaker"
	handler := catalog.ForCaller(func(ref wire.ServiceReference) bool {
		ref.Guarantees[0] = "policy-mutated"
		return true
	})
	for range 2 {
		result, err := handler.Resolve(request)
		if err != nil || result.Status != wire.ResolutionStatusResolved {
			t.Fatalf("%+v %v", result, err)
		}
		if result.Reference.Guarantees[0] != "durable" {
			t.Fatal("snapshot was modified")
		}
		result.Reference.Guarantees[0] = "caller-mutated"
	}
}

func TestMissingPolicyDeniesAndInvalidInputIsTyped(t *testing.T) {
	candidate, request := example()
	catalog, _ := New([]Candidate{candidate})
	result, err := catalog.ForCaller(nil).Resolve(request)
	if err != nil || result.Status != wire.ResolutionStatusForbidden {
		t.Fatalf("%+v %v", result, err)
	}
	request.Scope = "invented"
	result, err = catalog.ForCaller(nil).Resolve(request)
	if err != nil || result.Status != wire.ResolutionStatusInvalidRequest {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestInvalidRegistration(t *testing.T) {
	candidate, _ := example()
	if _, err := New([]Candidate{candidate, candidate}); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	candidate.Reference.Scope = wire.ScopeAny
	if _, err := New([]Candidate{candidate}); err == nil {
		t.Fatal("nonconcrete placement accepted")
	}
}
