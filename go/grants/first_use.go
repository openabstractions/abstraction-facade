package grants

import (
	"context"
	"errors"
	"slices"
	"time"

	asks "github.com/openabstractions/abstraction-asks/go"
	askclient "github.com/openabstractions/abstraction-asks/go/client"
	rights "github.com/openabstractions/abstraction-rights/go/client"
)

// FirstUseWhy is the reason a rule written from a first-use answer records.
const FirstUseWhy = "asked at first use"

// QuestionOperator is the asks operator surface a first-use answer goes
// through. The answer is conditional on the question still being pending
// (ASK-O8): an answer that landed first, a delegated one included, reads
// conflict and writes no rule, so every rule-writing consumer is conditional.
type QuestionOperator interface {
	AnswerQuestionIfPendingContext(ctx context.Context, id, option string) (askclient.OperatorDecision, error)
}

// QuestionReader is the asks operator surface a rule retry reads the recorded
// answer through (ASK-O7).
type QuestionReader interface {
	ReadAttributedQuestionContext(ctx context.Context, id string) (askclient.AttributedQuestion, error)
}

// RuleOperator is the rights operator surface a first-use answer writes through.
type RuleOperator interface {
	ListPolicyContext(ctx context.Context, cursor string, limit int64) (rights.PolicyPage, error)
	SetRuleForContext(ctx context.Context, expected string, rule rights.PolicyRule, ttl time.Duration, why string) (rights.PolicyEdit, error)
	SetRulesForContext(ctx context.Context, expected string, rules []rights.PolicyRule, ttl time.Duration, why string) (rights.PolicyEdit, error)
}

// firstUseAllowOperator is the additive completion operation. Legacy custom
// RuleOperator implementations keep their ordinary setter and deny behavior.
type firstUseAllowOperator interface {
	ApplyFirstUseAllowContext(ctx context.Context, expected, id string) (rights.PolicyEdit, error)
}

// RuleReader is the rights operator surface a first-use rule is checked through.
type RuleReader interface {
	ReadRuleContext(ctx context.Context, subject rights.Subject, action, resource string) (rights.RuleRead, error)
}

// The content rights of a request input profile (storage STO-W12, inference
// INF-V4). One first-use answer on an input profile's write writes both.
const (
	ContentWriteAction = "abstraction.storage/content.write"
	ContentReadAction  = "abstraction.storage/content.read"
)

// InputProfileResources are the request input profiles a paired first-use
// question may name.
var InputProfileResources = []string{"input:image", "input:audio"}

// PairedInput reports whether action and resource name a request input
// profile's first-use question: content.write on input:image or input:audio.
// Its answer writes the same answer on content.read for that resource.
func PairedInput(action, resource string) bool {
	if action != ContentWriteAction {
		return false
	}
	for _, input := range InputProfileResources {
		if resource == input {
			return true
		}
	}
	return false
}

// OutputProfileResources are the output profiles a paired first-use question
// may name: images from an image@1 call or an image batch, speech from a
// speech@1 call, and a video job's clip (inference INF-S7, INF-V4).
var OutputProfileResources = []string{"output:image", "output:speech", "output:video"}

// PairedOutput reports whether action and resource name an output profile's
// first-use question: content.write on output:image, output:speech or
// output:video. Its answer writes the same answer on content.read for that
// resource. The read it grants covers only outputs the program's own work
// produced: the runtime reads a generated output back only for the account
// and program its provenance names (storage STO-W12, inference INF-S7).
func PairedOutput(action, resource string) bool {
	return action == ContentWriteAction && slices.Contains(OutputProfileResources, resource)
}

// Paired reports whether action and resource name a paired first-use
// question, an input profile's (PairedInput) or an output profile's
// (PairedOutput).
func Paired(action, resource string) bool {
	return asks.PairedPermission(action, resource)
}

