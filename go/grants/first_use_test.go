package grants

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	asks "github.com/openabstractions/abstraction-asks/go"
	askclient "github.com/openabstractions/abstraction-asks/go/client"
	rights "github.com/openabstractions/abstraction-rights/go/client"
)

// answering is the asks operator over one record. A record without an option
// is pending. AnswerQuestionContext replays a same-option answer (ASK-O3);
// AnswerQuestionIfPendingContext reads conflict for any answered record
// (ASK-O8). ReadAttributedQuestionContext returns the record (ASK-O7).
type answering struct{ record askclient.RecordMetadata }

func (a *answering) ReadQuestionAskerContext(context.Context, string) (askclient.QuestionAsker, error) {
	return askclient.QuestionAsker{}, &askclient.ServiceError{Code: "unknown_method"}
}

func (a *answering) AnswerQuestionContext(_ context.Context, id, option string) (askclient.OperatorDecision, error) {
	if a.record.Option != "" && a.record.Option != option {
		return askclient.OperatorDecision{Outcome: askclient.OperatorDecisionOutcomeConflict}, nil
	}
	record := a.record
	record.ID, record.Option = id, option
	a.record = record
	return askclient.OperatorDecision{Outcome: askclient.OperatorDecisionOutcomeAnswered, Record: &record}, nil
}

func (a *answering) AnswerQuestionIfPendingContext(_ context.Context, id, option string) (askclient.OperatorDecision, error) {
	if a.record.Option != "" {
		return askclient.OperatorDecision{Outcome: askclient.OperatorDecisionOutcomeConflict}, nil
	}
	record := a.record
	record.ID, record.Option = id, option
	a.record = record
	return askclient.OperatorDecision{Outcome: askclient.OperatorDecisionOutcomeAnswered, Record: &record}, nil
}

func (a *answering) ReadAttributedQuestionContext(_ context.Context, id string) (askclient.AttributedQuestion, error) {
	if a.record.Key == "" {
		return askclient.AttributedQuestion{Question: askclient.OperatorQuestion{Outcome: askclient.OperatorQuestionOutcomeUnknown}}, nil
	}
	record := a.record
	record.ID = id
	return askclient.AttributedQuestion{Question: askclient.OperatorQuestion{Outcome: askclient.OperatorQuestionOutcomeFound, Record: &record}}, nil
}

// answeredBy reads one answered record and the service it arrived through.
type answeredBy struct {
	record askclient.RecordMetadata
	by     askclient.Answerer
}

func (a answeredBy) ReadQuestionAskerContext(context.Context, string) (askclient.QuestionAsker, error) {
	return askclient.QuestionAsker{}, &askclient.ServiceError{Code: "unknown_method"}
}

func (a answeredBy) ReadAttributedQuestionContext(_ context.Context, id string) (askclient.AttributedQuestion, error) {
	record, by := a.record, a.by
	record.ID = id
	return askclient.AttributedQuestion{Question: askclient.OperatorQuestion{Outcome: askclient.OperatorQuestionOutcomeFound, Record: &record, AnsweredBy: &by}}, nil
}

// TestRetryFirstUseRuleRefusesAResponderAnswer: a delegated responder's allow
// came with a bounded permit; retrying the rule must not turn it into a
// permanent one. An operator's recorded answer still retries.
func TestRetryFirstUseRuleRefusesAResponderAnswer(t *testing.T) {
	program := filepath.Join(t.TempDir(), "opencode.exe")
	allowed := askclient.RecordMetadata{Key: asks.FirstUseKey, About: program, Text: program + " wants to abstraction.model/lookup on hf", Option: "allow"}
	p := &policy{listing: rights.PolicyPageOutcomePage}
	refused, err := RetryFirstUseRule(context.Background(), answeredBy{allowed, askclient.AnswererResponder}, p, "S-1", "q1")
	if err != nil || refused.RuleOutcome != RuleDelegated || refused.Rule != nil || len(p.edits) != 0 {
		t.Fatalf("a responder's answer must not be retried into a rule: %+v %v edits=%+v", refused, err, p.edits)
	}
	retried, err := RetryFirstUseRule(context.Background(), answeredBy{allowed, askclient.AnswererOperator}, p, "S-1", "q1")
	if err != nil || retried.Edit == nil || len(p.allows) != 1 || p.allows[0] != "q1" || len(p.edits) != 0 {
		t.Fatalf("an operator's answer must still retry: %+v %v edits=%+v", retried, err, p.edits)
	}
}

