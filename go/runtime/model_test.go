package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	download "github.com/openabstractions/abstraction-download/go"
	request "github.com/openabstractions/abstraction-download/go/abstraction/download/request"
	downloadserve "github.com/openabstractions/abstraction-download/go/serve"
	"github.com/openabstractions/abstraction-facade/go/client"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	model "github.com/openabstractions/abstraction-model/go"
	modelclient "github.com/openabstractions/abstraction-model/go/client"
)

type fixedModel struct {
	url, digest string
	size        int64
}

func (fixedModel) Registry() string { return "fixture" }
func (r fixedModel) Resolve(ctx context.Context, ref model.Ref) (download.Spec, error) {
	s := download.Spec{Artifact: download.Artifact{Digest: r.digest, Size: r.size}, Sources: []download.Source{{Scheme: "http", Locator: r.url}}}
	if ref.Repo == "private" {
		s.Sink.Final = "provider-private-result"
	}
	return s, nil
}

func TestResolvedModelToDurableDownload(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	body := []byte("portable model result")
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer source.Close()
	o := jobOptions(t)
	o.JobExecutor = downloadserve.HTTPExecution{}
	o.ModelEndpoint = o.JobEndpoint + "-model"
	o.ModelRegistry = model.NewServiceRegistry(fixedModel{source.URL, fmt.Sprintf("sha256:%x", sha256.Sum256(body)), int64(len(body))})
	h, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	lookup, err := m.ResolveModel(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	portable, err := lookup.ResolveContext(ctx, modelclient.Ref{Registry: "fixture", Repo: "weights"})
	if err != nil || portable.Outcome != "resolved" || portable.Request == nil {
		t.Fatalf("lookup %+v %v", portable, err)
	}
	private, err := lookup.ResolveContext(ctx, modelclient.Ref{Registry: "fixture", Repo: "private"})
	if err != nil || private.Outcome != "unsupported_mapping" || private.Request != nil {
		t.Fatalf("private mapping %+v %v", private, err)
	}
	jobs, err := m.ResolveJobs(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	w, err := jobs.GetHistoryWindow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := api.RequestIdentity{Key: "model-download", HistoryEpoch: w.HistoryEpoch}
	accepted, err := jobs.Submit(ctx, api.Submission{Identity: id, Kind: download.Kind, Spec: request.Encode(portable.Request)})
	if err != nil || accepted.Outcome != "accepted" {
		t.Fatalf("acceptance %+v %v", accepted, err)
	}
	for {
		state, err := jobs.ObserveWork(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if state.Snapshot != nil && state.Snapshot.State == "complete" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("download did not finish: %+v", state)
		case <-time.After(10 * time.Millisecond):
		}
	}
	var result bytes.Buffer
	if _, err := jobs.CopyResult(ctx, id, &result); err != nil || !bytes.Equal(result.Bytes(), body) {
		t.Fatalf("result %q %v", result.Bytes(), err)
	}
	h.model.Close()
	for {
		_, err = m.ResolveModel(ctx, client.Requirements{})
		if err != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("model readiness remained green")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, err := jobs.ObserveWork(ctx, id); err != nil {
		t.Fatal("model failure disabled job binding", err)
	}
}
