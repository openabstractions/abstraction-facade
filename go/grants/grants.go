// Package grants names the exact rules a person's "allow downloads" or "allow
// inference" writes for one program, and writes them one conditional edit at a
// time (research/rights-defaults/DECISION.md §2). The command line's
// `rights grant --for` and the Abstraction Panel's "Allow…" use it. A bundle is
// a list of exact rules: it adds no wildcard and no grant of its own, and each
// rule keeps its own provenance.
package grants

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	credentials "github.com/openabstractions/abstraction-credentials/go"
	host "github.com/openabstractions/abstraction-facade/go/runtime"
	inference "github.com/openabstractions/abstraction-inference/go"
	rights "github.com/openabstractions/abstraction-rights/go/client"
	routerservice "github.com/openabstractions/abstraction-router/go/service"
)

// Bundle names.
const (
	Downloads = "downloads"
	Inference = "inference"
)

// Names lists the bundles, in the order help shows them.
func Names() []string { return []string{Downloads, Inference} }

// Rule is one exact action on one resource.
type Rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
}

// For selects what a bundle covers.
type For struct {
	// Registries are the model registries a downloads bundle may look up.
	Registries []string `json:"registries,omitempty"`
	// Hosts are the inference hosts an inference bundle may complete on.
	Hosts []string `json:"hosts,omitempty"`
	// Credentials are the registered credential names the program may have
	// applied for it, in either bundle.
	Credentials []string `json:"credentials,omitempty"`
}

// ErrBundle marks a bundle that cannot be named from its selection.
var ErrBundle = errors.New("grants: invalid bundle")

// DefaultWhy is the reason a bundle records when the person gives none.
func DefaultWhy(bundle string) string { return "allow " + bundle }

// Rules returns the exact rules of a bundle, in the order they are written.
//
//   - downloads: acceptance.submit on the job acceptance contract, model lookup
//     on each registry, and apply on each credential.
//   - inference: route on the routes resource, complete on each host, and apply
//     on each credential. At least one host is required.
func Rules(bundle string, f For) ([]Rule, error) {
	for _, list := range [][]string{f.Registries, f.Hosts, f.Credentials} {
		for _, name := range list {
			if name == "" || len(name) > 64 || strings.ContainsAny(name, " \t\r\n,:") {
				return nil, fmt.Errorf("%w: %q is not a registry, host or credential name", ErrBundle, name)
			}
		}
	}
	var rules []Rule
	switch bundle {
	case Downloads:
		if len(f.Hosts) != 0 {
			return nil, fmt.Errorf("%w: downloads names registries and credentials, not hosts", ErrBundle)
		}
		rules = append(rules, Rule{host.JobSubmitAction, host.JobResource})
		for _, registry := range f.Registries {
			rules = append(rules, Rule{host.ModelLookupAction, registry})
		}
	case Inference:
		if len(f.Hosts) == 0 || len(f.Registries) != 0 {
			return nil, fmt.Errorf("%w: inference names at least one host, and no registries", ErrBundle)
		}
		rules = append(rules, Rule{routerservice.ActionRoute, host.RoutesResource})
		for _, name := range f.Hosts {
			rules = append(rules, Rule{inference.ActionComplete, inference.ResourceHost(name)})
		}
	default:
		return nil, fmt.Errorf("%w: no bundle called %q; name %s", ErrBundle, bundle, strings.Join(Names(), " or "))
	}
	for _, name := range f.Credentials {
		rules = append(rules, Rule{credentials.ActionApply, credentials.ResourceFor(name)})
	}
	return slices.CompactFunc(rules, func(a, b Rule) bool { return a == b }), nil
}

// Operator is the rights operator surface a bundle writes through.
type Operator interface {
	SetRuleForContext(ctx context.Context, expected string, rule rights.PolicyRule, ttl time.Duration, why string) (rights.PolicyEdit, error)
}

// Result is what one bundle write did. Landed lists every rule applied, in
// order. When a rule did not apply, Stopped names it, Outcome is its edit
// outcome and Current is the rule the service observed; no later rule was
// sent. Revision is the policy revision after the last applied edit, or the
// one the refusal carried.
type Result struct {
	Outcome  string             `json:"outcome"`
	Revision string             `json:"revision,omitempty"`
	Landed   []Rule             `json:"landed"`
	Stopped  *Rule              `json:"stopped,omitempty"`
	Current  *rights.PolicyRule `json:"current,omitempty"`
}

// Write sets each rule as a permit for subject, one conditional edit at a time,
// starting at revision and continuing at the revision each applied edit
// returns. It stops at the first rule that is not applied and never retries. A
// transport error leaves that rule's outcome uncertain; the error is returned
// with the rules that landed before it.
func Write(ctx context.Context, operator Operator, revision string, subject rights.Subject, rules []Rule, ttl time.Duration, why string) (Result, error) {
	result := Result{Outcome: "applied", Revision: revision, Landed: []Rule{}}
	for _, rule := range rules {
		edit, err := operator.SetRuleForContext(ctx, result.Revision, rights.PolicyRule{Subject: subject, Action: rule.Action, Resource: rule.Resource, Permit: true}, ttl, why)
		if err != nil {
			stopped := rule
			result.Outcome, result.Stopped = "uncertain", &stopped
			return result, err
		}
		if edit.Outcome != rights.PolicyEditOutcomeApplied {
			stopped := rule
			result.Outcome, result.Stopped, result.Current = edit.Outcome.String(), &stopped, edit.Current
			if edit.Revision != "" {
				result.Revision = edit.Revision
			}
			return result, nil
		}
		result.Revision = edit.Revision
		result.Landed = append(result.Landed, rule)
	}
	return result, nil
}
