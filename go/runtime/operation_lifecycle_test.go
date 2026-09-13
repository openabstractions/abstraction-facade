package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	request "github.com/openabstractions/abstraction-download/go/abstraction/download/request"
	downloadserve "github.com/openabstractions/abstraction-download/go/serve"
	"github.com/openabstractions/abstraction-facade/go/client"
	job "github.com/openabstractions/abstraction-job/go"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
)

func lifecycleClient(t *testing.T, ctx context.Context, endpoint string) *client.JobsClient {
	t.Helper()
	c, err := client.New(endpoint).ResolveJobs(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func lifecycleSubmit(t *testing.T, ctx context.Context, c *client.JobsClient, key, url string, body []byte) (api.RequestIdentity, api.Receipt) {
	t.Helper()
	h, err := c.GetHistoryWindow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := api.RequestIdentity{Key: key, HistoryEpoch: h.HistoryEpoch}
	payload := request.Encode(&request.Request{Artifact: request.Artifact{Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(body)), Size: int64(len(body))}, Sources: []request.Source{{Scheme: "http", Locator: url}}})
	result, err := c.Submit(ctx, api.Submission{Identity: id, Kind: "download", Spec: payload})
	if err != nil || result.Receipt == nil {
		t.Fatalf("submit: %+v %v", result, err)
	}
	return id, *result.Receipt
}
func lifecycleObserve(t *testing.T, ctx context.Context, c *client.JobsClient, id api.RequestIdentity, terminal bool) api.OperationSnapshot {
	t.Helper()
	for {
		result, err := c.ObserveWork(ctx, id)
		if err != nil || result.Snapshot == nil {
			t.Fatalf("observe: %+v %v", result, err)
		}
		s := *result.Snapshot
		done := s.State == "complete" || s.State == "failed" || s.State == "cancelled"
		if !terminal || done {
			return s
		}
		select {
		case <-ctx.Done():
			t.Fatalf("terminal observation: %v; last=%+v", ctx.Err(), s)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestOperationLifecycleServiceOnlyRestartAndChunks(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Program proof unavailable on current shared transport")
	}
	body := bytes.Repeat([]byte("bounded service-owned result\n"), 8192)
	started, release := make(chan struct{}, 4), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		_, _ = w.Write(body)
	}))
	defer func() { unblock(); server.Close() }()
	o := jobOptions(t)
	o.JobExecutor = downloadserve.HTTPExecution{}
	_, stop := runJobRuntime(t, o)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := lifecycleClient(t, ctx, o.Endpoint)
	caller, expire := context.WithCancel(ctx)
	id, receipt := lifecycleSubmit(t, caller, c, "caller-owned-key", server.URL, body)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	expire()
	if _, err := c.ObserveWork(caller, id); !errors.Is(err, context.Canceled) {
		t.Fatalf("expired caller wait: %v", err)
	}
	observed := lifecycleObserve(t, ctx, c, id, false)
	if observed.State != "running" || observed.Receipt.OperationId != receipt.OperationId {
		t.Fatalf("caller expiry changed work: %+v", observed)
	}
	read, err := c.ReadResult(ctx, id, 0, 65536)
	if err != nil || read.Outcome != "not_ready" || read.Chunk != nil {
		t.Fatalf("premature bytes: %+v %v", read, err)
	}
	// The application reconstructs only its persisted binding/owner/identity.
	recovered, err := client.NewJobs(c.Endpoint(), client.JobsOptions{ExpectedOwner: receipt.LogicalOwner})
	if err != nil {
		t.Fatal(err)
	}
	stop()
	_, stopAgain := runJobRuntime(t, o)
	defer stopAgain()
	unblock()
	final := lifecycleObserve(t, ctx, recovered, id, true)
	if final.State != "complete" || final.Receipt.OperationId != receipt.OperationId {
		t.Fatalf("restart terminal: %+v", final)
	}
	var got []byte
	chunks := 0
	for {
		part, err := recovered.ReadResult(ctx, id, int64(len(got)), 65536)
		if err != nil || part.Outcome != "data" || part.Chunk == nil {
			t.Fatalf("chunk: %+v %v", part, err)
		}
		chunk := part.Chunk
		if chunk.Offset != int64(len(got)) || chunk.Total != int64(len(body)) || len(chunk.Data) > 65536 || chunk.Receipt.OperationId != receipt.OperationId {
			t.Fatalf("chunk bounds/identity: %+v", chunk)
		}
		got = append(got, chunk.Data...)
		chunks++
		if chunk.Eof {
			break
		}
		if len(chunk.Data) == 0 {
			t.Fatal("non-EOF empty chunk")
		}
	}
	if chunks < 2 || !bytes.Equal(got, body) {
		t.Fatalf("result differs: chunks=%d bytes=%d", chunks, len(got))
	}
	eof, err := recovered.ReadResult(ctx, id, int64(len(body)), 65536)
	if err != nil || eof.Chunk == nil || !eof.Chunk.Eof || len(eof.Chunk.Data) != 0 {
		t.Fatalf("exact EOF: %+v %v", eof, err)
	}
	beyond, err := recovered.ReadResult(ctx, id, int64(len(body)+1), 65536)
	if err != nil || beyond.Outcome != "invalid" || beyond.Chunk != nil {
		t.Fatalf("past-end is not EOF: %+v %v", beyond, err)
	}
	for _, r := range [][2]int64{{-1, 1}, {0, 0}, {0, 65537}} {
		if _, err := recovered.ReadResult(ctx, id, r[0], r[1]); err == nil {
			t.Fatalf("invalid range accepted: %v", r)
		}
	}
}

