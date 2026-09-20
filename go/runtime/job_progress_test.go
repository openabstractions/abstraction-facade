package runtime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	request "github.com/openabstractions/abstraction-download/go/abstraction/download/request"
	downloadserve "github.com/openabstractions/abstraction-download/go/serve"
	"github.com/openabstractions/abstraction-facade/go/client"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
)

// The resolved Go client observes the progress total from the declared size
// while work waits on its source, and from Content-Length when no size was
// declared.
func TestResolvedJobsObserveProgressTotal(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Program proof unavailable on current shared transport")
	}
	body := make([]byte, 200<<10)
	for i := range body {
		body[i] = byte(i * 13)
	}
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/held" {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write(body)
	}))
	defer server.Close()
	defer close(release)
	o := jobOptions(t)
	o.JobExecutor = downloadserve.HTTPExecution{}
	_, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	jobs, err := client.New(o.Endpoint).ResolveJobOperations(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	history, err := jobs.GetHistoryWindow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	submit := func(key, path string, size int64) api.RequestIdentity {
		t.Helper()
		id := api.RequestIdentity{Key: key, HistoryEpoch: history.HistoryEpoch}
		spec := request.Encode(&request.Request{Artifact: request.Artifact{Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(body)), Size: size},
			Sources: []request.Source{{Scheme: "http", Locator: server.URL + path}}})
		if v, err := jobs.Submit(ctx, api.Submission{Identity: id, Kind: "download", Spec: spec}); err != nil || v.Outcome.String() != "accepted" {
			t.Fatalf("submit %s: %+v %v", key, v, err)
		}
		return id
	}
	snapshot := func(id api.RequestIdentity) *api.OperationSnapshot {
		t.Helper()
		v, err := jobs.ObserveWork(ctx, id)
		if err != nil || v.Snapshot == nil {
			t.Fatalf("observe: %+v %v", v, err)
		}
		return v.Snapshot
	}
	until := func(what string, check func() bool) {
		t.Helper()
		for !check() {
			select {
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %s", what)
			case <-time.After(20 * time.Millisecond):
			}
		}
	}

	declared := submit("declared-size", "/held", int64(len(body)))
	until("declared total while held", func() bool { return snapshot(declared).Progress.Total == int64(len(body)) })
	if s := snapshot(declared); s.State.String() == "complete" {
		t.Fatalf("held work completed: %+v", s)
	}
	// One service runner works one operation at a time; let the held one finish.
	release <- struct{}{}
	until("declared completion", func() bool { return snapshot(declared).State.String() == "complete" })
	if s := snapshot(declared); s.Progress.Total != int64(len(body)) || s.Progress.Done != int64(len(body)) {
		t.Fatalf("declared snapshot: %+v", s)
	}
	undeclared := submit("content-length", "/open", 0)
	until("undeclared completion", func() bool { return snapshot(undeclared).State.String() == "complete" })
	if s := snapshot(undeclared); s.Progress.Total != int64(len(body)) || s.Progress.Done != int64(len(body)) {
		t.Fatalf("undeclared snapshot: %+v", s)
	}
}
