package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	casapi "github.com/openabstractions/abstraction-cas/go/api"
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-facade/go/client"
)

func TestResolvedConfigObservationComposition(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	options := jobOptions(t)
	options.ConfigStore = casapi.BoundedFileStore{MaxBytes: config.MaxUserFileBytes}
	options.ConfigUserKey = filepath.Join(t.TempDir(), "selected-state")
	changes := make(chan struct{}, 1)
	options.ConfigObservationSource = func(context.Context) (<-chan struct{}, func(), error) {
		return changes, func() {}, nil
	}
	_, stop := runJobRuntime(t, options)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m := client.New(options.Endpoint)
	observer, err := m.ResolveConfigObserver(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := observer.ObserveContext(ctx, wire.RunOverrides{}, "", 0)
	if err != nil || first.Outcome != "snapshot" || first.Snapshot == nil {
		t.Fatal(first, err)
	}
	editor, err := m.ResolveConfigEditor(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := editor.ReadUserContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"intermediate", "latest"} {
		current.Values.Store = value
		result, e := editor.ReplaceUserContext(ctx, current.Revision, current.Values)
		if e != nil || result.Outcome != "applied" {
			t.Fatal(result, e)
		}
		current = result.Snapshot
	}
	changes <- struct{}{}
	next, err := observer.ObserveContext(ctx, wire.RunOverrides{}, first.Cursor, 1000)
	if err != nil || next.Snapshot == nil || next.Snapshot.Store != "latest" {
		t.Fatal(next, err)
	}
	short, endWait := context.WithTimeout(ctx, 15*time.Millisecond)
	_, err = observer.ObserveContext(short, wire.RunOverrides{}, next.Cursor, 1000)
	endWait()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("wait budget", err)
	}
	close(changes)
	for {
		_, err = m.ResolveConfigObserver(ctx, client.Requirements{})
		var refused *client.BindingError
		if errors.As(err, &refused) && refused.Status == "not_ready" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("failed observer remained ready", err)
		case <-time.After(time.Millisecond):
		}
	}
	failure, err := observer.ObserveContext(ctx, wire.RunOverrides{}, next.Cursor, 0)
	if err != nil || failure.Outcome != "unavailable" {
		t.Fatal(failure, err)
	}
	if _, err := m.ResolveConfigEditor(ctx, client.Requirements{}); err != nil {
		t.Fatal("observer failure disabled editing", err)
	}
	if _, err := m.ResolveJobs(ctx, client.Requirements{}); err != nil {
		t.Fatal("observer failure disabled jobs", err)
	}
	stop()
	changes = make(chan struct{}, 1)
	_, stop = runJobRuntime(t, options)
	defer stop()
	observer, err = m.ResolveConfigObserver(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	gap, err := observer.ObserveContext(ctx, wire.RunOverrides{}, next.Cursor, 0)
	if err != nil || gap.Outcome != "gap" || gap.Snapshot != nil {
		t.Fatal(gap, err)
	}
	recovered, err := observer.ObserveContext(ctx, wire.RunOverrides{}, "", 0)
	if err != nil || recovered.Snapshot == nil || recovered.Snapshot.Store != "latest" {
		t.Fatal(recovered, err)
	}
}

func TestConfigRuntimeRequiresMatchingObservationSource(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	options := jobOptions(t)
	options.ConfigStore = casapi.BoundedFileStore{MaxBytes: config.MaxUserFileBytes}
	options.ConfigUserKey = filepath.Join(t.TempDir(), "selected-state")
	_, stop := runJobRuntime(t, options)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	m := client.New(options.Endpoint)
	if _, err := m.ResolveConfigObserver(ctx, client.Requirements{}); err == nil {
		t.Fatal("custom store inherited native observation")
	}
	if _, err := m.ResolveConfigEditor(ctx, client.Requirements{}); err != nil {
		t.Fatal(err)
	}
}
