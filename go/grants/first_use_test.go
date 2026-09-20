package grants

import (
	"context"
	"testing"
	"time"

	asks "github.com/openabstractions/abstraction-asks/go"
	askclient "github.com/openabstractions/abstraction-asks/go/client"
	rights "github.com/openabstractions/abstraction-rights/go/client"
)

type answering struct{ record askclient.RecordMetadata }

func (a answering) AnswerQuestionContext(_ context.Context, id, option string) (askclient.OperatorDecision, error) {
	record := a.record
	record.ID, record.Option = id, option
	return askclient.OperatorDecision{Outcome: askclient.OperatorDecisionOutcomeAnswered, Record: &record}, nil
}

type policy struct {
	listing rights.PolicyPageOutcome
	edits   []rights.PolicyRule
	whys    []string
}

func (p *policy) ListPolicyContext(context.Context, string, int64) (rights.PolicyPage, error) {
	return rights.PolicyPage{Outcome: p.listing, Revision: "rev-1"}, nil
}

func (p *policy) SetRuleForContext(_ context.Context, expected string, rule rights.PolicyRule, _ time.Duration, why string) (rights.PolicyEdit, error) {
	p.edits, p.whys = append(p.edits, rule), append(p.whys, why)
	return rights.PolicyEdit{Outcome: rights.PolicyEditOutcomeApplied, Revision: "rev-2", Current: &rule}, nil
}

func TestAnsweringAFirstUseQuestionWritesTheRuleItNames(t *testing.T) {
	const program = `C:\ComfyUI\python_embeded\python.exe`
	firstUse := askclient.RecordMetadata{Key: asks.FirstUseKey, About: program, Text: program + " wants to abstraction.model/lookup on hf"}
	for _, c := range []struct {
		option string
		permit *bool
	}{{"allow", boolRef(true)}, {"never", boolRef(false)}, {"refuse", nil}} {
		p := &policy{listing: rights.PolicyPageOutcomePage}
		answered, err := AnswerQuestion(context.Background(), answering{firstUse}, p, "S-1", "q1", c.option)
		if err != nil {
			t.Fatal(err)
		}
		if c.permit == nil {
			if len(p.edits) != 0 || answered.Rule != nil {
				t.Fatalf("%s wrote %+v", c.option, p.edits)
			}
			continue
		}
		want := rights.PolicyRule{Subject: rights.Subject{Account: "S-1", Program: program}, Action: "abstraction.model/lookup", Resource: "hf", Permit: *c.permit}
		if len(p.edits) != 1 || p.edits[0] != want || p.whys[0] != FirstUseWhy || answered.Edit == nil || answered.Edit.Outcome != rights.PolicyEditOutcomeApplied {
			t.Fatalf("%s wrote %+v why %v; answered %+v", c.option, p.edits, p.whys, answered)
		}
	}
	other := askclient.RecordMetadata{Key: "download.reach", About: "example.com", Text: "app wants to fetch from example.com"}
	p := &policy{listing: rights.PolicyPageOutcomePage}
	if answered, err := AnswerQuestion(context.Background(), answering{other}, p, "S-1", "q2", "allow"); err != nil || answered.Rule != nil || len(p.edits) != 0 {
		t.Fatalf("another question wrote a rule %+v %v", answered, err)
	}
	refused := &policy{listing: rights.PolicyPageOutcomeForbidden}
	if answered, err := AnswerQuestion(context.Background(), answering{firstUse}, refused, "S-1", "q3", "allow"); err != nil || answered.RuleOutcome != "forbidden" || answered.Edit != nil {
		t.Fatalf("a refused listing %+v %v", answered, err)
	}
	if _, _, _, ok := asks.FirstUse(asks.FirstUseKey, program, program+" wants to two words on hf"); ok {
		t.Fatal("an action with a space read as a first-use question")
	}
}

func boolRef(v bool) *bool { return &v }

type ruleRecord struct {
	read rights.RuleRead
	err  error
}

func (r ruleRecord) ReadRuleContext(context.Context, rights.Subject, string, string) (rights.RuleRead, error) {
	return r.read, r.err
}

// An answered first-use question whose rule is on record as the answer wrote
// it reads written. A missing, expired or opposite rule reads not_written, and
// a read the rights service refused keeps its word.
func TestAFirstUseRuleStateComparesTheRuleOnRecord(t *testing.T) {
	const program = `C:\ComfyUI\python_embeded\python.exe`
	allowed := askclient.RecordMetadata{ID: "q1", Key: asks.FirstUseKey, About: program, Text: program + " wants to abstraction.model/lookup on hf", Option: "allow"}
	permit := &rights.RuleRecord{Rule: rights.PolicyRule{Permit: true}}
	deny := &rights.RuleRecord{Rule: rights.PolicyRule{Permit: false}}
	for _, c := range []struct {
		reader ruleRecord
		want   string
	}{
		{ruleRecord{read: rights.RuleRead{Outcome: rights.RuleReadOutcomeFound, Record: permit}}, RuleWritten},
		{ruleRecord{read: rights.RuleRead{Outcome: rights.RuleReadOutcomeFound, Record: deny}}, RuleNotWritten},
		{ruleRecord{read: rights.RuleRead{Outcome: rights.RuleReadOutcomeUnknown}}, RuleNotWritten},
		{ruleRecord{read: rights.RuleRead{Outcome: rights.RuleReadOutcomeExpired, Record: permit}}, RuleNotWritten},
		{ruleRecord{read: rights.RuleRead{Outcome: rights.RuleReadOutcomeForbidden}}, "forbidden"},
		{ruleRecord{err: context.DeadlineExceeded}, "unavailable"},
	} {
		if got := FirstUseRuleState(context.Background(), c.reader, "S-1", allowed); got != c.want {
			t.Fatalf("%+v read %q, want %q", c.reader, got, c.want)
		}
	}
	pending := allowed
	pending.Option = ""
	if _, ok := FirstUseRule("S-1", pending); ok {
		t.Fatal("a pending question named a rule")
	}
}
