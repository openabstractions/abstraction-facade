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

// requireDecision returns nil for a permit, the decision error for an evaluated
// refusal (denied, not_granted, forbidden), and unavailable joined with the
// cause for every decision that could not be obtained, including an absent
// decider, an unavailable policy, an unknown action and missing caller proof.
func requireDecision(ctx context.Context, decisions Decider, peer *identity.Peer, action, resource string, unavailable error) error {
	if absentDecider(decisions) || action == "" {
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
