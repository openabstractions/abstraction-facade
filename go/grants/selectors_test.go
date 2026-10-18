package grants

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	credentials "github.com/openabstractions/abstraction-credentials/go"
	logging "github.com/openabstractions/abstraction-logging/go"
	rights "github.com/openabstractions/abstraction-rights/go"
	wire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

func TestSelectorDefinitionsPreserveGrammarAndFreezeFiniteBundle(t *testing.T) {
	unserved, err := SelectorPermissionDefinitions(SelectorServices{})
	if err != nil {
		t.Fatal(err)
	}
	served, err := SelectorPermissionDefinitions(SelectorServices{Credentials: true, ApplicationLogs: true})
	if err != nil {
		t.Fatal(err)
	}
	target, err := credentials.PrepareCredentialTarget("alpha")
	if err != nil {
		t.Fatal(err)
	}
	selection := wire.PermissionSelection{Permission: wire.PermissionKey{Action: credentials.ActionSecretRead, ResourceProfile: credentials.NamedSecretResourceProfile}, Selector: wire.ResourceSelector{Kind: wire.ResourceSelectorKindOne, Target: target}}
	if !unserved.ValidStoredSelection(selection) || unserved.ValidSelection(selection) {
		t.Fatal("availability changed stored grammar")
	}
	if _, err = unserved.PrepareSelection(selection); !errors.Is(err, rights.ErrPermissionUnavailable) {
		t.Fatal(err)
	}
	for _, d := range []wire.PermissionKey{{Action: credentials.ActionApply, ResourceProfile: credentials.NamedUseResourceProfile}, {Action: logging.ApplicationLogReadAction, ResourceProfile: rights.LegacyExactResourceProfile}} {
		row, ok := served.SelectorDefinition(d)
		if !ok || len(row.SupportedSelectors) != 0 {
			t.Fatal("legacy full-key meaning overwritten", row)
		}
	}
	bundles := served.PermissionBundles()
	if len(bundles) != 1 || len(bundles[0].Members) != 6 || bundles[0].BundleRef != credentials.FullControlBundle {
		t.Fatal(bundles)
	}
	if len(unserved.PermissionBundles()[0].SupportedSelectors) != 0 {
		t.Fatal("unserved bundle advertised")
	}
	create, _ := served.SelectorDefinition(wire.PermissionKey{Action: credentials.ActionSecretCreate, ResourceProfile: credentials.NamedSecretResourceProfile})
	revoke, _ := served.SelectorDefinition(wire.PermissionKey{Action: credentials.ActionSecretRevoke, ResourceProfile: credentials.NamedSecretResourceProfile})
	if create.Label != "Create" || !strings.Contains(create.Definition.Disclosure, "existing slot conflicts") || strings.Contains(create.Definition.Disclosure, "Store or replace") || !strings.Contains(revoke.Definition.Disclosure, "retain revoked metadata") || !strings.Contains(bundles[0].Disclosure, "Create a new slot") {
		t.Fatal("trusted operation wording overstates the receiving behavior", create, revoke, bundles[0])
	}
	second, err := SelectorPermissionDefinitions(SelectorServices{Credentials: true, ApplicationLogs: true})
	if err != nil || served.DefinitionDigest() != second.DefinitionDigest() || bundles[0].DefinitionDigest != second.PermissionBundles()[0].DefinitionDigest {
		t.Fatal("nondeterministic compiled digest", err)
	}
}

func TestSelectorProductionDefinitionsBidirectionalBlocks(t *testing.T) {
	catalog, err := SelectorPermissionDefinitions(SelectorServices{Credentials: true, ApplicationLogs: true})
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s := wire.Subject{Account: "test-account", Program: exe}
	p, err := rights.LoadDecisionPolicyWithDefinitions(filepath.Join(t.TempDir(), "rights.json"), []string{credentials.ActionApply, credentials.ActionManage, credentials.ActionSecretRead, credentials.ActionSecretRotate, logging.ApplicationLogReadAction}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	revision := func() string {
		v, err := p.OperatorSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		return v.Revision
	}
	set := func(selection wire.PermissionSelection, permit bool) {
		v, err := p.SetSelectorRuleContext(context.Background(), revision(), wire.SelectorRule{Subject: s, Selection: selection, Permit: permit}, rights.RuleEdit{By: s}, func() error { return nil })
		if err != nil || v.Outcome != wire.PolicyEditOutcomeApplied {
			t.Fatal(v, err)
		}
	}
	target, err := credentials.PrepareCredentialTarget("alpha")
	if err != nil {
		t.Fatal(err)
	}
	use := wire.PermissionKey{Action: credentials.ActionApply, ResourceProfile: credentials.ScopedUseResourceProfile}
	set(wire.PermissionSelection{Permission: use, Selector: wire.ResourceSelector{Kind: wire.ResourceSelectorKindAll}}, true)
	if err = p.Set(s, credentials.ActionApply, "credential:alpha", false); err != nil {
		t.Fatal(err)
	}
	query := wire.CheckBatchRequest{Resources: []wire.ConcreteResource{{Permission: use, Target: target}}}
	if v := p.CheckBatchContext(context.Background(), s, query); v.Outcomes[0] != wire.DecisionOutcomeDenied {
		t.Fatal("legacy named block bypassed", v)
	}
	if err = p.Set(s, credentials.ActionManage, credentials.ResourceAccount, true); err != nil {
		t.Fatal(err)
	}
	rotate := wire.PermissionKey{Action: credentials.ActionSecretRotate, ResourceProfile: credentials.NamedSecretResourceProfile}
	set(wire.PermissionSelection{Permission: rotate, Selector: wire.ResourceSelector{Kind: wire.ResourceSelectorKindOne, Target: target}}, false)
	operation := wire.ConcreteResource{Permission: rotate, Target: target}
	legacy := wire.LegacyOperationRequest{LegacyAction: credentials.ActionManage, LegacyResource: credentials.ResourceAccount, ConcreteOperation: &operation}
	if v := p.CheckLegacyOperationContext(context.Background(), s, legacy); v.Outcome != wire.DecisionOutcomeDenied {
		t.Fatal("typed management block bypassed", v)
	}
	read := wire.PermissionKey{Action: credentials.ActionSecretRead, ResourceProfile: credentials.NamedSecretResourceProfile}
	set(wire.PermissionSelection{Permission: read, Selector: wire.ResourceSelector{Kind: wire.ResourceSelectorKindOne, Target: target}}, true)
	if err = p.Set(s, credentials.ActionManage, credentials.ResourceAccount, false); err != nil {
		t.Fatal(err)
	}
	if v := p.CheckBatchContext(context.Background(), s, wire.CheckBatchRequest{Resources: []wire.ConcreteResource{{Permission: read, Target: target}}}); v.Outcomes[0] != wire.DecisionOutcomePermitted {
		t.Fatal("raw Read manufactured legacy equivalent", v)
	}
	log := wire.PermissionKey{Action: logging.ApplicationLogReadAction, ResourceProfile: logging.ApplicationLogResourceProfile}
	set(wire.PermissionSelection{Permission: log, Selector: wire.ResourceSelector{Kind: wire.ResourceSelectorKindAll}}, true)
	if err = p.Set(s, logging.ApplicationLogReadAction, "source:windows/security", false); err != nil {
		t.Fatal(err)
	}
	if v := p.CheckBatchContext(context.Background(), s, wire.CheckBatchRequest{CollectionAdmission: &log}); *v.CollectionOutcome != wire.DecisionOutcomePermitted {
		t.Fatal("native source became app log authority", v)
	}
}
