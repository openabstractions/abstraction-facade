package runtime

import (
	"context"

	asksservice "github.com/openabstractions/abstraction-asks/go/application"
	identity "github.com/openabstractions/abstraction-identity"
	logservice "github.com/openabstractions/abstraction-logging/go/service"
	modelservice "github.com/openabstractions/abstraction-model/go/service"
	resourceservice "github.com/openabstractions/abstraction-resource/go/service"
	routerservice "github.com/openabstractions/abstraction-router/go/service"
)

// Rights actions and resources enforced at logging, model and router services.
const (
	LogHistoryAction   = "abstraction.logging/history.read"
	LogHistoryResource = "abstraction.logging/history"
	// ModelLookupAction's resource is the requested registry name.
	ModelLookupAction = "abstraction.model/lookup"
)

// The rights rule of an application Ask (ASK-R1).
const (
	QuestionAskAction   = "abstraction.asks/question.ask"
	QuestionAskResource = "account"
)

// AskPolicyFromRights decides each application Ask on QuestionAskResource. The
// question key does not change the rule. No permit is cached.
func AskPolicyFromRights(decisions Decider) asksservice.AskPolicy {
	return func(ctx context.Context, peer *identity.Peer, _ string) error {
		return requireDecision(ctx, decisions, peer, QuestionAskAction, QuestionAskResource, asksservice.ErrAskPolicyUnavailable)
	}
}

// HistoryPolicyFromRights decides before each history read, observation and
// post-wait recheck. No permit is cached.
func HistoryPolicyFromRights(decisions Decider) logservice.HistoryPolicy {
	return func(ctx context.Context, peer *identity.Peer) error {
		return requireDecision(ctx, decisions, peer, LogHistoryAction, LogHistoryResource, logservice.ErrHistoryPolicyUnavailable)
	}
}

// ModelPolicyFromRights decides before each lookup, using the requested
// registry as the resource. No permit is cached.
func ModelPolicyFromRights(decisions Decider) modelservice.LookupPolicy {
	return func(ctx context.Context, peer *identity.Peer, registry string) error {
		return requireDecision(ctx, decisions, peer, ModelLookupAction, registry, modelservice.ErrPolicyUnavailable)
	}
}

// The rights rule of a resource table read (CONTRACT.md RES-T4). The resource
// is the account, not the resource being read: a program either sees the
// machine's holders or sees its own rows.
const (
	ResourceTableReadAction   = resourceservice.ActionTableRead
	ResourceTableReadResource = resourceservice.ResourceAccount
)

// ResourceTablePolicyFromRights decides before each read of the resource
// table. A refusal narrows the answer to the caller's own rows; it does not
// end the call. No permit is cached.
func ResourceTablePolicyFromRights(decisions Decider) resourceservice.Policy {
	return func(ctx context.Context, peer *identity.Peer, action, resource string) error {
		return requireDecision(ctx, decisions, peer, action, resource, resourceservice.ErrPolicyUnavailable)
	}
}

// RoutesResource is the one resource of routing
// (research/rights-defaults/DECISION.md §1). The per-host switch is inference
// complete on host:<name>.
const RoutesResource = routerservice.ResourceRoutes

// RouterPolicyFromRights decides before each router operation: inventory reads
// use routerservice.ActionInventory on routerservice.ResourceInventory, and
// routing uses routerservice.ActionRoute on routerservice.ResourceRoutes. No
// permit is cached.
func RouterPolicyFromRights(decisions Decider) routerservice.Policy {
	return func(ctx context.Context, peer *identity.Peer, action, resource string) error {
		return requireDecision(ctx, decisions, peer, action, resource, routerservice.ErrPolicyUnavailable)
	}
}
