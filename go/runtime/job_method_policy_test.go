package runtime

import (
	"context"
	"errors"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func policyFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		files[path] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
func TestRuntimeJobMethodPolicy(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Program proof refusal remains covered at connection boundary")
	}
	options := jobOptions(t)
	var deny atomic.Bool
	var observations atomic.Int32
	options.JobMethodPolicy = func(ctx context.Context, peer *identity.Peer, service, method string) error {
		if peer == nil || ctx.Err() != nil {
			return errors.New("caller context missing")
		}
		if _, err := peer.Path.AtLeast(listen.Program.Path); err != nil {
			return err
		}
		if service == "abstraction.job/operations@1" {
			observations.Add(1)
			return nil
		}
		if deny.Load() && (method == "Submit" || method == "CancelWork") {
			return errors.New("writes revoked")
		}
		return nil
	}
	_, stop := runJobRuntime(t, options)
	defer stop()
	transport := listen.FrameClient{Endpoint: options.JobEndpoint, Timeout: time.Second, MaxFrame: acceptanceprovider.MaxFrameBytes}
	client := api.NewRecoverableAcceptanceClient(transport)
	window, err := client.GetHistoryWindow()
	if err != nil {
		t.Fatal(err)
	}
	submission := api.Submission{Identity: api.RequestIdentity{Key: "retained", HistoryEpoch: window.HistoryEpoch}, Kind: "download", Spec: []byte(`{}`)}
	accepted, err := client.Submit(submission)
	if err != nil || accepted.Outcome != "accepted" {
		t.Fatal(accepted, err)
	}
	before := policyFiles(t, options.JobRoot)
	deny.Store(true)
	rejected := submission
	rejected.Identity.Key = "denied"
	if result, err := client.Submit(rejected); err != nil || result.Outcome != "forbidden" {
		t.Fatal(result, err)
	}
	if result, err := client.CancelWork(submission.Identity); err != nil || result.Outcome != "forbidden" {
		t.Fatal(result, err)
	}
	operations := api.NewOperationControlClient(transport)
	if result, err := operations.ObserveWork(submission.Identity); err != nil || result.Outcome != "observed" || result.Snapshot.CancellationRequested {
		t.Fatal(result, err)
	}
	if result, err := operations.ReadResult(submission.Identity, 0, 64); err != nil || result.Outcome != "unsupported" {
		t.Fatal(result, err)
	}
	if observations.Load() != 2 || !reflect.DeepEqual(before, policyFiles(t, options.JobRoot)) {
		t.Fatal("denied methods changed durable work")
	}
}
