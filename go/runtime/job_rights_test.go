package runtime

import (
	"context"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
)

func TestResolvedRightsEnforceJobSubmissionAndOwnCancellation(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	const submitAction, cancelAction, resource = "abstraction.job/acceptance.submit", "abstraction.job/acceptance.cancel", "abstraction.job/acceptance@1"
	eachDecider(t, []string{submitAction, cancelAction, JobInventoryAction}, func(t *testing.T, f deciderFixture, o Options) {
		policy := f.policy
		enforce := JobPolicyFromRights(f.decider, JobRightsActions)
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
		if err != nil || denied.Outcome.String() != "forbidden" {
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
		if err != nil || accepted.Outcome.String() != "accepted" {
			t.Fatalf("granted submit %+v %v", accepted, err)
		}
		id := accepted.Receipt.Identity
		// A caller's own work is scoped by ownership: cancelling it needs no rule.
		if result, err := jobs.CancelWork(ctx, id); err != nil || result.Outcome.String() != "requested" && result.Outcome.String() != "already_terminal" {
			t.Fatalf("own cancellation without a cancel rule %+v %v", result, err)
		}
		// Account-wide inventory and cancelling by operation id are decided by
		// their own rules, whoever submitted the work.
		operator, err := m.ResolveJobOperator(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		if page, err := operator.ListAccountWork(ctx, "", 8); err != nil || page.Outcome.String() != "forbidden" {
			t.Fatalf("account inventory with no rule %+v %v", page, err)
		}
		if result, err := operator.CancelOperation(ctx, accepted.Receipt.OperationID); err != nil || result.Outcome.String() != "forbidden" {
			t.Fatalf("operator cancellation with no rule %+v %v", result, err)
		}
		for _, action := range []string{JobInventoryAction, cancelAction} {
			if err := policy.Set(subject, action, resource, true); err != nil {
				t.Fatal(err)
			}
		}
		if page, err := operator.ListAccountWork(ctx, "", 8); err != nil || page.Outcome.String() != "page" || len(page.Snapshots) != 1 {
			t.Fatalf("account inventory under its rule %+v %v", page, err)
		}
		if result, err := operator.CancelOperation(ctx, accepted.Receipt.OperationID); err != nil || result.Outcome.String() != "requested" && result.Outcome.String() != "already_terminal" {
			t.Fatalf("operator cancellation under its rule %+v %v", result, err)
		}
		if err := policy.Revoke(subject, submitAction, resource); err != nil {
			t.Fatal(err)
		}
		before = policyFiles(t, o.JobRoot)
		if revoked, err := jobs.Submit(ctx, submission("rights-revoked-submit")); err != nil || revoked.Outcome.String() != "forbidden" {
			t.Fatalf("revoked submit %+v %v", revoked, err)
		}
		if replay, err := jobs.Submit(ctx, submission("rights-granted-submit")); err != nil || replay.Outcome.String() != "forbidden" {
			t.Fatalf("revoked duplicate submit %+v %v", replay, err)
		}
		f.outage(t, h)
		if outage, err := jobs.Submit(ctx, submission("rights-outage-submit")); err != nil || outage.Outcome.String() != "unavailable" || outage.Receipt != nil {
			t.Fatalf("decision outage submit %+v %v", outage, err)
		}
		if !reflect.DeepEqual(before, policyFiles(t, o.JobRoot)) {
			t.Fatal("revoked or unavailable decisions changed durable work")
		}
		if reconciled, err := jobs.Reconcile(ctx, id); err != nil || reconciled.Outcome.String() != "accepted" {
			t.Fatalf("unmapped reconcile after rights outage %+v %v", reconciled, err)
		}
		// The outage recorded no seal: the identity is still absent, and only this
		// explicit reconciliation seals it now.
		if absent, err := jobs.Reconcile(ctx, submission("rights-outage-submit").Identity); err != nil || absent.Outcome.String() != "definitely_not_accepted" {
			t.Fatalf("identity after outage %+v %v", absent, err)
		}
	})
}