// FirstUseRule is the explicit legacy projection of the rule an answer writes.
// Current services use FirstUsePermissionRules after ReadQuestionAsker.
// for account: the exact permit for allow, the exact deny for never. ok is
// false for another question, a pending one, or another option. A paired
// input-profile answer writes one more rule, FirstUseRules' second.
func FirstUseRule(account string, record askclient.RecordMetadata) (rights.PolicyRule, bool) {
	rules := FirstUseRules(account, record)
	if len(rules) == 0 {
		return rights.PolicyRule{}, false
	}
	return rules[0], true
}

// FirstUseRules is the explicit legacy projection of every rule an answer
// writes for account, in order: the rule the question names, then, for a
// paired input- or output-profile question (Paired), the same answer on
// content.read for the same program and resource (INF-V4). Nil for another
// question, a pending one, or another option. It is retained for native legacy
// integration; operator consumers select the legacy reader only after a service
// explicitly refuses ReadQuestionAsker with unknown_method.
func FirstUseRules(account string, record askclient.RecordMetadata) []rights.PolicyRule {
	permission, ok := asks.LegacyPermission(account, record.Key, record.About, record.Text)
	if !ok {
		return nil
	}
	return FirstUsePermissionRules(account, record, permission)
}

// FirstUsePermissionRules derives the exact complete effect from structured
// trusted occurrence data. Presentation wording supplies no policy target.
func FirstUsePermissionRules(account string, record askclient.RecordMetadata, permission asks.PermissionRequest) []rights.PolicyRule {
	if record.Key != asks.FirstUseKey || !asks.ValidPermissionRequest(permission) || permission.Targets[0].Account != account || (record.Option != "allow" && record.Option != asks.Never.Name) {
		return nil
	}
	permit := record.Option == "allow"
	var rules []rights.PolicyRule
	for _, target := range permission.Targets {
		rules = append(rules, rights.PolicyRule{Subject: rights.Subject{Account: target.Account, Program: target.Program}, Action: target.Action, Resource: target.Resource, Permit: permit})
	}
	return rules
}

// Rule states of an answered first-use question.
const (
	// RuleWritten: the rule on record permits or denies as the answer did.
	RuleWritten = "written"
	// RuleNotWritten: no unexpired rule on record matches the answer.
	// RetryFirstUseRule writes it from the recorded answer.
	RuleNotWritten = "not_written"
	// RuleDelegated: a delegated responder recorded the answer under a bounded
	// permit (ASK-R4). RetryFirstUseRule refuses to make it permanent.
	RuleDelegated = "delegated"
)

// FirstUseRuleState reads the rule an answered first-use question names and
// reports RuleWritten, RuleNotWritten, or the rights service's read outcome
// when it could not tell (forbidden, unavailable, invalid). A transport error
// reads unavailable. A record FirstUseRule refuses reads invalid.
func FirstUseRuleState(ctx context.Context, policy RuleReader, account string, record askclient.RecordMetadata) string {
	rules := FirstUseRules(account, record)
	return firstUseRulesState(ctx, policy, rules)
}

func firstUseRulesState(ctx context.Context, policy RuleReader, rules []rights.PolicyRule) string {
	if len(rules) == 0 {
		return "invalid"
	}
	// Every rule the answer writes must be on record; the first that is not
	// decides the state.
	for _, rule := range rules {
		read, err := policy.ReadRuleContext(ctx, rule.Subject, rule.Action, rule.Resource)
		switch {
		case err != nil:
			return "unavailable"
		case read.Outcome == rights.RuleReadOutcomeFound && read.Record != nil && read.Record.Rule.Permit == rule.Permit:
			continue
		case read.Outcome == rights.RuleReadOutcomeFound || read.Outcome == rights.RuleReadOutcomeExpired || read.Outcome == rights.RuleReadOutcomeUnknown:
			return RuleNotWritten
		default:
			return read.Outcome.String()
		}
	}
	return RuleWritten
}

