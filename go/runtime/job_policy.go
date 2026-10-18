package runtime

import (
	"context"
	"errors"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
	rights "github.com/openabstractions/abstraction-rights/go/client"
)

// Job rights actions and their resource (research/rights-defaults/DECISION.md
// §1, JOB-A13). Submission spends on the account's behalf; account-wide
// inventory and cancelling another program's work cross programs.
const (
	JobSubmitAction    = "abstraction.job/acceptance.submit"
	JobCancelAction    = "abstraction.job/acceptance.cancel"
	JobInventoryAction = "abstraction.job/inventory.read"
	JobResource        = "abstraction.job/acceptance@1"
)

// JobKindResource is the resource submitting one kind of job is decided on
// (JOB-A13): job:<kind>, such as job:inference or job:download. A submit rule
// on JobResource itself covers every kind; operator programs hold that one.
func JobKindResource(kind string) string { return "job:" + kind }

// jobKindName accepts the kind names a rule can name: a lower-case letter,
// then up to 63 lower-case letters, digits, dots, hyphens or underscores.
func jobKindName(kind string) bool {
	if kind == "" || len(kind) > 64 || kind[0] < 'a' || kind[0] > 'z' {
		return false
	}
	for _, r := range kind {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// JobRightsActions maps the generated job methods a rule gates to their rights
// catalogue actions. Keys are "<service wire name>#<method>". A caller's own
// work, cancellation included, is scoped by ownership and has no entry.
var JobRightsActions = map[string]string{
	"abstraction.job/acceptance@1#Submit":        JobSubmitAction,
	"abstraction.job/operator@1#ListAccountWork": JobInventoryAction,
	"abstraction.job/operator@1#CancelOperation": JobCancelAction,
}

// JobPolicyFromRights decides before each mapped job method on JobResource.
// Submit is decided per kind, and an explicit deny wins: a deny on
// JobResource refuses every kind, and a deny on JobKindResource of the
// submission's kind refuses that kind even under a permit on JobResource.
// Otherwise a permit on either admits it, so a program allowed to submit
// inference work alone cannot queue a download (JOB-A13). The kind's rule is
// read without asking the person when JobResource already permits. A kind
// no rule can name is decided on JobResource alone. Methods without an
// action keep the host's same-owner Program authorization. No positive decision is cached. An evaluated refusal yields
// the job contract's forbidden outcome. A decision that cannot be obtained,
// including an absent decider, yields unavailable [JOB-A9]. Neither changes
// state.
func JobPolicyFromRights(decisions Decider, actions map[string]string) acceptanceprovider.MethodPolicy {
	return func(ctx context.Context, peer *identity.Peer, service, method string) error {
		action, mapped := actions[service+"#"+method]
		if !mapped {
			return nil
		}
		unavailable := acceptanceprovider.ErrPolicyUnavailable
		err := requireDecision(ctx, decisions, peer, action, JobResource, unavailable)
		kind, kinded := acceptanceprovider.SubmitKind(ctx)
		if action != JobSubmitAction || !kinded || !jobKindName(kind) || refused(err, "denied") {
			return err
		}
		resource := JobKindResource(kind)
		if refused(err, "not_granted") {
			// No contract-wide rule: the kind's rule decides, and its
			// not_granted asks the person (JOB-A13).
			return requireDecision(ctx, decisions, peer, action, resource, unavailable)
		}
		// A contract-wide permit, or no answer about it: the kind's rule is
		// read only for a deny, which wins over the permit, and asks nothing.
		kindErr := quietDecision(ctx, decisions, peer, action, resource, unavailable)
		switch {
		case refused(kindErr, "denied"):
			// Checked first: the kind's explicit deny is the answer even when
			// the contract-wide decision could not be obtained, so the
			// refusal is forbidden, never unavailable.
			return kindErr
		case err == nil && kindErr != nil && !refused(kindErr, "not_granted") && !refused(kindErr, "forbidden"):
			// Whether the kind is denied is unknown, so the permit cannot
			// be relied on.
			return kindErr
		}
		return err
	}
}

// refused reports an evaluated refusal with this outcome.
func refused(err error, outcome string) bool {
	var decision *rights.DecisionError
	return errors.As(err, &decision) && decision.Outcome == outcome
}

// A SubjectDecider decides one action on one resource for the account and
// program a job journal recorded, asking nobody, and answers the decision's
// outcome word: permitted, denied, not_granted, forbidden, or another word
// for a decision that could not be obtained.
type SubjectDecider func(ctx context.Context, account, program, action, resource string) (string, error)

// errJobStandingUnavailable is a standing decision that could not be obtained.
var errJobStandingUnavailable = errors.New("runtime jobs: the submitter's rule could not be decided")

// JobStandingFromRights decides accepted work again as Submit decided it
// (JOB-A13, JOB-A18), asking nobody: abstraction.job/acceptance.submit on
// JobResource and on the kind's JobKindResource for the subject the journal
// recorded. An explicit deny on either refuses. Otherwise a permit on either
// keeps the work, an account-wide default's permit included (rights
// RIGHTS-R22), and the work ends once neither permits: the person Blocked
// the program, removed its rule or turned the default off. A refusal reads
// rights:<word>, the kind's word where the kind's rule answered. A decision
// that cannot be obtained, or a contract-wide permit whose kind's deny cannot
// be ruled out, is an error.
func JobStandingFromRights(decide SubjectDecider) acceptanceprovider.StandingDecider {
	evaluated := func(word string) bool {
		return word == "permitted" || word == "denied" || word == "not_granted" || word == "forbidden"
	}
	return func(ctx context.Context, subject acceptanceprovider.AuthenticatedSubject, kind string) (string, error) {
		if decide == nil {
			return "", errJobStandingUnavailable
		}
		contract, err := decide(ctx, subject.Account, subject.Program, JobSubmitAction, JobResource)
		if err != nil {
			return "", errors.Join(errJobStandingUnavailable, err)
		}
		if contract == "denied" {
			return "rights:denied", nil
		}
		word := ""
		if jobKindName(kind) {
			if word, err = decide(ctx, subject.Account, subject.Program, JobSubmitAction, JobKindResource(kind)); err != nil {
				return "", errors.Join(errJobStandingUnavailable, err)
			}
			if word == "denied" {
				return "rights:denied", nil
			}
			if !evaluated(word) {
				return "", errJobStandingUnavailable
			}
		}
		switch {
		case contract == "permitted" || word == "permitted":
			return "", nil
		case !evaluated(contract):
			return "", errJobStandingUnavailable
		case word != "":
			return "rights:" + word, nil
		}
		return "rights:" + contract, nil
	}
}
