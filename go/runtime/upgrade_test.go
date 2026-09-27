package runtime

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	identity "github.com/openabstractions/abstraction-identity"
	job "github.com/openabstractions/abstraction-job/go"
)

func assertDarwinNativeJobRefusal(t *testing.T, options Options) bool {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return false
	}
	h, err := listenJobs(options.JobEndpoint, options.JobRoot, "", "test-owner", nil, nil, true)
	if h != nil {
		h.Close()
		t.Fatal("insufficiently proven caller created a job host")
	}
	if !errors.Is(err, identity.ErrNotProven) {
		t.Fatalf("expected startup proof refusal, got %v", err)
	}
	for _, path := range []string{options.JobEndpoint, options.JobEndpoint + ".lock"} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("proof refusal created job endpoint artifact %q: %v", path, err)
		}
	}
	t.Skip("native Darwin sockets refuse Program at startup; job upgrade behavior needs a running XPC host")
	return true
}

type upgradeExecutor struct {
	entered, draining, release chan struct{}
}

func (*upgradeExecutor) Profile() string { return "upgrade-test-v1" }
func (*upgradeExecutor) Prepare(_ string, _ string, spec []byte) ([]byte, error) {
	return spec, nil
}
func (e *upgradeExecutor) Serve(ctx context.Context, _ job.Store) error {
	close(e.entered)
	<-ctx.Done()
	close(e.draining)
	<-e.release
	return nil
}

func TestUpgradeRootExclusionSurvivesListenerCloseUntilDrain(t *testing.T) {
	a, b := jobOptions(t), jobOptions(t)
	if assertDarwinNativeJobRefusal(t, a) {
		return
	}
	b.JobEndpoint += "-replacement"
	executor := &upgradeExecutor{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(executor.release) }) }
	old, err := listenJobs(a.JobEndpoint, a.JobRoot, "", "test-owner", nil, executor, true)
	if err != nil {
		t.Fatal(err)
	}
	owner := old.provider.LogicalOwner()
	done := make(chan error, 1)
	go func() { done <- old.Serve(context.Background()) }()
	t.Cleanup(func() {
		release()
		old.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("old executor did not drain")
		}
	})
	select {
	case <-executor.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not start")
	}
	refuses := func(endpoint, phase string) {
		t.Helper()
		replacement, err := listenJobs(endpoint, a.JobRoot, "", "test-owner", nil, executor, true)
		if err == nil {
			replacement.Close()
			t.Errorf("replacement opened shared root %s", phase)
		}
	}
	refuses(b.JobEndpoint, "while first host serves on a different endpoint")
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-executor.draining:
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not begin drain")
	}
	refuses(b.JobEndpoint, "while old executor drains")
	refuses(a.JobEndpoint, "after original endpoint closed but before drain")
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		// Leave a completion token for the bounded cleanup join.
		done <- nil
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not complete drain")
	}
	replacement, err := listenJobs(b.JobEndpoint, a.JobRoot, "", "test-owner", nil, executor, true)
	if err != nil {
		t.Fatal("replacement refused after drain", err)
	}
	defer replacement.Close()
	if replacement.provider.LogicalOwner() != owner {
		t.Fatal("upgrade changed logical owner")
	}
}

func TestUpgradeCloseBeforeServeReleasesRoot(t *testing.T) {
	a, b := jobOptions(t), jobOptions(t)
	if assertDarwinNativeJobRefusal(t, a) {
		return
	}
	b.JobEndpoint += "-replacement"
	old, err := listenJobs(a.JobEndpoint, a.JobRoot, "", "test-owner", nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { old.Close() })
	owner := old.provider.LogicalOwner()
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := listenJobs(b.JobEndpoint, a.JobRoot, "", "test-owner", nil, nil, true)
	if err != nil {
		t.Fatal("unserved host retained root", err)
	}
	defer replacement.Close()
	if replacement.provider.LogicalOwner() != owner {
		t.Fatal("replacement changed logical owner")
	}
}
