package client

import (
	"context"
	"slices"

	"github.com/openabstractions/abstraction-identity/listen"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
)

// JobOperatorClient lists every program's accepted work and cancels another
// program's operation through one fixed job service
// (abstraction.job/operator@1). The service decides each call by a rights
// rule; this client adds no authority.
type JobOperatorClient struct{ inventory *InventoryClient }

// NewJobOperatorWithTransport retains the configured service transport.
func NewJobOperatorWithTransport(transport listen.FrameClient, options JobsOptions) (*JobOperatorClient, error) {
	inventory, err := NewJobInventoryWithTransport(transport, options)
	if err != nil {
		return nil, err
	}
	return &JobOperatorClient{inventory: inventory}, nil
}

func (m *Machine) ResolveJobOperator(ctx context.Context, need Requirements) (*JobOperatorClient, error) {
	endpoint, err := m.resolve(ctx, "abstraction.job", JobOperatorContract, need)
	if err != nil {
		return nil, err
	}
	return NewJobOperatorWithTransport(endpoint, JobsOptions{RequiredGuarantees: need.Guarantees})
}

func (c *JobOperatorClient) wire(ctx context.Context) *api.JobOperatorClient {
	return api.NewJobOperatorClient(jobCall{ctx, c.inventory.binding.transport})
}

// ListAccountWork reads one bounded page of every scope's accepted work.
func (c *JobOperatorClient) ListAccountWork(ctx context.Context, cursor string, limit int64) (api.InventoryPage, error) {
	if limit < 1 || limit > 64 || len(cursor) > 128 {
		return api.InventoryPage{}, jobError("invalid_request", "inventory cursor or limit out of range")
	}
	p, err := c.wire(ctx).ListAccountWork(cursor, limit)
	if err != nil {
		return api.InventoryPage{}, err
	}
	if err = c.inventory.validate(p, cursor, limit); err != nil {
		return api.InventoryPage{}, err
	}
	return p, nil
}

// CancelOperation records cancellation intent on the operation with this
// receipt operation id, whichever program submitted it.
func (c *JobOperatorClient) CancelOperation(ctx context.Context, operationID string) (api.OperatorCancellation, error) {
	if operationID == "" || len(operationID) > 128 {
		return api.OperatorCancellation{}, jobError("invalid_request", "operation id out of range")
	}
	result, err := c.wire(ctx).CancelOperation(operationID)
	if err != nil {
		return api.OperatorCancellation{}, err
	}
	if !slices.Contains(api.OperatorCancellationOutcomeValues(), result.Outcome) {
		return api.OperatorCancellation{}, jobError("invalid_cancellation", "unknown operator cancellation outcome")
	}
	return result, nil
}
