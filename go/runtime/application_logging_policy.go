package runtime

import (
	"context"
	"errors"
	identity "github.com/openabstractions/abstraction-identity"
	logging "github.com/openabstractions/abstraction-logging/go"
	logwire "github.com/openabstractions/abstraction-logging/go/abstraction/logging"
	logservice "github.com/openabstractions/abstraction-logging/go/service"
	rights "github.com/openabstractions/abstraction-rights/go"
	rwire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
)

// The local policy and a designated external enforcer have equivalent shared
// snapshot semantics. Receiving adapters never replace a missing @2 boundary.
type localSelectorDecider interface {
	CheckBatchContext(context.Context, rwire.Subject, rwire.CheckBatchRequest) rwire.BatchDecision
	CheckLegacyOperationContext(context.Context, rwire.Subject, rwire.LegacyOperationRequest) rwire.Decision
}
type externalSelectorDecider interface {
	CheckBatchForContext(context.Context, rwire.Subject, rwire.CheckBatchRequest) (rwire.BatchDecision, error)
	CheckLegacyOperationForContext(context.Context, rwire.Subject, rwire.LegacyOperationRequest) (rwire.Decision, error)
}
type batchFrameCounter struct{ size int }

var errCountedFrame = errors.New("rights: counted frame")

func (c *batchFrameCounter) ExchangeFrame(frame []byte) ([]byte, error) {
	c.size = len(frame)
	return nil, errCountedFrame
}

func applicationLogBatch(q logservice.ApplicationHistoryQuery) (rwire.CheckBatchRequest, error) {
	key := rwire.PermissionKey{Action: logging.ApplicationLogReadAction, ResourceProfile: logging.ApplicationLogResourceProfile}
	request := rwire.CheckBatchRequest{Resources: []rwire.ConcreteResource{}}
	switch q.Selection {
	case logwire.ApplicationLogSelectionOneApp:
		target := logging.EncodeApplicationResource(q.Target)
		if target == nil {
			return request, errors.New("logging: invalid selected target")
		}
		request.Resources = append(request.Resources, rwire.ConcreteResource{Permission: key, Target: target})
	case logwire.ApplicationLogSelectionAllApps:
		if q.Target.Program != "" || q.Target.CapturedFileKey != "" {
			return request, errors.New("logging: all-app target has no program")
		}
		request.CollectionAdmission = &key
	default:
		return request, errors.New("logging: invalid selection")
	}
	for _, emitter := range q.Emitters {
		target := logging.EncodeApplicationResource(emitter)
		if target == nil {
			return request, errors.New("logging: invalid emitter")
		}
		request.Resources = append(request.Resources, rwire.ConcreteResource{Permission: key, Target: target})
	}
	if len(request.Resources) > rights.MaxBatchResources {
		return request, errors.New("logging: batch exceeds shared resource limit")
	}
	return request, nil
}

func applicationLogFrameFits(subject rwire.Subject, request rwire.CheckBatchRequest) bool {
	counter := &batchFrameCounter{}
	_, err := rwire.NewSelectorAuthorizationClient(counter).CheckBatchFor(subject, request)
	return errors.Is(err, errCountedFrame) && counter.size > 0 && counter.size <= rights.MaxSelectorFrameBytes
}

func checkedApplicationLogBatch(result rwire.BatchDecision, q logservice.ApplicationHistoryQuery) ([]bool, error) {
	prefix := 0
	if q.Selection == logwire.ApplicationLogSelectionOneApp {
		prefix = 1
	}
	if result.Outcome != rwire.BatchCheckOutcomeChecked || result.Revision == "" || len(result.Outcomes) != prefix+len(q.Emitters) {
		if result.Outcome == rwire.BatchCheckOutcomeForbidden {
			return nil, &rightsclient.DecisionError{Outcome: "forbidden"}
		}
		return nil, logservice.ErrHistoryPolicyUnavailable
	}
	admission := rwire.DecisionOutcomeUnavailable
	if prefix == 1 {
		if result.CollectionOutcome != nil {
			return nil, logservice.ErrHistoryPolicyUnavailable
		}
		admission = result.Outcomes[0]
	} else if result.CollectionOutcome != nil {
		admission = *result.CollectionOutcome
	}
	if admission != rwire.DecisionOutcomePermitted {
		return nil, classifyDecision(&rightsclient.DecisionError{Outcome: admission.String(), PolicyRevision: result.Revision}, logservice.ErrHistoryPolicyUnavailable)
	}
	allowed := make([]bool, len(q.Emitters))
	for i, outcome := range result.Outcomes[prefix:] {
		switch outcome {
		case rwire.DecisionOutcomePermitted:
			allowed[i] = true
		case rwire.DecisionOutcomeDenied, rwire.DecisionOutcomeNotGranted:
		default:
			return nil, logservice.ErrHistoryPolicyUnavailable
		}
	}
	return allowed, nil
}

