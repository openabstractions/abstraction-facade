package runtime

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	rights "github.com/openabstractions/abstraction-rights/go"
	wire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
)

// Both decision paths satisfy Decider.
var (
	_ Decider = (*rightsclient.Client)(nil)
	_ Decider = (*rights.DecisionPolicy)(nil)
)

// deciderFixture is one decision path the resource rights tests run against.
type deciderFixture struct {
	policy  *rights.DecisionPolicy
	decider Decider
	// outage makes every further decision unobtainable.
	outage func(t *testing.T, h *Host)
}

// eachDecider runs a rights test twice: through the IPC decision service, with
// this process designated its enforcer, and through the in-process decision
// policy, with no enforcer designation. The in-process outage renames the
// policy file the installation created; the IPC outage closes the service.
func eachDecider(t *testing.T, actions []string, run func(t *testing.T, f deciderFixture, o Options)) {
	for _, mode := range []string{"ipc", "in-process"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy.json")
			policy, err := rights.LoadDecisionPolicy(path, actions)
			if err != nil {
				t.Fatal(err)
			}
			o := jobOptions(t)
			o.RightsPolicy, o.RightsEndpoint = policy, o.JobEndpoint+"-rights"
			f := deciderFixture{policy: policy}
			if mode == "ipc" {
				o.RightsEnforcer = func(ctx context.Context, peer *identity.Peer, a, r string) bool {
					process, e := peer.Process.AtLeast(listen.Program.Process)
					return e == nil && process.PID == os.Getpid() && slices.Contains(actions, a)
				}
				f.decider = rightsclient.New(o.RightsEndpoint)
				f.outage = func(t *testing.T, h *Host) { h.rights.Close() }
			} else {
				// An installation creates the policy file before it serves, and
				// requires it from then on.
				other := wire.Subject{Account: "installation-fixture", Program: filepath.Join(t.TempDir(), "other")}
				if err := policy.Set(other, actions[0], "fixture", false); err != nil {
					t.Fatal(err)
				}
				policy.StateRequired = true
				f.decider = policy
				f.outage = func(t *testing.T, h *Host) {
					if err := os.Rename(path, path+".moved"); err != nil {
						t.Fatal(err)
					}
				}
			}
			run(t, f, o)
		})
	}
}
