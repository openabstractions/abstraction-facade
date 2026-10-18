package protected

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	identity "github.com/openabstractions/abstraction-identity"
)

const (
	testTicket  = "0123456789abcdef0123456789abcdef"
	testSubject = `C:\Apps\reader.exe`
)

var testRequest = []byte(`{"source":"windows/security"}`)

type fakeHelpers struct {
	now      time.Time
	process  identity.Process
	refuse   bool
	answered map[string]time.Time
}

func newFake() (*fakeHelpers, *Broker) {
	f := &fakeHelpers{now: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), process: identity.Process{PID: 100, StartTime: time.Unix(10, 0)}, answered: map[string]time.Time{}}
	b := NewBroker(func(_ context.Context, id string) (time.Time, error) {
		at, ok := f.answered[id]
		if !ok {
			return time.Time{}, errors.New("not answered once")
		}
		return at, nil
	}, func(*identity.Peer) (identity.Process, error) {
		if f.refuse {
			return identity.Process{}, identity.ErrNotHelper
		}
		return f.process, nil
	})
	b.now = func() time.Time { return f.now }
	return f, b
}

func send(t *testing.T, b *Broker, m message) reply {
	t.Helper()
	frame, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var r reply
	if err := decodeStrict(b.Exchange(context.Background(), &identity.Peer{}, frame), &r); err != nil || r.Version != version {
		t.Fatalf("reply: %+v %v", r, err)
	}
	return r
}

func claimOf(ticket string) message {
	return message{Version: version, Kind: kindClaim, Operation: OperationSecurityLogPage, Ticket: ticket}
}

func completeOf(ticket string, result []byte, failure string) message {
	return message{Version: version, Kind: kindComplete, Operation: OperationSecurityLogPage, Ticket: ticket, Result: result, Failure: failure}
}

// TestBrokerLifecycle is FAC-H2's happy path: asked, claimed after the
// person's once, fetched from the claimer, delivered once to the owner.
func TestBrokerLifecycle(t *testing.T) {
	f, b := newFake()
	if err := b.Register(testTicket, OperationSecurityLogPage, testSubject, testRequest); err != nil {
		t.Fatal(err)
	}
	if err := b.Register(testTicket, OperationSecurityLogPage, testSubject, testRequest); err != nil {
		t.Fatalf("the same binding again: %v", err)
	}
	if state, _, _ := b.Take(testTicket, OperationSecurityLogPage, testSubject, testRequest); state != StateAsked {
		t.Fatalf("state %s", state)
	}
	if r := send(t, b, claimOf(testTicket)); r.Refused == "" {
		t.Fatal("claimed before the person said once")
	}
	f.answered[testTicket] = f.now.Add(-10 * time.Second)
	r := send(t, b, claimOf(testTicket))
	if r.Refused != "" || !bytes.Equal(r.Request, testRequest) {
		t.Fatalf("claim: %+v", r)
	}
	if state, _, _ := b.Take(testTicket, OperationSecurityLogPage, testSubject, testRequest); state != StateClaimed {
		t.Fatalf("state %s", state)
	}
	if r := send(t, b, claimOf(testTicket)); r.Refused == "" {
		t.Fatal("a second claim succeeded")
	}
	claimer := f.process
	f.process = identity.Process{PID: 101, StartTime: claimer.StartTime}
	if r := send(t, b, completeOf(testTicket, []byte("page"), "")); r.Refused == "" {
		t.Fatal("another process completed the claim")
	}
	f.process = identity.Process{PID: claimer.PID, StartTime: claimer.StartTime.Add(time.Second)}
	if r := send(t, b, completeOf(testTicket, []byte("page"), "")); r.Refused == "" {
		t.Fatal("a recycled pid completed the claim")
	}
	f.process = claimer
	if r := send(t, b, completeOf(testTicket, []byte("page"), "")); r.Refused != "" {
		t.Fatalf("completion: %s", r.Refused)
	}
	if r := send(t, b, completeOf(testTicket, []byte("again"), "")); r.Refused == "" {
		t.Fatal("a second completion succeeded")
	}
	for name, take := range map[string][3]string{
		"another subject":   {OperationSecurityLogPage, `C:\Apps\other.exe`, string(testRequest)},
		"another request":   {OperationSecurityLogPage, testSubject, `{"source":"windows/system"}`},
		"another operation": {"security-log-page-2", testSubject, string(testRequest)},
	} {
		if state, result, _ := b.Take(testTicket, take[0], take[1], []byte(take[2])); state != StateUnknown || result != nil {
			t.Fatalf("%s took %s %q", name, state, result)
		}
	}
	state, result, failure := b.Take(testTicket, OperationSecurityLogPage, testSubject, testRequest)
	if state != StateDelivered || string(result) != "page" || failure != "" {
		t.Fatalf("take: %s %q %q", state, result, failure)
	}
	if state, _, _ := b.Take(testTicket, OperationSecurityLogPage, testSubject, testRequest); state != StateUnknown {
		t.Fatalf("delivered twice: %s", state)
	}
}

