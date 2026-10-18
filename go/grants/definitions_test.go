package grants

import (
	"strings"
	"testing"

	askswire "github.com/openabstractions/abstraction-asks/go/abstraction/asks/api"
	credentials "github.com/openabstractions/abstraction-credentials/go"
	cwire "github.com/openabstractions/abstraction-credentials/go/abstraction/credentials/api"
	iwire "github.com/openabstractions/abstraction-inference/go/abstraction/inference/api"
	jwire "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	wire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

func TestDefinitionsCoverGeneratedActionOwnersAndKeepUnservedOperationsExplicit(t *testing.T) {
	for _, owner := range [][]string{askswire.ResourceActions, cwire.ResourceActions, iwire.ResourceActions, jwire.ResourceActions, wire.ResourceActions} {
		for _, action := range owner {
			if _, ok := PermissionDefinition(action); !ok {
				t.Fatalf("generated owner action %s lacks a definition", action)
			}
		}
	}
	remove, ok := PermissionDefinition("abstraction.storage/content.remove")
	if !ok || remove.Served || len(remove.EnforcedBy) != 0 || remove.FirstUse != wire.PermissionFirstUseNone {
		t.Fatal("unserved ContentRemover claims receiving enforcement")
	}
	lookup, _ := PermissionDefinition(ModelLookupAction)
	if !strings.Contains(lookup.Disclosure, "manifest@1 profile is defined and unserved") {
		t.Fatal("lookup hides its unserved manifest subset")
	}
	for _, operation := range lookup.Operations {
		if strings.Contains(operation, "manifest@1") {
			t.Fatal("lookup claims an unserved operation")
		}
	}
	if len(PermissionDefinitions()) != 41 {
		t.Fatal("36 legacy definitions, notification posting and register_self among them, plus five explicit saved-secret definitions expected")
	}
	selection, _ := PermissionDefinition("abstraction.inference/selection.manage_for")
	if selection.OwnerContract != "abstraction.inference/selection-operator@2" || len(selection.EnforcedBy) != 1 {
		t.Fatal("cross-program selection claims an earlier receiver")
	}
	for _, operation := range selection.Operations {
		if strings.Contains(operation, "operator@1") {
			t.Fatal("cross-program selection lists own-only operation")
		}
	}
	table, _ := PermissionDefinition("abstraction.resource/table.read")
	for _, operation := range table.Operations {
		if strings.HasSuffix(operation, "#Resources") {
			t.Fatal("public capacity method claims table read permission")
		}
	}
	if FirstUseMode(JobSubmitAction, "abstraction.job/acceptance@1") != wire.PermissionFirstUseNone || FirstUseMode(JobSubmitAction, "job:download") != wire.PermissionFirstUseExact {
		t.Fatal("job-kind first-use eligibility widened")
	}
	verb, object, ok := FirstUseWords(JobSubmitAction, "abstraction.job/acceptance@1")
	if !ok || verb != "submit background work" || object != "" {
		t.Fatal("existing broad job record lost its display caption")
	}
}

func TestCredentialDefinitionAdmitsOnlyExactNamedUseAndExplainsSlotLifetime(t *testing.T) {
	d, ok := PermissionDefinition(credentials.ActionApply)
	if !ok || d.ResourceProfile != credentials.NamedUseResourceProfile || d.ResourceType != "abstraction.credentials.api.CredentialResource" || !d.Served {
		t.Fatal("credential owner binding missing")
	}
	for _, resource := range []string{"credential:demo-secret", "credential:UPPER_1.2"} {
		if FirstUseMode(credentials.ActionApply, resource) != wire.PermissionFirstUseExact {
			t.Fatal("valid named Use refused")
		}
		verb, object, ok := FirstUseWords(credentials.ActionApply, resource)
		if !ok || !strings.Contains(verb, "credential") || object == "" {
			t.Fatal("named Use explanation missing")
		}
		text := FirstUseDefinitionDetails(credentials.ActionApply, resource)
		for _, word := range []string{"designated", "recreated registration", "no holder management", "freezes no current restrictions"} {
			if !strings.Contains(text, word) {
				t.Fatalf("Use disclosure omits %q", word)
			}
		}
	}
	for _, resource := range []string{"account", "credential:*", "credential:", "credential:a/b"} {
		if FirstUseMode(credentials.ActionApply, resource) != wire.PermissionFirstUseNone {
			t.Fatal("invalid named Use admitted")
		}
	}
	for _, action := range []string{credentials.ActionManage, credentials.ActionRead, "abstraction.credentials/read", "app/unknown"} {
		if FirstUseMode(action, "credential:demo-secret") != wire.PermissionFirstUseNone {
			t.Fatal("unsupported action admitted")
		}
	}
	if FirstUseMode(ContentWriteAction, "output:image") != wire.PermissionFirstUseConsumingPaired || FirstUseMode(ContentWriteAction, "arbitrary") != wire.PermissionFirstUseNone {
		t.Fatal("paired effect widened")
	}
}