type lifecyclePausedExecutor struct {
	downloadserve.HTTPExecution
	ready <-chan struct{}
}

func (e lifecyclePausedExecutor) Serve(ctx context.Context, s job.Store) error {
	select {
	case <-e.ready:
		return e.HTTPExecution.Serve(ctx, s)
	case <-ctx.Done():
		return nil
	}
}

func TestOperationLifecycleCancellationAndFailure(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Program proof unavailable on current shared transport")
	}
	for _, mode := range []string{"pending", "running", "completion-race", "failure"} {
		t.Run(mode, func(t *testing.T) {
			body := []byte("complete if cancellation loses")
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 1 {
					close(entered)
				}
				if mode == "failure" {
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				w.Header().Set("Content-Length", fmt.Sprint(len(body)))
				_, _ = w.Write(body)
			}))
			defer func() { unblock(); server.Close() }()
			o := jobOptions(t)
			execute := make(chan struct{})
			var startOnce sync.Once
			start := func() { startOnce.Do(func() { close(execute) }) }
			defer start()
			o.JobExecutor = lifecyclePausedExecutor{ready: execute}
			_, stop := runJobRuntime(t, o)
			defer stop()
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			c := lifecycleClient(t, ctx, o.Endpoint)
			id, receipt := lifecycleSubmit(t, ctx, c, "explicit-"+mode, server.URL, body)
			if mode != "pending" {
				start()
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if mode != "failure" {
				before := lifecycleObserve(t, ctx, c, id, false)
				wanted := "running"
				if mode == "pending" {
					wanted = "pending"
				}
				if before.State != wanted {
					t.Fatalf("cancellation precondition: %+v", before)
				}
				if mode == "completion-race" {
					unblock()
				}
				ack, err := c.CancelWork(ctx, id)
				if err != nil || (ack.Outcome != "requested" && ack.Outcome != "already_terminal") {
					t.Fatalf("cancel: %+v %v", ack, err)
				}
				start()
			}
			final := lifecycleObserve(t, ctx, c, id, true)
			if final.Receipt.OperationId != receipt.OperationId {
				t.Fatal("terminal operation changed")
			}
			switch mode {
			case "failure":
				if final.State != "failed" || final.Failure == nil || final.Failure.Classification != "permanent" {
					t.Fatalf("failure lost: %+v", final)
				}
			case "completion-race":
				if final.State != "cancelled" && final.State != "complete" {
					t.Fatalf("cancel/completion race: %+v", final)
				}
			default:
				if final.State != "cancelled" {
					t.Fatalf("cancel acknowledgement was not terminal cancellation: %+v", final)
				}
			}
			if mode == "pending" && requests.Load() != 0 {
				t.Fatal("cancelled pending work reached HTTP provider")
			}
			result, err := c.ReadResult(ctx, id, 0, 65536)
			if err != nil {
				t.Fatal(err)
			}
			if final.State == "complete" {
				if result.Chunk == nil || !bytes.Equal(result.Chunk.Data, body) {
					t.Fatalf("completed race bytes: %+v", result)
				}
			} else if result.Outcome != "unavailable" || result.Chunk != nil {
				t.Fatalf("failed/cancelled is not EOF: %+v", result)
			}
		})
	}
}