// TestAnAnswerThatLandedFirstWritesNoRule: a delegated answer recorded before
// the person's answer makes the person's answer conflict, the same option
// included (ASK-O8), and no permanent rule is written over the delegated
// answer's bounded permit.
func TestAnAnswerThatLandedFirstWritesNoRule(t *testing.T) {
	program := filepath.Join(t.TempDir(), "opencode.exe")
	delegated := askclient.RecordMetadata{Key: asks.FirstUseKey, About: program, Text: program + " wants to abstraction.model/lookup on hf", Option: "allow"}
	p := &policy{listing: rights.PolicyPageOutcomePage}
	answered, err := AnswerQuestion(context.Background(), &answering{delegated}, p, "S-1", "q1", "allow")
	if err != nil || answered.Decision.Outcome != askclient.OperatorDecisionOutcomeConflict || answered.Rule != nil || len(p.edits) != 0 {
		t.Fatalf("an answer that landed first must be conflict with no rule: %+v %v edits=%+v", answered, err, p.edits)
	}
}

// TestRetryFirstUseRuleWritesFromTheRecordedAnswer: retrying a rule reads the
// recorded answer and writes the rule it names; it never answers again.
func TestRetryFirstUseRuleWritesFromTheRecordedAnswer(t *testing.T) {
	program := filepath.Join(t.TempDir(), "opencode.exe")
	firstUse := askclient.RecordMetadata{Key: asks.FirstUseKey, About: program, Text: program + " wants to abstraction.model/lookup on hf"}
	answeredRecord := firstUse
	answeredRecord.Option = "allow"
	p := &policy{listing: rights.PolicyPageOutcomePage}
	retried, err := RetryFirstUseRule(context.Background(), &answering{answeredRecord}, p, "S-1", "q1")
	want := rights.PolicyRule{Subject: rights.Subject{Account: "S-1", Program: program}, Action: "abstraction.model/lookup", Resource: "hf", Permit: true}
	if err != nil || retried.Decision.Outcome != askclient.OperatorDecisionOutcomeAnswered || len(p.allows) != 1 || p.allows[0] != "q1" || retried.Rule == nil || *retried.Rule != want || len(p.edits) != 0 {
		t.Fatalf("retry must write the recorded answer's rule: %+v %v edits=%+v", retried, err, p.edits)
	}
	for _, c := range []struct {
		record askclient.RecordMetadata
		want   askclient.OperatorDecisionOutcome
	}{
		{firstUse, askclient.OperatorDecisionOutcomeConflict},
		{askclient.RecordMetadata{}, askclient.OperatorDecisionOutcomeUnknown},
	} {
		p := &policy{listing: rights.PolicyPageOutcomePage}
		got, err := RetryFirstUseRule(context.Background(), &answering{c.record}, p, "S-1", "q1")
		if err != nil || got.Decision.Outcome != c.want || got.Rule != nil || len(p.edits) != 0 {
			t.Fatalf("retry of %+v = %+v %v edits=%+v", c.record, got, err, p.edits)
		}
	}
}

type policy struct {
	listing rights.PolicyPageOutcome
	edits   []rights.PolicyRule
	whys    []string
	allows  []string
}

func (p *policy) ApplyFirstUseAllowContext(_ context.Context, expected, id string) (rights.PolicyEdit, error) {
	p.allows = append(p.allows, id)
	return rights.PolicyEdit{Outcome: rights.PolicyEditOutcomeApplied, Revision: "rev-2"}, nil
}

func (p *policy) ListPolicyContext(context.Context, string, int64) (rights.PolicyPage, error) {
	return rights.PolicyPage{Outcome: p.listing, Revision: "rev-1"}, nil
}