// TestBrokerRefusals is FAC-H2's refusals: helper identity, grammar, the
// claim window, the helper deadline, the hold window, binding and capacity.
func TestBrokerRefusals(t *testing.T) {
	t.Run("not the helper", func(t *testing.T) {
		f, b := newFake()
		mustRegister(t, b, testTicket)
		f.answered[testTicket] = f.now
		f.refuse = true
		if r := send(t, b, claimOf(testTicket)); !strings.Contains(r.Refused, "helper") {
			t.Fatalf("refusal %q", r.Refused)
		}
		if state, _, _ := b.Take(testTicket, OperationSecurityLogPage, testSubject, testRequest); state != StateAsked {
			t.Fatalf("a refused peer moved the ticket: %s", state)
		}
	})
	t.Run("malformed", func(t *testing.T) {
		f, b := newFake()
		mustRegister(t, b, testTicket)
		f.answered[testTicket] = f.now
		for _, frame := range []string{
			``, `{}`, `not json`,
			`{"version":2,"kind":"claim","operation":"security-log-page","ticket":"` + testTicket + `"}`,
			`{"version":1,"kind":"claim","operation":"security-log-page","ticket":"` + testTicket + `","extra":1}`,
			`{"version":1,"kind":"claim","operation":"read-anything","ticket":"` + testTicket + `"}`,
			`{"version":1,"kind":"claim","operation":"security-log-page","ticket":"ABC"}`,
			`{"version":1,"kind":"run","operation":"security-log-page","ticket":"` + testTicket + `"}`,
			`{"version":1,"kind":"claim","operation":"security-log-page","ticket":"` + testTicket + `","failure":"denied"}`,
			`{"version":1,"kind":"claim","operation":"security-log-page","ticket":"` + testTicket + `"} {}`,
		} {
			var r reply
			if err := decodeStrict(b.Exchange(context.Background(), &identity.Peer{}, []byte(frame)), &r); err != nil || r.Refused == "" {
				t.Fatalf("%q answered %+v %v", frame, r, err)
			}
		}
		if state, _, _ := b.Take(testTicket, OperationSecurityLogPage, testSubject, testRequest); state != StateAsked {
			t.Fatalf("a malformed frame moved the ticket: %s", state)
		}
	})
	t.Run("claim window", func(t *testing.T) {
		f, b := newFake()
		mustRegister(t, b, testTicket)
		f.answered[testTicket] = f.now.Add(-ClaimWindow - time.Second)
		if r := send(t, b, claimOf(testTicket)); r.Refused == "" {
			t.Fatal("claimed after the window")
		}
		f.answered[testTicket] = f.now.Add(time.Minute)
		if r := send(t, b, claimOf(testTicket)); r.Refused == "" {
			t.Fatal("claimed with an answer from the future")
		}
		if r := send(t, b, claimOf("ffffffffffffffffffffffffffffffff")); r.Refused == "" {
			t.Fatal("claimed an unregistered ticket")
		}
	})
	t.Run("helper deadline", func(t *testing.T) {
		f, b := newFake()
		mustRegister(t, b, testTicket)
		f.answered[testTicket] = f.now
		if r := send(t, b, claimOf(testTicket)); r.Refused != "" {
			t.Fatal(r.Refused)
		}
		f.now = f.now.Add(HelperDeadline + 16*time.Second)
		if r := send(t, b, completeOf(testTicket, []byte("late"), "")); r.Refused == "" {
			t.Fatal("completed after the deadline")
		}
		if state, _, _ := b.Take(testTicket, OperationSecurityLogPage, testSubject, testRequest); state != StateUnknown {
			t.Fatalf("a late ticket stayed: %s", state)
		}
	})
	t.Run("completions", func(t *testing.T) {
		f, b := newFake()
		mustRegister(t, b, testTicket)
		f.answered[testTicket] = f.now
		send(t, b, claimOf(testTicket))
		for _, bad := range []message{
			completeOf(testTicket, nil, ""),
			completeOf(testTicket, []byte("x"), FailureDenied),
			completeOf(testTicket, nil, "exploded"),
			completeOf(testTicket, bytes.Repeat([]byte("x"), maxResult+1), ""),
		} {
			if r := send(t, b, bad); r.Refused == "" {
				t.Fatalf("accepted %+v", bad.Failure)
			}
		}
		if r := send(t, b, completeOf(testTicket, nil, FailureDenied)); r.Refused != "" {
			t.Fatal(r.Refused)
		}
		state, result, failure := b.Take(testTicket, OperationSecurityLogPage, testSubject, testRequest)
		if state != StateFailed || result != nil || failure != FailureDenied {
			t.Fatalf("take: %s %q %q", state, result, failure)
		}
	})
	t.Run("hold window", func(t *testing.T) {
		f, b := newFake()
		mustRegister(t, b, testTicket)
		f.answered[testTicket] = f.now
		send(t, b, claimOf(testTicket))
		send(t, b, completeOf(testTicket, []byte("page"), ""))
		f.now = f.now.Add(HoldWindow + time.Second)
		if state, result, _ := b.Take(testTicket, OperationSecurityLogPage, testSubject, testRequest); state != StateUnknown || result != nil {
			t.Fatalf("a stale result was delivered: %s %q", state, result)
		}
	})
	t.Run("hold window swept at every entry point", func(t *testing.T) {
		f, b := newFake()
		mustRegister(t, b, testTicket)
		f.answered[testTicket] = f.now
		send(t, b, claimOf(testTicket))
		send(t, b, completeOf(testTicket, []byte("page"), ""))
		held := b.tickets[testTicket].result
		f.now = f.now.Add(HoldWindow + time.Second)
		// A helper's malformed frame is the only later call: no Register, no Take.
		b.Exchange(context.Background(), &identity.Peer{}, []byte("{}"))
		b.mu.Lock()
		left := len(b.tickets)
		b.mu.Unlock()
		if left != 0 || string(held) == "page" {
			t.Fatalf("a stale result outlived its window: %d tickets, bytes %q", left, held)
		}
	})
	t.Run("hold window by timer with no later call", func(t *testing.T) {
		f, b := newFake()
		b.hold = 50 * time.Millisecond
		mustRegister(t, b, testTicket)
		f.answered[testTicket] = f.now
		send(t, b, claimOf(testTicket))
		send(t, b, completeOf(testTicket, []byte("page"), ""))
		b.mu.Lock()
		held := b.tickets[testTicket].result
		b.mu.Unlock()
		deadline := time.Now().Add(5 * time.Second)
		for {
			b.mu.Lock()
			left := len(b.tickets)
			b.mu.Unlock()
			if left == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the fetched result outlived its window with no later call")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if string(held) == "page" {
			t.Fatalf("the dropped result's bytes remain: %q", held)
		}
	})
	t.Run("binding and capacity", func(t *testing.T) {
		f, b := newFake()
		mustRegister(t, b, testTicket)
		if err := b.Register(testTicket, OperationSecurityLogPage, testSubject, []byte(`{"source":"other"}`)); !errors.Is(err, ErrRefused) {
			t.Fatalf("a second binding under one ticket: %v", err)
		}
		for _, bad := range [][3]string{{"XYZ", OperationSecurityLogPage, testSubject}, {testTicket + "0", "read-anything", testSubject}, {testTicket + "1", OperationSecurityLogPage, ""}} {
			if err := b.Register(bad[0], bad[1], bad[2], testRequest); !errors.Is(err, ErrRefused) {
				t.Fatalf("registered %v: %v", bad, err)
			}
		}
		if err := b.Register(testTicket+"2", OperationSecurityLogPage, testSubject, bytes.Repeat([]byte("x"), maxRequest+1)); !errors.Is(err, ErrRefused) {
			t.Fatalf("an oversized request: %v", err)
		}
		for i := 1; i < MaxTickets; i++ {
			mustRegister(t, b, fmt.Sprintf("%032x", i))
		}
		if err := b.Register(fmt.Sprintf("%032x", MaxTickets), OperationSecurityLogPage, testSubject, testRequest); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("ticket %d: %v", MaxTickets+1, err)
		}
		f.now = f.now.Add(ticketAge + time.Second)
		mustRegister(t, b, fmt.Sprintf("%032x", MaxTickets))
		b.Forget(fmt.Sprintf("%032x", MaxTickets))
		if state, _, _ := b.Take(fmt.Sprintf("%032x", MaxTickets), OperationSecurityLogPage, testSubject, testRequest); state != StateUnknown {
			t.Fatalf("a forgotten ticket: %s", state)
		}
	})
}