// ApplicationHistoryPolicyFromRights uses one final shared CheckBatch snapshot
// for collection admission, selection and every distinct persisted emitter.
// The exact generated request/frame also owns outgoing scanner batch budgets.
func ApplicationHistoryPolicyFromRights(decisions Decider) logservice.ApplicationHistoryPolicy {
	return logservice.ApplicationHistoryPolicy{
		Fits: func(peer *identity.Peer, q logservice.ApplicationHistoryQuery) bool {
			subject, err := rightsclient.SubjectFromPeer(peer)
			if err != nil {
				return false
			}
			request, err := applicationLogBatch(q)
			return err == nil && applicationLogFrameFits(subject, request)
		},
		Check: func(ctx context.Context, peer *identity.Peer, q logservice.ApplicationHistoryQuery) ([]bool, error) {
			if absentDecider(decisions) {
				return nil, logservice.ErrHistoryPolicyUnavailable
			}
			ctx, cancel := context.WithTimeout(ctx, rights.DecisionReadBudget)
			defer cancel()
			subject, err := rightsclient.SubjectFromPeer(peer)
			if err != nil || ctx.Err() != nil {
				return nil, errors.Join(logservice.ErrHistoryPolicyUnavailable, err)
			}
			request, err := applicationLogBatch(q)
			if err != nil || !applicationLogFrameFits(subject, request) {
				return nil, logservice.ErrHistoryPolicyUnavailable
			}
			var result rwire.BatchDecision
			switch d := decisions.(type) {
			case localSelectorDecider:
				result = d.CheckBatchContext(ctx, subject, request)
			case externalSelectorDecider:
				result, err = d.CheckBatchForContext(ctx, subject, request)
			default:
				return nil, logservice.ErrHistoryPolicyUnavailable
			}
			if err != nil || ctx.Err() != nil {
				return nil, errors.Join(logservice.ErrHistoryPolicyUnavailable, err, ctx.Err())
			}
			return checkedApplicationLogBatch(result, q)
		},
	}
}

func requireLegacyLogHistory(ctx context.Context, decisions Decider, peer *identity.Peer) error {
	if absentDecider(decisions) {
		return logservice.ErrHistoryPolicyUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, rights.DecisionReadBudget)
	defer cancel()
	subject, err := rightsclient.SubjectFromPeer(peer)
	if err != nil || ctx.Err() != nil {
		return errors.Join(logservice.ErrHistoryPolicyUnavailable, err)
	}
	request := rwire.LegacyOperationRequest{LegacyAction: LogHistoryAction, LegacyResource: LogHistoryResource}
	var decision rwire.Decision
	switch d := decisions.(type) {
	case localSelectorDecider:
		decision = d.CheckLegacyOperationContext(ctx, subject, request)
	case externalSelectorDecider:
		decision, err = d.CheckLegacyOperationForContext(ctx, subject, request)
	default:
		return logservice.ErrHistoryPolicyUnavailable
	}
	if err != nil || ctx.Err() != nil {
		return errors.Join(logservice.ErrHistoryPolicyUnavailable, err, ctx.Err())
	}
	if decision.Outcome == rwire.DecisionOutcomePermitted && decision.PolicyRevision != "" {
		return nil
	}
	return classifyDecision(&rightsclient.DecisionError{Outcome: decision.Outcome.String(), PolicyRevision: decision.PolicyRevision}, logservice.ErrHistoryPolicyUnavailable)
}
