package client

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
)

type jobHandler struct {
	owner            string
	submitted        api.Submission
	submits, cancels int
	result           func(api.RequestIdentity) api.AcceptanceResult
}

func (h *jobHandler) GetHistoryWindow() (api.HistoryWindow, error) {
	return api.HistoryWindow{LogicalOwner: h.owner, HistoryEpoch: "epoch", MinimumRetentionMs: 1000}, nil
}
func (h *jobHandler) Submit(s api.Submission) (api.AcceptanceResult, error) {
	h.submits++
	h.submitted = s
	return h.result(s.Identity), nil
}
func (h *jobHandler) Reconcile(id api.RequestIdentity) (api.AcceptanceResult, error) {
	return h.result(id), nil
}
func (h *jobHandler) CancelWork(api.RequestIdentity) (api.CancellationResult, error) {
	h.cancels++
	return api.CancellationResult{Outcome: "requested"}, nil
}

type jobTransport struct {
	dispatcher api.RecoverableAcceptanceDispatcher
	calls      int
	lost       bool
}

func (t *jobTransport) ExchangeFrameContext(ctx context.Context, frame []byte) ([]byte, error) {
	t.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := t.dispatcher.ExchangeFrame(frame)
	if t.lost {
		t.lost = false
		return nil, context.DeadlineExceeded
	}
	return result, err
}
func receipt(id api.RequestIdentity) api.AcceptanceResult {
	return api.AcceptanceResult{Outcome: "accepted", Receipt: &api.Receipt{Identity: id, LogicalOwner: "owner", OperationId: "operation", AcceptedGuarantees: []string{"bound", "explicit"}, HistoryRetentionMs: 1000}}
}
func jobsFixture(t *testing.T) (*JobsClient, *jobHandler, *jobTransport) {
	t.Helper()
	c, err := NewJobs("test-endpoint", JobsOptions{RequiredGuarantees: []string{"bound"}})
	if err != nil {
		t.Fatal(err)
	}
	h := &jobHandler{owner: "owner", result: receipt}
	tr := &jobTransport{dispatcher: api.RecoverableAcceptanceDispatcher{Handler: h}}
	c.transport = tr
	return c, h, tr
}
func TestJobsUnknownReplyKeepsBindingAndFreshCallContext(t *testing.T) {
	c, h, tr := jobsFixture(t)
	ctx := context.Background()
	history, err := c.GetHistoryWindow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := api.RequestIdentity{Key: "persisted", HistoryEpoch: history.HistoryEpoch}
	submission := api.Submission{Identity: id, Kind: "download", Spec: []byte("opaque"), RequiredGuarantees: []string{"explicit"}}
	tr.lost = true
	if _, err = c.Submit(ctx, submission); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if h.submits != 1 || h.cancels != 0 {
		t.Fatal("uncertain submit retried or canceled")
	}
	if !slices.Equal(submission.RequiredGuarantees, []string{"explicit"}) || !slices.Equal(h.submitted.RequiredGuarantees, []string{"explicit", "bound"}) {
		t.Fatal("binding requirements lost or caller mutated")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	before := tr.calls
	if _, err = c.Reconcile(canceled, id); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if tr.calls != before {
		t.Fatal("canceled call performed I/O")
	}
	got, err := c.Reconcile(context.Background(), id)
	if err != nil || got.Outcome != "accepted" || got.Receipt.OperationId != "operation" {
		t.Fatalf("fresh recovery: %+v %v", got, err)
	}
	if h.submits != 1 || h.cancels != 0 {
		t.Fatal("recovery resubmitted or canceled")
	}
	if _, err = c.CancelWork(ctx, id); err != nil || h.cancels != 1 {
		t.Fatalf("explicit cancel: %v", err)
	}
	h.result = func(api.RequestIdentity) api.AcceptanceResult { return api.AcceptanceResult{Outcome: "unknown"} }
	got, err = c.Reconcile(ctx, id)
	if err != nil || got.Outcome != "unknown" || api.CanResolveFresh(got) {
		t.Fatalf("unknown reinterpreted: %+v %v", got, err)
	}
}
func TestJobsRejectsHostileReceipts(t *testing.T) {
	cases := map[string]func(*api.AcceptanceResult){
		"missing":     func(v *api.AcceptanceResult) { v.Receipt = nil },
		"identity":    func(v *api.AcceptanceResult) { v.Receipt.Identity.Key = "other" },
		"epoch":       func(v *api.AcceptanceResult) { v.Receipt.Identity.HistoryEpoch = "other" },
		"owner":       func(v *api.AcceptanceResult) { v.Receipt.LogicalOwner = "other" },
		"operation":   func(v *api.AcceptanceResult) { v.Receipt.OperationId = "" },
		"retention":   func(v *api.AcceptanceResult) { v.Receipt.HistoryRetentionMs = 0 },
		"weak":        func(v *api.AcceptanceResult) { v.Receipt.AcceptedGuarantees = []string{"explicit"} },
		"duplicate":   func(v *api.AcceptanceResult) { v.Receipt.AcceptedGuarantees = []string{"bound", "bound"} },
		"empty":       func(v *api.AcceptanceResult) { v.Receipt.AcceptedGuarantees = []string{"bound", ""} },
		"nonaccepted": func(v *api.AcceptanceResult) { v.Outcome = "unknown" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			c, h, _ := jobsFixture(t)
			if _, err := c.GetHistoryWindow(context.Background()); err != nil {
				t.Fatal(err)
			}
			h.result = func(id api.RequestIdentity) api.AcceptanceResult { v := receipt(id); change(&v); return v }
			v, err := c.Reconcile(context.Background(), api.RequestIdentity{Key: "key", HistoryEpoch: "epoch"})
			var protocol *api.ServiceError
			if !errors.As(err, &protocol) || protocol.Code != "invalid_acceptance" || v.Receipt != nil {
				t.Fatalf("accepted hostile receipt %+v: %v", v, err)
			}
		})
	}
}
func TestJobsValidatesBeforeSendAndPreservesRecoveredOwner(t *testing.T) {
	c, h, tr := jobsFixture(t)
	if _, err := c.Submit(context.Background(), api.Submission{}); err == nil || tr.calls != 0 {
		t.Fatal("invalid submission sent")
	}
	if _, err := c.CancelWork(context.Background(), api.RequestIdentity{}); err == nil || tr.calls != 0 {
		t.Fatal("invalid cancellation sent")
	}
	c.owner = "recovered-owner"
	if _, err := c.GetHistoryWindow(context.Background()); err == nil {
		t.Fatal("owner pin replaced")
	}
	if c.owner != "recovered-owner" || h.submits != 0 {
		t.Fatal("owner or work changed")
	}
	for _, g := range [][]string{{""}, {"same", "same"}} {
		if _, err := NewJobs("endpoint", JobsOptions{RequiredGuarantees: g}); err == nil {
			t.Fatal("invalid guarantees accepted")
		}
	}
}
func TestJobsConcurrentOwnerPin(t *testing.T) {
	c, _, _ := jobsFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, owner := range []string{"one", "two"} {
		wg.Add(1)
		go func(owner string) { defer wg.Done(); results <- c.bindOwner(owner) }(owner)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("owners accepted: %d", success)
	}
}
