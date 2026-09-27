package grants

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	rights "github.com/openabstractions/abstraction-rights/go/client"
)

// recorder applies edits at the revision it last issued and refuses the edit
// named in refuse with that outcome.
type recorder struct {
	revision int
	edits    []rights.PolicyRule
	whys     []string
	refuse   map[int]rights.PolicyEditOutcome
	fail     int
}

func (r *recorder) SetRuleForContext(_ context.Context, expected string, rule rights.PolicyRule, _ time.Duration, why string) (rights.PolicyEdit, error) {
	index := len(r.edits)
	r.edits, r.whys = append(r.edits, rule), append(r.whys, why)
	if r.fail == index+1 {
		return rights.PolicyEdit{}, errors.New("reply lost")
	}
	if expected != fmt.Sprintf("rev-%d", r.revision) {
		return rights.PolicyEdit{Outcome: rights.PolicyEditOutcomeConflict, Revision: fmt.Sprintf("rev-%d", r.revision)}, nil
	}
	if outcome, ok := r.refuse[index]; ok {
		current := rule
		current.Permit = false
		return rights.PolicyEdit{Outcome: outcome, Revision: fmt.Sprintf("rev-%d", r.revision), Current: &current}, nil
	}
	r.revision++
	return rights.PolicyEdit{Outcome: rights.PolicyEditOutcomeApplied, Revision: fmt.Sprintf("rev-%d", r.revision), Current: &rule}, nil
}

func TestBundlesNameExactRules(t *testing.T) {
	downloads, err := Rules(Downloads, For{Registries: []string{"hf"}, Credentials: []string{"hf"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Rule{{"abstraction.job/acceptance.submit", "abstraction.job/acceptance@1"}, {"abstraction.model/lookup", "hf"}, {"abstraction.credentials/apply", "credential:hf"}}
	if !reflect.DeepEqual(downloads, want) {
		t.Fatalf("downloads %v, want %v", downloads, want)
	}
	inference, err := Rules(Inference, For{Hosts: []string{"openrouter"}, Credentials: []string{"openrouter"}})
	if err != nil {
		t.Fatal(err)
	}
	want = []Rule{{"abstraction.router/inventory.read", "abstraction.router/inventory"}, {"abstraction.router/route", "abstraction.router/routes"}, {"abstraction.inference/complete", "host:openrouter"}, {"abstraction.credentials/apply", "credential:openrouter"}}
	if !reflect.DeepEqual(inference, want) {
		t.Fatalf("inference %v, want %v", inference, want)
	}
	for _, bad := range []struct {
		bundle string
		f      For
	}{
		{"everything", For{}}, {Inference, For{}}, {Inference, For{Hosts: []string{"a"}, Registries: []string{"hf"}}},
		{Downloads, For{Hosts: []string{"a"}}}, {Downloads, For{Registries: []string{"h f"}}}, {Downloads, For{Credentials: []string{""}}},
	} {
		if _, err := Rules(bad.bundle, bad.f); !errors.Is(err, ErrBundle) {
			t.Fatalf("%s %+v: %v", bad.bundle, bad.f, err)
		}
	}
}

// Every rule is written with the reason at the revision the previous edit
// returned. A refusal mid-bundle stops the bundle and reports what landed; a
// lost reply is uncertain.
func TestWriteStopsAtTheFirstRuleThatDoesNotApply(t *testing.T) {
	subject := rights.Subject{Account: "S-1", Program: `C:\apps\python.exe`}
	rules, _ := Rules(Downloads, For{Registries: []string{"hf", "ollama"}})
	ctx := context.Background()

	all := &recorder{}
	result, err := Write(ctx, all, "rev-0", subject, rules, 0, "allow downloads")
	if err != nil || result.Outcome != "applied" || !reflect.DeepEqual(result.Landed, rules) || result.Revision != "rev-3" || result.Stopped != nil {
		t.Fatalf("whole bundle %+v %v", result, err)
	}
	for i, edit := range all.edits {
		if !edit.Permit || edit.Subject != subject || all.whys[i] != "allow downloads" || edit.Action != rules[i].Action || edit.Resource != rules[i].Resource {
			t.Fatalf("edit %d %+v why %q", i, edit, all.whys[i])
		}
	}

	conflict := &recorder{refuse: map[int]rights.PolicyEditOutcome{1: rights.PolicyEditOutcomeConflict}}
	result, err = Write(ctx, conflict, "rev-0", subject, rules, 0, "allow downloads")
	if err != nil || result.Outcome != "conflict" || len(result.Landed) != 1 || result.Landed[0] != rules[0] || *result.Stopped != rules[1] || result.Current == nil || len(conflict.edits) != 2 {
		t.Fatalf("conflict mid-bundle %+v %v; %d edits sent", result, err, len(conflict.edits))
	}

	stale := &recorder{}
	result, err = Write(ctx, stale, "rev-old", subject, rules, 0, "allow downloads")
	if err != nil || result.Outcome != "conflict" || len(result.Landed) != 0 || *result.Stopped != rules[0] || len(stale.edits) != 1 {
		t.Fatalf("stale revision %+v %v", result, err)
	}

	lost := &recorder{fail: 3}
	result, err = Write(ctx, lost, "rev-0", subject, rules, 0, "allow downloads")
	if err == nil || result.Outcome != "uncertain" || len(result.Landed) != 2 || *result.Stopped != rules[2] {
		t.Fatalf("lost reply %+v %v", result, err)
	}
}