func mustRegister(t *testing.T, b *Broker, id string) {
	t.Helper()
	if err := b.Register(id, OperationSecurityLogPage, testSubject, testRequest); err != nil {
		t.Fatal(err)
	}
}

func TestHelperArgumentGrammar(t *testing.T) {
	args, err := HelperArguments(OperationSecurityLogPage, testTicket)
	if err != nil || strings.Join(args, " ") != "security-log-page --ticket "+testTicket {
		t.Fatalf("%q %v", args, err)
	}
	op, ticket, err := ParseHelperArguments(args)
	if err != nil || op != OperationSecurityLogPage || ticket != testTicket {
		t.Fatalf("%q %q %v", op, ticket, err)
	}
	for _, bad := range [][]string{
		nil,
		{OperationSecurityLogPage},
		{OperationSecurityLogPage, "--ticket"},
		{OperationSecurityLogPage, "--ticket", testTicket, "extra"},
		{"cmd.exe", "--ticket", testTicket},
		{OperationSecurityLogPage, "-ticket", testTicket},
		{OperationSecurityLogPage, "--ticket", "ABCDEF"},
		{OperationSecurityLogPage, "--ticket", "abc def"},
		{OperationSecurityLogPage, "--ticket", "abc\"def"},
		{OperationSecurityLogPage, "--ticket", strings.Repeat("a", 129)},
		{OperationSecurityLogPage, "--ticket", ""},
	} {
		if _, _, err := ParseHelperArguments(bad); !errors.Is(err, ErrRefused) {
			t.Fatalf("%q parsed: %v", bad, err)
		}
	}
	if _, err := HelperArguments("security-log-page --other", testTicket); !errors.Is(err, ErrRefused) {
		t.Fatalf("a spliced operation: %v", err)
	}
}
