package client

import (
	"context"
	"errors"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	"slices"
	"testing"
)

func TestBoundJobsPreservesValidationAndContext(t *testing.T) {
	c, h, tr := jobsFixture(t)
	b := c.WithContext(context.Background())
	if _, err := b.GetHistoryWindow(); err != nil {
		t.Fatal(err)
	}
	id := api.RequestIdentity{Key: "bound", HistoryEpoch: "epoch"}
	s := api.Submission{Identity: id, Kind: "download", Spec: []byte("opaque"), RequiredGuarantees: []string{"explicit"}}
	if _, err := b.Submit(s); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.submitted.RequiredGuarantees, []string{"explicit", "bound"}) {
		t.Fatal("binding requirements lost")
	}
	h.result = func(id api.RequestIdentity) api.AcceptanceResult {
		r := receipt(id)
		r.Receipt.AcceptedGuarantees = []string{"explicit"}
		return r
	}
	if _, err := b.Submit(s); err == nil {
		t.Fatal("weakened receipt accepted")
	}
	h.result = func(id api.RequestIdentity) api.AcceptanceResult {
		r := receipt(id)
		r.Receipt.LogicalOwner = "other"
		return r
	}
	if _, err := b.Reconcile(id); err == nil {
		t.Fatal("owner changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := tr.calls
	if _, err := c.WithContext(ctx).Reconcile(id); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if tr.calls != before || h.cancels != 0 {
		t.Fatal("canceled waiting made effects")
	}
	h.result = receipt
	if _, err := c.WithContext(context.Background()).Reconcile(id); err != nil {
		t.Fatal(err)
	}
}
