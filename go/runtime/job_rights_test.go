package runtime

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	rights "github.com/openabstractions/abstraction-rights/go"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
)

func TestResolvedRightsEnforceJobSubmissionAndCancellation(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	const submitAction, cancelAction, resource = "abstraction.job/acceptance.submit", "abstraction.job/acceptance.cancel", "abstraction.job/acceptance@1"
	policy, err := rights.LoadDecisionPolicy(filepath.Join(t.TempDir(), "policy.json"), []string{submitAction, cancelAction})
	if err != nil {
		t.Fatal(err)
	}
	o := jobOptions(t)
	o.RightsPolicy, o.RightsEndpoint = policy, o.JobEndpoint+"-rights"
	// This isolated fixture designates its own process as the job enforcer.
	o.RightsEnforcer = func(ctx context.Context, peer *identity.Peer, a, r string) bool {
		process, e := peer.Process.AtLeast(listen.Program.Process)
		return e == nil && process.PID == os.Getpid() && (a == submitAction || a == cancelAction) && r == resource
	}
	enforce := JobPolicyFromRights(rightsclient.New(o.RightsEndpoint), JobRightsActions)
	subjects := make(chan rightsclient.Subject, 16)
	o.JobMethodPolicy = func(ctx context.Context, peer *identity.Peer, service, method string) error {
		if subject, e := rightsclient.SubjectFromPeer(peer); e == nil {
			select {
			case subjects <- subject:
			default:
			}
		}
		return enforce(ctx, peer, service, method)
	}
	h, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	jobs, err := m.ResolveJobs(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	window, err := jobs.GetHistoryWindow(ctx)
	if err != nil {
		t.Fatal("unmapped method refused", err)
	}
	submission := func(key string) api.Submission {
		return api.Submission{Identity: api.RequestIdentity{Key: key, HistoryEpoch: window.HistoryEpoch}, Kind: "download", Spec: []byte(`{}`)}
	}
	before := policyFiles(t, o.JobRoot)
	denied, err := jobs.Submit(ctx, submission("rights-denied-submit"))
	if err != nil || denied.Outcome != "forbidden" {
		t.Fatalf("ungranted submit %+v %v", denied, err)
	}
	if !reflect.DeepEqual(before, policyFiles(t, o.JobRoot)) {
		t.Fatal("denied submit changed durable work")
	}
	var subject rightsclient.Subject
	select {
	case subject = <-subjects:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := policy.Set(subject, submitAction, resource, true); err != nil {
		t.Fatal(err)
	}
	accepted, err := jobs.Submit(ctx, submission("rights-granted-submit"))
	if err != nil || accepted.Outcome != "accepted" {
		t.Fatalf("granted submit %+v %v", accepted, err)
	}
	id := accepted.Receipt.Identity
	if result, err := jobs.CancelWork(ctx, id); err != nil || result.Outcome != "forbidden" {
		t.Fatalf("submit grant admitted cancellation %+v %v", result, err)
	}
	if observed, err := jobs.ObserveWork(ctx, id); err != nil || observed.Snapshot == nil || observed.Snapshot.CancellationRequested {
		t.Fatalf("denied cancellation changed intent %+v %v", observed, err)
	}
	if err := policy.Set(subject, cancelAction, resource, true); err != nil {
		t.Fatal(err)
	}
	if result, err := jobs.CancelWork(ctx, id); err != nil || result.Outcome != "requested" && result.Outcome != "already_terminal" {
		t.Fatalf("granted cancellation %+v %v", result, err)
	}
	if err := policy.Revoke(subject, submitAction, resource); err != nil {
		t.Fatal(err)
	}
	before = policyFiles(t, o.JobRoot)
	if revoked, err := jobs.Submit(ctx, submission("rights-revoked-submit")); err != nil || revoked.Outcome != "forbidden" {
		t.Fatalf("revoked submit %+v %v", revoked, err)
	}
	if replay, err := jobs.Submit(ctx, submission("rights-granted-submit")); err != nil || replay.Outcome != "forbidden" {
		t.Fatalf("revoked duplicate submit %+v %v", replay, err)
	}
	h.rights.Close()
	if outage, err := jobs.Submit(ctx, submission("rights-outage-submit")); err != nil || outage.Outcome != "unavailable" || outage.Receipt != nil {
		t.Fatalf("decision outage submit %+v %v", outage, err)
	}
	if outage, err := jobs.CancelWork(ctx, id); err != nil || outage.Outcome != "unavailable" {
		t.Fatalf("decision outage cancel %+v %v", outage, err)
	}
	if !reflect.DeepEqual(before, policyFiles(t, o.JobRoot)) {
		t.Fatal("revoked or unavailable decisions changed durable work")
	}
	if reconciled, err := jobs.Reconcile(ctx, id); err != nil || reconciled.Outcome != "accepted" {
		t.Fatalf("unmapped reconcile after rights outage %+v %v", reconciled, err)
	}
	// The outage recorded no seal: the identity is still absent, and only this
	// explicit reconciliation seals it now.
	if absent, err := jobs.Reconcile(ctx, submission("rights-outage-submit").Identity); err != nil || absent.Outcome != "definitely_not_accepted" {
		t.Fatalf("identity after outage %+v %v", absent, err)
	}
}
