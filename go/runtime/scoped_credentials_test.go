package runtime

import (
	"context"
	"slices"
	"testing"
)

type scopedCredentialFixture struct{ scoped, applier bool }

func (*scopedCredentialFixture) Serve(context.Context) error    { return nil }
func (*scopedCredentialFixture) Close() error                   { return nil }
func (f *scopedCredentialFixture) ApplierAvailable() bool       { return f.applier }
func (f *scopedCredentialFixture) ScopedAvailable() bool        { return f.scoped }
func (f *scopedCredentialFixture) ScopedApplierAvailable() bool { return f.scoped && f.applier }

func TestScopedCredentialProfilesRequireActualComposition(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		for _, applier := range []bool{false, true} {
			h := &Host{}
			h.addCredentialCandidates(Options{Credentials: &scopedCredentialFixture{scoped, applier}, CredentialsEndpoint: "local-credential-fixture"})
			contracts := []string{}
			for _, candidate := range h.candidates {
				contracts = append(contracts, candidate.Reference.Contract)
			}
			if !slices.Contains(contracts, "abstraction.credentials/holder@1") || slices.Contains(contracts, "abstraction.credentials/applier@1") != applier {
				t.Fatal("legacy availability changed")
			}
			if slices.Contains(contracts, "abstraction.credentials/reader@1") != scoped || slices.Contains(contracts, "abstraction.credentials/holder@2") != scoped || slices.Contains(contracts, "abstraction.credentials/applier@2") != (scoped && applier) {
				t.Fatal("unconfigured scoped profile advertised")
			}
		}
	}
}
