package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/client"
	"github.com/openabstractions/abstraction-facade/go/resolution"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
	logging "github.com/openabstractions/abstraction-logging/go"
)

func jobOptions(t *testing.T) Options {
	t.Helper()
	dir, err := os.MkdirTemp("", "oa-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	endpoint := func(name string) string {
		if runtime.GOOS == "windows" {
			return fmt.Sprintf(`\\.\pipe\oa-jobs-runtime-%d-%s`, time.Now().UnixNano(), name)
		}
		return filepath.Join(dir, name+".sock")
	}
	return Options{Endpoint: endpoint("runtime"), LogEndpoint: endpoint("log"), ConfigEndpoint: endpoint("config"), JobEndpoint: endpoint("job"), JobRoot: filepath.Join(dir, "private"), JobOwner: "test-runtime-owner", Sink: sink{records: make(chan logging.Record, 8)}}
}

func runJobRuntime(t *testing.T, o Options) (*Host, func()) {
	t.Helper()
	h, err := Listen(o)
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
				t.Error("runtime did not drain")
			}
		})
	}
	t.Cleanup(stop)
	return h, stop
}

func resolveJobs(t *testing.T, endpoint string) wire.ResolveResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v, err := resolution.NewClient(endpoint, time.Second).Resolve(ctx, wire.ResolveRequest{Capability: "abstraction.job", Contracts: []string{"abstraction.job/acceptance@1"}, Guarantees: []string{acceptanceprovider.GuaranteeServiceRestart}, Scope: wire.ScopeLocal})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

type loseRuntimeReply struct{ transport listen.FrameClient }

func (l loseRuntimeReply) ExchangeFrame(frame []byte) ([]byte, error) {
	if _, err := l.transport.ExchangeFrame(frame); err != nil {
		return nil, err
	}
	return nil, errors.New("acceptance reply lost")
}

func TestRuntimeJobCallerChild(t *testing.T) {
	endpoint := os.Getenv("OA_JOB_RUNTIME_ENDPOINT")
	if endpoint == "" {
		return
	}
	resolved := resolveJobs(t, endpoint)
	if resolved.Status != wire.ResolutionStatusResolved {
		t.Fatal(resolved)
	}
	c := api.NewRecoverableAcceptanceClient(listen.FrameClient{Endpoint: resolved.Reference.Endpoint, Timeout: time.Second, MaxFrame: acceptanceprovider.MaxFrameBytes})
	v, err := c.Reconcile(api.RequestIdentity{Key: os.Getenv("OA_JOB_KEY"), HistoryEpoch: os.Getenv("OA_JOB_EPOCH")})
	if err != nil || v.Outcome != "accepted" || v.Receipt == nil || v.Receipt.OperationId != os.Getenv("OA_JOB_OPERATION") {
		t.Fatalf("restarted caller: %+v %v", v, err)
	}
}

