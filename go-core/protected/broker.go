package protected

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

// State is where one ticket stands (FAC-H2).
type State int

const (
	// StateUnknown: no ticket for that request, or one bound to another.
	StateUnknown State = iota
	// StateAsked: registered; the person has not said once, or the helper
	// has not claimed yet.
	StateAsked
	// StateClaimed: the helper holds the frozen request.
	StateClaimed
	// StateDelivered: Take returned the helper's result, this one time.
	StateDelivered
	// StateFailed: Take returned the helper's failure, this one time.
	StateFailed
)

func (s State) String() string {
	switch s {
	case StateAsked:
		return "asked"
	case StateClaimed:
		return "claimed"
	case StateDelivered:
		return "delivered"
	case StateFailed:
		return "failed"
	}
	return "unknown"
}

type phase int

const (
	phaseAsked phase = iota
	phaseClaimed
	phaseFetched
)

// Approval reads, at claim time, when the person recorded once on the
// system-access question whose ID is ticket. It returns an error when the
// question is not answered once by the person through the operator.
type Approval func(ctx context.Context, ticket string) (answered time.Time, err error)

// Verify admits a helper connection and names its process (identity ID-H1).
type Verify func(*identity.Peer) (identity.Process, error)

// HelperVerifier is the runtime's Verify: helperPath is the installed helper
// TrustedHelper returned, and self is the runtime's own principal.
func HelperVerifier(helperPath string, self identity.User) Verify {
	return func(p *identity.Peer) (identity.Process, error) {
		if err := identity.CheckElevatedHelper(p, self, helperPath); err != nil {
			return identity.Process{}, err
		}
		return p.Process.AtLeast(identity.ProofKernel)
	}
}

type ticket struct {
	operation, subject string
	request            []byte
	registered         time.Time
	phase              phase
	claimedBy          identity.Process
	claimed            time.Time
	result             []byte
	failure            string
	fetched            time.Time
	// drop ends a fetched result HoldWindow after it arrived, whether or not
	// anything else calls the broker again.
	drop *time.Timer
}

// end clears the result's bytes and stops its timer.
func (t *ticket) end() {
	if t.drop != nil {
		t.drop.Stop()
	}
	clear(t.result)
	t.result = nil
}

// Broker is the runtime's half of a protected operation (FAC-H2). It keeps
// every ticket in memory only.
type Broker struct {
	approve Approval
	verify  Verify
	now     func() time.Time
	hold    time.Duration
	mu      sync.Mutex
	tickets map[string]*ticket
	slots   chan struct{}
}

// NewBroker returns a Broker that reads the person's answer with approve and
// admits helpers with verify.
func NewBroker(approve Approval, verify Verify) *Broker {
	return &Broker{approve: approve, verify: verify, now: time.Now, hold: HoldWindow, tickets: map[string]*ticket{}, slots: make(chan struct{}, 4)}
}

// Room reports whether a new ticket fits now (MaxTickets). The runtime
// asks it before it admits a system-access question, so no question waits
// on a ticket that cannot be registered.
func (b *Broker) Room() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	if len(b.tickets) >= MaxTickets {
		return fmt.Errorf("%w: %d protected requests are open", ErrUnavailable, MaxTickets)
	}
	return nil
}

// Register binds the ticket, the system-access question's ID, to the frozen
// request of operation for subject. Registering the same binding again is a
// no-op; a different binding under the same ticket is refused.
func (b *Broker) Register(id, operation, subject string, request []byte) error {
	if !ValidTicket(id) || !ValidOperation(operation) || subject == "" || len(request) == 0 || len(request) > maxRequest {
		return fmt.Errorf("%w: ticket, operation, subject or request out of bounds", ErrRefused)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	if t := b.tickets[id]; t != nil {
		if t.operation == operation && t.subject == subject && bytes.Equal(t.request, request) {
			return nil
		}
		return fmt.Errorf("%w: ticket bound to another request", ErrRefused)
	}
	if len(b.tickets) >= MaxTickets {
		return fmt.Errorf("%w: %d protected requests are open", ErrUnavailable, MaxTickets)
	}
	b.tickets[id] = &ticket{operation: operation, subject: subject, request: bytes.Clone(request), registered: b.now()}
	return nil
}

// Take reports the ticket's state for exactly this binding. A fetched result
// is returned once, as StateDelivered or StateFailed, and the ticket ends.
func (b *Broker) Take(id, operation, subject string, request []byte) (State, []byte, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	t := b.tickets[id]
	if t == nil || t.operation != operation || t.subject != subject || !bytes.Equal(t.request, request) {
		return StateUnknown, nil, ""
	}
	switch t.phase {
	case phaseAsked:
		return StateAsked, nil, ""
	case phaseClaimed:
		return StateClaimed, nil, ""
	}
	delete(b.tickets, id)
	result := bytes.Clone(t.result)
	t.end()
	if t.failure != "" {
		return StateFailed, nil, t.failure
	}
	return StateDelivered, result, ""
}

// Forget ends a ticket in any state.
func (b *Broker) Forget(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t := b.tickets[id]; t != nil {
		t.end()
		delete(b.tickets, id)
	}
	b.pruneLocked()
}

// pruneLocked runs at every entry point: an asked ticket lives at most
// ticketAge, and a fetched result at most hold.
func (b *Broker) pruneLocked() {
	now := b.now()
	for id, t := range b.tickets {
		if now.Sub(t.registered) > ticketAge || t.phase == phaseFetched && now.Sub(t.fetched) > b.hold {
			t.end()
			delete(b.tickets, id)
		}
	}
}

// expire is the fetched result's timer: it ends the ticket when it still
// holds that result.
func (b *Broker) expire(id string, t *ticket) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tickets[id] == t && t.phase == phaseFetched {
		t.end()
		delete(b.tickets, id)
	}
}

