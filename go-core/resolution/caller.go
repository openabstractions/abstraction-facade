package resolution

import (
	"context"
	"runtime"
	"strconv"

	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

// CallerService is the wire name of the resolver endpoint's caller echo.
const CallerService = "abstraction.facade/caller@1"

// ObserveCaller states what the receiving boundary bound for peer, in the shape
// of abstraction.facade/caller@1. A nil peer is unavailable. Account, program
// and pid are reported only at the proof listen.Program requires, the evidence
// every runtime-hosted local service acts on; the attribute list carries the
// proof actually established and this platform's ceiling for each.
func ObserveCaller(peer *identity.Peer) wire.CallerObservation {
	refused := wire.CallerObservation{Outcome: wire.CallerOutcomeUnavailable, PID: -1, Attributes: []wire.CallerAttribute{}}
	if peer == nil {
		return refused
	}
	user, err := peer.User.AtLeast(listen.Program.User)
	if err != nil {
		return refused
	}
	program, err := peer.Path.AtLeast(listen.Program.Path)
	if err != nil {
		return refused
	}
	process, err := peer.Process.AtLeast(listen.Program.Process)
	if err != nil {
		return refused
	}
	account := user.SID
	if user.Kind != "windows" {
		account = strconv.Itoa(user.UID)
	}
	limits, err := identity.CeilingFor(identity.Transport(peer.Transport))
	if err != nil {
		return refused
	}
	best := []identity.Proof{limits.Best.User, limits.Best.Process, limits.Best.Path, limits.Best.Package, limits.Best.Code}
	proofs := []identity.Proof{peer.User.Proof(), peer.Process.Proof(), peer.Path.Proof(), peer.Package.Proof(), peer.Code.Proof()}
	attributes := make([]wire.CallerAttribute, 0, len(proofs))
	for i, name := range []string{"user", "process", "path", "package", "code"} {
		attributes = append(attributes, wire.CallerAttribute{Attribute: name, Proof: proofs[i].String(), Ceiling: best[i].String()})
	}
	return wire.CallerObservation{Outcome: wire.CallerOutcomeObserved, Mechanism: "identity/" + runtime.GOOS,
		Account: account, Program: program, PID: int64(process.PID), Attributes: attributes,
		Platform: limits.Platform, Transport: limits.Transport, Bindable: limits.Bindable, Stronger: limits.Stronger}
}

type callerEcho struct{ call *listen.FramedCall }

// Observe rechecks the binding before echoing it.
func (c callerEcho) Observe() (wire.CallerObservation, error) {
	peer, err := c.call.Peer()
	if err != nil {
		return ObserveCaller(nil), nil
	}
	return ObserveCaller(peer), nil
}

// ObserveCaller asks the resolver endpoint how it bound this client, under the
// same server trust as Resolve. It changes nothing at the runtime.
func (c *Client) ObserveCaller(ctx context.Context) (wire.CallerObservation, error) {
	return wire.NewCallerClient(contextTransport{ctx, c.transport}).Observe()
}
