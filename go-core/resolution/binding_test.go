package resolution

import (
	"context"
	"errors"
	wire "github.com/openabstractions/abstraction-facade/go-core/go/abstraction/facade"
	"github.com/openabstractions/abstraction-identity/listen"
	"testing"
	"time"
)

func TestBindLocalTrustAndWaiting(t *testing.T) {
	ref := wire.ServiceReference{Scope: wire.ScopeLocal, Transport: LocalTransport, Endpoint: "selected", Provider: "untrusted-label"}
	expected := listen.ServerExpectation{Program: "independent"}
	transport, err := BindLocal(context.Background(), ref, &expected, nil)
	if err != nil || transport.Server == nil || transport.Server.Program != "independent" {
		t.Fatal(transport, err)
	}
	expected.Program = "mutated"
	if transport.Server.Program != "independent" {
		t.Fatal("expectation alias")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	policy := func(got context.Context, _ wire.ServiceReference) (listen.ServerExpectation, error) {
		actual, ok := got.Deadline()
		if !ok || actual != deadline {
			t.Error("shared absolute waiting budget changed")
		}
		cancel()
		return expected, nil
	}
	if _, err = BindLocal(ctx, ref, &expected, policy); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	calls := 0
	if _, err = BindLocal(ctx, ref, &expected, func(context.Context, wire.ServiceReference) (listen.ServerExpectation, error) {
		calls++
		return expected, nil
	}); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal(err, calls)
	}
	ref.Scope = wire.ScopeRemote
	if _, err = BindLocal(context.Background(), ref, &expected, nil); !errors.Is(err, ErrUnsupportedTransport) {
		t.Fatal(err)
	}
}
