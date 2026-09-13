package client

import (
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	"testing"
)

func TestInventoryRejectsInconsistentPagesWithoutPinningOwner(t *testing.T) {
	id := api.RequestIdentity{Key: "key", HistoryEpoch: "epoch"}
	s := api.OperationSnapshot{Receipt: *receipt(id).Receipt, State: "pending"}
	page := func() api.InventoryPage {
		return api.InventoryPage{Outcome: "page", Snapshots: []api.OperationSnapshot{s}, Next: "cursor:1"}
	}
	for name, mutate := range map[string]func(*api.InventoryPage){
		"refusal data":    func(p *api.InventoryPage) { p.Outcome = "gap" },
		"no progress":     func(p *api.InventoryPage) { p.Next = "cursor:0" },
		"complete cursor": func(p *api.InventoryPage) { p.Complete = true },
		"duplicate":       func(p *api.InventoryPage) { p.Snapshots = append(p.Snapshots, s) },
		"bad state":       func(p *api.InventoryPage) { p.Snapshots[0].State = "invented" },
		"bad identity":    func(p *api.InventoryPage) { p.Snapshots[0].Receipt.Identity.Key = "" },
		"mixed owner": func(p *api.InventoryPage) {
			other := s
			other.Receipt.LogicalOwner = "other"
			other.Receipt.OperationId = "another"
			p.Snapshots = append(p.Snapshots, other)
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := NewJobInventory("endpoint", JobsOptions{})
			p := page()
			mutate(&p)
			if c.validate(p, "cursor:0", 64) == nil {
				t.Fatal("invalid page accepted")
			}
			if c.binding.owner != "" {
				t.Fatal("invalid page pinned owner")
			}
		})
	}
	c, _ := NewJobInventory("endpoint", JobsOptions{RequiredGuarantees: []string{"read-guarantee"}})
	if err := c.validate(page(), "cursor:0", 1); err != nil {
		t.Fatal(err)
	}
	p := page()
	p.Snapshots[0].Receipt.LogicalOwner = "changed"
	if c.validate(p, "cursor:0", 1) == nil {
		t.Fatal("owner changed")
	}
	if err := c.validate(api.InventoryPage{Outcome: "page", Snapshots: []api.OperationSnapshot{}, Next: "cursor:2"}, "cursor:1", 1); err != nil {
		t.Fatal("empty scan page", err)
	}
}
