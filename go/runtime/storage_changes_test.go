package runtime

import (
	"bytes"
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
	"github.com/openabstractions/abstraction-identity/listen"
	rights "github.com/openabstractions/abstraction-rights/go"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
	storage "github.com/openabstractions/abstraction-storage/go"
	storageclient "github.com/openabstractions/abstraction-storage/go/client"
)

const observeAction = "abstraction.storage/content.observe"

func digestOf(body []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(body)) }

// externalBlob writes or removes a committed object behind the service's back,
// as another adopter sharing the store would.
func externalBlob(t *testing.T, root string, body []byte, present bool) string {
	t.Helper()
	d := digestOf(body)
	path := filepath.Join(root, "blobs", "sha256-"+d[7:])
	var err error
	if present {
		err = os.WriteFile(path, body, 0600)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// awaitChange observes from cursor until an entry of kind for digest arrives.
func awaitChange(ctx context.Context, t *testing.T, changes *storageclient.Changes, cursor, kind, digest string) (string, []storageclient.Change) {
	t.Helper()
	var seen []storageclient.Change
	for {
		page, err := changes.Observe(ctx, cursor, 16, 2000)
		if err != nil || page.Outcome.String() != "page" {
			t.Fatalf("observe from %q: %+v %v", cursor, page, err)
		}
		seen = append(seen, page.Changes...)
		cursor = page.Next
		for _, c := range page.Changes {
			if c.Kind.String() == kind && c.Digest == digest {
				return cursor, seen
			}
		}
		if ctx.Err() != nil {
			t.Fatalf("no %s %s; saw %+v", kind, digest, seen)
		}
	}
}

func objectDigests(objects []storageclient.ListedObject) map[string]bool {
	out := map[string]bool{}
	for _, o := range objects {
		out[o.Digest] = true
	}
	return out
}

func TestResolvedStorageChangesEnforcedByRights(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	root := t.TempDir()
	content, err := storage.NewContentStore("provider-private", root)
	if err != nil {
		t.Fatal(err)
	}
	provider := &writableFixture{ContentStore: content}
	records := filepath.Join(t.TempDir(), "writer-records.json")
	policy, err := rights.LoadDecisionPolicy(filepath.Join(t.TempDir(), "policy.json"), []string{readAction, writeAction, observeAction})
	if err != nil {
		t.Fatal(err)
	}
	preexisting := externalBlob(t, root, []byte("present before the runtime started"), true)
	subjects := make(chan rightsclient.Subject, 64)
	start := func() (*Host, func(), *client.Machine) {
		o := jobOptions(t)
		o.RightsPolicy, o.RightsEndpoint = policy, o.JobEndpoint+"-rights"
		o.RightsEnforcer = func(ctx context.Context, peer *identity.Peer, a, r string) bool {
			process, e := peer.Process.AtLeast(listen.Program.Process)
			return e == nil && process.PID == os.Getpid() && (a == readAction || a == writeAction || a == observeAction)
		}
		decisions := rightsclient.New(o.RightsEndpoint)
		observe := ContentPolicyFromRights(decisions, observeAction)
		o.Storage, o.StorageEndpoint = provider, o.JobEndpoint+"-storage"
		o.StoragePolicy = ContentPolicyFromRights(decisions, readAction)
		o.StorageWritePolicy, o.StorageWriteLimit, o.StorageWriteRecordPath = ContentPolicyFromRights(decisions, writeAction), 1<<20, records
		o.StorageChangesPolicy = func(ctx context.Context, peer *identity.Peer, resource string) error {
			if subject, e := rightsclient.SubjectFromPeer(peer); e == nil {
				select {
				case subjects <- subject:
				default:
				}
			}
			return observe(ctx, peer, resource)
		}
		o.StorageChangesInterval, o.StorageChangesCapacity = 25*time.Millisecond, 4
		h, stop := runJobRuntime(t, o)
		return h, stop, client.New(o.Endpoint)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, stop, m := start()
	changes, err := m.ResolveStorageChanges(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := m.ResolveStorageWriter(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}

	// Refused before an observe grant, for both Observe and List.
	if page, err := changes.Observe(ctx, "", 16, 0); err != nil || page.Outcome.String() != "forbidden" {
		t.Fatalf("ungranted observe %+v %v", page, err)
	}
	if page, err := changes.List(ctx, "", 16); err != nil || page.Outcome.String() != "forbidden" {
		t.Fatalf("ungranted list %+v %v", page, err)
	}
	var subject rightsclient.Subject
	select {
	case subject = <-subjects:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	grant := func(action, resource string) {
		t.Helper()
		if err := policy.Set(subject, action, resource, true); err != nil {
			t.Fatal(err)
		}
	}
	body := bytes.Repeat([]byte("committed through the writer "), 100)
	committed := digestOf(body)
	grant(observeAction, "abstraction.storage/changes")
	grant(readAction, preexisting)
	grant(readAction, committed)
	grant(writeAction, committed)

	// A new subscriber builds initial state from the snapshot; the present object is not journaled.
	objects, cursor, err := changes.Snapshot(ctx, 1)
	if err != nil || !objectDigests(objects)[preexisting] || len(objects) != 1 {
		t.Fatalf("initial snapshot %+v %q %v", objects, cursor, err)
	}
	if page, err := changes.Observe(ctx, cursor, 16, 0); err != nil || page.Outcome.String() != "page" || len(page.Changes) != 0 || !page.AtEnd {
		t.Fatalf("present objects were journaled: %+v %v", page, err)
	}

	// A writer commit wakes a waiting authorized subscriber.
	type result struct {
		page storageclient.ChangePage
		err  error
	}
	waiting := make(chan result, 1)
	go func() {
		p, e := changes.Observe(ctx, cursor, 16, 10000)
		waiting <- result{p, e}
	}()
	time.Sleep(100 * time.Millisecond)
	request, _ := storageclient.NewRequestID()
	if stored, err := writer.Write(ctx, request, committed, bytes.NewReader(body), int64(len(body))); err != nil || stored.Evidence.String() != "hashed" {
		t.Fatalf("write %+v %v", stored, err)
	}
	woke := <-waiting
	if woke.err != nil || woke.page.Outcome.String() != "page" || len(woke.page.Changes) != 1 || woke.page.Changes[0].Kind.String() != "added" || woke.page.Changes[0].Digest != committed || woke.page.Changes[0].Size != int64(len(body)) {
		t.Fatalf("commit notification %+v %v", woke.page, woke.err)
	}
	cursor = woke.page.Next

	// External additions and deletions are found by polling.
	external := []byte("delivered by another adopter")
	externalDigest := digestOf(external)
	grant(readAction, externalDigest)
	externalBlob(t, root, external, true)
	cursor, _ = awaitChange(ctx, t, changes, cursor, "added", externalDigest)
	externalBlob(t, root, external, false)
	cursor, _ = awaitChange(ctx, t, changes, cursor, "removed", externalDigest)

	// Entries the caller may not read are skipped while the cursor advances past them.
	hidden := externalBlob(t, root, []byte("not readable by this caller"), true)
	time.Sleep(150 * time.Millisecond)
	visibleBody := []byte("readable by this caller")
	grant(readAction, digestOf(visibleBody))
	visible := externalBlob(t, root, visibleBody, true)
	var seen []storageclient.Change
	cursor, seen = awaitChange(ctx, t, changes, cursor, "added", visible)
	for _, c := range seen {
		if c.Digest == hidden {
			t.Fatalf("unreadable entry disclosed: %+v", seen)
		}
	}
	if page, err := changes.Observe(ctx, cursor, 16, 0); err != nil || page.Outcome.String() != "page" || len(page.Changes) != 0 || !page.AtEnd {
		t.Fatalf("cursor did not advance past the skipped entry: %+v %v", page, err)
	}
	if objects, _, err := changes.Snapshot(ctx, 16); err != nil || objectDigests(objects)[hidden] || !objectDigests(objects)[visible] {
		t.Fatalf("snapshot filtering %+v %v", objects, err)
	}

	// A slow subscriber falls behind the four-entry journal, gets gap, and recovers from the snapshot.
	slow := cursor
	var burst []string
	for i := 0; i < 6; i++ {
		b := []byte(fmt.Sprintf("burst object %d", i))
		grant(readAction, digestOf(b))
		burst = append(burst, externalBlob(t, root, b, true))
	}
	for {
		objects, _, err := changes.Snapshot(ctx, 64)
		if err != nil {
			t.Fatal(err)
		}
		if found := objectDigests(objects); found[burst[5]] && found[burst[0]] {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("burst not polled")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if page, err := changes.Observe(ctx, slow, 16, 0); err != nil || page.Outcome.String() != "gap" || len(page.Changes) != 0 || page.Next != slow {
		t.Fatalf("slow subscriber %+v %v", page, err)
	}
	objects, recovered, err := changes.Snapshot(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	found := objectDigests(objects)
	for _, d := range append(burst, committed, visible, preexisting) {
		if !found[d] {
			t.Fatalf("recovery snapshot lacks %s: %+v", d, objects)
		}
	}
	if page, err := changes.Observe(ctx, recovered, 16, 0); err != nil || page.Outcome.String() != "page" || !page.AtEnd {
		t.Fatalf("observe after recovery %+v %v", page, err)
	}

	// Revocation refuses the next call.
	if err := policy.Revoke(subject, observeAction, "abstraction.storage/changes"); err != nil {
		t.Fatal(err)
	}
	if page, err := changes.Observe(ctx, recovered, 16, 0); err != nil || page.Outcome.String() != "forbidden" || page.Next != recovered {
		t.Fatalf("revoked observe %+v %v", page, err)
	}
	if page, err := changes.List(ctx, "", 16); err != nil || page.Outcome.String() != "forbidden" {
		t.Fatalf("revoked list %+v %v", page, err)
	}
	grant(observeAction, "abstraction.storage/changes")

	// A runtime restart starts a new epoch: old cursors gap, and present objects are listed without being journaled.
	stop()
	_, stop, m = start()
	defer stop()
	changes, err = m.ResolveStorageChanges(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	if page, err := changes.Observe(ctx, recovered, 16, 0); err != nil || page.Outcome.String() != "gap" {
		t.Fatalf("pre-restart cursor %+v %v", page, err)
	}
	objects, restarted, err := changes.Snapshot(ctx, 16)
	if err != nil || !objectDigests(objects)[committed] || !objectDigests(objects)[preexisting] {
		t.Fatalf("snapshot after restart %+v %v", objects, err)
	}
	if page, err := changes.Observe(ctx, restarted, 16, 0); err != nil || page.Outcome.String() != "page" || len(page.Changes) != 0 {
		t.Fatalf("restart journaled present objects %+v %v", page, err)
	}
}
