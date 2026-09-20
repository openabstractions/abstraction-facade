package runtime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
)

func TestResolvedRightsEnforceStorage(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	const action = "abstraction.storage/content.read"
	body := []byte("a decision governs each read")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	path := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	eachDecider(t, []string{action}, func(t *testing.T, f deciderFixture, o Options) {
		policy := f.policy
		o.Storage = &contentFixture{path: path, digest: digest, size: int64(len(body))}
		o.StorageEndpoint = o.JobEndpoint + "-storage"
		enforce := ContentPolicyFromRights(f.decider, action)
		subjects := make(chan rightsclient.Subject, 8)
		o.StoragePolicy = func(ctx context.Context, peer *identity.Peer, resource string) error {
			subject, e := rightsclient.SubjectFromPeer(peer)
			if e != nil {
				return e
			}
			select {
			case subjects <- subject:
			default:
			}
			return enforce(ctx, peer, resource)
		}
		h, stop := runJobRuntime(t, o)
		defer stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m := client.New(o.Endpoint)
		content, err := m.ResolveStorage(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		denied, err := content.Open(ctx, digest)
		if err != nil || denied.Outcome.String() != "forbidden" {
			t.Fatalf("initial %+v %v", denied, err)
		}
		var subject rightsclient.Subject
		select {
		case subject = <-subjects:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if err := policy.Set(subject, action, digest, true); err != nil {
			t.Fatal(err)
		}
		decisions, err := m.ResolveRights(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		granted, err := decisions.DecideContext(ctx, action, digest)
		if err != nil || granted.Outcome.String() != "permitted" {
			t.Fatalf("decision %+v %v", granted, err)
		}
		opened, err := content.Open(ctx, digest)
		if err != nil || opened.Resource == nil {
			t.Fatalf("authorized %+v %v", opened, err)
		}
		resource := *opened.Resource
		page, err := content.Read(ctx, resource, 0, 65536)
		if err != nil || page.Chunk == nil || string(page.Chunk.Data) != string(body) {
			t.Fatalf("content %+v %v", page, err)
		}
		if err := policy.Revoke(subject, action, digest); err != nil {
			t.Fatal(err)
		}
		revoked, err := decisions.DecideContext(ctx, action, digest)
		if err != nil || revoked.Outcome.String() == "permitted" || revoked.PolicyRevision == granted.PolicyRevision {
			t.Fatalf("revoke %+v %v", revoked, err)
		}
		page, err = content.Read(ctx, resource, 0, 1)
		if err != nil || page.Outcome.String() != "forbidden" {
			t.Fatalf("revoked read %+v %v", page, err)
		}
		f.outage(t, h)
		page, err = content.Read(ctx, resource, 0, 1)
		if err != nil || page.Outcome.String() != "unavailable" {
			t.Fatalf("policy outage %+v %v", page, err)
		}
		closed, err := content.Close(ctx, resource)
		if err != nil || closed.Outcome.String() != "closed" {
			t.Fatalf("release during outage %+v %v", closed, err)
		}
		if _, err := m.ResolveConfig(ctx, client.Requirements{}); err != nil {
			t.Fatal("rights failure disabled config", err)
		}
	})
}
