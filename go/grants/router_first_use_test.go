package grants

import (
	"strings"
	"testing"

	asks "github.com/openabstractions/abstraction-asks/go"
	wire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

// default-policy D5: an app that reads the router's inventory (OpenCode's
// plugin calls Offer) is asked at first use, by the person: no tier row names
// the action, so it is sensitive.
func TestRouterInventoryReadIsAskedAtFirstUseByThePerson(t *testing.T) {
	if FirstUseMode(RouterInventoryAction, "abstraction.router/inventory") != wire.PermissionFirstUseExact {
		t.Fatal("router inventory read is not asked at first use")
	}
	found := false
	for _, action := range FirstUseActions() {
		found = found || action == RouterInventoryAction
	}
	if !found {
		t.Fatal("the runtime's first-use actions omit router inventory read")
	}
	if FirstUseTier(RouterInventoryAction, "abstraction.router/inventory", TierFacts{}) != asks.TierSensitive {
		t.Fatal("router inventory read is answerable by an assistant")
	}
	if verb, _, ok := FirstUseWords(RouterInventoryAction, "abstraction.router/inventory"); !ok || strings.Contains(verb, "abstraction.") {
		t.Fatal("router inventory read has no plain first-use words", verb)
	}
	if FirstUseMode("abstraction.router/route", "abstraction.router/routes") != wire.PermissionFirstUseNone {
		t.Fatal("router route widened to first use")
	}
}
