package grants

import (
	"strings"
	"testing"

	asks "github.com/openabstractions/abstraction-asks/go"
	rights "github.com/openabstractions/abstraction-rights/go"
	wire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

// asks ASK-R10: the enrollment is asked at first use only on apps, is the
// person's (administrative) and reads as the popup sentence.
func TestTheEnrollmentIsAFirstUseRightOnApps(t *testing.T) {
	if FirstUseMode(asks.AnswerAction, rights.MetaScopeApps) != wire.PermissionFirstUseExact {
		t.Fatal("the enrollment is not asked at first use")
	}
	if FirstUseMode(asks.AnswerAction, "first-use@1:[]") != wire.PermissionFirstUseNone {
		t.Fatal("an exact delegation is asked at first use")
	}
	if FirstUseTier(asks.AnswerAction, rights.MetaScopeApps, TierFacts{}) != asks.TierAdministrative {
		t.Fatal("the enrollment is not the person's to answer")
	}
	verb, object, ok := FirstUseWords(asks.AnswerAction, rights.MetaScopeApps)
	if !ok || verb+" "+object != "answer routine questions and the choices apps share with it for you" {
		t.Fatalf("the popup words %q %q %v", verb, object, ok)
	}
	if d := FirstUseDisclosure(asks.AnswerAction, rights.MetaScopeApps); !strings.Contains(d, "cannot be undone") || !strings.Contains(d, "Panel") {
		t.Fatalf("the disclosure %q", d)
	}
}
