package runtime

import (
	"context"
	"errors"
	identity "github.com/openabstractions/abstraction-identity"
	rights "github.com/openabstractions/abstraction-rights/go/client"
	storage "github.com/openabstractions/abstraction-storage/go/service"
)

// ContentPolicyFromRights asks a fixed decision service before each content
// access. Its host must explicitly trust this process to relay native subjects.
// The action is a configured catalogue identifier, for example
// abstraction.storage/content.read. No positive decision is cached.
func ContentPolicyFromRights(decisions *rights.Client, action string) storage.Policy {
	return func(ctx context.Context, peer *identity.Peer, resource string) error {
		if decisions == nil || action == "" {
			return storage.ErrPolicyUnavailable
		}
		err := decisions.Require(ctx, peer, action, resource)
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
		return errors.Join(storage.ErrPolicyUnavailable, err)
	}
}
