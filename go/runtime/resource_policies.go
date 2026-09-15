package runtime

import (
	"context"
	"errors"
	identity "github.com/openabstractions/abstraction-identity"
	logservice "github.com/openabstractions/abstraction-logging/go/service"
	modelservice "github.com/openabstractions/abstraction-model/go/service"
	rights "github.com/openabstractions/abstraction-rights/go/client"
	routerservice "github.com/openabstractions/abstraction-router/go/service"
)

// Rights actions and resources enforced at logging, model and router services.
const (
	LogHistoryAction   = "abstraction.logging/history.read"
	LogHistoryResource = "abstraction.logging/history"
	// ModelLookupAction's resource is the requested registry name.
	ModelLookupAction = "abstraction.model/lookup"
)

// requireDecision asks a fixed decision service. It returns nil for a permit,
// the decision error for an evaluated refusal, and unavailable joined with the
// cause for decision-service failures and unusable catalogue decisions.
func requireDecision(ctx context.Context, decisions *rights.Client, peer *identity.Peer, action, resource string, unavailable error) error {
	if decisions == nil {
		return unavailable
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
	return errors.Join(unavailable, err)
}

// HistoryPolicyFromRights asks the decision service before each history read,
// observation and post-wait recheck. Its host must explicitly trust this process
// to relay native subjects. No permit is cached.
func HistoryPolicyFromRights(decisions *rights.Client) logservice.HistoryPolicy {
	return func(ctx context.Context, peer *identity.Peer) error {
		return requireDecision(ctx, decisions, peer, LogHistoryAction, LogHistoryResource, logservice.ErrHistoryPolicyUnavailable)
	}
}

// ModelPolicyFromRights asks the decision service before each lookup, using the
// requested registry as the resource. No permit is cached.
func ModelPolicyFromRights(decisions *rights.Client) modelservice.LookupPolicy {
	return func(ctx context.Context, peer *identity.Peer, registry string) error {
		return requireDecision(ctx, decisions, peer, ModelLookupAction, registry, modelservice.ErrPolicyUnavailable)
	}
}

// RouterPolicyFromRights asks the decision service before each router
// operation: inventory reads use routerservice.ActionInventory on
// routerservice.ResourceInventory, and routing uses routerservice.ActionRoute on
// the requested model. No permit is cached.
func RouterPolicyFromRights(decisions *rights.Client) routerservice.Policy {
	return func(ctx context.Context, peer *identity.Peer, action, resource string) error {
		return requireDecision(ctx, decisions, peer, action, resource, routerservice.ErrPolicyUnavailable)
	}
}
