package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	credentials "github.com/openabstractions/abstraction-credentials/go"
	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	rights "github.com/openabstractions/abstraction-rights/go"
	rwire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
	rightsservice "github.com/openabstractions/abstraction-rights/go/authorization"
)

func TestRuntimeRightsSelectorResolutionAndStateWatch(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Program proof unavailable")
	}
	// The fixture exercises native resolver/state lifetime with one registered
	// operation. Target validation reuses the owning Credentials codec only.
	const fixtureAction = "fixture.runtime/read"
	const fixtureProfile = "fixture.runtime/target@1"
	base, err := rights.NewDefinitionCatalog(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := rwire.SelectorPermissionDefinition{Definition: rwire.PermissionDefinition{Action: fixtureAction, OwnerContract: "fixture.runtime/reader@1", Operations: []string{"fixture.runtime/reader@1#Read"}, ResourceProfile: fixtureProfile, ResourceType: "CredentialResource", EnforcedBy: []string{"fixture.runtime/reader@1"}, Disclosure: "Read fixture target", Lifecycle: "One test name", FirstUse: rwire.PermissionFirstUseNone, Served: true}, Label: "Fixture Read", SupportedSelectors: []rwire.ResourceSelectorKind{rwire.ResourceSelectorKindOne}, Collection: "fixture.runtime/targets@1", RequestType: "CredentialResource"}
	catalog, err := base.WithSelectors([]rwire.SelectorPermissionDefinition{d}, map[string]rights.ResourceProfile{fixtureProfile: {Prepare: func(raw []byte) ([]byte, error) {
		v, err := credentials.DecodeCredentialTarget(raw)
		if err != nil {
			return nil, err
		}
		return credentials.PrepareCredentialTarget(v.Name)
	}, Validate: func(raw []byte) bool { _, err := credentials.DecodeCredentialTarget(raw); return err == nil }, Equal: bytes.Equal}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rights.json")
	p, err := rights.LoadDecisionPolicyWithDefinitions(path, []string{fixtureAction}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	o := jobOptions(t)
	o.RightsPolicy, o.RightsEndpoint = p, o.JobEndpoint+"-selectors"
	o.RightsOperator = func(ctx context.Context, peer *identity.Peer) error {
		process, err := peer.Process.AtLeast(listen.Program.Process)
		if err != nil || process.PID != os.Getpid() {
			return rightsservice.ErrOperatorForbidden
		}
		return ctx.Err()
	}
	o.RightsSelectorEnforcer = func(ctx context.Context, peer *identity.Peer, s rwire.PermissionSelection) bool {
		process, err := peer.Process.AtLeast(listen.Program.Process)
		return err == nil && process.PID == os.Getpid() && s.Permission.Action == fixtureAction && ctx.Err() == nil
	}
	h, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	authorizer, err := m.ResolveRightsSelector(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	operator, err := m.ResolveRightsSelectorOperator(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	defs, err := authorizer.ListPermissionDefinitionsContext(ctx, "", 64)
	if err != nil || defs.Outcome != rwire.PolicyPageOutcomePage {
		t.Fatal(defs, err)
	}
	rules, err := operator.ListSelectorRulesContext(ctx, "", 64)
	if err != nil || rules.Revision != defs.Revision {
		t.Fatal(rules, err)
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	subject := rwire.Subject{Account: account.Uid, Program: identity.CanonicalProgramPath(exe)}
	target, err := credentials.PrepareCredentialTarget("alpha")
	if err != nil {
		t.Fatal(err)
	}
	selection := rwire.PermissionSelection{Permission: rwire.PermissionKey{Action: fixtureAction, ResourceProfile: fixtureProfile}, Selector: rwire.ResourceSelector{Kind: rwire.ResourceSelectorKindOne, Target: target}}
	edit, err := operator.SetSelectorRuleContext(ctx, rules.Revision, rwire.SelectorRule{Subject: subject, Selection: selection, Permit: true}, 0, "public fixture")
	if err != nil || edit.Outcome != rwire.PolicyEditOutcomeApplied {
		t.Fatal(edit, err)
	}
	decision, err := authorizer.CheckBatchForContext(ctx, subject, rwire.CheckBatchRequest{ExpectedRevision: edit.Revision, Resources: []rwire.ConcreteResource{{Permission: selection.Permission, Target: target}}})
	if err != nil || decision.Outcome != rwire.BatchCheckOutcomeChecked || decision.Outcomes[0] != rwire.DecisionOutcomePermitted {
		t.Fatal(decision, err)
	}
	retained, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	waitReady := func(want bool) {
		t.Helper()
		for {
			_, err := m.ResolveRightsSelectorOperator(ctx, client.Requirements{})
			if want && err == nil {
				return
			}
			var binding *client.ResolutionError
			if !want && errors.As(err, &binding) && binding.Status == "not_ready" {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("selector readiness did not follow state", err)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	waitReady(false)
	if _, err = m.ResolveRightsSelector(ctx, client.Requirements{}); err == nil {
		t.Fatal("checker retained stale readiness")
	}
	if err = os.WriteFile(path, retained, 0600); err != nil {
		t.Fatal(err)
	}
	waitReady(true)
	h.rights.Close()
	waitReady(false)
	if _, err = m.ResolveConfig(ctx, client.Requirements{}); err != nil {
		t.Fatal("rights outage disabled sibling", err)
	}
}
