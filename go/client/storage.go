package client

import (
	"context"
	"time"

	"github.com/openabstractions/abstraction-identity/listen"
	storage "github.com/openabstractions/abstraction-storage/go/client"
)

// LendingContract is the profile a lending service publishes.
const LendingContract = "abstraction.storage/lend@1"

// ResolveLending binds the runtime's lending boundary and returns its
// transport: the generated client belongs to the lending provider's own
// package, which this package does not depend on. Every call remains subject
// to the runtime's abstraction.storage/lend gate on the engine named.
func (m *Machine) ResolveLending(ctx context.Context, need Requirements) (listen.FrameClient, error) {
	endpoint, err := m.resolve(ctx, "abstraction.storage", LendingContract, need)
	if err != nil {
		return listen.FrameClient{}, err
	}
	// A lend walks every store by stat and may copy a file that was asked for.
	return endpoint.WithDefaults(5*time.Minute, 1<<20), nil
}

// ResolveStorage binds one content reader. Each resource remains subject to
// the selected service's policy; resolution itself grants no content access.
func (m *Machine) ResolveStorage(ctx context.Context, need Requirements) (*storage.Client, error) {
	endpoint, err := m.resolve(ctx, "abstraction.storage", StorageContract, need)
	if err != nil {
		return nil, err
	}
	return storage.NewWithTransport(endpoint), nil
}

// ResolveStorageChanges binds one change observer. Observation and listing
// remain subject to the selected service's observe and per-object read policies.
func (m *Machine) ResolveStorageChanges(ctx context.Context, need Requirements) (*storage.Changes, error) {
	endpoint, err := m.resolve(ctx, "abstraction.storage", StorageChangesContract, need)
	if err != nil {
		return nil, err
	}
	return storage.NewChangesWithTransport(endpoint), nil
}

// ResolveStorageInventory binds one inventory reader. Every call remains
// subject to the selected service's inventory.read gate and its per-digest
// read policy.
func (m *Machine) ResolveStorageInventory(ctx context.Context, need Requirements) (*storage.Inventory, error) {
	endpoint, err := m.resolve(ctx, "abstraction.storage", StorageInventoryContract, need)
	if err != nil {
		return nil, err
	}
	return storage.NewInventoryWithTransport(endpoint), nil
}

// ResolveStorageWriter binds one content writer. Every Begin, Append and Commit
// remains subject to the selected service's write policy.
func (m *Machine) ResolveStorageWriter(ctx context.Context, need Requirements) (*storage.Writer, error) {
	endpoint, err := m.resolve(ctx, "abstraction.storage", StorageWriterContract, need)
	if err != nil {
		return nil, err
	}
	return storage.NewWriterWithTransport(endpoint), nil
}
