package client

import (
	"context"
	storage "github.com/openabstractions/abstraction-storage/go/client"
)

// ResolveStorage binds one content reader. Each resource remains subject to
// the selected service's policy; resolution itself grants no content access.
func (m *Machine) ResolveStorage(ctx context.Context, need Requirements) (*storage.Client, error) {
	endpoint, err := m.resolve(ctx, "abstraction.storage", "abstraction.storage/content-reader@1", need)
	if err != nil {
		return nil, err
	}
	return storage.NewWithTransport(endpoint), nil
}

// ResolveStorageChanges binds one change observer. Observation and listing
// remain subject to the selected service's observe and per-object read policies.
func (m *Machine) ResolveStorageChanges(ctx context.Context, need Requirements) (*storage.Changes, error) {
	endpoint, err := m.resolve(ctx, "abstraction.storage", "abstraction.storage/content-changes@1", need)
	if err != nil {
		return nil, err
	}
	return storage.NewChangesWithTransport(endpoint), nil
}

// ResolveStorageWriter binds one content writer. Every Begin, Append and Commit
// remains subject to the selected service's write policy.
func (m *Machine) ResolveStorageWriter(ctx context.Context, need Requirements) (*storage.Writer, error) {
	endpoint, err := m.resolve(ctx, "abstraction.storage", "abstraction.storage/content-writer@1", need)
	if err != nil {
		return nil, err
	}
	return storage.NewWriterWithTransport(endpoint), nil
}
