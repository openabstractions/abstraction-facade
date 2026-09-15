package runtime

import (
	"context"
	"errors"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
	rights "github.com/openabstractions/abstraction-rights/go/client"
)

// JobRightsActions maps the mutating generated job methods to their rights
// catalogue actions. Keys are "<service wire name>#<method>".
var JobRightsActions = map[string]string{
	"abstraction.job/acceptance@1#Submit":     "abstraction.job/acceptance.submit",
	"abstraction.job/acceptance@1#CancelWork": "abstraction.job/acceptance.cancel",
}

// JobPolicyFromRights asks a fixed decision service before each mapped job
// method, using the service wire name as the resource. Methods without an action
// keep the host's same-owner Program authorization. Its host must explicitly
// trust this process to relay native subjects. No positive decision is cached.
// An evaluated refusal yields the job contract's forbidden outcome. A decision
// that cannot be obtained, including an unconfigured decision service, yields
// unavailable [JOB-A9]. Neither changes state.
func JobPolicyFromRights(decisions *rights.Client, actions map[string]string) acceptanceprovider.MethodPolicy {
	return func(ctx context.Context, peer *identity.Peer, service, method string) error {
		action, mapped := actions[service+"#"+method]
		if !mapped {
			return nil
		}
		if decisions == nil {
			return acceptanceprovider.ErrPolicyUnavailable
		}
		err := decisions.Require(ctx, peer, action, service)
		if err == nil {
			return nil
		}
		var decision *rights.DecisionError
		if errors.As(err, &decision) {
			switch decision.Outcome {
			case "denied", "not_granted", "forbidden":
				return err
			}
		}
		return errors.Join(acceptanceprovider.ErrPolicyUnavailable, err)
	}
}
