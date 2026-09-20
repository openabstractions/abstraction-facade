//go:build windows || linux

package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

// installedButStopped is default discovery with an installation selected and
// no runtime listening at its endpoint.
func installedButStopped(t *testing.T) (*Machine, bootstrap.Selection) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	principal, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	selected := bootstrap.Selection{Endpoint: unusedEndpoint(t, "activation"), Server: listen.ServerExpectation{Principal: identity.User{Kind: "windows", SID: principal.Uid}, Program: exe}}
	if runtime.GOOS == "linux" {
		selected.Server.Principal = identity.User{Kind: "posix", UID: os.Geteuid(), GID: -1}
	}
	machine := Discover()
	machine.selectInstalled = func(context.Context) (bootstrap.Selection, error) { return selected, nil }
	return machine, selected
}

// serveResolver answers one resolver exchange at endpoint, as an activated
// runtime would.
func serveResolver(t *testing.T, endpoint string) <-chan error {
	t.Helper()
	listener, err := listen.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		call, err := listen.ReceiveFramed(ctx, conn, listen.Program, 1<<20)
		if err != nil {
			done <- err
			return
		}
		defer call.Close()
		dispatcher := wire.ResolverDispatcher{Handler: selectionResolver{}}
		reply, err := dispatcher.ExchangeFrame(call.Frame)
		if err == nil {
			err = call.Reply(reply)
		}
		done <- err
	}()
	return done
}

func TestDiscoveryActivatesAnInstalledButStoppedRuntimeOnce(t *testing.T) {
	machine, selected := installedButStopped(t)
	var activations atomic.Int32
	var served <-chan error
	machine.activate = func(ctx context.Context, selection bootstrap.Selection) error {
		activations.Add(1)
		if selection.Server.Program != selected.Server.Program {
			t.Errorf("activated %q, want the selected installation %q", selection.Server.Program, selected.Server.Program)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("activation has no budget")
		}
		served = serveResolver(t, selected.Endpoint)
		return nil
	}
	if _, err := machine.ResolveLog(context.Background(), Requirements{}); err != nil {
		t.Fatal(err)
	}
	if activations.Load() != 1 {
		t.Fatalf("activations %d", activations.Load())
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryReportsAnUpgradeInProgressAsItsOwnStatus(t *testing.T) {
	machine, _ := installedButStopped(t)
	refusal := fmt.Errorf("%w: start refused: an upgrade of this installation is in progress", bootstrap.ErrUpgradeInProgress)
	calls := 0
	machine.activate = func(context.Context, bootstrap.Selection) error { calls++; return refusal }
	_, err := machine.ResolveLog(context.Background(), Requirements{})
	var refused *ResolutionError
	if !errors.As(err, &refused) || refused.Status != UpgradeInProgress || !errors.Is(err, bootstrap.ErrUpgradeInProgress) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	if _, ok := refused.Refusal(); ok {
		t.Fatal("upgrade_in_progress reported as a resolver refusal")
	}
}

func TestDiscoveryReportsARefusedActivationAsRuntimeUnavailable(t *testing.T) {
	machine, _ := installedButStopped(t)
	calls := 0
	machine.activate = func(context.Context, bootstrap.Selection) error {
		calls++
		return fmt.Errorf("%w: the caller's token is elevated", bootstrap.ErrActivationRefused)
	}
	_, err := machine.ResolveLog(context.Background(), Requirements{})
	resolutionError(t, err, RuntimeUnavailable, "the installed runtime at "+machineEndpoint(t, machine))
	if !errors.Is(err, bootstrap.ErrActivationRefused) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func machineEndpoint(t *testing.T, m *Machine) string {
	selected, err := m.selectInstalled(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return selected.Endpoint
}

// Nothing installed is runtime_unavailable, and nothing is started.
func TestDiscoveryWithNoInstallationActivatesNothing(t *testing.T) {
	machine := Discover()
	machine.selectInstalled = func(context.Context) (bootstrap.Selection, error) {
		return bootstrap.Selection{}, bootstrap.ErrNoTrustedInstallation
	}
	machine.activate = func(context.Context, bootstrap.Selection) error {
		t.Fatal("activated with no installation")
		return nil
	}
	_, err := machine.ResolveLog(context.Background(), Requirements{})
	resolutionError(t, err, RuntimeUnavailable, "the installed runtime")
	if !errors.Is(err, bootstrap.ErrNoTrustedInstallation) {
		t.Fatal(err)
	}
}

// Explicit endpoints are the caller's choice; an absent one is reported as is.
func TestExplicitEndpointsAreNeverActivated(t *testing.T) {
	_, selected := installedButStopped(t)
	for name, machine := range map[string]*Machine{
		"verified":   NewVerified(selected.Endpoint, selected.Server),
		"unverified": NewUnverified(selected.Endpoint),
	} {
		machine.activate = func(context.Context, bootstrap.Selection) error { t.Fatalf("%s endpoint activated", name); return nil }
		_, err := machine.ResolveLog(context.Background(), Requirements{})
		resolutionError(t, err, RuntimeUnavailable, "the explicit endpoint "+selected.Endpoint)
	}
}
