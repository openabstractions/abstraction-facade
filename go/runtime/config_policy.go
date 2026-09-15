package runtime

import (
	"context"
	"errors"
	configservice "github.com/openabstractions/abstraction-config/go/service"
	identity "github.com/openabstractions/abstraction-identity"
	rights "github.com/openabstractions/abstraction-rights/go/client"
)

// ConfigEditAction and ConfigEditResource name the rights rule that authorizes
// replacing the service user's configuration rung.
const (
	ConfigEditAction   = "abstraction.config/user.replace"
	ConfigEditResource = "abstraction.config/editor@1"
)

// ConfigEditPolicyFromRights asks a fixed decision service before each
// ReplaceUser. Its host must explicitly trust this process to relay native
// subjects. Evaluated denials refuse with forbidden; decision-service failures
// and unavailable catalogue decisions report unavailable. No permit is cached.
func ConfigEditPolicyFromRights(decisions *rights.Client) configservice.EditPolicy {
	return func(ctx context.Context, peer *identity.Peer) error {
		if decisions == nil {
			return configservice.ErrEditPolicyUnavailable
		}
		err := decisions.Require(ctx, peer, ConfigEditAction, ConfigEditResource)
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
		return errors.Join(configservice.ErrEditPolicyUnavailable, err)
	}
}
