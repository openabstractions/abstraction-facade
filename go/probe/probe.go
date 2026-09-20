// Package probe is the one list of capability probes that `openabstractions
// probe` and the Abstraction Panel's Explore section run. A probe is one call
// through the resolved service, made as the calling program, whose typed
// outcome and deciding rights rule are reported. Most probes read. A probe
// that writes says so (Probe.Writes), is never a capability's default
// operation, and is never part of All.
package probe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	credentials "github.com/openabstractions/abstraction-credentials/go"
	download "github.com/openabstractions/abstraction-download/go"
	request "github.com/openabstractions/abstraction-download/go/abstraction/download/request"
	facadewire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/client"
	host "github.com/openabstractions/abstraction-facade/go/runtime"
	jobapi "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	model "github.com/openabstractions/abstraction-model/go/client"
	router "github.com/openabstractions/abstraction-router/go/client"
	routerservice "github.com/openabstractions/abstraction-router/go/service"
	storageapi "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
)

// Rule is the rights rule that decides a probe's call in a runtime that
// enforces one. A resource in angle brackets is named by the probe's argument.
type Rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
}

// Subject is a rights subject: an account and an absolute program path.
type Subject struct {
	Account string `json:"account"`
	Program string `json:"program"`
}

// Probe is one call a person can make as a program.
type Probe struct {
	Capability string `json:"capability"`
	Operation  string `json:"operation"`
	Contract   string `json:"contract"`
	Rule       *Rule  `json:"rule,omitempty"`
	// Argument names the argument the call needs, or is empty.
	Argument string `json:"argument,omitempty"`
	// Default marks the operation a capability runs when none is named.
	Default bool `json:"default,omitempty"`
	// Writes marks a call that changes state. It runs only when named.
	Writes bool `json:"writes,omitempty"`
	// Note is shown beside the probe in help and in the Panel.
	Note string `json:"note,omitempty"`
	run  func(ctx context.Context, m *client.Machine, argument string) (outcome string, summary any, err error)
}

// Result is one call's typed answer.
type Result struct {
	Capability string  `json:"capability"`
	Operation  string  `json:"operation"`
	Contract   string  `json:"contract"`
	Subject    Subject `json:"subject"`
	Outcome    string  `json:"outcome"`
	Resolution string  `json:"resolution,omitempty"`
	Detail     string  `json:"detail,omitempty"`
	Rule       *Rule   `json:"rule,omitempty"`
	Summary    any     `json:"summary,omitempty"`
}

// ErrArgument marks an argument a probe cannot use.
var ErrArgument = errors.New("probe: invalid argument")

var local = client.Requirements{Scope: client.ScopeLocal}

// List returns every probe, in the order help lists them and All runs them.
func List() []Probe { return append([]Probe(nil), probes...) }

// All returns the probes `probe all` and a bulk call run: each capability's
// default operation that needs no argument and writes nothing.
func All() []Probe {
	all := []Probe{}
	for _, p := range probes {
		if p.Default && p.Argument == "" && !p.Writes {
			all = append(all, p)
		}
	}
	return all
}

// Find returns the named probe, or the capability's default when operation is
// empty. known reports whether the capability exists.
func Find(capability, operation string) (p Probe, known, found bool) {
	for _, candidate := range probes {
		if candidate.Capability != capability {
			continue
		}
		known = true
		if candidate.Operation == operation || (operation == "" && candidate.Default) {
			return candidate, true, true
		}
	}
	return Probe{}, known, false
}

// CheckArgument refuses an argument the probe does not take, a missing one,
// and one whose shape the call cannot use, wrapping ErrArgument.
func CheckArgument(p Probe, argument string) error {
	switch {
	case p.Argument != "" && argument == "":
		return fmt.Errorf("%w: %s %s needs %s", ErrArgument, p.Capability, p.Operation, p.Argument)
	case p.Argument == "" && argument != "":
		return fmt.Errorf("%w: %s %s takes no argument", ErrArgument, p.Capability, p.Operation)
	}
	switch p.Capability + " " + p.Operation {
	case "model resolve":
		if registry, repo, ok := strings.Cut(argument, ":"); !ok || registry == "" || repo == "" {
			return fmt.Errorf("%w: model resolve: name REGISTRY:REPO[@REVISION], for example ollama:llama3.2@latest", ErrArgument)
		}
	case "rights decide":
		if action, resource, ok := strings.Cut(argument, " "); !ok || action == "" || resource == "" {
			return fmt.Errorf("%w: rights decide: name ACTION and RESOURCE", ErrArgument)
		}
	case "jobs submit":
		if u, err := url.Parse(argument); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return fmt.Errorf("%w: jobs submit: name an http:// or https:// URL without credentials", ErrArgument)
		}
	}
	return nil
}

