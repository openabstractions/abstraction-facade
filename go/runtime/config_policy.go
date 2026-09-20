package runtime

import (
	"context"

	configservice "github.com/openabstractions/abstraction-config/go/service"
	identity "github.com/openabstractions/abstraction-identity"
)

// ConfigEditAction and ConfigEditResource name the rights rule that authorizes
// replacing the service user's configuration rung.
const (
	ConfigEditAction   = "abstraction.config/user.replace"
	ConfigEditResource = "abstraction.config/editor@1"
)

// ConfigEditPolicyFromRights decides before each ReplaceUser. Evaluated denials
// refuse with forbidden; decision failures and unavailable catalogue decisions
// report unavailable. No permit is cached.
func ConfigEditPolicyFromRights(decisions Decider) configservice.EditPolicy {
	return func(ctx context.Context, peer *identity.Peer) error {
		return requireDecision(ctx, decisions, peer, ConfigEditAction, ConfigEditResource, configservice.ErrEditPolicyUnavailable)
	}
}