func TestRuntimeResolvedJobRestart(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current shared transport lacks Program proof; provider connection tests assert refusal")
	}
	o := jobOptions(t)
	_, stop := runJobRuntime(t, o)
	resolved := resolveJobs(t, o.Endpoint)
	if resolved.Status != wire.ResolutionStatusResolved || resolved.Reference.Provider != o.JobOwner || resolved.Reference.Endpoint != o.JobEndpoint {
		t.Fatal(resolved)
	}
	for _, g := range acceptanceprovider.Guarantees() {
		found := false
		for _, actual := range resolved.Reference.Guarantees {
			if actual == g {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing actual guarantee %s", g)
		}
	}
	transport := listen.FrameClient{Endpoint: resolved.Reference.Endpoint, Timeout: time.Second, MaxFrame: acceptanceprovider.MaxFrameBytes}
	c := api.NewRecoverableAcceptanceClient(transport)
	window, err := c.GetHistoryWindow()
	if err != nil {
		t.Fatal(err)
	}
	id := api.RequestIdentity{Key: "saved-before-send", HistoryEpoch: window.HistoryEpoch}
	c = api.NewRecoverableAcceptanceClient(loseRuntimeReply{transport})
	_, err = c.Submit(api.Submission{Identity: id, Kind: "download", Spec: []byte(`{"source":"test"}`), RequiredGuarantees: []string{acceptanceprovider.GuaranteeServiceRestart}})
	if err == nil {
		t.Fatal("expected lost reply")
	}
	// Test-side evidence inspects the private provider state; the child only
	// receives the saved identity and resolved service endpoint, never this root.
	paths, err := filepath.Glob(filepath.Join(o.JobRoot, "jobs", "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("one job: %v %v", paths, err)
	}
	operation := strings.TrimSuffix(filepath.Base(paths[0]), ".json")
	stop()
	_, _ = runJobRuntime(t, o)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestRuntimeJobCallerChild$")
	cmd.Env = append(os.Environ(), "OA_JOB_RUNTIME_ENDPOINT="+o.Endpoint, "OA_JOB_KEY="+id.Key, "OA_JOB_EPOCH="+id.HistoryEpoch, "OA_JOB_OPERATION="+operation)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("new caller process: %v %s", err, output)
	}
	paths, _ = filepath.Glob(filepath.Join(o.JobRoot, "jobs", "*.json"))
	if len(paths) != 1 {
		t.Fatal("restart duplicated job")
	}
}

func TestRuntimeJobsOptionalAndDenied(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current shared transport lacks Program proof")
	}
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			o := jobOptions(t)
			o.JobPolicy = func(*identity.Peer) bool { return false }
			if !configured {
				o.JobRoot = ""
				o.JobOwner = ""
				o.JobEndpoint = ""
			}
			_, _ = runJobRuntime(t, o)
			r := resolveJobs(t, o.Endpoint)
			want := wire.ResolutionStatusUnavailable
			if configured {
				want = wire.ResolutionStatusForbidden
			}
			if r.Status != want {
				t.Fatal(r)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			m := client.New(o.Endpoint)
			if _, err := m.ResolveLog(ctx, client.Requirements{}); err != nil {
				t.Fatal(err)
			}
			if _, err := m.ResolveConfig(ctx, client.Requirements{}); err != nil {
				t.Fatal(err)
			}
			if configured {
				c := api.NewRecoverableAcceptanceClient(listen.FrameClient{Endpoint: o.JobEndpoint, Timeout: time.Second, MaxFrame: acceptanceprovider.MaxFrameBytes})
				v, err := c.Submit(api.Submission{Identity: api.RequestIdentity{Key: "denied", HistoryEpoch: "claimed-epoch"}, Kind: "download", Spec: []byte(`{}`)})
				if err != nil || v.Outcome != "forbidden" {
					t.Fatalf("direct denial: %+v %v", v, err)
				}
				files, err := os.ReadDir(filepath.Join(o.JobRoot, "acceptance", "requests"))
				if err != nil || len(files) != 0 {
					t.Fatalf("denied request changed state: %v %v", files, err)
				}
			}
		})
	}
}

func TestRuntimeJobsReadinessBeforeDrain(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current shared transport lacks Program proof")
	}
	o := jobOptions(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var block atomic.Bool
	var enterOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	o.JobPolicy = func(*identity.Peer) bool {
		if block.CompareAndSwap(true, false) {
			enterOnce.Do(func() { close(entered) })
			<-release
		}
		return true
	}
	h, _ := runJobRuntime(t, o)
	r := resolveJobs(t, o.Endpoint)
	if r.Status != wire.ResolutionStatusResolved {
		t.Fatal(r)
	}
	block.Store(true)
	done := make(chan error, 1)
	go func() {
		c := api.NewRecoverableAcceptanceClient(listen.FrameClient{Endpoint: o.JobEndpoint, Timeout: time.Second, MaxFrame: acceptanceprovider.MaxFrameBytes})
		_, err := c.GetHistoryWindow()
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("policy not entered")
	}
	h.jobs.Close()
	r = resolveJobs(t, o.Endpoint)
	if r.Status != wire.ResolutionStatusNotReady {
		t.Fatalf("closed admission still ready: %+v", r)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := client.New(o.Endpoint).ResolveConfig(ctx, client.Requirements{}); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request did not stop")
	}
}

