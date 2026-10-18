package runtime

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	job "github.com/openabstractions/abstraction-job/go"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
)

// JOB-A18: a recheck pass a rights change started is waited for when the
// job host closes. Close does not return while the pass's decision is still
// out, and once it returns the store is written no more.
func TestClosingTheJobHostWaitsForARecheckPass(t *testing.T) {
	options := jobOptions(t)
	if assertDarwinNativeJobRefusal(t, options) {
		return
	}
	h, err := listenJobs(options.JobEndpoint, options.JobRoot, "", "test-owner", nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	decide := func(context.Context, acceptanceprovider.AuthenticatedSubject, string) (string, error) {
		close(entered)
		<-release
		return "rights:denied", nil
	}
	var changed func()
	h.standing(decide, func(f func()) func() { changed = f; return func() {} })

	executable := filepath.Join(t.TempDir(), "fetch.exe")
	kind, account := "posix", "1000"
	if runtime.GOOS == "windows" {
		kind, account = "windows", "S-1-5-21-1-2-3-1001"
	}
	scope, err := acceptanceprovider.LocalSubjectScope(kind, account, executable)
	if err != nil {
		t.Fatal(err)
	}
	binding := acceptanceprovider.Binding{Scope: scope, Origin: "local",
		Subject: &acceptanceprovider.AuthenticatedSubject{AccountKind: kind, Account: account, Executable: executable, Program: executable}}
	window, err := h.provider.Bind(scope).GetHistoryWindow()
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := h.provider.BindBinding(binding).Submit(api.Submission{Identity: api.RequestIdentity{Key: "close", HistoryEpoch: window.HistoryEpoch},
		Kind: "download", Spec: []byte(`{"source":"close"}`)})
	if err != nil || accepted.Receipt == nil {
		t.Fatalf("submit %+v %v", accepted, err)
	}

	changed()
	<-entered
	closed := make(chan struct{})
	go func() {
		h.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while a recheck pass was still deciding")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-closed
	store, err := job.NewFileStore(options.JobRoot)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Load(accepted.Receipt.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	after, err := store.Load(accepted.Receipt.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt.Time) || after.State != before.State {
		t.Fatalf("the store was written after Close returned: %+v then %+v", before, after)
	}
	// A notice after Close starts nothing.
	h.provider.RecheckSoon(context.Background())
}
