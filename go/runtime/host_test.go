package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	configwire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	client "github.com/openabstractions/abstraction-facade/go/client"
	"github.com/openabstractions/abstraction-facade/go/resolution"
	logging "github.com/openabstractions/abstraction-logging/go"
)

type sink struct {
	records          chan logging.Record
	entered, release chan struct{}
}

func (s sink) Write(r logging.Record) error {
	if r.Msg == "blocked" {
		close(s.entered)
		<-s.release
	}
	s.records <- r
	return nil
}

func TestResolvedRuntime(t *testing.T) {
	dir, err := os.MkdirTemp("", "oa-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	endpoint := func(name string) string {
		if runtime.GOOS == "windows" {
			return fmt.Sprintf(`\\.\pipe\oa-runtime-test-%d-%s`, time.Now().UnixNano(), name)
		}
		return filepath.Join(dir, name+".sock")
	}
	out := sink{make(chan logging.Record, 4), make(chan struct{}), make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(out.release) }) }
	options := Options{Endpoint: endpoint("runtime"), LogEndpoint: endpoint("selected-log"), ConfigEndpoint: endpoint("selected-config"), Sink: out}
	h, err := Listen(options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx) }()
	defer func() {
		release()
		cancel()
		h.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("runtime did not stop")
		}
	}()
	m := client.New(options.Endpoint)
	log, err := m.ResolveLog(ctx, client.Requirements{})
	if runtime.GOOS == "darwin" {
		if err == nil {
			t.Fatal("unexpected successful Program-bound resolution")
		}
		t.Log("UNPROVEN successful runtime resolution on Darwin; current Program proof refusal retained")
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Log(20, "selected endpoint", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-out.records:
		if r.Msg != "selected endpoint" {
			t.Fatalf("wrong record: %+v", r)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	config, err := m.ResolveConfig(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = config.ReadWithOverrides(configwire.RunOverrides{}); err != nil {
		t.Fatal(err)
	}
	if probe := os.Getenv("OA_CPP_RESOLUTION_PROBE"); probe != "" {
		output, err := exec.CommandContext(ctx, probe, "--runtime", options.Endpoint).CombinedOutput()
		if err != nil {
			t.Fatalf("C++ installed consumer: %v\n%s", err, output)
		}
		select {
		case record := <-out.records:
			if record.Msg != "cpp-resolved-log" {
				t.Fatalf("C++ delivered wrong record: %+v", record)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		t.Logf("installed C++ consumer → Go runtime: %s", output)
	} else {
		t.Log("C++ consumer not requested; this run verifies Go runtime clients only")
	}
	_, err = m.ResolveRouter(ctx, client.Requirements{})
	var refused *client.BindingError
	if !errors.As(err, &refused) || refused.Status != wire.ResolutionStatusUnavailable {
		t.Fatalf("missing router: %v", err)
	}
	_, err = m.ResolveLog(ctx, client.Requirements{Guarantees: []string{"not-promised"}})
	if !errors.As(err, &refused) || refused.Status != wire.ResolutionStatusUnmetRequirements {
		t.Fatalf("weakened demands: %v", err)
	}
	// Keep the installed binding stable: a stopped provider is not replaced by
	// another provider or a local file sink, while the config sibling still works.
	blockedCall := make(chan error, 1)
	go func() { blockedCall <- log.Log(20, "blocked", nil) }()
	select {
	case <-out.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	h.logging.Close()
	deadline := time.Now().Add(time.Second)
	for {
		_, err = m.ResolveLog(ctx, client.Requirements{})
		if errors.As(err, &refused) && refused.Status == wire.ResolutionStatusNotReady {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stopped host still ready: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	if err := log.Log(20, "must fail", nil); err == nil {
		t.Fatal("stale binding succeeded")
	}
	if _, err = config.ReadWithOverrides(configwire.RunOverrides{}); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case <-blockedCall:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := h.resolver.Update(nil); err == nil {
		t.Fatal("accepted nil catalogue")
	}
	if _, err := resolution.Listen(endpoint("invalid"), nil, nil); err == nil {
		t.Fatal("accepted missing policy")
	}
}