func (p *policy) SetRuleForContext(_ context.Context, expected string, rule rights.PolicyRule, _ time.Duration, why string) (rights.PolicyEdit, error) {
	p.edits, p.whys = append(p.edits, rule), append(p.whys, why)
	return rights.PolicyEdit{Outcome: rights.PolicyEditOutcomeApplied, Revision: "rev-2", Current: &rule}, nil
}
func (p *policy) SetRulesForContext(_ context.Context, expected string, rules []rights.PolicyRule, _ time.Duration, why string) (rights.PolicyEdit, error) {
	for _, rule := range rules {
		p.edits, p.whys = append(p.edits, rule), append(p.whys, why)
	}
	return rights.PolicyEdit{Outcome: rights.PolicyEditOutcomeApplied, Revision: "rev-2", Current: &rules[0]}, nil
}

// legacyPolicy deliberately declares exactly RuleOperator's original methods.
type legacyPolicy struct{ p *policy }

var _ RuleOperator = legacyPolicy{}

func (p legacyPolicy) ListPolicyContext(ctx context.Context, cursor string, limit int64) (rights.PolicyPage, error) {
	return p.p.ListPolicyContext(ctx, cursor, limit)
}
func (p legacyPolicy) SetRuleForContext(ctx context.Context, expected string, rule rights.PolicyRule, ttl time.Duration, why string) (rights.PolicyEdit, error) {
	return p.p.SetRuleForContext(ctx, expected, rule, ttl, why)
}
func (p legacyPolicy) SetRulesForContext(ctx context.Context, expected string, rules []rights.PolicyRule, ttl time.Duration, why string) (rights.PolicyEdit, error) {
	return p.p.SetRulesForContext(ctx, expected, rules, ttl, why)
}

func TestLegacyRuleOperatorRetainsAnswersAndOrdinaryDenyBehavior(t *testing.T) {
	program := filepath.Join(t.TempDir(), "opencode.exe")
	firstUse := askclient.RecordMetadata{Key: asks.FirstUseKey, About: program, Text: program + " wants to abstraction.model/lookup on hf"}
	for _, option := range []string{"allow", "never", "refuse"} {
		t.Run(option, func(t *testing.T) {
			p := &policy{listing: rights.PolicyPageOutcomePage}
			result, err := AnswerQuestion(context.Background(), &answering{firstUse}, legacyPolicy{p}, "S-1", "q1", option)
			if result.Decision.Outcome != askclient.OperatorDecisionOutcomeAnswered || result.Decision.Record == nil || result.Decision.Record.ID != "q1" || result.Decision.Record.Option != option {
				t.Fatalf("answer was not retained: %+v %v", result, err)
			}
			if option == "allow" {
				if err == nil || result.RuleOutcome != "unavailable" || result.Edit != nil || len(p.edits) != 0 || len(p.allows) != 0 {
					t.Fatalf("unsupported Allow wrote rights: %+v %v edits=%+v", result, err, p.edits)
				}
				record := *result.Decision.Record
				if retry, err := RetryFirstUseRule(context.Background(), &answering{record}, legacyPolicy{p}, "S-1", "q1"); err == nil || retry.Decision.Record == nil || retry.Decision.Record.ID != "q1" || retry.Edit != nil || len(p.edits) != 0 {
					t.Fatalf("unsupported Retry changed answer or rights: %+v %v", retry, err)
				}
				return
			}
			if err != nil || option == "never" && (len(p.edits) != 1 || p.edits[0].Permit || result.Edit == nil) || option == "refuse" && (len(p.edits) != 0 || result.Rule != nil) {
				t.Fatalf("legacy %s behavior changed: %+v %v edits=%+v", option, result, err, p.edits)
			}
		})
	}
}

