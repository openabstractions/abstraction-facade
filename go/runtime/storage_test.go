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
	storage "github.com/openabstractions/abstraction-storage/go"
	storageservice "github.com/openabstractions/abstraction-storage/go/service"
)

type contentFixture struct {
	path, digest string
	size         int64
	finds        atomic.Int32
}

func (*contentFixture) Name() string { return "private-test-provider" }
func (s *contentFixture) Find(digest string) (storage.Ref, bool) {
	s.finds.Add(1)
	return storage.Ref{Store: s.Name(), Digest: digest, Size: s.size}, digest == s.digest
}
func (*contentFixture) Place(string, int64) (storage.Ref, error) {
	return storage.Ref{}, storage.ErrReadOnly
}
func (s *contentFixture) Path(storage.Ref) string { return s.path }

func TestStorageRequiresExplicitPolicy(t *testing.T) {
	for _, o := range []Options{{Storage: &contentFixture{}}, {StoragePolicy: func(context.Context, *identity.Peer, string) error { return nil }}, {StorageEndpoint: "unused"}} {
		if h, err := Listen(o); err == nil {
			h.Close()
			t.Fatal("incomplete storage configuration admitted")
		}
	}
}

func TestResolvedStorageAuthorizationAndLifetime(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	body := bytes.Repeat([]byte("bounded-content"), 10000)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	path := filepath.Join(t.TempDir(), "provider-private")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	provider := &contentFixture{path: path, digest: digest, size: int64(len(body))}
	var allowed atomic.Bool
	var policyOnline atomic.Bool
	o := jobOptions(t)
	o.Storage, o.StorageEndpoint = provider, o.JobEndpoint+"-storage"
	o.StoragePolicy = func(ctx context.Context, peer *identity.Peer, key string) error {
		if !policyOnline.Load() {
			return fmt.Errorf("decision lookup: %w", storageservice.ErrPolicyUnavailable)
		}
		if !allowed.Load() || key != digest {
			return errors.New("content denied")
		}
		return ctx.Err()
	}
	h, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	c, err := m.ResolveStorage(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	unavailable, err := c.Open(ctx, digest)
	if err != nil || unavailable.Outcome != "unavailable" || provider.finds.Load() != 0 {
		t.Fatalf("policy unavailable %+v %v lookups=%d", unavailable, err, provider.finds.Load())
	}
	policyOnline.Store(true)
	denied, err := c.Open(ctx, digest)
	if err != nil || denied.Outcome != "forbidden" || provider.finds.Load() != 0 {
		t.Fatalf("denied=%+v err=%v lookups=%d", denied, err, provider.finds.Load())
	}
	allowed.Store(true)
	opened, err := c.Open(ctx, digest)
	if err != nil || opened.Outcome != "opened" || opened.Resource == nil {
		t.Fatalf("open %+v %v", opened, err)
	}
	resource := *opened.Resource
	policyOnline.Store(false)
	blocked, err := c.Read(ctx, resource, 0, 1)
	if err != nil || blocked.Outcome != "unavailable" {
		t.Fatalf("policy outage %+v %v", blocked, err)
	}
	policyOnline.Store(true)
	if resource.Verification != "unverified" || resource.Digest != digest {
		t.Fatalf("resource %+v", resource)
	}
	var got bytes.Buffer
	for {
		page, err := c.Read(ctx, resource, int64(got.Len()), 65536)
		if err != nil || page.Outcome != "data" || page.Chunk == nil {
			t.Fatalf("read %+v %v", page, err)
		}
		got.Write(page.Chunk.Data)
		if page.Chunk.Eof {
			break
		}
	}
	if !bytes.Equal(got.Bytes(), body) || fmt.Sprintf("sha256:%x", sha256.Sum256(got.Bytes())) != digest {
		t.Fatal("content verification failed")
	}
	allowed.Store(false)
	page, err := c.Read(ctx, resource, 0, 1)
	if err != nil || page.Outcome != "forbidden" {
		t.Fatalf("revoked %+v %v", page, err)
	}
	closed, err := c.Close(ctx, resource)
	if err != nil || closed.Outcome != "closed" {
		t.Fatalf("release after revocation %+v %v", closed, err)
	}
	allowed.Store(true)
	page, err = c.Read(ctx, resource, 0, 1)
	if err != nil || page.Outcome != "gap" {
		t.Fatalf("closed resource %+v %v", page, err)
	}
	h.storage.Close()
	for {
		_, err = m.ResolveStorage(ctx, client.Requirements{})
		if err != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("storage readiness remained green")
		case <-time.After(time.Millisecond):
		}
	}
	var refusal *client.BindingError
	if !errors.As(err, &refusal) || refusal.Status != "not_ready" {
		t.Fatalf("stopped storage %v", err)
	}
	if _, err := m.ResolveConfig(ctx, client.Requirements{}); err != nil {
		t.Fatal("storage failure disabled config", err)
	}
}
