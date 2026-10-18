package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
)

// JOB-A18, JOB-A13: work is decided again as Submit decided it, asking
// nobody: an explicit deny on the contract or the kind refuses, a permit on
// either keeps the work (an account default's permit reads as one), and the
// work ends once neither permits. An unanswered decision is no refusal.
func TestJobStandingDecidesAsSubmitDid(t *testing.T) {
	subject := acceptanceprovider.AuthenticatedSubject{AccountKind: "windows", Account: "S-1-5-21-7-1001", Program: `C:\apps\fetch.exe`}
	for _, c := range []struct {
		name, contract, kind, want string
		unavailable                bool
	}{
		{name: "kind permit (default on)", contract: "not_granted", kind: "permitted"},
		{name: "contract permit", contract: "permitted", kind: "not_granted"},
		{name: "default off, no rule", contract: "not_granted", kind: "not_granted", want: "rights:not_granted"},
		{name: "app Blocked on the kind", contract: "permitted", kind: "denied", want: "rights:denied"},
		{name: "app Blocked on the contract", contract: "denied", kind: "permitted", want: "rights:denied"},
		{name: "forbidden", contract: "not_granted", kind: "forbidden", want: "rights:forbidden"},
		{name: "kind unanswered under a permit", contract: "permitted", kind: "unavailable", unavailable: true},
		{name: "contract unanswered", contract: "unavailable", kind: "not_granted", unavailable: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			var asked []string
			decide := func(_ context.Context, account, program, action, resource string) (string, error) {
				if account != subject.Account || program != subject.Program || action != JobSubmitAction {
					t.Errorf("decided %s %s %s", account, program, action)
				}
				asked = append(asked, resource)
				if resource == JobResource {
					return c.contract, nil
				}
				return c.kind, nil
			}
			got, err := JobStandingFromRights(decide)(context.Background(), subject, "download")
			if c.unavailable {
				if err == nil {
					t.Fatalf("an unanswered decision read %q", got)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("standing %q %v, want %q", got, err, c.want)
			}
			if len(asked) > 0 && asked[0] != JobResource {
				t.Fatalf("decided %v", asked)
			}
		})
	}
	if _, err := JobStandingFromRights(nil)(context.Background(), subject, "download"); err == nil {
		t.Fatal("an absent decider decided")
	}
	failing := func(context.Context, string, string, string, string) (string, error) { return "", errors.New("down") }
	if _, err := JobStandingFromRights(failing)(context.Background(), subject, "download"); err == nil {
		t.Fatal("a failing decider decided")
	}
}
