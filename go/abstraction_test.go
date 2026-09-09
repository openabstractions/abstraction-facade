package abstraction_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go"
	job "github.com/openabstractions/abstraction-job/go"
)

// The whole integration, and the point of this test is what is NOT in it: no
// store, no runner, no path, no hostname, and no branch on who does the work.
//
// It is also the two-binding test in the only form available today. Every line
// is written against interfaces — Machine, download.Client,
// job.Subscription, job.Job — so when a service binding exists this same test
// runs against it unchanged. If any line here has to change to accommodate a
// second binding, then the abstraction was not one.
func TestAnApplicationHoldsNothing(t *testing.T) {
	payload := make([]byte, 3<<20)
	rand.New(rand.NewSource(1)).Read(payload)
	want := sha256.Sum256(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "payload.bin", time.Now(), bytes.NewReader(payload))
	}))
	defer srv.Close()

	// Point the machine at a scratch store, the way a setup step would. It is
	// the only configuration in this test, it is not passed to anything below,
	// and nothing after this line names a location.
	t.Setenv("ABSTRACTION_STORE", t.TempDir())
	dest := t.TempDir()

	a, err := abstraction.Discover()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("bindings: %v", a.Bindings())

	downloads := a.Download()

	// A live collection, bound BEFORE anything is submitted. This is what a UI
	// holds, and it must already contain work that predates this process.
	watching := downloads.Jobs()
	defer watching.Close()

	h, err := downloads.Get(srv.URL+"/payload.bin", dest)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("submitted %s, going to %q", h.ID(), downloads.Where())

	waitFor(t, watching, h.ID())
	if err := h.TakeDelivery(); err != nil {
		t.Fatal(err)
	}
	final, err := h.Destination()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(got); sum != want {
		t.Fatalf("digest %s, want %s", hex.EncodeToString(sum[:]), hex.EncodeToString(want[:]))
	}
	t.Logf("delivered %d bytes, digest matches", len(got))
}

// The README promised a caller of this entry point that a NAS or BITS would
// fetch the bytes. Measured, a facade caller always gets "here": the facade
// links no tier, deliberately, and nothing told anybody. "here" alone is not a
// diagnosis — "no system downloader is installed" and "the tier is not linked
// into this binary" have different fixes and a person can act on neither.
func TestItSaysWhyItIsFetchingHere(t *testing.T) {
	t.Setenv("ABSTRACTION_STORE", t.TempDir())

	a, err := abstraction.Discover()
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, b := range a.Bindings() {
		if strings.HasPrefix(b, "download: ") {
			line = b
		}
	}
	if !strings.HasPrefix(line, "download: here") {
		t.Skipf("something on this machine answers: %q", line)
	}
	if !strings.Contains(line, "no system downloader") {
		t.Fatalf("%q says where, not why; nothing here can be acted on", line)
	}
	t.Log(line)
}

// waitFor watches the collection — never the handle, never the filesystem —
// until the job reaches a terminal state. What a UI does.
//
// Where the bytes went is the handle's answer, not this loop's: an application
// that had to decode another layer's wire format to find its own file would be
// reading past the interface that exists to seal it.
func waitFor(t *testing.T, sub job.Subscription, id string) {
	t.Helper()
	deadline := time.After(90 * time.Second)
	for {
		for _, r := range sub.Records() {
			if r.ID != id {
				continue
			}
			switch r.State {
			case job.StateComplete, job.StateTransferred:
				return
			case job.StateFailed, job.StateCancelled:
				t.Fatalf("job %s ended %s: %s", id, r.State, r.Error)
			}
		}
		select {
		case <-sub.Changes():
		case <-deadline:
			t.Fatalf("job %s never finished", id)
		}
	}
}