func TestAnsweringAFirstUseQuestionWritesTheRuleItNames(t *testing.T) {
	program := filepath.Join(t.TempDir(), "python_embeded", "python.exe")
	firstUse := askclient.RecordMetadata{Key: asks.FirstUseKey, About: program, Text: program + " wants to abstraction.model/lookup on hf"}
	for _, c := range []struct {
		option string
		permit *bool
	}{{"allow", boolRef(true)}, {"never", boolRef(false)}, {"refuse", nil}} {
		p := &policy{listing: rights.PolicyPageOutcomePage}
		answered, err := AnswerQuestion(context.Background(), &answering{firstUse}, p, "S-1", "q1", c.option)
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
		if answered.Rule == nil || *answered.Rule != want || answered.Edit == nil || answered.Edit.Outcome != rights.PolicyEditOutcomeApplied || c.option == "allow" && (len(p.allows) != 1 || p.allows[0] != "q1" || len(p.edits) != 0) || c.option == "never" && (len(p.edits) != 1 || p.edits[0] != want || p.whys[0] != FirstUseWhy) {
			t.Fatalf("%s wrote %+v why %v; answered %+v", c.option, p.edits, p.whys, answered)
		}
	}
	other := askclient.RecordMetadata{Key: "download.reach", About: "example.com", Text: "app wants to fetch from example.com"}
	p := &policy{listing: rights.PolicyPageOutcomePage}
	if answered, err := AnswerQuestion(context.Background(), &answering{other}, p, "S-1", "q2", "allow"); err != nil || answered.Rule != nil || len(p.edits) != 0 {
		t.Fatalf("another question wrote a rule %+v %v", answered, err)
	}
	refused := &policy{listing: rights.PolicyPageOutcomeForbidden}
	if answered, err := AnswerQuestion(context.Background(), &answering{firstUse}, refused, "S-1", "q3", "allow"); err != nil || answered.RuleOutcome != "forbidden" || answered.Edit != nil {
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
	program := filepath.Join(t.TempDir(), "python_embeded", "python.exe")
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

// INF-V4: one answer to a paired input-profile question writes the same
// answer on content.write and content.read for that resource and program, and
// for no other program. A first-use question on any other action still writes
// one rule.
func TestAPairedInputAnswerWritesBothContentRules(t *testing.T) {
	program := filepath.Join(t.TempDir(), "opencode.exe")
	paired := askclient.RecordMetadata{Key: asks.FirstUseKey, About: program,
		Text: program + " wants to abstraction.storage/content.write on input:audio"}
	for _, option := range []string{"allow", "never"} {
		p := &policy{listing: rights.PolicyPageOutcomePage}
		answered, err := AnswerQuestion(context.Background(), &answering{paired}, p, "S-1", "q1", option)
		if err != nil {
			t.Fatal(err)
		}
		subject := rights.Subject{Account: "S-1", Program: program}
		permit := option == "allow"
		want := []rights.PolicyRule{
			{Subject: subject, Action: ContentWriteAction, Resource: "input:audio", Permit: permit},
			{Subject: subject, Action: ContentReadAction, Resource: "input:audio", Permit: permit},
		}
		if answered.Rule == nil || *answered.Rule != want[0] || option == "allow" && (len(p.allows) != 1 || len(p.edits) != 0) || option == "never" && (len(p.edits) != 2 || p.edits[0] != want[0] || p.edits[1] != want[1]) {
			t.Fatalf("%s wrote %+v", option, p.edits)
		}
		if answered.Paired == nil || *answered.Paired != want[1] || answered.PairedEdit == nil {
			t.Fatalf("%s answered %+v", option, answered)
		}
	}
	// A write that did not apply writes no read rule.
	conflicted := &conflictingPolicy{}
	if answered, err := AnswerQuestion(context.Background(), &answering{paired}, conflicted, "S-1", "q2", "allow"); err != nil || len(conflicted.allows) != 1 || conflicted.allows[0] != "q2" || len(conflicted.edits) != 0 || answered.PairedEdit == nil || answered.PairedEdit.Outcome != rights.PolicyEditOutcomeConflict {
		t.Fatalf("a conflicted paired edit did not share one outcome: %+v %+v %v", conflicted.edits, answered, err)
	}
	// The state reads written only when both rules are on record.
	allowed := paired
	allowed.ID, allowed.Option = "q1", "allow"
	if got := FirstUseRuleState(context.Background(), pairedRules{ContentWriteAction: true}, "S-1", allowed); got != RuleNotWritten {
		t.Fatalf("write rule alone read %q", got)
	}
	if got := FirstUseRuleState(context.Background(), pairedRules{ContentWriteAction: true, ContentReadAction: true}, "S-1", allowed); got != RuleWritten {
		t.Fatalf("both rules read %q", got)
	}
	if PairedInput(ContentWriteAction, "output:image") || PairedInput(ContentReadAction, "input:image") || !PairedInput(ContentWriteAction, "input:image") {
		t.Fatal("PairedInput names another question")
	}
}

// INF-V4, INF-S7: an output profile's question pairs the same way: one
// answer on content.write for output:video writes the same answer on
// content.read, for that program alone. Image and speech outputs pair too;
// live output and a read question pair nothing.
func TestAPairedOutputAnswerWritesBothContentRules(t *testing.T) {
	program := filepath.Join(t.TempDir(), "comfyui.exe")
	paired := askclient.RecordMetadata{Key: asks.FirstUseKey, About: program,
		Text: program + " wants to abstraction.storage/content.write on output:video"}
	p := &policy{listing: rights.PolicyPageOutcomePage}
	answered, err := AnswerQuestion(context.Background(), &answering{paired}, p, "S-1", "q1", "allow")
	if err != nil {
		t.Fatal(err)
	}
	subject := rights.Subject{Account: "S-1", Program: program}
	want := []rights.PolicyRule{
		{Subject: subject, Action: ContentWriteAction, Resource: "output:video", Permit: true},
		{Subject: subject, Action: ContentReadAction, Resource: "output:video", Permit: true},
	}
	if len(p.allows) != 1 || p.allows[0] != "q1" || len(p.edits) != 0 || answered.Rule == nil || *answered.Rule != want[0] || answered.Paired == nil || *answered.Paired != want[1] {
		t.Fatalf("answering an output question wrote %+v (%+v)", p.edits, answered)
	}
	for _, other := range [][2]string{{ContentWriteAction, "output:live"}, {ContentReadAction, "output:video"}, {ContentReadAction, "output:speech"}} {
		if PairedOutput(other[0], other[1]) || Paired(other[0], other[1]) {
			t.Fatalf("%s on %s read as a paired output question", other[0], other[1])
		}
	}
	if !PairedOutput(ContentWriteAction, "output:image") || !PairedOutput(ContentWriteAction, "output:speech") || !Paired(ContentWriteAction, "input:image") {
		t.Fatal("a paired question was not read as one")
	}
}

type conflictingPolicy struct {
	edits  []rights.PolicyRule
	allows []string
}

func (p *conflictingPolicy) ApplyFirstUseAllowContext(_ context.Context, expected, id string) (rights.PolicyEdit, error) {
	p.allows = append(p.allows, id)
	return rights.PolicyEdit{Outcome: rights.PolicyEditOutcomeConflict, Revision: "rev-9"}, nil
}

func (p *conflictingPolicy) ListPolicyContext(context.Context, string, int64) (rights.PolicyPage, error) {
	return rights.PolicyPage{Outcome: rights.PolicyPageOutcomePage, Revision: "rev-1"}, nil
}

func (p *conflictingPolicy) SetRuleForContext(_ context.Context, _ string, rule rights.PolicyRule, _ time.Duration, _ string) (rights.PolicyEdit, error) {
	p.edits = append(p.edits, rule)
	return rights.PolicyEdit{Outcome: rights.PolicyEditOutcomeConflict, Revision: "rev-9"}, nil
}
func (p *conflictingPolicy) SetRulesForContext(_ context.Context, _ string, rules []rights.PolicyRule, _ time.Duration, _ string) (rights.PolicyEdit, error) {
	p.edits = append(p.edits, rules...)
	return rights.PolicyEdit{Outcome: rights.PolicyEditOutcomeConflict, Revision: "rev-9"}, nil
}

// pairedRules reads a permit for each action set true and no rule otherwise.
type pairedRules map[string]bool

func (r pairedRules) ReadRuleContext(_ context.Context, _ rights.Subject, action, _ string) (rights.RuleRead, error) {
	if r[action] {
		return rights.RuleRead{Outcome: rights.RuleReadOutcomeFound, Record: &rights.RuleRecord{Rule: rights.PolicyRule{Permit: true}}}, nil
	}
	return rights.RuleRead{Outcome: rights.RuleReadOutcomeUnknown}, nil
}
