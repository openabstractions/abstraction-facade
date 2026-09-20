package grants

import (
	"context"
	"time"

	asks "github.com/openabstractions/abstraction-asks/go"
	askclient "github.com/openabstractions/abstraction-asks/go/client"
	rights "github.com/openabstractions/abstraction-rights/go/client"
)

// FirstUseWhy is the reason a rule written from a first-use answer records.
const FirstUseWhy = "asked at first use"

// QuestionOperator is the asks operator surface a first-use answer goes through.
type QuestionOperator interface {
	AnswerQuestionContext(ctx context.Context, id, option string) (askclient.OperatorDecision, error)
}

// RuleOperator is the rights operator surface a first-use answer writes through.
type RuleOperator interface {
	ListPolicyContext(ctx context.Context, cursor string, limit int64) (rights.PolicyPage, error)
	SetRuleForContext(ctx context.Context, expected string, rule rights.PolicyRule, ttl time.Duration, why string) (rights.PolicyEdit, error)
}

// RuleReader is the rights operator surface a first-use rule is checked through.
type RuleReader interface {
	ReadRuleContext(ctx context.Context, subject rights.Subject, action, resource string) (rights.RuleRead, error)
}

// FirstUseRule is the rule an answer to a runtime's first-use question writes
// for account: the exact permit for allow, the exact deny for never. ok is
// false for another question, a pending one, or another option.
func FirstUseRule(account string, record askclient.RecordMetadata) (rights.PolicyRule, bool) {
	program, action, resource, ok := asks.FirstUse(record.Key, record.About, record.Text)
	if !ok || (record.Option != "allow" && record.Option != asks.Never.Name) {
		return rights.PolicyRule{}, false
	}
	return rights.PolicyRule{Subject: rights.Subject{Account: account, Program: program}, Action: action, Resource: resource, Permit: record.Option == "allow"}, true
}

// Rule states of an answered first-use question.
const (
	// RuleWritten: the rule on record permits or denies as the answer did.
	RuleWritten = "written"
	// RuleNotWritten: no unexpired rule on record matches the answer. Answering
	// the same option again writes it.
	RuleNotWritten = "not_written"
)

// FirstUseRuleState reads the rule an answered first-use question names and
// reports RuleWritten, RuleNotWritten, or the rights service's read outcome
// when it could not tell (forbidden, unavailable, invalid). A transport error
// reads unavailable. A record FirstUseRule refuses reads invalid.
func FirstUseRuleState(ctx context.Context, policy RuleReader, account string, record askclient.RecordMetadata) string {
	rule, ok := FirstUseRule(account, record)
	if !ok {
		return "invalid"
	}
	read, err := policy.ReadRuleContext(ctx, rule.Subject, rule.Action, rule.Resource)
	switch {
	case err != nil:
		return "unavailable"
	case read.Outcome == rights.RuleReadOutcomeFound && read.Record != nil && read.Record.Rule.Permit == rule.Permit:
		return RuleWritten
	case read.Outcome == rights.RuleReadOutcomeFound || read.Outcome == rights.RuleReadOutcomeExpired || read.Outcome == rights.RuleReadOutcomeUnknown:
		return RuleNotWritten
	}
	return read.Outcome.String()
}

// Answered is a person's answer to a question and, for a first-use question,
// the rule the answer wrote. Rule is nil when the answer writes none: another
// question, a refusal, or an answer the question service did not record.
// RuleOutcome is the rights service's word when no edit was sent: the listing
// outcome, or empty.
type Answered struct {
	Decision    askclient.OperatorDecision `json:"decision"`
	Rule        *rights.PolicyRule         `json:"rule,omitempty"`
	Edit        *rights.PolicyEdit         `json:"edit,omitempty"`
	RuleOutcome string                     `json:"rule_outcome,omitempty"`
}

// AnswerQuestion records the person's option, then, when the question is a
// runtime's first-use question, writes the rule the option names for account:
// allow writes the exact permit, never writes the exact deny, and refuse writes
// nothing (research/rights-defaults/DECISION.md §2). The rule is written at the
// revision listed just before, with FirstUseWhy, and a conflict is reported,
// never retried. Answering the same option again replays the answer and tries
// the rule again. The question service grants nothing.
func AnswerQuestion(ctx context.Context, questions QuestionOperator, policy RuleOperator, account, id, option string) (Answered, error) {
	decision, err := questions.AnswerQuestionContext(ctx, id, option)
	result := Answered{Decision: decision}
	if err != nil || decision.Outcome != askclient.OperatorDecisionOutcomeAnswered || decision.Record == nil {
		return result, err
	}
	rule, ok := FirstUseRule(account, *decision.Record)
	if !ok {
		return result, nil
	}
	result.Rule = &rule
	page, err := policy.ListPolicyContext(ctx, "", 1)
	if err != nil {
		return result, err
	}
	if page.Outcome != rights.PolicyPageOutcomePage {
		result.RuleOutcome = page.Outcome.String()
		return result, nil
	}
	edit, err := policy.SetRuleForContext(ctx, page.Revision, rule, 0, FirstUseWhy)
	if err != nil {
		return result, err
	}
	result.Edit = &edit
	return result, nil
}
