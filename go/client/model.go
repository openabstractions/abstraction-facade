package client

import (
	"context"
	model "github.com/openabstractions/abstraction-model/go/client"
)

// ResolveModel selects one model lookup service. Its portable result can be
// submitted to a separately resolved download service through the job API.
func (m *Machine) ResolveModel(ctx context.Context, need Requirements) (*model.Client, error) {
	endpoint, err := m.resolve(ctx, "abstraction.model", "abstraction.model/resolver@1", need)
	if err != nil {
		return nil, err
	}
	return model.NewWithTransport(endpoint), nil
}
