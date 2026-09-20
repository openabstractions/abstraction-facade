package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
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

const (
	readAction  = "abstraction.storage/content.read"
	writeAction = "abstraction.storage/content.write"
)

type writableFixture struct {
	*storage.ContentStore
	finds, places atomic.Int32
}

func (s *writableFixture) Find(d string) (storage.Ref, bool) {
	s.finds.Add(1)
	return s.ContentStore.Find(d)
}
func (s *writableFixture) Place(d string, n int64) (storage.Ref, error) {
	s.places.Add(1)
	return s.ContentStore.Place(d, n)
}

func TestStorageWriterRequiresCompleteConfiguration(t *testing.T) {
	allow := func(context.Context, *identity.Peer, string) error { return nil }
	store, err := storage.NewContentStore("fixture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []Options{
		{Storage: store, StoragePolicy: allow, StorageWritePolicy: allow},
		{Storage: store, StoragePolicy: allow, StorageWriteLimit: 10},
		{Storage: store, StoragePolicy: allow, StorageWritePolicy: allow, StorageWriteLimit: 10},
		{Storage: store, StoragePolicy: allow, StorageWritePolicy: allow, StorageWriteLimit: 10, StorageWriteRecordPath: "relative-records.json"},
		{Storage: &contentFixture{}, StoragePolicy: allow, StorageWritePolicy: allow, StorageWriteLimit: 10},
		{StorageWritePolicy: allow, StorageWriteLimit: 10},
	} {
		if h, err := Listen(o); err == nil {
			h.Close()
			t.Fatal("incomplete storage writer configuration admitted")
		}
	}
}

