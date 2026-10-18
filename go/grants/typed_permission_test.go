package grants

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	asks "github.com/openabstractions/abstraction-asks/go"
	askswire "github.com/openabstractions/abstraction-asks/go/abstraction/asks/api"
	rightswire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

type typedQuestionsFixture struct {
	details  askswire.TypedPermissionDetails
	decision askswire.OperatorDecision
}

func (q *typedQuestionsFixture) ReadPermissionDetailsContext(context.Context, string) (askswire.TypedPermissionDetails, error) {
	return q.details, nil
}
func (q *typedQuestionsFixture) AnswerPermissionIfPendingContext(_ context.Context, id, option string) (askswire.OperatorDecision, error) {
	if q.decision.Outcome != 0 {
		return q.decision, nil
	}
	r := q.details.Question.Question.Record
	r.Option, r.Yes, r.Answered = option, option == "allow", time.Now().UTC().Format(time.RFC3339Nano)
	by := askswire.AnswererOperator
	q.details.Question.Question.AnsweredBy = &by
	return askswire.OperatorDecision{Outcome: askswire.OperatorDecisionOutcomeAnswered, Record: r}, nil
}

type typedRulesFixture struct {
	revision string
	id       string
	calls    int
	err      error
}

func (p *typedRulesFixture) ListSelectorRulesContext(context.Context, string, int64) (rightswire.SelectorRulePage, error) {
	return rightswire.SelectorRulePage{Outcome: rightswire.PolicyPageOutcomePage, Revision: "revision"}, nil
}
func (p *typedRulesFixture) ApplyPermissionAllowContext(_ context.Context, revision, id string) (rightswire.SelectorEdit, error) {
	p.revision, p.id, p.calls = revision, id, p.calls+1
	return rightswire.SelectorEdit{Outcome: rightswire.PolicyEditOutcomeApplied, Revision: "next"}, p.err
}
func typedQuestions() *typedQuestionsFixture {
	effect := rightswire.PermissionEffect{Subject: rightswire.Subject{Account: "account", Program: filepath.Join(os.TempDir(), "requester.exe")}, Selections: []rightswire.PermissionSelection{{Permission: rightswire.PermissionKey{Action: "abstraction.credentials/read", ResourceProfile: "abstraction.credentials/named@1"}, Selector: rightswire.ResourceSelector{Kind: rightswire.ResourceSelectorKindAll}}}}
	return &typedQuestionsFixture{details: askswire.TypedPermissionDetails{Question: askswire.AttributedQuestion{Question: askswire.OperatorQuestion{Outcome: askswire.OperatorQuestionOutcomeFound, Record: &askswire.RecordMetadata{ID: "question", Asker: "requester", About: effect.Subject.Program, Key: asks.TypedPermissionKey, Text: "OA permission request", Options: []string{"allow", "refuse"}, Asked: time.Now().UTC().Format(time.RFC3339Nano)}}}, Effect: &effect, EffectDigest: asks.PermissionEffectDigest(effect), Origin: "application", Display: &askswire.PermissionDisplay{Selections: []askswire.PermissionDisplaySelection{{Operation: "Read secret values", Target: "All saved secrets", Collection: "Saved secrets in this account", AllEffect: "Present and future saved secrets", Disclosure: "Exports raw secret values"}}}}}
}

func TestTypedPermissionCompletionUsesRetainedIDAndSeparateEffect(t *testing.T) {
	ctx := context.Background()
	q, p := typedQuestions(), &typedRulesFixture{}
	out, err := AnswerPermissionQuestion(ctx, q, p, "account", "question", "allow")
	if err != nil || out.Effect == nil || out.Edit == nil || out.Edit.Outcome != rightswire.PolicyEditOutcomeApplied || p.id != "question" || p.revision != "revision" || p.calls != 1 {
		t.Fatalf("completion = %+v, %v; policy %+v", out, err, p)
	}
	p.err = errors.New("lost effect reply")
	out, err = RetryPermissionEffect(ctx, q, p, "account", "question")
	if err == nil || out.Decision.Record == nil || out.Effect == nil || out.Edit != nil {
		t.Fatalf("uncertain effect inferred applied: %+v, %v", out, err)
	}
	q, p = typedQuestions(), &typedRulesFixture{}
	out, err = AnswerPermissionQuestion(ctx, q, p, "account", "question", "refuse")
	if err != nil || out.Effect != nil || out.Edit != nil || p.calls != 0 {
		t.Fatalf("refusal wrote effect: %+v, %v", out, err)
	}
	q = typedQuestions()
	out, err = AnswerPermissionQuestion(ctx, q, nil, "account", "question", "allow")
	if err == nil || out.Decision.Record == nil || out.Effect == nil || out.Edit != nil || out.RuleOutcome != "unavailable" {
		t.Fatalf("unavailable owner claimed completion: %+v %v", out, err)
	}
}
func TestTypedPermissionCompletionRejectsForeignOrResponderAnswers(t *testing.T) {
	for _, mutate := range []func(*typedQuestionsFixture){
		func(q *typedQuestionsFixture) { q.details.Effect.Subject.Account = "other" },
		func(q *typedQuestionsFixture) {
			by := askswire.AnswererResponder
			q.details.Question.Question.AnsweredBy = &by
		},
		func(q *typedQuestionsFixture) {
			q.details.Question.Question.Record.Option = "refuse"
			q.details.Question.Question.Record.Yes = false
		},
	} {
		q, p := typedQuestions(), &typedRulesFixture{}
		_, _ = q.AnswerPermissionIfPendingContext(context.Background(), "question", "allow")
		mutate(q)
		if _, err := RetryPermissionEffect(context.Background(), q, p, "account", "question"); err == nil || p.calls != 0 {
			t.Fatalf("invalid effect reached policy: %v, %+v", err, p)
		}
	}
}
