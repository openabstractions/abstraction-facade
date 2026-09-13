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