// ListedFirstUseRuleState reads the structured target and who answered (ASK-O7).
// A rule not on record for a
// responder's answer reads RuleDelegated, which RetryFirstUseRule refuses, in
// place of RuleNotWritten. A failed details read reports unavailable; retry
// independently reads the pinned target again before writing.
func ListedFirstUseRuleState(ctx context.Context, questions QuestionReader, policy RuleReader, account string, record askclient.RecordMetadata) string {
	detailsReader, ok := questions.(askclient.DetailsReader)
	if !ok {
		return "unavailable"
	}
	details, err := askclient.ReadDetailsContext(ctx, detailsReader, account, record.ID)
	if err != nil {
		return "unavailable"
	}
	read := details.Question.Question
	if read.Outcome != askclient.OperatorQuestionOutcomeFound || read.Record == nil || details.Permission == nil {
		return "invalid"
	}
	state := firstUseRulesState(ctx, policy, FirstUsePermissionRules(account, *read.Record, *details.Permission))
	if state != RuleNotWritten {
		return state
	}
	if read.AnsweredBy != nil && *read.AnsweredBy == askclient.AnswererResponder {
		return RuleDelegated
	}
	return state
}

// Answered is a person's answer to a question and, for a first-use question,
// the rule the answer wrote. Rule is nil when the answer writes none: another
// question, a refusal, or an answer the question service did not record.
// RuleOutcome is the rights service's word when no edit was sent: the listing
// outcome, or empty.
type Answered struct {
	Decision askclient.OperatorDecision `json:"decision"`
	Rule     *rights.PolicyRule         `json:"rule,omitempty"`
	Edit     *rights.PolicyEdit         `json:"edit,omitempty"`
	// Paired is the content.read rule written in the same edit as Rule.
	// PairedEdit reports that shared atomic outcome without claiming the
	// primary rule's Current as the paired rule's current state (INF-V4).
	Paired      *rights.PolicyRule `json:"paired,omitempty"`
	PairedEdit  *rights.PolicyEdit `json:"paired_edit,omitempty"`
	RuleOutcome string             `json:"rule_outcome,omitempty"`
}

// AnswerQuestion records the person's option, then, when the question is a
// runtime's first-use question, writes the rule the option names for account:
// allow writes the exact permit, never writes the exact deny, and refuse writes
// nothing (research/rights-defaults/DECISION.md §2). The rule is written at the
// revision listed just before, with FirstUseWhy, and a conflict is reported,
// never retried. The answer is recorded only while the question is pending
// (ASK-O8): an answered question reads conflict, the same option included,
// and no rule is written. RetryFirstUseRule writes a rule that did not land.
// The question service grants nothing.
func AnswerQuestion(ctx context.Context, questions QuestionOperator, policy RuleOperator, account, id, option string) (Answered, error) {
	decision, err := questions.AnswerQuestionIfPendingContext(ctx, id, option)
	result := Answered{Decision: decision}
	if err != nil || decision.Outcome != askclient.OperatorDecisionOutcomeAnswered || decision.Record == nil {
		return result, err
	}
	if result.Decision.Record.Key != asks.FirstUseKey {
		return result, nil
	}
	detailsReader, ok := questions.(askclient.DetailsReader)
	if !ok {
		return result, errors.New("asks: structured question reader unavailable")
	}
	details, err := askclient.ReadDetailsContext(ctx, detailsReader, account, id)
	if err != nil {
		return result, err
	}
	read := details.Question.Question
	if read.Outcome != askclient.OperatorQuestionOutcomeFound || read.Record == nil || read.Record.ID != id || read.Record.Option != option || details.Permission == nil {
		return result, errors.New("asks: the answered permission occurrence is unavailable")
	}
	return writeFirstUseRule(ctx, policy, account, result, *details.Permission)
}

