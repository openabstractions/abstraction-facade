package client

import (
	"context"
	"testing"
)

// The binding carries the provider's declared result retention [JOB-A11].
func TestJobsBindingCarriesResultRetention(t *testing.T) {
	c, h, _ := jobsFixture(t)
	if c.Binding().ResultRetentionMs != 0 {
		t.Fatal("retention known before any history window")
	}
	h.resultRetention = 86_400_000
	if w, err := c.GetHistoryWindow(context.Background()); err != nil || w.ResultRetentionMs != 86_400_000 {
		t.Fatalf("history window: %+v %v", w, err)
	}
	if got := c.Binding().ResultRetentionMs; got != 86_400_000 {
		t.Fatalf("binding retention: %d", got)
	}
	h.resultRetention = 0
	if _, err := c.GetHistoryWindow(context.Background()); err != nil || c.Binding().ResultRetentionMs != 0 {
		t.Fatalf("undeclared retention: %d %v", c.Binding().ResultRetentionMs, err)
	}
	h.resultRetention = -1
	if _, err := c.GetHistoryWindow(context.Background()); serviceCode(err) != "invalid_acceptance" {
		t.Fatalf("negative retention accepted: %v", err)
	}
}
