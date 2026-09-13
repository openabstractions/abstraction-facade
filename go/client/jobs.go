package client

import (
	"context"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/openabstractions/abstraction-identity/listen"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
)

// JobsOptions preserves binding guarantees and the acceptance logical owner.
// ExpectedOwner is the persisted logical owner, not the resolver provider ID.
// Zero Timeout supplies a fresh five-second maximum for each call.
type JobsOptions struct {
	RequiredGuarantees []string
	ExpectedOwner      string
	Timeout            time.Duration
}
type jobExchanger interface {
	ExchangeFrameContext(context.Context, []byte) ([]byte, error)
}

// JobsClient calls one fixed service. Each method accepts a fresh waiting context.
// Transport failure leaves acceptance unresolved; reconcile explicitly using the
// original identity and owner. A canceled wait never cancels accepted work.
// JobsClient supports concurrent calls and must not be copied after use.
type JobsClient struct {
	endpoint  string
	transport jobExchanger
	required  []string
	mu        sync.Mutex
	owner     string
}

// NewJobs restores endpoint-only compatibility and a persisted logical owner.
// Use NewJobsWithTransport to retain independent server trust on restoration.
// Persist endpoint, identity, owner and requirements before Submit. An empty
// ExpectedOwner is pinned by history or the first valid acceptance receipt.
func NewJobs(endpoint string, options JobsOptions) (*JobsClient, error) {
	return NewJobsWithTransport(listen.FrameClient{Endpoint: endpoint}, options)
}

// NewJobsWithTransport retains independently configured server trust and limits.
func NewJobsWithTransport(transport listen.FrameClient, options JobsOptions) (*JobsClient, error) {
	if transport.Endpoint == "" || options.Timeout < 0 || transport.Timeout < 0 {
		return nil, jobError("invalid_submission", "endpoint and nonnegative timeout required")
	}
	if err := api.ValidateSubmission(api.Submission{Identity: api.RequestIdentity{Key: "validation", HistoryEpoch: "validation"}, Kind: "validation", RequiredGuarantees: options.RequiredGuarantees}); err != nil {
		return nil, jobError("invalid_submission", err.Error())
	}
	if options.Timeout == 0 {
		options.Timeout = 5 * time.Second
	}
	return &JobsClient{endpoint: transport.Endpoint, transport: transport.WithDefaults(options.Timeout, 2<<20), required: slices.Clone(options.RequiredGuarantees), owner: options.ExpectedOwner}, nil
}

// Endpoint returns this binding's fixed endpoint for explicit restart recovery.
// Persist it with the owner, identity and complete operation requirements.
func (c *JobsClient) Endpoint() string { return c.endpoint }

// ResolveJobs binds once. Call failures and unknown acceptance never trigger
// another resolution or weaker requirements.
func (m *Machine) ResolveJobs(ctx context.Context, need Requirements) (*JobsClient, error) {
	endpoint, err := m.resolve(ctx, "abstraction.job", "abstraction.job/acceptance@1", need)
	if err != nil {
		return nil, err
	}
	return NewJobsWithTransport(endpoint, JobsOptions{RequiredGuarantees: need.Guarantees})
}

type jobCall struct {
	ctx       context.Context
	transport jobExchanger
}