// The child uses the same executable path as the recovering application, keeping
// the authenticated program scope stable while its process identity changes.
func TestOperationLifecycleCallerProcess(t *testing.T) {
	endpoint := os.Getenv("OA_OPERATION_CHILD_ENDPOINT")
	if endpoint == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := lifecycleClient(t, ctx, endpoint)
	var submission api.Submission
	if err := json.Unmarshal([]byte(os.Getenv("OA_OPERATION_CHILD_SUBMISSION")), &submission); err != nil {
		t.Fatal(err)
	}
	history, err := c.GetHistoryWindow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	submission.Identity.HistoryEpoch = history.HistoryEpoch
	result, err := c.Submit(ctx, submission)
	if err != nil || result.Receipt == nil {
		t.Fatalf("child submit: %+v %v", result, err)
	}
	encoded, err := json.Marshal(result.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("OA_RECEIPT=%s\n", encoded)
}

func TestOperationLifecycleContinuesAfterCallerExit(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Program proof unavailable on current shared transport")
	}
	body := bytes.Repeat([]byte("caller exited; service retained work\n"), 3000)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		_, _ = w.Write(body)
	}))
	defer func() { unblock(); server.Close() }()
	o := jobOptions(t)
	o.JobExecutor = downloadserve.HTTPExecution{}
	_, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	spec := request.Encode(&request.Request{Artifact: request.Artifact{Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(body)), Size: int64(len(body))}, Sources: []request.Source{{Scheme: "http", Locator: server.URL}}})
	submission := api.Submission{Identity: api.RequestIdentity{Key: "exited-caller-key"}, Kind: "download", Spec: spec}
	encoded, err := json.Marshal(submission)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOperationLifecycleCallerProcess$", "-test.count=1")
	command.Env = append(os.Environ(), "OA_OPERATION_CHILD_ENDPOINT="+o.Endpoint, "OA_OPERATION_CHILD_SUBMISSION="+string(encoded))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("caller process: %v\n%s", err, output)
	}
	if command.ProcessState == nil || !command.ProcessState.Exited() || !command.ProcessState.Success() {
		t.Fatal("caller exit not observed")
	}
	var receipt api.Receipt
	found := false
	for _, line := range strings.Split(string(output), "\n") {
		if text, ok := strings.CutPrefix(line, "OA_RECEIPT="); ok {
			if found {
				t.Fatal("duplicate receipt output")
			}
			found = true
			if err := json.Unmarshal([]byte(text), &receipt); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found || receipt.Identity.Key != submission.Identity.Key || receipt.OperationId == "" {
		t.Fatalf("missing child receipt: %s", output)
	}
	recovered, err := client.NewJobs(o.JobEndpoint, client.JobsOptions{ExpectedOwner: receipt.LogicalOwner})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	before := lifecycleObserve(t, ctx, recovered, receipt.Identity, false)
	if before.State != "running" || before.Receipt.OperationId != receipt.OperationId {
		t.Fatalf("work did not outlive child: %+v", before)
	}
	// Only now, after OS-confirmed caller exit, can HTTP deliver result bytes.
	unblock()
	final := lifecycleObserve(t, ctx, recovered, receipt.Identity, true)
	if final.State != "complete" || final.Receipt.OperationId != receipt.OperationId {
		t.Fatalf("post-exit completion: %+v", final)
	}
	var got []byte
	for {
		result, err := recovered.ReadResult(ctx, receipt.Identity, int64(len(got)), 65536)
		if err != nil || result.Chunk == nil {
			t.Fatalf("post-exit read: %+v %v", result, err)
		}
		chunk := result.Chunk
		if chunk.Receipt.OperationId != receipt.OperationId || chunk.Total != int64(len(body)) || chunk.Offset != int64(len(got)) {
			t.Fatalf("post-exit chunk identity/bounds: %+v", chunk)
		}
		got = append(got, chunk.Data...)
		if chunk.Eof {
			break
		}
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("post-exit result differs: %d bytes", len(got))
	}
}