func refusal(why string) []byte {
	//unchecked: a struct of plain fields, which json.Marshal cannot fail on
	out, _ := json.Marshal(reply{Version: version, Refused: why})
	return out
}

// Exchange answers one frame from the peer that sent it. It never returns
// an error: every refusal is a reply the helper reads.
func (b *Broker) Exchange(ctx context.Context, peer *identity.Peer, frame []byte) []byte {
	b.mu.Lock()
	b.pruneLocked()
	b.mu.Unlock()
	process, err := b.verify(peer)
	if err != nil {
		return refusal("not this account's installed elevated helper")
	}
	var m message
	if err := decodeStrict(frame, &m); err != nil || m.Version != version || !ValidOperation(m.Operation) || !ValidTicket(m.Ticket) {
		return refusal("malformed message")
	}
	switch m.Kind {
	case kindClaim:
		if len(m.Result) != 0 || m.Failure != "" {
			return refusal("malformed claim")
		}
		return b.claim(ctx, process, m)
	case kindComplete:
		return b.complete(process, m)
	}
	return refusal("malformed message")
}

func (b *Broker) claim(ctx context.Context, process identity.Process, m message) []byte {
	b.mu.Lock()
	t := b.tickets[m.Ticket]
	if t == nil || t.operation != m.Operation || t.phase != phaseAsked {
		b.mu.Unlock()
		return refusal("no such request waiting")
	}
	b.mu.Unlock()
	answered, err := b.approve(ctx, m.Ticket)
	if err != nil {
		return refusal("the person has not allowed this request")
	}
	now := b.now()
	if answered.After(now.Add(time.Second)) || now.Sub(answered) > ClaimWindow {
		return refusal("the approval is outside its window")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tickets[m.Ticket] != t || t.phase != phaseAsked {
		return refusal("no such request waiting")
	}
	t.phase, t.claimedBy, t.claimed = phaseClaimed, process, now
	//unchecked: a struct of plain fields, which json.Marshal cannot fail on
	out, _ := json.Marshal(reply{Version: version, Request: bytes.Clone(t.request)})
	return out
}

func (b *Broker) complete(process identity.Process, m message) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.tickets[m.Ticket]
	if t == nil || t.operation != m.Operation || t.phase != phaseClaimed {
		return refusal("no such request claimed")
	}
	if process.PID != t.claimedBy.PID || !process.StartTime.Equal(t.claimedBy.StartTime) {
		return refusal("another process claimed this request")
	}
	now := b.now()
	if now.Sub(t.claimed) > HelperDeadline+15*time.Second {
		delete(b.tickets, m.Ticket)
		return refusal("the helper's deadline passed")
	}
	switch {
	case m.Failure == "" && len(m.Result) > 0 && len(m.Result) <= maxResult:
	case (m.Failure == FailureDenied || m.Failure == FailureFailed) && len(m.Result) == 0:
	default:
		return refusal("malformed completion")
	}
	t.phase, t.result, t.failure, t.fetched = phaseFetched, bytes.Clone(m.Result), m.Failure, now
	id := m.Ticket
	t.drop = time.AfterFunc(b.hold, func() { b.expire(id, t) })
	//unchecked: a struct of plain fields, which json.Marshal cannot fail on
	out, _ := json.Marshal(reply{Version: version})
	return out
}

// Serve answers helper connections on l until ctx ends, at most four at a
// time; it closes l when ctx ends.
func (b *Broker) Serve(ctx context.Context, l listen.Listener) error {
	stop := context.AfterFunc(ctx, func() { l.Close() })
	defer stop()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case b.slots <- struct{}{}:
		default:
			c.Close()
			continue
		}
		go func() {
			defer func() { <-b.slots }()
			b.serveConn(ctx, c)
		}()
	}
}

func (b *Broker) serveConn(parent context.Context, c listen.Conn) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	call, err := listen.ReceiveFramed(ctx, c, listen.Program, MaxFrame)
	if err != nil {
		return
	}
	defer call.Close()
	peer, err := call.Peer()
	out := refusal("caller identity unavailable")
	if err == nil {
		out = b.Exchange(ctx, peer, call.Frame)
	}
	//unchecked: a helper that left before its reply has nothing to read; its ticket keeps its state
	call.Reply(out)
}