func (c jobCall) ExchangeFrame(frame []byte) ([]byte, error) {
	if err := c.ctx.Err(); err != nil {
		return nil, err
	}
	return c.transport.ExchangeFrameContext(c.ctx, frame)
}
func (c *JobsClient) wire(ctx context.Context) *api.RecoverableAcceptanceClient {
	return api.NewRecoverableAcceptanceClient(jobCall{ctx, c.transport})
}
func jobError(code, message string) error { return &api.ServiceError{Code: code, Message: message} }
func jobIdentity(id api.RequestIdentity) error {
	if id.Key == "" || id.HistoryEpoch == "" {
		return jobError("invalid_submission", "explicit key and history epoch required")
	}
	return nil
}
func (c *JobsClient) bindOwner(owner string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if owner == "" || c.owner != "" && c.owner != owner {
		return jobError("invalid_acceptance", "logical owner changed or absent")
	}
	c.owner = owner
	return nil
}
func (c *JobsClient) GetHistoryWindow(ctx context.Context) (api.HistoryWindow, error) {
	result, err := c.wire(ctx).GetHistoryWindow()
	if err != nil {
		return api.HistoryWindow{}, err
	}
	if result.HistoryEpoch == "" || result.MinimumRetentionMs <= 0 {
		return api.HistoryWindow{}, jobError("invalid_acceptance", "invalid history window")
	}
	if err := c.bindOwner(result.LogicalOwner); err != nil {
		return api.HistoryWindow{}, err
	}
	return result, nil
}
func (c *JobsClient) validateResult(result api.AcceptanceResult, id api.RequestIdentity, required []string) error {
	// Nonaccepted responses carry no owner to pin. ValidateResult requires a
	// nonempty owner argument, whose value matters only for accepted receipts.
	owner := "unbound"
	if result.Receipt != nil {
		owner = result.Receipt.LogicalOwner
	}
	if err := api.ValidateResult(result, id, owner); err != nil {
		return jobError("invalid_acceptance", err.Error())
	}
	if result.Outcome != "accepted" {
		return nil
	}
	for _, g := range required {
		if !slices.Contains(result.Receipt.AcceptedGuarantees, g) {
			return jobError("invalid_acceptance", "receipt weakens required guarantees")
		}
	}
	return c.bindOwner(owner)
}
func (c *JobsClient) Submit(ctx context.Context, submission api.Submission) (api.AcceptanceResult, error) {
	if err := api.ValidateSubmission(submission); err != nil {
		return api.AcceptanceResult{}, jobError("invalid_submission", err.Error())
	}
	outgoing := submission
	outgoing.RequiredGuarantees = slices.Clone(submission.RequiredGuarantees)
	for _, g := range c.required {
		if !slices.Contains(outgoing.RequiredGuarantees, g) {
			outgoing.RequiredGuarantees = append(outgoing.RequiredGuarantees, g)
		}
	}
	result, err := c.wire(ctx).Submit(outgoing)
	if err != nil {
		return api.AcceptanceResult{}, err
	}
	if err := c.validateResult(result, submission.Identity, outgoing.RequiredGuarantees); err != nil {
		return api.AcceptanceResult{}, err
	}
	return result, nil
}

// Reconcile validates the binding requirements. When a submission adds further
// guarantees, restore NewJobs with that complete persisted set before recovery.
func (c *JobsClient) Reconcile(ctx context.Context, id api.RequestIdentity) (api.AcceptanceResult, error) {
	if err := jobIdentity(id); err != nil {
		return api.AcceptanceResult{}, err
	}
	result, err := c.wire(ctx).Reconcile(id)
	if err != nil {
		return api.AcceptanceResult{}, err
	}
	if err := c.validateResult(result, id, c.required); err != nil {
		return api.AcceptanceResult{}, err
	}
	return result, nil
}

// CancelWork explicitly requests cancellation. Requested acknowledges intent;
// it does not establish that execution effects have stopped.
func (c *JobsClient) CancelWork(ctx context.Context, id api.RequestIdentity) (api.CancellationResult, error) {
	if err := jobIdentity(id); err != nil {
		return api.CancellationResult{}, err
	}
	return c.wire(ctx).CancelWork(id)
}

// ObserveWork reads the current state from this binding without changing work intent.
func (c *JobsClient) ObserveWork(ctx context.Context, id api.RequestIdentity) (api.ObservationResult, error) {
	if err := jobIdentity(id); err != nil {
		return api.ObservationResult{}, err
	}
	result, err := api.NewOperationControlClient(jobCall{ctx, c.transport}).ObserveWork(id)
	if err != nil {
		return api.ObservationResult{}, err
	}
	if err = c.validateObservation(result, id); err != nil {
		return api.ObservationResult{}, err
	}
	return result, nil
}
func (c *JobsClient) validateObservation(result api.ObservationResult, id api.RequestIdentity) error {
	invalid := func() error { return jobError("invalid_observation", "inconsistent operation observation") }
	if result.Outcome != "observed" {
		if !slices.Contains([]string{"unknown", "forbidden", "invalid", "definitely_not_accepted"}, result.Outcome) || result.Snapshot != nil {
			return invalid()
		}
		return nil
	}
	s := result.Snapshot
	if s == nil || !slices.Contains([]string{"pending", "running", "transferred", "complete", "failed", "cancelled"}, s.State) || s.Progress.Done < 0 || s.Progress.Total < 0 {
		return invalid()
	}
	if s.Failure != nil && (!slices.Contains([]string{"retryable", "permanent", "unknown"}, s.Failure.Classification)) {
		return invalid()
	}
	return c.validateResult(api.AcceptanceResult{Outcome: "accepted", Receipt: &s.Receipt}, id, c.required)
}

