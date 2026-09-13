package client_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	client "github.com/openabstractions/abstraction-facade/go/client"
	host "github.com/openabstractions/abstraction-facade/go/runtime"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
	logging "github.com/openabstractions/abstraction-logging/go"
)

type jobSink struct{}

func (jobSink) Write(logging.Record) error { return nil }
func jobRuntimeOptions(t *testing.T) host.Options {
	t.Helper()
	dir, err := os.MkdirTemp("", "oa-jobs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	endpoint := func(name string) string {
		if runtime.GOOS == "windows" {
			return fmt.Sprintf(`\\.\pipe\oa-go-jobs-%d-%s`, time.Now().UnixNano(), name)
		}
		return filepath.Join(dir, name)
	}
	return host.Options{Endpoint: endpoint("resolver"), LogEndpoint: endpoint("log"), ConfigEndpoint: endpoint("config"), JobEndpoint: endpoint("job"), JobRoot: filepath.Join(dir, "private"), JobOwner: "durable-owner", Sink: jobSink{}}
}
func runJobHost(t *testing.T, o host.Options) func() {
	t.Helper()
	h, err := host.Listen(o)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			h.Close()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(5 * time.Second):
				t.Error("host cleanup timed out")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}
func TestResolvedGoJobsSurviveServiceRestart(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Program-bound local calls remain unproven on Darwin; semantic client tests still run")
	}
	options := jobRuntimeOptions(t)
	stop := runJobHost(t, options)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	required := []string{acceptanceprovider.GuaranteeServiceRestart}
	c, err := client.New(options.Endpoint).ResolveJobs(ctx, client.Requirements{Guarantees: required, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	window, err := c.GetHistoryWindow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := api.RequestIdentity{Key: "persisted-before-send", HistoryEpoch: window.HistoryEpoch}
	accepted, err := c.Submit(ctx, api.Submission{Identity: id, Kind: "download", Spec: []byte(`{"source":"test"}`)})
	if err != nil || accepted.Outcome != "accepted" {
		t.Fatalf("acceptance: %+v %v", accepted, err)
	}
	stop()
	runJobHost(t, options)
	recovered, err := client.NewJobs(c.Endpoint(), client.JobsOptions{ExpectedOwner: accepted.Receipt.LogicalOwner, RequiredGuarantees: required})
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range []*client.JobsClient{c, recovered} {
		result, err := binding.Reconcile(ctx, id)
		if err != nil || result.Receipt == nil || result.Receipt.OperationId != accepted.Receipt.OperationId {
			t.Fatalf("restart recovery: %+v %v", result, err)
		}
	}
	result, err := recovered.CancelWork(ctx, id)
	if err != nil || result.Outcome != "requested" {
		t.Fatalf("explicit cancellation: %+v %v", result, err)
	}
	_, err = client.New(options.Endpoint).ResolveJobs(ctx, client.Requirements{Guarantees: []string{"unprovided"}})
	var refusal *client.BindingError
	if !errors.As(err, &refusal) || refusal.Status != "unmet_requirements" {
		t.Fatalf("weakened requirements: %v", err)
	}
}
func TestResolvedGoJobsAbsenceIsTyped(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Program-bound local calls remain unproven on Darwin")
	}
	o := jobRuntimeOptions(t)
	o.JobRoot = ""
	o.JobOwner = ""
	o.JobEndpoint = ""
	runJobHost(t, o)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.New(o.Endpoint).ResolveJobs(ctx, client.Requirements{})
	var refusal *client.BindingError
	if !errors.As(err, &refusal) || refusal.Status != "unavailable" {
		t.Fatalf("absence: %v", err)
	}
}
