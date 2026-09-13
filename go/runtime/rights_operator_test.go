package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	rights "github.com/openabstractions/abstraction-rights/go"
	rightsservice "github.com/openabstractions/abstraction-rights/go/authorization"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolvedRightsOperatorEnforcesStorageAndRestart(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	const action = "abstraction.storage/content.read"
	body := bytes.Repeat([]byte("operator-authorized-content"), 6000)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	path := filepath.Join(t.TempDir(), "private-content")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(t.TempDir(), "policy.json")
	policy, err := rights.LoadDecisionPolicy(policyPath, []string{action})
	if err != nil {
		t.Fatal(err)
	}
	o := jobOptions(t)
	o.RightsPolicy = policy
	o.RightsEndpoint = o.JobEndpoint + "-rights"
	var allow, outage atomic.Bool
	o.RightsOperator = func(ctx context.Context, peer *identity.Peer) error {
		if outage.Load() {
			return errors.New("operator policy unavailable")
		}
		process, err := peer.Process.AtLeast(listen.Program.Process)
		if err != nil || process.PID != os.Getpid() || !allow.Load() {
			return rightsservice.ErrOperatorForbidden
		}
		return ctx.Err()
	}
	o.RightsEnforcer = func(ctx context.Context, peer *identity.Peer, a, r string) bool {
		process, err := peer.Process.AtLeast(listen.Program.Process)
		return err == nil && process.PID == os.Getpid() && a == action && ctx.Err() == nil
	}
	provider := &contentFixture{path: path, digest: digest, size: int64(len(body))}
	o.Storage = provider
	o.StorageEndpoint = o.JobEndpoint + "-storage"
	enforce := ContentPolicyFromRights(rightsclient.New(o.RightsEndpoint), action)
	subjects := make(chan rightsclient.Subject, 8)
	o.StoragePolicy = func(ctx context.Context, peer *identity.Peer, resource string) error {
		subject, err := rightsclient.SubjectFromPeer(peer)
		if err != nil {
			return err
		}
		select {
		case subjects <- subject:
		default:
		}
		return enforce(ctx, peer, resource)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var cursor, revision string
	func() {
		h, stop := runJobRuntime(t, o)
		defer stop()
		m := client.New(o.Endpoint)
		content, err := m.ResolveStorage(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		denied, err := content.Open(ctx, digest)
		if err != nil || denied.Outcome != "forbidden" || provider.finds.Load() != 0 {
			t.Fatal(denied, err)
		}
		var subject rightsclient.Subject
		select {
		case subject = <-subjects:
		case <-ctx.Done():
			t.Fatal("receiving subject unavailable")
		}
		operator, err := m.ResolveRightsOperator(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		hidden, err := operator.ListPolicyContext(ctx, "", 1)
		if err != nil || hidden.Outcome != "forbidden" {
			t.Fatal(hidden, err)
		}
		rule := rightsclient.PolicyRule{Subject: subject, Action: action, Resource: digest, Permit: true}
		refused, err := operator.SetRuleContext(ctx, "unobserved", rule)
		if err != nil || refused.Outcome != "forbidden" {
			t.Fatal(refused, err)
		}
		allow.Store(true)
		page, err := operator.ListPolicyContext(ctx, "", 64)
		if err != nil || page.Outcome != "page" || len(page.Catalog) != 1 || page.Catalog[0] != action || len(page.Rules) != 0 {
			t.Fatal(page, err)
		}
		initial := page.Revision
		grant, err := operator.SetRuleContext(ctx, initial, rule)
		if err != nil || grant.Outcome != "applied" || grant.Current == nil || !grant.Current.Permit {
			t.Fatal(grant, err)
		}
		retry, err := operator.SetRuleContext(ctx, initial, rule)
		if err != nil || retry.Outcome != "conflict" || retry.Revision != grant.Revision {
			t.Fatal(retry, err)
		}
		opened, err := content.Open(ctx, digest)
		if err != nil || opened.Resource == nil {
			t.Fatal(opened, err)
		}
		resource := *opened.Resource
		var got bytes.Buffer
		for {
			chunk, err := content.Read(ctx, resource, int64(got.Len()), 65536)
			if err != nil || chunk.Chunk == nil || len(chunk.Chunk.Data) > 65536 {
				t.Fatal(chunk, err)
			}
			got.Write(chunk.Chunk.Data)
			if chunk.Chunk.Eof {
				break
			}
		}
		if !bytes.Equal(got.Bytes(), body) {
			t.Fatal("bounded operator-authorized content differs")
		}
		rule.Resource = "other"
		rule.Permit = false
		second, err := operator.SetRuleContext(ctx, grant.Revision, rule)
		if err != nil || second.Outcome != "applied" {
			t.Fatal(second, err)
		}
		page, err = operator.ListPolicyContext(ctx, "", 1)
		if err != nil || len(page.Rules) != 1 || page.Next == "" || page.Complete {
			t.Fatal(page, err)
		}
		oldCursor := page.Next
		allow.Store(false)
		refused, err = operator.RevokeRuleContext(ctx, page.Revision, subject, action, digest)
		if err != nil || refused.Outcome != "forbidden" {
			t.Fatal(refused, err)
		}
		allow.Store(true)
		outage.Store(true)
		unavailable, err := operator.RevokeRuleContext(ctx, page.Revision, subject, action, digest)
		if err != nil || unavailable.Outcome != "unavailable" {
			t.Fatal(unavailable, err)
		}
		outage.Store(false)
		if chunk, err := content.Read(ctx, resource, 0, 1); err != nil || chunk.Outcome != "data" {
			t.Fatal("refused edit changed grant", chunk, err)
		}
		revoked, err := operator.RevokeRuleContext(ctx, page.Revision, subject, action, digest)
		if err != nil || revoked.Outcome != "applied" || revoked.Current != nil {
			t.Fatal(revoked, err)
		}
		if chunk, err := content.Read(ctx, resource, 0, 1); err != nil || chunk.Outcome != "forbidden" {
			t.Fatal("revoked grant still enforced", chunk, err)
		}
		gap, err := operator.ListPolicyContext(ctx, oldCursor, 1)
		if err != nil || gap.Outcome != "gap" {
			t.Fatal(gap, err)
		}
		rule.Resource = "third"
		added, err := operator.SetRuleContext(ctx, revoked.Revision, rule)
		if err != nil || added.Outcome != "applied" {
			t.Fatal(added, err)
		}
		page, err = operator.ListPolicyContext(ctx, "", 1)
		if err != nil || page.Next == "" {
			t.Fatal(page, err)
		}
		cursor, revision = page.Next, page.Revision
		h.rights.Close()
		for {
			_, err = m.ResolveRightsOperator(ctx, client.Requirements{})
			if err != nil {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("operator retained readiness")
			case <-time.After(time.Millisecond):
			}
		}
		var binding *client.BindingError
		if !errors.As(err, &binding) || binding.Status != "not_ready" {
			t.Fatal(err)
		}
		if _, err = m.ResolveRights(ctx, client.Requirements{}); !errors.As(err, &binding) || binding.Status != "not_ready" {
			t.Fatal("shared decision readiness", err)
		}
		if chunk, err := content.Read(ctx, resource, 0, 1); err != nil || chunk.Outcome != "unavailable" {
			t.Fatal("decision outage hidden", chunk, err)
		}
		if closed, err := content.Close(ctx, resource); err != nil || closed.Outcome != "closed" {
			t.Fatal(closed, err)
		}
		if _, err = m.ResolveConfig(ctx, client.Requirements{}); err != nil {
			t.Fatal("rights disabled configuration", err)
		}
	}()
	reopened, err := rights.LoadDecisionPolicy(policyPath, []string{action})
	if err != nil {
		t.Fatal(err)
	}
	o.RightsPolicy = reopened
	_, stop := runJobRuntime(t, o)
	defer stop()
	operator, err := client.New(o.Endpoint).ResolveRightsOperator(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	gap, err := operator.ListPolicyContext(ctx, cursor, 1)
	if err != nil || gap.Outcome != "gap" {
		t.Fatal("restart gap hidden", gap, err)
	}
	page, err := operator.ListPolicyContext(ctx, "", 64)
	if err != nil || page.Revision != revision || len(page.Rules) != 2 {
		t.Fatal("durable policy changed", page, err)
	}
}
func TestRuntimeRightsOperatorRequiresExplicitPolicy(t *testing.T) {
	authorize := func(context.Context, *identity.Peer) error { return nil }
	if h, err := Listen(Options{RightsOperator: authorize}); err == nil {
		h.Close()
		t.Fatal("operator without policy admitted")
	}
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	p, err := rights.LoadDecisionPolicy(filepath.Join(t.TempDir(), "policy.json"), []string{"abstraction.storage/content.read"})
	if err != nil {
		t.Fatal(err)
	}
	o := jobOptions(t)
	o.RightsPolicy = p
	o.RightsEndpoint = o.JobEndpoint + "-rights"
	_, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	if _, err = m.ResolveRightsOperator(ctx, client.Requirements{}); err == nil {
		t.Fatal("unconfigured operator advertised")
	}
	decisions, err := m.ResolveRights(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	if decision, err := decisions.DecideContext(ctx, "abstraction.storage/content.read", "x"); err != nil || decision.Outcome != "not_granted" {
		t.Fatal(decision, err)
	}
}
