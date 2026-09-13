package client

import (
	"context"
	"github.com/openabstractions/abstraction-identity/listen"
	"slices"
	"unicode/utf8"

	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
)

// InventoryClient reads this caller's accepted work through one fixed service.
// Cursors are transient and scoped by the receiving service. A gap requires an
// explicit new traversal; callers deduplicate operation IDs across changing pages.
type InventoryClient struct{ binding *JobsClient }

func NewJobInventory(endpoint string, options JobsOptions) (*InventoryClient, error) {
	return NewJobInventoryWithTransport(listen.FrameClient{Endpoint: endpoint}, options)
}

// NewJobInventoryWithTransport retains the configured service transport.
func NewJobInventoryWithTransport(transport listen.FrameClient, options JobsOptions) (*InventoryClient, error) {
	c, err := NewJobsWithTransport(transport, options)
	if err != nil {
		return nil, err
	}
	return &InventoryClient{binding: c}, nil
}

func (m *Machine) ResolveJobInventory(ctx context.Context, need Requirements) (*InventoryClient, error) {
	endpoint, err := m.resolve(ctx, "abstraction.job", "abstraction.job/inventory@1", need)
	if err != nil {
		return nil, err
	}
	return NewJobInventoryWithTransport(endpoint, JobsOptions{RequiredGuarantees: need.Guarantees})
}

func (c *InventoryClient) ListWork(ctx context.Context, cursor string, limit int64) (api.InventoryPage, error) {
	if limit < 1 || limit > 64 || len(cursor) > 128 || !utf8.ValidString(cursor) {
		return api.InventoryPage{}, jobError("invalid_request", "inventory cursor or limit out of range")
	}
	p, err := api.NewJobInventoryClient(jobCall{ctx, c.binding.transport}).ListWork(cursor, limit)
	if err != nil {
		return api.InventoryPage{}, err
	}
	if err = c.validate(p, cursor, limit); err != nil {
		return api.InventoryPage{}, err
	}
	return p, nil
}

func (c *InventoryClient) validate(p api.InventoryPage, cursor string, limit int64) error {
	invalid := func() error { return jobError("invalid_inventory", "inconsistent inventory page") }
	if p.Outcome != "page" {
		if !slices.Contains([]string{"gap", "forbidden", "invalid", "unavailable"}, p.Outcome) || len(p.Snapshots) != 0 || p.Next != "" || p.Complete {
			return invalid()
		}
		return nil
	}
	if int64(len(p.Snapshots)) > limit || len(p.Next) > 128 || !utf8.ValidString(p.Next) || p.Complete && p.Next != "" || !p.Complete && (p.Next == "" || p.Next == cursor) {
		return invalid()
	}
	// Validate the whole page before pinning an owner on the reusable binding.
	check := &JobsClient{}
	seen := map[string]bool{}
	for i := range p.Snapshots {
		s := &p.Snapshots[i]
		if err := jobIdentity(s.Receipt.Identity); err != nil {
			return invalid()
		}
		if err := check.validateObservation(api.ObservationResult{Outcome: "observed", Snapshot: s}, s.Receipt.Identity); err != nil {
			return invalid()
		}
		if seen[s.Receipt.OperationId] {
			return invalid()
		}
		seen[s.Receipt.OperationId] = true
	}
	if check.owner != "" {
		return c.binding.bindOwner(check.owner)
	}
	return nil
}
