package client

import (
	"context"
	"errors"
	"testing"

	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
)

func serviceCode(err error) string {
	var se *api.ServiceError
	if errors.As(err, &se) {
		return string(se.Code)
	}
	return ""
}

func TestJobsClientValidatesAttemptsAndTerminalFailure(t *testing.T) {
	fresh := func() *JobsClient {
		c, err := NewJobs("unused-endpoint", JobsOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	ctx := context.Background()
	if _, err := fresh().Reconcile(ctx, api.RequestIdentity{Key: "k", HistoryEpoch: "e", Attempt: -1}); serviceCode(err) != "invalid_submission" {
		t.Fatalf("negative attempt: %v", err)
	}
	original := api.RequestIdentity{Key: "k", HistoryEpoch: "e"}
	retry := api.RequestIdentity{Key: "k", HistoryEpoch: "e", Attempt: 1}
	if err := fresh().validateResult(receipt(original), retry, nil); err == nil {
		t.Fatal("receipt for the original attempt validated for a retry")
	}
	if err := fresh().validateResult(receipt(retry), retry, nil); err != nil {
		t.Fatal(err)
	}
	if err := fresh().validateResult(api.AcceptanceResult{Outcome: api.AcceptanceOutcomeUnavailable, Reason: "policy decision unavailable"}, retry, nil); err != nil {
		t.Fatalf("unavailable outcome: %v", err)
	}
	observed := func(state api.WorkState, class api.FailureClass) api.ObservationResult {
		return api.ObservationResult{Outcome: api.ObservationOutcomeObserved, Snapshot: &api.OperationSnapshot{Receipt: *receipt(retry).Receipt, State: state, Failure: &api.WorkFailure{Classification: class, Message: "download attempt failed", Cause: "a_future_cause"}}}
	}
	for _, c := range []struct {
		state api.WorkState
		class api.FailureClass
		valid bool
	}{
		{api.WorkStateFailed, api.FailureClassPermanent, true},
		{api.WorkStatePending, api.FailureClassPermanent, false},
		{api.WorkStateRunning, api.FailureClassPermanent, false},
		{api.WorkStatePending, api.FailureClassRetryable, true},
		{api.WorkStateFailed, api.FailureClassUnknown, true},
	} {
		err := fresh().validateObservation(observed(c.state, c.class), retry)
		if (err == nil) != c.valid {
			t.Fatalf("%s %s: %v", c.state, c.class, err)
		}
	}
}
