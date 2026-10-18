package grants

import (
	"testing"

	asks "github.com/openabstractions/abstraction-asks/go"
)

// asks ASK-R6, PROPOSAL §15 part 1: each first-use permission of the slice
// has its tier, and anything else is the person's.
func TestFirstUseTiersFollowTheSlice(t *testing.T) {
	facts := TierFacts{
		LocalServer:    func(name string) bool { return name == "lemonade" },
		ListedRegistry: func(name string) bool { return name == "hf" || name == "ollama" },
	}
	cases := []struct {
		action, resource string
		want             asks.Tier
	}{
		{CompleteAction, "host:lemonade", asks.TierRoutine},
		{CompleteAction, "host:openrouter", asks.TierAdministrative},
		{CompleteAction, "host:", asks.TierAdministrative},
		{CompleteAction, "lemonade", asks.TierAdministrative},
		{ModelLookupAction, "hf", asks.TierRoutine},
		{ModelLookupAction, "ollama", asks.TierRoutine},
		{ModelLookupAction, "example-registry", asks.TierSensitive},
		{JobSubmitAction, "job:inference", asks.TierRoutine},
		{JobSubmitAction, "job:download", asks.TierSensitive},
		{JobSubmitAction, "job:other", asks.TierSensitive},
		{ContentWriteAction, "output:image", asks.TierRoutine},
		{ContentReadAction, "output:speech", asks.TierRoutine},
		{ContentWriteAction, "output:video", asks.TierRoutine},
		{ContentWriteAction, "input:image", asks.TierSensitive},
		{LendAction, "server:ollama", asks.TierSensitive},
		{"abstraction.asks/question.answer", "apps", asks.TierAdministrative},
		{"abstraction.rights/permission.manage", "app:comfyui", asks.TierAdministrative},
		{"abstraction.credentials/apply", "openrouter", asks.TierSensitive},
	}
	for _, c := range cases {
		if got := FirstUseTier(c.action, c.resource, facts); got != c.want {
			t.Errorf("FirstUseTier(%q, %q) = %q, want %q", c.action, c.resource, got, c.want)
		}
	}
	// With no facts nothing on a server or a registry is routine.
	if got := FirstUseTier(CompleteAction, "host:lemonade", TierFacts{}); got != asks.TierAdministrative {
		t.Errorf("a server with no facts: %q", got)
	}
	if got := FirstUseTier(ModelLookupAction, "hf", TierFacts{}); got != asks.TierSensitive {
		t.Errorf("a registry with no facts: %q", got)
	}
	for _, class := range FirstUseTierClasses {
		if class.Words == "" || class.Tier == "" {
			t.Errorf("a row without words or tier: %+v", class)
		}
	}
}
