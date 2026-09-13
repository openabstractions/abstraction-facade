package runtime

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	downloadserve "github.com/openabstractions/abstraction-download/go/serve"
	"github.com/openabstractions/abstraction-facade/go/client"
	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
)

func TestUpgradeResumesPartialHTTPThroughTypedResult(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Program proof unavailable on current shared transport")
	}
	const prefix = 9 << 20
	body := bytes.Repeat([]byte("upgrade!"), (prefix+65536)/8)
	resumed := make(chan int64, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"immutable-upgrade-fixture"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Header.Get("Range") == "bytes=0-0" {
			w.WriteHeader(http.StatusOK)
			return
		} // Keep this fixture on the stream path.
		if value := r.Header.Get("Range"); value != "" {
			from, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(value, "bytes="), "-"), 10, 64)
			if err != nil || from <= 0 || from >= int64(len(body)) {
				http.Error(w, "invalid resume", http.StatusBadRequest)
				return
			}
			resumed <- from
			w.Header().Set("Content-Length", strconv.FormatInt(int64(len(body))-from, 10))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(body)-1, len(body)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(body[from:])
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(body[:prefix]); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	o := jobOptions(t)
	o.ManagedJobs = true
	o.JobOwner = ""
	o.JobExecutor = downloadserve.HTTPExecution{}
	_, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	c := lifecycleClient(t, ctx, o.Endpoint)
	id, receipt := lifecycleSubmit(t, ctx, c, "upgrade-partial", server.URL, body)
	for {
		snapshot := lifecycleObserve(t, ctx, c, id, false)
		if snapshot.Progress.Done >= 8<<20 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("partial progress unavailable", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	stop()
	// Host-side compatibility probe: an admission-only implementation must refuse
	// this execution profile before it can become an unsupported old writer.
	if _, err := acceptanceprovider.Open(o.JobRoot, receipt.LogicalOwner); err == nil {
		t.Fatal("admission-only writer opened execution store")
	}
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	o.JobExecutor = lifecyclePausedExecutor{ready: release}
	_, stopAgain := runJobRuntime(t, o)
	defer stopAgain()
	recovered, err := client.NewJobs(c.Endpoint(), client.JobsOptions{ExpectedOwner: receipt.LogicalOwner})
	if err != nil {
		t.Fatal(err)
	}
	before := lifecycleObserve(t, ctx, recovered, id, false)
	if before.Receipt.OperationId != receipt.OperationId || before.Receipt.LogicalOwner != receipt.LogicalOwner || before.Progress.Done < 8<<20 || before.Progress.Done > prefix {
		t.Fatalf("replacement lost receipt or partial progress: %+v", before)
	}
	reconciled, err := recovered.Reconcile(ctx, id)
	if err != nil || reconciled.Receipt == nil || reconciled.Receipt.OperationId != receipt.OperationId {
		t.Fatal(reconciled, err)
	}
	unblock()
	final := lifecycleObserve(t, ctx, recovered, id, true)
	if final.State != "complete" {
		t.Fatalf("replacement failed: %+v", final)
	}
	select {
	case from := <-resumed:
		if from != before.Progress.Done {
			t.Fatalf("resume offset %d, retained progress %d", from, before.Progress.Done)
		}
	case <-ctx.Done():
		t.Fatal("replacement did not resume HTTP range")
	}
	var got []byte
	for {
		part, err := recovered.ReadResult(ctx, id, int64(len(got)), 65536)
		if err != nil || part.Outcome != "data" || part.Chunk == nil {
			t.Fatal(part, err)
		}
		chunk := part.Chunk
		if chunk.Offset != int64(len(got)) || chunk.Total != int64(len(body)) || len(chunk.Data) > 65536 || chunk.Receipt.OperationId != receipt.OperationId || chunk.Receipt.LogicalOwner != receipt.LogicalOwner {
			t.Fatal("chunk bounds/receipt changed")
		}
		got = append(got, chunk.Data...)
		if chunk.Eof {
			break
		}
		if len(chunk.Data) == 0 {
			t.Fatal("empty non-final chunk")
		}
	}
	if !bytes.Equal(got, body) {
		t.Fatal("typed result differs after partial upgrade")
	}
}
