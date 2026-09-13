package resolution

import (
	"context"
	"errors"
	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
	"github.com/openabstractions/abstraction-identity/listen"
)

// ProviderTrust selects independent configured trust for a validated reference.
// Reference metadata may select a policy entry; it supplies no identity proof.
type ProviderTrust func(context.Context, wire.ServiceReference) (listen.ServerExpectation, error)

var ErrUnsupportedTransport = errors.New("facade binding: unsupported_transport")

// BindLocal retains the installation's same-runtime program/principal profile,
// or an independently configured provider policy. A nil installation is reserved
// for explicitly selected endpoint-only compatibility. No connection is opened.
func BindLocal(ctx context.Context, ref wire.ServiceReference, installation *listen.ServerExpectation, policy ProviderTrust) (listen.FrameClient, error) {
	if err := ctx.Err(); err != nil {
		return listen.FrameClient{}, err
	}
	if ref.Scope != wire.ScopeLocal || ref.Transport != LocalTransport || ref.Endpoint == "" {
		return listen.FrameClient{}, ErrUnsupportedTransport
	}
	server := installation
	if policy != nil {
		expected, err := policy(ctx, ref)
		if err != nil {
			return listen.FrameClient{}, err
		}
		server = &expected
	}
	if err := ctx.Err(); err != nil {
		return listen.FrameClient{}, err
	}
	return (listen.FrameClient{Endpoint: ref.Endpoint, Server: server}).WithDefaults(0, 0), nil
}
