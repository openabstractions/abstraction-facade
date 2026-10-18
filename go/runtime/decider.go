package runtime

import (
	"context"
	"errors"
	"reflect"

	identity "github.com/openabstractions/abstraction-identity"
	rights "github.com/openabstractions/abstraction-rights/go/client"
)

// Decider decides one exact rights rule for the caller a receiving service
// bound. Require returns nil only for permitted, a *rights.DecisionError for
// every other decision outcome, and any other error when no decision was
// obtained. Two implementations exist:
//
//   - *rights.Client (abstraction-rights/go/client) asks a decision service over
//     IPC through DecideFor. That service must designate this process an
//     enforcer.
//   - *DecisionPolicy (abstraction-rights/go) decides in this process, from the
//     subject client.SubjectFromPeer derives. A runtime that hosts its own
//     decision policy uses it and needs no enforcer designation.
//
// Every call decides afresh; a helper here caches no permit.
type Decider interface {
	Require(ctx context.Context, peer *identity.Peer, action, resource string) error
}

// absentDecider reports a nil interface or a typed nil pointer inside one.
func absentDecider(decisions Decider) bool {
	if decisions == nil {
		return true
	}
	v := reflect.ValueOf(decisions)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// QuietDecider is a Decider that can also decide without admitting a
// first-use question. A host whose Require admits questions implements it,
// so a decision consulted only to find a deny asks the person nothing.
type QuietDecider interface {
	RequireQuietly(ctx context.Context, peer *identity.Peer, action, resource string) error
}

// quietDecision is requireDecision through RequireQuietly where the decider
// has it, and through Require otherwise.
func quietDecision(ctx context.Context, decisions Decider, peer *identity.Peer, action, resource string, unavailable error) error {
	if quiet, ok := decisions.(QuietDecider); ok && !absentDecider(decisions) && action != "" {
		return classifyDecision(quiet.RequireQuietly(ctx, peer, action, resource), unavailable)
	}
	return requireDecision(ctx, decisions, peer, action, resource, unavailable)
}

// requireDecision returns nil for a permit, the decision error for an evaluated
// refusal (denied, not_granted, forbidden), and unavailable joined with the
// cause for every decision that could not be obtained, including an absent
// decider, an unavailable policy, an unknown action and missing caller proof.
func requireDecision(ctx context.Context, decisions Decider, peer *identity.Peer, action, resource string, unavailable error) error {
	if absentDecider(decisions) || action == "" {
		return unavailable
	}
	return classifyDecision(decisions.Require(ctx, peer, action, resource), unavailable)
}

// classifyDecision keeps an evaluated refusal as it is and joins every other
// error with unavailable.
func classifyDecision(err error, unavailable error) error {
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