// RetryFirstUseRule reads the question's recorded answer (ASK-O7) and writes
// the rule a first-use answer names, as AnswerQuestion would have. It never
// answers. A pending question reads conflict; the read's unknown, invalid,
// forbidden and unavailable keep their words. Decision carries the recorded
// record when the read found an answer.
func RetryFirstUseRule(ctx context.Context, questions QuestionReader, policy RuleOperator, account, id string) (Answered, error) {
	detailsReader, ok := questions.(askclient.DetailsReader)
	if !ok {
		return Answered{}, errors.New("asks: structured question reader unavailable")
	}
	details, err := askclient.ReadDetailsContext(ctx, detailsReader, account, id)
	if err != nil {
		return Answered{}, err
	}
	read := details.Question.Question
	switch {
	case read.Outcome != askclient.OperatorQuestionOutcomeFound || read.Record == nil:
		outcome := askclient.OperatorDecisionOutcomeUnavailable
		switch read.Outcome {
		case askclient.OperatorQuestionOutcomeUnknown:
			outcome = askclient.OperatorDecisionOutcomeUnknown
		case askclient.OperatorQuestionOutcomeInvalid:
			outcome = askclient.OperatorDecisionOutcomeInvalid
		case askclient.OperatorQuestionOutcomeForbidden:
			outcome = askclient.OperatorDecisionOutcomeForbidden
		}
		return Answered{Decision: askclient.OperatorDecision{Outcome: outcome}}, nil
	case read.Record.Option == "":
		return Answered{Decision: askclient.OperatorDecision{Outcome: askclient.OperatorDecisionOutcomeConflict}}, nil
	case read.AnsweredBy != nil && *read.AnsweredBy == askclient.AnswererResponder:
		// A delegated answer came with a bounded permit (ASK-R4); a retry never
		// turns it into a permanent rule.
		return Answered{Decision: askclient.OperatorDecision{Outcome: askclient.OperatorDecisionOutcomeAnswered, Record: read.Record}, RuleOutcome: RuleDelegated}, nil
	}
	result := Answered{Decision: askclient.OperatorDecision{Outcome: askclient.OperatorDecisionOutcomeAnswered, Record: read.Record}}
	if details.Permission == nil {
		return result, nil
	}
	return writeFirstUseRule(ctx, policy, account, result, *details.Permission)
}

// writeFirstUseRule writes the answer's one or two exact rules in one rights
// edit. A paired answer cannot leave only its write grant behind.
func writeFirstUseRule(ctx context.Context, policy RuleOperator, account string, result Answered, permission asks.PermissionRequest) (Answered, error) {
	rules := FirstUsePermissionRules(account, *result.Decision.Record, permission)
	if len(rules) == 0 {
		return result, nil
	}
	rule := rules[0]
	result.Rule = &rule
	page, err := policy.ListPolicyContext(ctx, "", 1)
	if err != nil {
		return result, err
	}
	if page.Outcome != rights.PolicyPageOutcomePage {
		result.RuleOutcome = page.Outcome.String()
		return result, nil
	}
	var edit rights.PolicyEdit
	if len(rules) == 2 {
		paired := rules[1]
		result.Paired = &paired
	}
	if result.Decision.Record.Option == "allow" {
		apply, supported := policy.(firstUseAllowOperator)
		if !supported {
			result.RuleOutcome = rights.PolicyEditOutcomeUnavailable.String()
			return result, errors.New("rights: first-use Allow completion is unavailable")
		}
		edit, err = apply.ApplyFirstUseAllowContext(ctx, page.Revision, result.Decision.Record.ID)
	} else if len(rules) == 2 {
		edit, err = policy.SetRulesForContext(ctx, page.Revision, rules, 0, FirstUseWhy)
	} else {
		edit, err = policy.SetRuleForContext(ctx, page.Revision, rule, 0, FirstUseWhy)
	}
	if err != nil {
		return result, err
	}
	result.Edit = &edit
	if len(rules) == 2 {
		pairedEdit := edit
		pairedEdit.Current = nil
		result.PairedEdit = &pairedEdit
	}
	return result, nil
}