// RuleFor is the probe's rule with a resource named by the argument filled in,
// or nil when the probe has none or the argument does not name it yet.
func RuleFor(p Probe, argument string) *Rule {
	if p.Capability == "rights" {
		if action, resource, ok := strings.Cut(argument, " "); ok && action != "" && resource != "" {
			return &Rule{action, resource}
		}
		return nil
	}
	if p.Rule == nil {
		return nil
	}
	rule := *p.Rule
	if strings.HasPrefix(rule.Resource, "<") {
		if argument == "" {
			return nil
		}
		rule.Resource = argument
		if p.Capability == "model" {
			rule.Resource, _, _ = strings.Cut(argument, ":")
		}
	}
	return &rule
}

// Decisions are the fixed rules the probes name, each once.
func Decisions() []Rule {
	rules := []Rule{}
	for _, p := range probes {
		if p.Rule == nil || strings.HasPrefix(p.Rule.Resource, "<") {
			continue
		}
		seen := false
		for _, r := range rules {
			seen = seen || r == *p.Rule
		}
		if !seen {
			rules = append(rules, *p.Rule)
		}
	}
	return rules
}

// Self names the running program as a rights subject.
func Self() Subject {
	subject := Subject{}
	if account, err := user.Current(); err == nil {
		subject.Account = account.Uid
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		subject.Program = filepath.Clean(exe)
	}
	return subject
}

// Answered reports whether an outcome is the service doing what was asked.
func Answered(outcome string) bool {
	switch outcome {
	case "read", "listed", "resolved", "page", "applied", "opened", "data", "permitted", "found", "observed", "accepted", "requested", "already_terminal":
		return true
	}
	return false
}

// Run makes the probe's call through m as this program. The caller checks the
// argument first (CheckArgument) and confirms a probe that writes. A refusal
// carries no summary: an empty list there would read as an answer.
func Run(ctx context.Context, m *client.Machine, p Probe, argument string) Result {
	result := Result{Capability: p.Capability, Operation: p.Operation, Contract: p.Contract, Subject: Self(), Rule: RuleFor(p, argument)}
	if err := CheckArgument(p, argument); err != nil {
		result.Outcome, result.Detail = "invalid", err.Error()
		return result
	}
	outcome, summary, err := p.run(ctx, m, argument)
	var resolution *client.ResolutionError
	switch {
	case err == nil:
		result.Outcome = outcome
		// A decision's every outcome is its answer.
		if Answered(outcome) || p.Capability == "rights" {
			result.Summary = summary
		}
	case errors.As(err, &resolution):
		result.Outcome, result.Resolution, result.Detail = string(resolution.Status), string(resolution.Status), err.Error()
	case ServiceCode(err) != "":
		result.Outcome, result.Detail = ServiceCode(err), err.Error()
	default:
		result.Outcome, result.Detail = "error", err.Error()
	}
	return result
}

// ServiceCode finds a generated ServiceError's code anywhere in err's chain.
// Each definition generates its own ServiceError type with Code and Message.
func ServiceCode(err error) string {
	for current := err; current != nil; {
		v := reflect.ValueOf(current)
		if v.Kind() == reflect.Pointer && !v.IsNil() && v.Elem().Kind() == reflect.Struct {
			code := v.Elem().FieldByName("Code")
			if code.IsValid() && code.Kind() == reflect.String && v.Elem().Type().Name() == "ServiceError" && code.String() != "" {
				return code.String()
			}
		}
		next := errors.Unwrap(current)
		if next == nil {
			if joined, ok := current.(interface{ Unwrap() []error }); ok {
				for _, e := range joined.Unwrap() {
					if code := ServiceCode(e); code != "" {
						return code
					}
				}
			}
		}
		current = next
	}
	return ""
}

