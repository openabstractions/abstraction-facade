// Package resolution preserves the original imports through the pure core.
package resolution

import (
	"context"
	core "github.com/openabstractions/abstraction-facade/go-core/resolution"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-identity/listen"
	"time"
)

type Candidate = core.Candidate
type Catalog = core.Catalog
type Authorize = core.Authorize
type Resolver = core.Resolver
type Host = core.Host
type PeerPolicy = core.PeerPolicy
type Client = core.Client

const LocalTransport = core.LocalTransport

func New(c []Candidate) (*Catalog, error)     { return core.New(c) }
func DefaultEndpoint() string                 { return core.DefaultEndpoint() }
func CheckedDefaultEndpoint() (string, error) { return core.CheckedDefaultEndpoint() }
func Listen(endpoint string, c *Catalog, p PeerPolicy) (*Host, error) {
	return core.Listen(endpoint, c, p)
}
func HandleConnection(ctx context.Context, c listen.Conn, catalog *Catalog, p PeerPolicy) error {
	return core.HandleConnection(ctx, c, catalog, p)
}
func NewClient(endpoint string, timeout time.Duration) *Client {
	return core.NewClient(endpoint, timeout)
}
func ValidateResult(request wire.ResolveRequest, result wire.ResolveResult) error {
	return core.ValidateResult(request, result)
}

func NewUnverifiedClient(endpoint string, timeout time.Duration) *Client {
	return core.NewUnverifiedClient(endpoint, timeout)
}
func NewVerifiedClient(endpoint string, timeout time.Duration, server listen.ServerExpectation) *Client {
	return core.NewVerifiedClient(endpoint, timeout, server)
}