func TestResolvedStorageWriterIdentitySurvivesRestart(t *testing.T) {
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
	allow := func(ctx context.Context, _ *identity.Peer, _ string) error { return ctx.Err() }
	start := func() (*Host, func(), *client.Machine) {
		o := jobOptions(t)
		o.Storage, o.StorageEndpoint, o.StoragePolicy = provider, o.JobEndpoint+"-storage", allow
		o.StorageWritePolicy, o.StorageWriteLimit, o.StorageWriteRecordPath = allow, 1<<20, records
		h, stop := runJobRuntime(t, o)
		return h, stop, client.New(o.Endpoint)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	body := bytes.Repeat([]byte("durable identity "), 5000)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	later := bytes.Repeat([]byte("resumed after restart "), 4000)
	laterDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(later))
	other := []byte("different content under an old identity")
	otherDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(other))
	committedID, unfinishedID := "restart-committed-0001", "restart-unfinished-001"

	_, stop, m := start()
	writer, err := m.ResolveStorageWriter(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := writer.Write(ctx, committedID, digest, bytes.NewReader(body), int64(len(body)))
	if err != nil || first.Evidence.String() != "hashed" {
		t.Fatalf("first lifetime write %+v %v", first, err)
	}
	begun, err := writer.Begin(ctx, unfinishedID, laterDigest, int64(len(later)))
	if err != nil || begun.Outcome.String() != "started" {
		t.Fatalf("unfinished begin %+v %v", begun, err)
	}
	if a, err := writer.Append(ctx, *begun.Upload, 0, later[:1000]); err != nil || a.Outcome.String() != "accepted" {
		t.Fatalf("unfinished append %+v %v", a, err)
	}
	stop()
	// A crashed lifetime leaves staging behind; graceful stop already removed it.
	staged := filepath.Join(root, "incoming", "sha256-"+laterDigest[7:])
	if err := os.WriteFile(staged, later[:1000], 0600); err != nil {
		t.Fatal(err)
	}

	_, _, m = start()
	if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart left unfinished staging: %v", err)
	}
	writer, err = m.ResolveStorageWriter(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := m.ResolveStorage(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	places := provider.places.Load()
	for _, id := range []string{committedID, unfinishedID} {
		refused, err := writer.Begin(ctx, id, otherDigest, int64(len(other)))
		if err != nil || refused.Outcome.String() != "conflict" {
			t.Fatalf("%s accepted different content after restart: %+v %v", id, refused, err)
		}
	}
	if provider.places.Load() != places {
		t.Fatal("identity conflict reached provider placement")
	}
	duplicate, err := writer.Write(ctx, committedID, digest, bytes.NewReader(body), int64(len(body)))
	if err != nil || duplicate != first {
		t.Fatalf("committed identity after restart %+v %v", duplicate, err)
	}
	resumed, err := writer.Write(ctx, unfinishedID, laterDigest, bytes.NewReader(later), int64(len(later)))
	if err != nil || resumed.Evidence.String() != "hashed" {
		t.Fatalf("unfinished identity after restart %+v %v", resumed, err)
	}
	if got := readAll(ctx, t, reader, laterDigest); !bytes.Equal(got, later) {
		t.Fatal("restarted upload read different bytes")
	}
}

func readAll(ctx context.Context, t *testing.T, reader *storageclient.Client, digest string) []byte {
	t.Helper()
	opened, err := reader.Open(ctx, digest)
	if err != nil || opened.Resource == nil {
		t.Fatalf("open %+v %v", opened, err)
	}
	var got bytes.Buffer
	for {
		page, err := reader.Read(ctx, *opened.Resource, int64(got.Len()), 65536)
		if err != nil || page.Chunk == nil {
			t.Fatalf("read %+v %v", page, err)
		}
		got.Write(page.Chunk.Data)
		if page.Chunk.EOF {
			break
		}
	}
	if closed, err := reader.Close(ctx, *opened.Resource); err != nil || closed.Outcome.String() != "closed" {
		t.Fatalf("close %+v %v", closed, err)
	}
	return got.Bytes()
}

func TestResolvedStorageWriterEnforcedByRights(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	root := t.TempDir()
	content, err := storage.NewContentStore("provider-private", root)
	if err != nil {
		t.Fatal(err)
	}
	provider := &writableFixture{ContentStore: content}
	staged := func(digest string) string { return filepath.Join(root, "incoming", "sha256-"+digest[7:]) }
	blobs := func() int {
		entries, err := os.ReadDir(filepath.Join(root, "blobs"))
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	const limit = 1 << 20
	body := bytes.Repeat([]byte("service-owned write "), 7500)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	policy, err := rights.LoadDecisionPolicy(filepath.Join(t.TempDir(), "policy.json"), []string{readAction, writeAction})
	if err != nil {
		t.Fatal(err)
	}
	o := jobOptions(t)
	o.RightsPolicy, o.RightsEndpoint = policy, o.JobEndpoint+"-rights"
	// This isolated fixture designates its own process as the storage enforcer.
	o.RightsEnforcer = func(ctx context.Context, peer *identity.Peer, a, r string) bool {
		process, e := peer.Process.AtLeast(listen.Program.Process)
		return e == nil && process.PID == os.Getpid() && (a == readAction || a == writeAction)
	}
	decisions := rightsclient.New(o.RightsEndpoint)
	o.Storage, o.StorageEndpoint = provider, o.JobEndpoint+"-storage"
	o.StoragePolicy = ContentPolicyFromRights(decisions, readAction)
	enforceWrite := ContentPolicyFromRights(decisions, writeAction)
	subjects := make(chan rightsclient.Subject, 64)
	o.StorageWritePolicy = func(ctx context.Context, peer *identity.Peer, resource string) error {
		if subject, e := rightsclient.SubjectFromPeer(peer); e == nil {
			select {
			case subjects <- subject:
			default:
			}
		}
		return enforceWrite(ctx, peer, resource)
	}
	o.StorageWriteLimit = limit
	o.StorageWriteRecordPath = filepath.Join(t.TempDir(), "writer-records.json")
	h, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	writer, err := m.ResolveStorageWriter(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := m.ResolveStorage(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	request, err := storageclient.NewRequestID()
	if err != nil {
		t.Fatal(err)
	}

	denied, err := writer.Begin(ctx, request, digest, int64(len(body)))
	if err != nil || denied.Outcome.String() != "forbidden" || provider.finds.Load() != 0 || provider.places.Load() != 0 {
		t.Fatalf("unauthorized writer %+v %v finds=%d places=%d", denied, err, provider.finds.Load(), provider.places.Load())
	}
	var subject rightsclient.Subject
	select {
	case subject = <-subjects:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := policy.Set(subject, readAction, digest, true); err != nil {
		t.Fatal(err)
	}
	readOnly, err := writer.Begin(ctx, request, digest, int64(len(body)))
	if err != nil || readOnly.Outcome.String() != "forbidden" || provider.places.Load() != 0 {
		t.Fatalf("read grant admitted write %+v %v", readOnly, err)
	}
	if err := policy.Set(subject, writeAction, digest, true); err != nil {
		t.Fatal(err)
	}

	begun, err := writer.Begin(ctx, request, digest, int64(len(body)))
	if err != nil || begun.Outcome.String() != "started" || begun.Limit != limit {
		t.Fatalf("authorized begin %+v %v", begun, err)
	}
	upload := *begun.Upload
	if a, err := writer.Append(ctx, upload, 0, body[:65536]); err != nil || a.Outcome.String() != "accepted" {
		t.Fatalf("append %+v %v", a, err)
	}
	if partial, err := reader.Open(ctx, digest); err != nil || partial.Outcome.String() != "not_found" {
		t.Fatalf("partial upload visible %+v %v", partial, err)
	}
	// A lost Begin/Append reply is reconciled through the same identity.
	resumed, err := writer.Begin(ctx, request, digest, int64(len(body)))
	if err != nil || resumed.Outcome.String() != "started" || resumed.Upload.Handle != upload.Handle || resumed.Upload.Received != 65536 {
		t.Fatalf("resume %+v %v", resumed, err)
	}
	stored, err := writer.Write(ctx, request, digest, bytes.NewReader(body), int64(len(body)))
	if err != nil || stored.Evidence.String() != "hashed" || stored.Size != int64(len(body)) {
		t.Fatalf("write %+v %v", stored, err)
	}
	if got := readAll(ctx, t, reader, digest); !bytes.Equal(got, body) {
		t.Fatal("separate reader observed different bytes")
	}

	duplicate, err := writer.Write(ctx, request, digest, bytes.NewReader(body), int64(len(body)))
	if err != nil || duplicate != stored || blobs() != 1 {
		t.Fatalf("duplicate %+v %v objects=%d", duplicate, err, blobs())
	}
	other := []byte("different content under the same identity")
	otherDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(other))
	if err := policy.Set(subject, writeAction, otherDigest, true); err != nil {
		t.Fatal(err)
	}
	placed := provider.places.Load()
	_, err = writer.Write(ctx, request, otherDigest, bytes.NewReader(other), int64(len(other)))
	var outcome *storageclient.OutcomeError
	if !errors.As(err, &outcome) || outcome.Outcome != "conflict" || provider.places.Load() != placed || blobs() != 1 {
		t.Fatalf("conflicting identity %v places=%d objects=%d", err, provider.places.Load()-placed, blobs())
	}

	oversized, err := storageclient.NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	big, err := writer.Begin(ctx, oversized, otherDigest, limit+1)
	if err != nil || big.Outcome.String() != "too_large" || big.Limit != limit || provider.places.Load() != placed {
		t.Fatalf("oversized %+v %v", big, err)
	}
	interrupted, err := storageclient.NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	short, err := writer.Begin(ctx, interrupted, otherDigest, int64(len(other)))
	if err != nil || short.Outcome.String() != "started" {
		t.Fatalf("interrupted begin %+v %v", short, err)
	}
	if a, err := writer.Append(ctx, *short.Upload, 0, other[:10]); err != nil || a.Outcome.String() != "accepted" {
		t.Fatalf("interrupted append %+v %v", a, err)
	}
	if over, err := writer.Append(ctx, *short.Upload, 10, append(other[10:], 'x')); err != nil || over.Outcome.String() != "too_large" || over.Received != 10 {
		t.Fatalf("append beyond declared size %+v %v", over, err)
	}
	if err := policy.Set(subject, readAction, otherDigest, true); err != nil {
		t.Fatal(err)
	}
	if hidden, err := reader.Open(ctx, otherDigest); err != nil || hidden.Outcome.String() != "not_found" {
		t.Fatalf("interrupted upload visible %+v %v", hidden, err)
	}
	if err := policy.Revoke(subject, writeAction, otherDigest); err != nil {
		t.Fatal(err)
	}
	if a, err := writer.Append(ctx, *short.Upload, 10, other[10:]); err != nil || a.Outcome.String() != "forbidden" {
		t.Fatalf("revoked append %+v %v", a, err)
	}
	if info, err := os.Stat(staged(otherDigest)); err != nil || info.Size() != 10 {
		t.Fatalf("revoked append changed staging: %v", err)
	}
	if c, err := writer.Commit(ctx, *short.Upload); err != nil || c.Outcome.String() != "forbidden" {
		t.Fatalf("revoked commit %+v %v", c, err)
	}
	if hidden, err := reader.Open(ctx, otherDigest); err != nil || hidden.Outcome.String() != "not_found" {
		t.Fatalf("revoked commit visible %+v %v", hidden, err)
	}
	if a, err := writer.Abort(ctx, *short.Upload); err != nil || a.Outcome.String() != "aborted" {
		t.Fatalf("abort after revocation %+v %v", a, err)
	}
	if _, err := os.Stat(staged(otherDigest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("aborted staging remains: %v", err)
	}
	revokedBegin, err := writer.Begin(ctx, interrupted, otherDigest, int64(len(other)))
	if err != nil || revokedBegin.Outcome.String() != "forbidden" || provider.places.Load() != placed+1 {
		t.Fatalf("revoked writer %+v %v places=%d", revokedBegin, err, provider.places.Load()-placed)
	}

	h.rights.Close()
	outage, err := writer.Begin(ctx, oversized, otherDigest, int64(len(other)))
	if err != nil || outage.Outcome.String() != "unavailable" {
		t.Fatalf("policy outage %+v %v", outage, err)
	}
	h.storage.Close()
	for {
		_, err = m.ResolveStorageWriter(ctx, client.Requirements{})
		if err != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("writer readiness remained green")
		case <-time.After(time.Millisecond):
		}
	}
	var refusal *client.ResolutionError
	if !errors.As(err, &refusal) || refusal.Status != "not_ready" {
		t.Fatalf("stopped writer %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "incoming")); err != nil || len(entries) != 0 {
		t.Fatalf("stopped writer left staged uploads: %d %v", len(entries), err)
	}
	if _, err := m.ResolveConfig(ctx, client.Requirements{}); err != nil {
		t.Fatal("storage stop disabled config", err)
	}
}