var probes = []Probe{
	{Capability: "config", Operation: "read", Contract: "abstraction.config/reader@1", Default: true,
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			c, err := m.ResolveConfig(ctx, local)
			if err != nil {
				return "", nil, err
			}
			s, err := c.ReadContext(ctx)
			return "read", map[string]any{"stamp": s.Stamp, "store_set": s.Store != "", "off": len(s.Off)}, err
		}},
	{Capability: "config", Operation: "user", Contract: "abstraction.config/editor@1",
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			e, err := m.ResolveConfigEditor(ctx, local)
			if err != nil {
				return "", nil, err
			}
			s, err := e.ReadUserContext(ctx)
			return "read", map[string]any{"revision": s.Revision, "off": len(s.Values.Off)}, err
		}},
	{Capability: "config", Operation: "rewrite", Contract: "abstraction.config/editor@1", Writes: true,
		Rule: &Rule{host.ConfigEditAction, host.ConfigEditResource},
		Note: "WRITES: replaces your user settings with their current values at the read revision",
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			e, err := m.ResolveConfigEditor(ctx, local)
			if err != nil {
				return "", nil, err
			}
			s, err := e.ReadUserContext(ctx)
			if err != nil {
				return "", nil, err
			}
			r, err := e.ReplaceUserContext(ctx, s.Revision, s.Values)
			return r.Outcome.String(), map[string]any{"read_revision": s.Revision, "revision": r.Snapshot.Revision}, err
		}},
	{Capability: "storage", Operation: "list", Contract: "abstraction.storage/content-changes@1", Default: true,
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			c, err := m.ResolveStorageChanges(ctx, local)
			if err != nil {
				return "", nil, err
			}
			page, err := c.List(ctx, "", 16)
			objects := []string{}
			for _, o := range page.Objects {
				objects = append(objects, o.Digest+" "+strconv.FormatInt(o.Size, 10))
			}
			return page.Outcome.String(), map[string]any{"objects": objects, "complete": page.Complete}, err
		}},
	{Capability: "storage", Operation: "read", Contract: "abstraction.storage/content-reader@1", Argument: "DIGEST",
		run: func(ctx context.Context, m *client.Machine, digest string) (string, any, error) {
			c, err := m.ResolveStorage(ctx, local)
			if err != nil {
				return "", nil, err
			}
			opened, err := c.Open(ctx, digest)
			if err != nil || opened.Outcome != storageapi.OpenOutcomeOpened || opened.Resource == nil {
				return opened.Outcome.String(), nil, err
			}
			defer c.Close(context.WithoutCancel(ctx), *opened.Resource)
			read, err := c.Read(ctx, *opened.Resource, 0, 64)
			summary := map[string]any{}
			if read.Chunk != nil {
				summary["total"], summary["first_bytes"] = read.Chunk.Total, len(read.Chunk.Data)
			}
			return read.Outcome.String(), summary, err
		}},
	{Capability: "storage", Operation: "inventory", Contract: "abstraction.storage/inventory@1",
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			requests := []facadewire.ResolveRequest{}
			for _, contract := range []string{"abstraction.storage/inventory@1", "abstraction.storage/holds@1"} {
				requests = append(requests, facadewire.ResolveRequest{Capability: "abstraction.storage", Contracts: []string{contract}, Guarantees: []string{}, Scope: facadewire.ScopeLocal})
			}
			observed, err := m.Observe(ctx, requests)
			if err != nil {
				return "", nil, &client.ResolutionError{Status: client.RuntimeUnavailable, Capability: "abstraction.storage",
					Contract: "abstraction.storage/inventory@1", LookedFor: "the runtime resolver", Err: err}
			}
			statuses := map[string]string{}
			for _, c := range observed.Capabilities {
				statuses[c.Request.Contracts[0]] = "unobserved"
				if c.Result != nil {
					statuses[c.Request.Contracts[0]] = c.Result.Status.String()
				}
			}
			// The Go facade has no inventory@1 or holds@1 client; the probe reports
			// whether the runtime serves them to this program.
			return statuses["abstraction.storage/inventory@1"], map[string]any{"resolution": statuses, "client": "none in the Go facade"}, nil
		}},
	{Capability: "model", Operation: "resolve", Contract: "abstraction.model/resolver@1", Argument: "REGISTRY:REPO[@REVISION]", Default: true,
		Rule: &Rule{host.ModelLookupAction, "<registry>"},
		run: func(ctx context.Context, m *client.Machine, reference string) (string, any, error) {
			registry, repo, _ := strings.Cut(reference, ":")
			repo, revision, _ := strings.Cut(repo, "@")
			c, err := m.ResolveModel(ctx, local)
			if err != nil {
				return "", nil, err
			}
			r, err := c.ResolveContext(ctx, model.Ref{Registry: registry, Repo: repo, Revision: revision})
			return r.Outcome.String(), map[string]any{"request": r.Request != nil}, err
		}},
	{Capability: "router", Operation: "hosts", Contract: "abstraction.router/router@1", Default: true,
		Rule: &Rule{routerservice.ActionInventory, routerservice.ResourceInventory},
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			c, err := m.ResolveRouter(ctx, local)
			if err != nil {
				return "", nil, err
			}
			s, err := c.HostsContext(ctx, false)
			hosts := []map[string]any{}
			for _, h := range s.Hosts {
				hosts = append(hosts, map[string]any{"host": h.Host, "up": h.Up, "hosted": h.Hosted, "why": h.Why})
			}
			return "listed", map[string]any{"hosts": hosts}, err
		}},
	{Capability: "router", Operation: "models", Contract: "abstraction.router/router@1",
		Rule: &Rule{routerservice.ActionInventory, routerservice.ResourceInventory},
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			c, err := m.ResolveRouter(ctx, local)
			if err != nil {
				return "", nil, err
			}
			s, err := c.ModelsContext(ctx, false)
			return "listed", map[string]any{"families": len(s.Models)}, err
		}},
	{Capability: "router", Operation: "pick", Contract: "abstraction.router/router@1", Argument: "MODEL",
		Rule: &Rule{routerservice.ActionRoute, host.RoutesResource},
		run: func(ctx context.Context, m *client.Machine, name string) (string, any, error) {
			c, err := m.ResolveRouter(ctx, local)
			if err != nil {
				return "", nil, err
			}
			r, err := c.PickContext(ctx, router.PickRequest{Model: name})
			return r.Decision.Verdict, map[string]any{"host": r.Decision.Host, "model": r.Decision.Model, "withheld": r.Decision.Withheld}, err
		}},
	{Capability: "jobs", Operation: "inventory", Contract: "abstraction.job/inventory@1", Default: true,
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			c, err := m.ResolveJobInventory(ctx, local)
			if err != nil {
				return "", nil, err
			}
			page, err := c.ListWork(ctx, "", 32)
			labels := []string{}
			for _, s := range page.Snapshots {
				labels = append(labels, s.Label+" "+s.State.String())
			}
			return page.Outcome.String(), map[string]any{"work": labels, "complete": page.Complete}, err
		}},
	{Capability: "jobs", Operation: "all", Contract: "abstraction.job/operator@1",
		Rule: &Rule{host.JobInventoryAction, host.JobResource},
		Note: "every program's work in this account",
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			c, err := m.ResolveJobOperator(ctx, local)
			if err != nil {
				return "", nil, err
			}
			page, err := c.ListAccountWork(ctx, "", 32)
			work := []string{}
			for _, s := range page.Snapshots {
				work = append(work, s.Receipt.OperationID+" "+s.Label+" "+s.State.String())
			}
			return page.Outcome.String(), map[string]any{"work": work, "complete": page.Complete}, err
		}},
	{Capability: "jobs", Operation: "cancel", Contract: "abstraction.job/operator@1", Argument: "OPERATION", Writes: true,
		Rule: &Rule{host.JobCancelAction, host.JobResource},
		Note: "WRITES: records cancellation intent on any program's operation by its id",
		run: func(ctx context.Context, m *client.Machine, operation string) (string, any, error) {
			c, err := m.ResolveJobOperator(ctx, local)
			if err != nil {
				return "", nil, err
			}
			r, err := c.CancelOperation(ctx, operation)
			return r.Outcome.String(), map[string]any{"operation": operation}, err
		}},
	{Capability: "jobs", Operation: "submit", Contract: "abstraction.job/acceptance@1", Argument: "URL", Writes: true,
		Rule: &Rule{host.JobSubmitAction, host.JobResource},
		Note: "WRITES: accepts a download of URL as this program; the same URL presents the same request again",
		run: func(ctx context.Context, m *client.Machine, locator string) (string, any, error) {
			c, err := m.ResolveJobs(ctx, local)
			if err != nil {
				return "", nil, err
			}
			window, err := c.GetHistoryWindow(ctx)
			if err != nil {
				return "", nil, err
			}
			spec := request.Encode(&request.Request{Sources: []request.Source{{Scheme: strings.SplitN(locator, ":", 2)[0], Locator: locator}}})
			sum := sha256.Sum256(append([]byte("openabstractions-probe\x00"), spec...))
			submission := jobapi.Submission{Identity: jobapi.RequestIdentity{Key: "probe-" + hex.EncodeToString(sum[:16]), HistoryEpoch: window.HistoryEpoch},
				Kind: download.Kind, Spec: spec, RequiredGuarantees: slices.Clone(jobapi.AdmissionGuarantees), Label: "openabstractions probe"}
			r, err := c.Submit(ctx, submission)
			summary := map[string]any{"key": submission.Identity.Key}
			if r.Receipt != nil {
				summary["operation"] = r.Receipt.OperationID
			}
			return r.Outcome.String(), summary, err
		}},
	{Capability: "logging", Operation: "history", Contract: "abstraction.logging/reader@1", Default: true,
		Rule: &Rule{host.LogHistoryAction, host.LogHistoryResource},
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			c, err := m.ResolveLogReader(ctx, local)
			if err != nil {
				return "", nil, err
			}
			page, err := c.ReadContext(ctx, "", 16, 65536)
			return page.Outcome.String(), map[string]any{"records": len(page.Records), "at_end": page.AtEnd}, err
		}},
	{Capability: "credentials", Operation: "list", Contract: "abstraction.credentials/holder@1", Default: true,
		Rule: &Rule{credentials.ActionRead, credentials.ResourceAccount},
		Note: "metadata only; no probe reads a secret",
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			c, err := m.ResolveCredentials(ctx, local)
			if err != nil {
				return "", nil, err
			}
			page, err := c.List(ctx, "", 32)
			names := []string{}
			for _, r := range page.Records {
				names = append(names, r.Name+" "+r.Kind+" "+string(r.State))
			}
			return page.Outcome.String(), map[string]any{"credentials": names}, err
		}},
	{Capability: "inference", Operation: "hosts", Contract: "abstraction.inference/chat@1", Default: true,
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			// chat@1 has no read call: resolve it, then list the hosted hosts the
			// router reaches for it.
			if _, err := m.ResolveInference(ctx, local); err != nil {
				return "", nil, err
			}
			hosted := []string{}
			if r, err := m.ResolveRouter(ctx, local); err == nil {
				if s, err := r.HostsContext(ctx, false); err == nil {
					for _, h := range s.Hosts {
						if h.Hosted {
							hosted = append(hosted, h.Host)
						}
					}
				}
			}
			return "resolved", map[string]any{"hosted_hosts": hosted}, nil
		}},
	{Capability: "asks", Operation: "pending", Contract: "abstraction.asks/operator@1", Default: true,
		run: func(ctx context.Context, m *client.Machine, _ string) (string, any, error) {
			c, err := m.ResolveAsksOperator(ctx, local)
			if err != nil {
				return "", nil, err
			}
			page, err := c.ListQuestionsContext(ctx, "", 16)
			pending := 0
			for _, q := range page.Records {
				if q.Option == "" {
					pending++
				}
			}
			return page.Outcome.String(), map[string]any{"pending": pending, "listed": len(page.Records)}, err
		}},
	{Capability: "rights", Operation: "decide", Contract: "abstraction.rights/authorization@1", Argument: "ACTION RESOURCE", Default: true,
		Note: "the decision for this program",
		run: func(ctx context.Context, m *client.Machine, argument string) (string, any, error) {
			action, resource, _ := strings.Cut(argument, " ")
			c, err := m.ResolveRights(ctx, local)
			if err != nil {
				return "", nil, err
			}
			d, err := c.DecideContext(ctx, action, resource)
			summary := map[string]any{}
			if d.PolicyRevision != "" {
				summary["policy_revision"] = d.PolicyRevision
			}
			return d.Outcome.String(), summary, err
		}},
}
