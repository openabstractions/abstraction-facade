package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	rights "github.com/openabstractions/abstraction-rights/go"
	rightsservice "github.com/openabstractions/abstraction-rights/go/authorization"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
)

// A resolved Go operator registers an action no definition lists, a storage
// enforcement point enforces it, and the rule's expiry and provenance read back.
func TestResolvedRightsCatalogueRegistrationExpiryAndProvenance(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	const registered = "fixture.app/content.read"
	body := bytes.Repeat([]byte("registered-action-content"), 400)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	path := filepath.Join(t.TempDir(), "private-content")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	policy, err := rights.LoadDecisionPolicy(filepath.Join(t.TempDir(), "policy.json"), []string{"fixture.seed/only"})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	var offset atomic.Int64
	policy.Clock = func() time.Time { return base.Add(time.Duration(offset.Load())) }
	o := jobOptions(t)
	o.RightsPolicy, o.RightsEndpoint = policy, o.JobEndpoint+"-rights"
	o.RightsActions = ResourceRightsActions()
	samePID := func(peer *identity.Peer) bool {
		process, err := peer.Process.AtLeast(listen.Program.Process)
		return err == nil && process.PID == os.Getpid()
	}
	o.RightsOperator = func(ctx context.Context, peer *identity.Peer) error {
		if !samePID(peer) {
			return rightsservice.ErrOperatorForbidden
		}
		return ctx.Err()
	}
	o.RightsEnforcer = func(ctx context.Context, peer *identity.Peer, a, r string) bool {
		return samePID(peer) && a == registered && ctx.Err() == nil
	}
	o.Storage = &contentFixture{path: path, digest: digest, size: int64(len(body))}
	o.StorageEndpoint = o.JobEndpoint + "-storage"
	o.StoragePolicy = ContentPolicyFromRights(rightsclient.New(o.RightsEndpoint), registered)
	_, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	content, err := m.ResolveStorage(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	operator, err := m.ResolveRightsOperator(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	me := rightsclient.Subject{Account: account.Uid, Program: filepath.Clean(exe)}
	at := base.Format(rights.StampFormat)

	page, err := operator.ListPolicyContext(ctx, "", 64)
	want := append(ResourceRightsActions(), "fixture.seed/only")
	slices.Sort(want)
	if err != nil || page.Outcome.String() != "page" || !slices.Equal(page.Catalog, want) {
		t.Fatal("runtime composition did not register resource actions", page, err)
	}
	composed, err := operator.RegisterActionContext(ctx, page.Revision, ConfigEditAction)
	if err != nil || composed.Outcome.String() != "applied" || composed.Revision != page.Revision || composed.Current == nil ||
		composed.Current.RegisteredBy != me || composed.Current.RegisteredAt != at {
		t.Fatal("composed registration provenance", composed, err)
	}
	if opened, err := content.Open(ctx, digest); err != nil || opened.Outcome.String() != "unavailable" {
		t.Fatal("unregistered action decided", opened, err)
	}
	stale, err := operator.RegisterActionContext(ctx, "unobserved", registered)
	if err != nil || stale.Outcome.String() != "conflict" || stale.Revision != page.Revision || stale.Current != nil {
		t.Fatal(stale, err)
	}
	added, err := operator.RegisterActionContext(ctx, page.Revision, registered)
	if err != nil || added.Outcome.String() != "applied" || added.Revision == page.Revision || added.Current == nil || added.Current.RegisteredBy != me {
		t.Fatal(added, err)
	}
	if replay, err := operator.RegisterActionContext(ctx, page.Revision, registered); err != nil || replay.Outcome.String() != "conflict" || replay.Revision != added.Revision || replay.Current == nil {
		t.Fatal(replay, err)
	}
	if opened, err := content.Open(ctx, digest); err != nil || opened.Outcome.String() != "forbidden" {
		t.Fatal("registered action granted without a rule", opened, err)
	}
	rule := rightsclient.PolicyRule{Subject: me, Action: registered, Resource: digest, Permit: true}
	grant, err := operator.SetRuleForContext(ctx, added.Revision, rule, time.Hour, "fixture grant")
	if err != nil || grant.Outcome.String() != "applied" || grant.Current == nil || !grant.Current.Permit {
		t.Fatal(grant, err)
	}
	if again, err := operator.SetRuleForContext(ctx, added.Revision, rule, time.Hour, "fixture grant"); err != nil || again.Outcome.String() != "conflict" {
		t.Fatal(again, err)
	}
	opened, err := content.Open(ctx, digest)
	if err != nil || opened.Outcome.String() != "opened" || opened.Resource == nil {
		t.Fatal("registered grant not enforced", opened, err)
	}
	read, err := operator.ReadRuleContext(ctx, me, registered, digest)
	if err != nil || read.Outcome.String() != "found" || read.Revision != grant.Revision || read.Record == nil {
		t.Fatal(read, err)
	}
	if r := read.Record; r.SetBy != me || r.SetAt != at || r.Why != "fixture grant" || r.Expires != base.Add(time.Hour).Format(rights.StampFormat) {
		t.Fatal("provenance", *r)
	}
	offset.Store(int64(2 * time.Hour))
	if chunk, err := content.Read(ctx, *opened.Resource, 0, 1); err != nil || chunk.Outcome.String() != "forbidden" {
		t.Fatal("expired grant still enforced", chunk, err)
	}
	if expiredRead, err := operator.ReadRuleContext(ctx, me, registered, digest); err != nil || expiredRead.Outcome.String() != "expired" || expiredRead.Record == nil {
		t.Fatal(expiredRead, err)
	}
	if closed, err := content.Close(ctx, *opened.Resource); err != nil || closed.Outcome.String() != "closed" {
		t.Fatal(closed, err)
	}
	if seeded, err := operator.RetireActionContext(ctx, grant.Revision, "fixture.seed/only"); err != nil || seeded.Outcome.String() != "invalid" {
		t.Fatal(seeded, err)
	}
	retired, err := operator.RetireActionContext(ctx, grant.Revision, registered)
	if err != nil || retired.Outcome.String() != "applied" || retired.Current != nil {
		t.Fatal(retired, err)
	}
	if gone, err := operator.ReadRuleContext(ctx, me, registered, digest); err != nil || gone.Outcome.String() != "unknown" || gone.Revision != retired.Revision {
		t.Fatal("retirement kept its rules", gone, err)
	}
	if absent, err := operator.RetireActionContext(ctx, retired.Revision, registered); err != nil || absent.Outcome.String() != "unknown" {
		t.Fatal(absent, err)
	}
	if opened, err := content.Open(ctx, digest); err != nil || opened.Outcome.String() != "unavailable" {
		t.Fatal("retired action decided", opened, err)
	}
}
