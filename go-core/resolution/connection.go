package resolution

import (
	"context"
	"slices"
	"time"

	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

// PeerPolicy makes registration access decisions using receiving-side evidence.
// The host supplies it; the wire request has no field for an asserted principal.
type PeerPolicy func(*identity.Peer, wire.ServiceReference) bool

// HandleConnection serves one bounded, identity-bound resolution exchange.
// The hosting runtime owns activation, the listener and catalogue refresh.
func HandleConnection(ctx context.Context, conn listen.Conn, catalog *Catalog, policy PeerPolicy) error {
	call, err := listen.ReceiveFramed(ctx, conn, listen.Program, 1<<20)
	if err != nil {
		return err
	}
	defer call.Close()
	peer, err := call.Peer()
	if err != nil {
		return err
	}
	resolver := catalog.ForCaller(func(ref wire.ServiceReference) bool {
		return policy != nil && policy(peer, ref)
	})
	dispatch := wire.ResolverDispatcher{Handler: resolver}
	reply, err := dispatch.ExchangeFrame(call.Frame)
	if err != nil {
		return err
	}
	return call.Reply(reply)
}

// Client resolves through the shared transport. An endpoint is supplied by the
// runtime bootstrap; this constructor neither invents a registry nor authenticates
// a remote server. Returned references still require service-boundary checks.
type Client struct{ transport listen.FrameClient }

// NewClient preserves the original endpoint-only, unverified transport.
// Deprecated: use NewVerifiedClient, or name compatibility with NewUnverifiedClient.
func NewClient(endpoint string, timeout time.Duration) *Client {
	return NewUnverifiedClient(endpoint, timeout)
}

// NewUnverifiedClient explicitly selects endpoint-only compatibility.
func NewUnverifiedClient(endpoint string, timeout time.Duration) *Client {
	return &Client{transport: listen.FrameClient{Endpoint: endpoint, Timeout: timeout, MaxFrame: 1 << 20}}
}

// NewVerifiedClient requires independent server trust evidence. It validates
// that evidence at the shared connection boundary before any resolver bytes.
// This authenticates the resolver only; returned providers need their own checks.
func NewVerifiedClient(endpoint string, timeout time.Duration, server listen.ServerExpectation) *Client {
	if server.Process != nil {
		process := *server.Process
		server.Process = &process
	}
	return &Client{transport: listen.FrameClient{Endpoint: endpoint, Timeout: timeout, MaxFrame: 1 << 20, Server: &server}}
}

type contextTransport struct {
	ctx       context.Context
	transport listen.FrameClient
}

func (t contextTransport) ExchangeFrame(frame []byte) ([]byte, error) {
	return t.transport.ExchangeFrameContext(t.ctx, frame)
}

func (c *Client) Resolve(ctx context.Context, request wire.ResolveRequest) (wire.ResolveResult, error) {
	result, err := wire.NewResolverClient(contextTransport{ctx, c.transport}).Resolve(request)
	if err != nil {
		return result, err
	}
	if err := ValidateResult(request, result); err != nil {
		return wire.ResolveResult{}, err
	}
	return result, nil
}

// ValidateResult checks cross-field meaning as well as generated wire shape.
// A resolver cannot send a weaker reference disguised as a successful selection.
func ValidateResult(request wire.ResolveRequest, result wire.ResolveResult) error {
	if !slices.Contains(wire.ResolutionStatusNames, result.Status) {
		return invalidResult("unknown result status")
	}
	if result.Status != wire.ResolutionStatusResolved {
		if result.Reference != nil {
			return invalidResult("refusal contains a service reference")
		}
		return nil
	}
	ref := result.Reference
	if ref == nil {
		return invalidResult("resolved result lacks a service reference")
	}
	if _, err := New([]Candidate{{Reference: *ref}}); err != nil {
		return invalidResult("invalid service reference")
	}
	if ref.Capability != request.Capability || !slices.Contains(request.Contracts, ref.Contract) ||
		(request.Scope != wire.ScopeAny && ref.Scope != request.Scope) || !containsAll(ref.Guarantees, request.Guarantees) {
		return invalidResult("service reference does not satisfy the request")
	}
	return nil
}

func invalidResult(message string) error {
	return &wire.ServiceError{Code: "invalid_resolution", Message: message}
}
