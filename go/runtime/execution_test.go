package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	request "github.com/openabstractions/abstraction-download/go/abstraction/download/request"
	downloadserve "github.com/openabstractions/abstraction-download/go/serve"
	"github.com/openabstractions/abstraction-facade/go/client"
	job "github.com/openabstractions/abstraction-job/go"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
)

func TestExecutionSurvivesCallerBudgetAndHostRestart(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Program proof unavailable on current shared transport")
	}
	body := bytes.Repeat([]byte("service-owned-result"), 2048)
	started, allow := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-allow:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write(body)
	}))
	defer server.Close()
	o := jobOptions(t)
	o.JobExecutor = downloadserve.HTTPExecution{}
	_, stop := runJobRuntime(t, o)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	jobs, err := client.New(o.Endpoint).ResolveJobs(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	history, err := jobs.GetHistoryWindow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	payload := request.Encode(&request.Request{Artifact: request.Artifact{Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(body)), Size: int64(len(body))}, Sources: []request.Source{{Scheme: "http", Locator: server.URL}}})
	submission := api.Submission{Identity: api.RequestIdentity{Key: "persisted-request", HistoryEpoch: history.HistoryEpoch}, Kind: "download", Spec: payload}
	accepted, err := jobs.Submit(ctx, submission)
	if err != nil || accepted.Receipt == nil {
		t.Fatalf("acceptance: %+v %v", accepted, err)
	}
	cancel() // Expiring a caller's budget leaves the service work alive.
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("accepted work was never executed")
	}
	stop() // Host shutdown cancels its HTTP attempt; accepted ownership persists.
	if _, err := acceptanceprovider.Open(o.JobRoot, o.JobOwner); err == nil {
		t.Fatal("admission-only writer opened execution-owned format")
	}
	close(allow)
	_, stopAgain := runJobRuntime(t, o)
	defer stopAgain()
	recovery, cancelRecovery := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancelRecovery()
	result, err := jobs.Reconcile(recovery, submission.Identity)
	if err != nil || result.Receipt == nil || result.Receipt.OperationId != accepted.Receipt.OperationId {
		t.Fatalf("owner changed: %+v %v", result, err)
	}
	// Private inspection is fixture evidence. SDK calls above never see these paths.
	store, err := job.NewFileStore(o.JobRoot)
	if err != nil {
		t.Fatal(err)
	}
	for {
		record, err := store.Load(result.Receipt.OperationId)
		if err != nil {
			t.Fatal(err)
		}
		if record.State == job.StateComplete {
			break
		}
		if record.State.Terminal() {
			t.Fatalf("execution failed: %+v", record)
		}
		select {
		case <-recovery.Done():
			t.Fatalf("work did not recover: %+v", record)
		case <-time.After(20 * time.Millisecond):
		}
	}
	got, err := os.ReadFile(filepath.Join(o.JobRoot, "results", result.Receipt.OperationId))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("result: %v, %d bytes", err, len(got))
	}
	all, err := store.List()
	if err != nil || len(all) != 1 {
		t.Fatalf("duplicate work: %d, %v", len(all), err)
	}
	again, err := jobs.Submit(recovery, submission)
	if err != nil || again.Receipt == nil || again.Receipt.OperationId != result.Receipt.OperationId {
		t.Fatalf("duplicate receipt: %+v %v", again, err)
	}
}
