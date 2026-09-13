package runtime

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go/client"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
)

func TestResolvedInventoryPagesAndDrainedClose(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	o := jobOptions(t)
	h, stop := runJobRuntime(t, o)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	jobs, err := m.ResolveJobs(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	w, err := jobs.GetHistoryWindow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		r, err := jobs.Submit(ctx, api.Submission{Identity: api.RequestIdentity{Key: fmt.Sprint(i), HistoryEpoch: w.HistoryEpoch}, Kind: "download", Spec: []byte(`{"source":"test"}`)})
		if err != nil || r.Outcome != "accepted" {
			t.Fatalf("submit %+v %v", r, err)
		}
	}
	inventory, err := m.ResolveJobInventory(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor := ""
	firstCursor := ""
	for i := 0; i < 20; i++ {
		p, err := inventory.ListWork(ctx, cursor, 1)
		if err != nil || p.Outcome != "page" {
			t.Fatalf("page %+v %v", p, err)
		}
		for _, s := range p.Snapshots {
			if seen[s.Receipt.OperationId] {
				t.Fatal("duplicate operation")
			}
			seen[s.Receipt.OperationId] = true
		}
		if firstCursor == "" {
			firstCursor = p.Next
		}
		if p.Complete {
			break
		}
		cursor = p.Next
	}
	if len(seen) != 3 {
		t.Fatalf("inventory lost operations: %d", len(seen))
	}
	gap, err := inventory.ListWork(ctx, firstCursor, 1)
	if err != nil || gap.Outcome != "gap" {
		t.Fatalf("old cursor %+v %v", gap, err)
	}
	canceled, abort := context.WithCancel(ctx)
	abort()
	if _, err := inventory.ListWork(canceled, "", 1); err == nil {
		t.Fatal("canceled wait succeeded")
	}
	if _, err := inventory.ListWork(ctx, "", 1); err != nil {
		t.Fatal("binding poisoned", err)
	}
	stop()
	p, err := h.jobs.provider.BindInventory("test").ListWork("", 1)
	if err != nil || p.Outcome != "unavailable" {
		t.Fatalf("inventory survived drained host %+v %v", p, err)
	}
}