func TestRuntimeJobConfigurationPair(t *testing.T) {
	o := jobOptions(t)
	o.JobOwner = ""
	if h, err := Listen(o); err == nil {
		h.Close()
		t.Fatal("accepted partial job configuration")
	}
}

func TestRuntimeJobConnectionBoundAndClose(t *testing.T) {
	o := jobOptions(t)
	h, _ := runJobRuntime(t, o)
	var connections []net.Conn
	defer func() {
		for _, c := range connections {
			c.Close()
		}
	}()
	for i := 0; i < 64; i++ {
		c, err := listen.Dial(o.JobEndpoint)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, c)
	}
	deadline := time.Now().Add(time.Second)
	for len(h.jobs.slots) != 64 {
		if time.Now().After(deadline) {
			t.Fatalf("active connections=%d", len(h.jobs.slots))
		}
		time.Sleep(time.Millisecond)
	}
	extra, err := listen.Dial(o.JobEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	extra.SetDeadline(time.Now().Add(time.Second))
	var one [1]byte
	if _, err := extra.Read(one[:]); err == nil {
		t.Fatal("over-limit connection remained admitted")
	}
	if len(h.jobs.slots) > 64 {
		t.Fatal("connection limit exceeded")
	}
	h.jobs.Close()
	for _, c := range connections {
		c.SetDeadline(time.Now().Add(time.Second))
		if _, err := c.Read(one[:]); err == nil {
			t.Fatal("incomplete frame survived close")
		}
	}
}

// Opt-in outside C++ consumer test. Its executable has its own owner+program
// scope; it never borrows the Go test client's identity namespace.
func TestRuntimeCppJobProbe(t *testing.T) {
	probe := os.Getenv("OA_CPP_JOB_PROBE")
	if probe == "" {
		t.Skip("set OA_CPP_JOB_PROBE to the built outside C++ consumer")
	}
	o := jobOptions(t)
	_, stop := runJobRuntime(t, o)
	const key = "cpp-caller-owned-key"
	var original *api.Receipt
	for attempt := 0; attempt < 3; attempt++ {
		if attempt == 2 {
			stop()
			_, stop = runJobRuntime(t, o)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		output, err := exec.CommandContext(ctx, probe, "--runtime", o.Endpoint, "--jobs", key).CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("C++ attempt %d: %v\n%s", attempt, err, output)
		}
		result, err := api.Decode(output)
		if err != nil || result.Outcome != "accepted" || result.Receipt == nil {
			t.Fatalf("C++ receipt: %+v %v\n%s", result, err, output)
		}
		r := result.Receipt
		if r.Identity.Key != key {
			t.Fatalf("caller key changed: %+v", r)
		}
		if err := api.ValidateResult(*result, r.Identity, o.JobOwner); err != nil {
			t.Fatal(err)
		}
		if original == nil {
			original = r
		} else if r.Identity != original.Identity || r.OperationId != original.OperationId || r.LogicalOwner != original.LogicalOwner {
			t.Fatalf("duplicate/restart receipt changed: %+v vs %+v", r, original)
		}
		paths, err := filepath.Glob(filepath.Join(o.JobRoot, "jobs", "*.json"))
		if err != nil || len(paths) != 1 || strings.TrimSuffix(filepath.Base(paths[0]), ".json") != r.OperationId {
			t.Fatalf("C++ admitted duplicate/wrong job: %v %v", paths, err)
		}
	}
	t.Log("outside C++ resolution, submit and reconcile preserved one operation across repeated calls and runtime restart")
}
