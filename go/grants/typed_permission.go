package grants

import (
	"context"
	"errors"

	askswire "github.com/openabstractions/abstraction-asks/go/abstraction/asks/api"
	askclient "github.com/openabstractions/abstraction-asks/go/client"
	rightswire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

type PermissionQuestionOperator interface {
	askclient.PermissionDetailsReader
	AnswerPermissionIfPendingContext(ctx context.Context, id, option string) (askswire.OperatorDecision, error)
}
type PermissionRuleOperator interface {
	ListSelectorRulesContext(ctx context.Context, cursor string, limit int64) (rightswire.SelectorRulePage, error)
	ApplyPermissionAllowContext(ctx context.Context, expectedRevision, id string) (rightswire.SelectorEdit, error)
}
type PermissionAnswered struct {
	Decision    askswire.OperatorDecision    `json:"decision"`
	Effect      *rightswire.PermissionEffect `json:"effect,omitempty"`
	Edit        *rightswire.SelectorEdit     `json:"edit,omitempty"`
	RuleOutcome string                       `json:"rule_outcome,omitempty"`
}

// Conditional answer and complete immutable rule effect are separate commits.
// Only the retained occurrence ID reaches Rights; no UI supplies rule targets.
func AnswerPermissionQuestion(ctx context.Context, questions PermissionQuestionOperator, policy PermissionRuleOperator, account, id, option string) (PermissionAnswered, error) {
	decision, err := questions.AnswerPermissionIfPendingContext(ctx, id, option)
	result := PermissionAnswered{Decision: decision}
	if err != nil || decision.Outcome != askswire.OperatorDecisionOutcomeAnswered || decision.Record == nil {
		return result, err
	}
	if option != "allow" {
		return result, nil
	}
	return completePermission(ctx, questions, policy, account, id, result)
}
func RetryPermissionEffect(ctx context.Context, questions askclient.PermissionDetailsReader, policy PermissionRuleOperator, account, id string) (PermissionAnswered, error) {
	return completePermission(ctx, questions, policy, account, id, PermissionAnswered{})
}
func completePermission(ctx context.Context, questions askclient.PermissionDetailsReader, policy PermissionRuleOperator, account, id string, result PermissionAnswered) (PermissionAnswered, error) {
	details, err := askclient.ReadPermissionDetailsContext(ctx, questions, account, id)
	if err != nil {
		return result, err
	}
	q := details.Question.Question
	if q.Outcome != askswire.OperatorQuestionOutcomeFound || q.Record == nil || q.Record.Option != "allow" || q.AnsweredBy == nil || *q.AnsweredBy != askswire.AnswererOperator || details.Effect == nil {
		return result, errors.New("asks: retained typed operator Allow required")
	}
	if result.Decision.Record != nil && (result.Decision.Record.ID != id || result.Decision.Record.Option != "allow") {
		return result, errors.New("asks: different permission answer won")
	}
	result.Decision = askswire.OperatorDecision{Outcome: askswire.OperatorDecisionOutcomeAnswered, Record: q.Record}
	result.Effect = details.Effect
	if policy == nil {
		result.RuleOutcome = "unavailable"
		return result, errors.New("rights: typed effect completion unavailable")
	}
	page, err := policy.ListSelectorRulesContext(ctx, "", 1)
	if err != nil {
		return result, err
	}
	if page.Outcome != rightswire.PolicyPageOutcomePage {
		result.RuleOutcome = page.Outcome.String()
		return result, nil
	}
	edit, err := policy.ApplyPermissionAllowContext(ctx, page.Revision, id)
	if err != nil {
		return result, err
	}
	result.Edit = &edit
	return result, nil
}
