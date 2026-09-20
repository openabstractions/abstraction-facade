package runtime

import (
	"context"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
)

// Job rights actions and their one resource (research/rights-defaults/DECISION.md
// §1, JOB-A13). Submission spends on the account's behalf; account-wide
// inventory and cancelling another program's work cross programs.
const (
	JobSubmitAction    = "abstraction.job/acceptance.submit"
	JobCancelAction    = "abstraction.job/acceptance.cancel"
	JobInventoryAction = "abstraction.job/inventory.read"
	JobResource        = "abstraction.job/acceptance@1"
)

// JobRightsActions maps the generated job methods a rule gates to their rights
// catalogue actions. Keys are "<service wire name>#<method>". A caller's own
// work, cancellation included, is scoped by ownership and has no entry.
var JobRightsActions = map[string]string{
	"abstraction.job/acceptance@1#Submit":        JobSubmitAction,
	"abstraction.job/operator@1#ListAccountWork": JobInventoryAction,
	"abstraction.job/operator@1#CancelOperation": JobCancelAction,
}

// JobPolicyFromRights decides before each mapped job method, on JobResource.
// Methods without an action keep the host's same-owner Program authorization.
// No positive decision is cached. An evaluated refusal yields the job contract's
// forbidden outcome. A decision that cannot be obtained, including an absent
// decider, yields unavailable [JOB-A9]. Neither changes state.
func JobPolicyFromRights(decisions Decider, actions map[string]string) acceptanceprovider.MethodPolicy {
	return func(ctx context.Context, peer *identity.Peer, service, method string) error {
		action, mapped := actions[service+"#"+method]
		if !mapped {
			return nil
		}
		return requireDecision(ctx, decisions, peer, action, JobResource, acceptanceprovider.ErrPolicyUnavailable)
	}
}