// ReadResult reads at most maxBytes (1..65536) from a completed service-owned
// result. Callers retain the original operation ID and total while assembling
// chunks and reject any change across calls; this endpoint client keeps no
// per-operation cache. Waiting cancellation never cancels the work.
func (c *JobsClient) ReadResult(ctx context.Context, id api.RequestIdentity, offset, maxBytes int64) (api.ResultRead, error) {
	if err := jobIdentity(id); err != nil {
		return api.ResultRead{}, err
	}
	if offset < 0 || maxBytes < 1 || maxBytes > 65536 {
		return api.ResultRead{}, jobError("invalid_request", "offset and result chunk limit out of range")
	}
	result, err := api.NewOperationControlClient(jobCall{ctx, c.transport}).ReadResult(id, offset, maxBytes)
	if err != nil {
		return api.ResultRead{}, err
	}
	if err = c.validateRead(result, id, offset, maxBytes); err != nil {
		return api.ResultRead{}, err
	}
	return result, nil
}
func (c *JobsClient) validateRead(result api.ResultRead, id api.RequestIdentity, offset, maxBytes int64) error {
	invalid := func() error { return jobError("invalid_result", "inconsistent result chunk") }
	if result.Outcome != "data" {
		if !slices.Contains([]string{"not_ready", "unavailable", "unsupported", "unknown", "forbidden", "invalid"}, result.Outcome) || result.Chunk != nil {
			return invalid()
		}
		return nil
	}
	chunk := result.Chunk
	if chunk == nil || chunk.Offset != offset || chunk.Total < 0 || offset > chunk.Total {
		return invalid()
	}
	n := int64(len(chunk.Data))
	if n > maxBytes || n > 65536 || n > chunk.Total-offset || chunk.Eof != (n == chunk.Total-offset) || n == 0 && !chunk.Eof {
		return invalid()
	}
	return c.validateResult(api.AcceptanceResult{Outcome: "accepted", Receipt: &chunk.Receipt}, id, c.required)
}

// ResolveJobOperations requires observation/result support during new discovery.
// Already accepted work remains bound to its retained endpoint and owner.
func (m *Machine) ResolveJobOperations(ctx context.Context, need Requirements) (*JobsClient, error) {
	endpoint, err := m.resolve(ctx, "abstraction.job", "abstraction.job/operations@1", need)
	if err != nil {
		return nil, err
	}
	return NewJobsWithTransport(endpoint, JobsOptions{RequiredGuarantees: need.Guarantees})
}

// CopyResult copies a complete result with one bounded chunk in memory. It pins
// operation ID and total for this call. Errors return bytes actually written;
// partial output belongs to the caller. Cancellation never cancels accepted work.
func (c *JobsClient) CopyResult(ctx context.Context, id api.RequestIdentity, dst io.Writer) (written int64, err error) {
	if dst == nil {
		return 0, jobError("invalid_request", "result writer required")
	}
	var operation string
	var total int64
	for {
		result, err := c.ReadResult(ctx, id, written, 65536)
		if err != nil {
			return written, err
		}
		if result.Outcome != "data" {
			return written, jobError(result.Outcome, "result copy unavailable")
		}
		chunk := result.Chunk
		if operation == "" {
			operation = chunk.Receipt.OperationId
			total = chunk.Total
		} else if operation != chunk.Receipt.OperationId || total != chunk.Total {
			return written, jobError("invalid_result", "result identity or total changed during copy")
		}
		if err := ctx.Err(); err != nil {
			return written, err
		}
		if len(chunk.Data) > 0 {
			n, err := dst.Write(chunk.Data)
			if n < 0 || n > len(chunk.Data) {
				return written, jobError("invalid_writer", "writer returned impossible byte count")
			}
			written += int64(n)
			if err != nil {
				return written, err
			}
			if n != len(chunk.Data) {
				return written, io.ErrShortWrite
			}
		}
		if chunk.Eof {
			return written, nil
		}
	}
}
