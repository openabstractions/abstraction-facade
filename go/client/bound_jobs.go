package client

import (
	"context"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
)

// BoundJobs supplies the generated interfaces with one caller waiting context.
// It retains its JobsClient's fixed provider, owner and guarantee validation.
// A new WithContext value supplies a fresh budget for later calls.
// Canceling the context only cancels waiting; CancelWork remains explicit.
type BoundJobs struct {
	client *JobsClient
	ctx    context.Context
}

// WithContext adapts this validated binding to the generated job interfaces.
// The context must be nonnil, as with the context-aware methods themselves.
func (c *JobsClient) WithContext(ctx context.Context) BoundJobs {
	if ctx == nil {
		panic("client: nil job context")
	}
	return BoundJobs{client: c, ctx: ctx}
}

var _ api.RecoverableAcceptance = BoundJobs{}
var _ api.OperationControl = BoundJobs{}

func (b BoundJobs) GetHistoryWindow() (api.HistoryWindow, error) {
	return b.client.GetHistoryWindow(b.ctx)
}
func (b BoundJobs) Submit(s api.Submission) (api.AcceptanceResult, error) {
	return b.client.Submit(b.ctx, s)
}
func (b BoundJobs) Reconcile(id api.RequestIdentity) (api.AcceptanceResult, error) {
	return b.client.Reconcile(b.ctx, id)
}
func (b BoundJobs) CancelWork(id api.RequestIdentity) (api.CancellationResult, error) {
	return b.client.CancelWork(b.ctx, id)
}
func (b BoundJobs) ObserveWork(id api.RequestIdentity) (api.ObservationResult, error) {
	return b.client.ObserveWork(b.ctx, id)
}
func (b BoundJobs) ReadResult(id api.RequestIdentity, offset, maxBytes int64) (api.ResultRead, error) {
	return b.client.ReadResult(b.ctx, id, offset, maxBytes)
}
