package probe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/openabstractions/abstraction-facade/go/client"
	"github.com/openabstractions/abstraction-identity/listen"
)

// Each probe is named once; each capability has one default operation; a probe
// that writes is labelled, never a default and never in All.
func TestTheListNamesWritesAndKeepsThemOutOfAll(t *testing.T) {
	seen, defaults, writes := map[string]bool{}, map[string]int{}, 0
	for _, p := range List() {
		name := p.Capability + " " + p.Operation
		if seen[name] || p.Contract == "" || p.run == nil {
			t.Fatalf("probe %s repeated or incomplete", name)
		}
		seen[name] = true
		if p.Default {
			defaults[p.Capability]++
		}
		if p.Writes {
			writes++
			if p.Default || !strings.HasPrefix(p.Note, "WRITES:") {
				t.Fatalf("write probe %s is a default or unlabelled: %+v", name, p)
			}
		}
	}
	for capability := range seen {
		if c := strings.Fields(capability)[0]; defaults[c] != 1 {
			t.Fatalf("capability %s has %d default operations", c, defaults[c])
		}
	}
	if !seen["config rewrite"] || !seen["jobs submit"] || !seen["jobs cancel"] || writes != 3 {
		t.Fatalf("config rewrite , jobs submit and jobs cancel are the write probes; %d write probes", writes)
	}
	for _, p := range All() {
		if p.Writes || p.Argument != "" || !p.Default {
			t.Fatalf("All holds %s %s", p.Capability, p.Operation)
		}
	}
	if len(All()) < 8 {
		t.Fatalf("All holds %d probes", len(All()))
	}
	if p, known, found := Find("config", ""); !known || !found || p.Operation != "read" {
		t.Fatalf("config default %+v", p)
	}
	if _, known, found := Find("config", "delete"); !known || found {
		t.Fatal("unknown operation found")
	}
	if _, known, _ := Find("nothing", ""); known {
		t.Fatal("unknown capability known")
	}
}

func TestArgumentsAndRules(t *testing.T) {
	model, _, _ := Find("model", "resolve")
	rights, _, _ := Find("rights", "decide")
	read, _, _ := Find("config", "read")
	for _, c := range []struct {
		p        Probe
		argument string
		ok       bool
	}{
		{model, "ollama:llama3.2@latest", true}, {model, "", false}, {model, "llama3.2", false},
		{rights, "a/b c", true}, {rights, "only", false}, {read, "", true}, {read, "x", false},
	} {
		if err := CheckArgument(c.p, c.argument); (err == nil) != c.ok || (err != nil && !errors.Is(err, ErrArgument)) {
			t.Fatalf("%s %s %q: %v", c.p.Capability, c.p.Operation, c.argument, err)
		}
	}
	if r := RuleFor(model, "ollama:llama3.2"); r == nil || r.Resource != "ollama" {
		t.Fatalf("model rule %+v", r)
	}
	if r := RuleFor(model, ""); r != nil {
		t.Fatalf("model rule without an argument %+v", r)
	}
	if r := RuleFor(rights, "act res"); r == nil || *r != (Rule{"act", "res"}) {
		t.Fatalf("rights rule %+v", r)
	}
	for _, r := range Decisions() {
		if strings.HasPrefix(r.Resource, "<") {
			t.Fatalf("decision with an unnamed resource %+v", r)
		}
	}
}

// With no runtime every probe is its resolution status, and a bad argument is
// invalid before anything is sent.
func TestRunWithNoRuntime(t *testing.T) {
	absent := client.New(listen.Endpoint(fmt.Sprintf("absent-probe-%d", os.Getpid())))
	for _, p := range List() {
		argument := map[string]string{"model": "ollama:llama3.2", "rights": "a b", "router": "m", "storage": "sha256:00", "jobs": "http://127.0.0.1:9/probe"}[p.Capability]
		if p.Argument == "" {
			argument = ""
		}
		r := Run(context.Background(), absent, p, argument)
		if r.Outcome != "runtime_unavailable" || r.Resolution != "runtime_unavailable" || r.Summary != nil || r.Subject.Program == "" {
			t.Fatalf("%s %s with no runtime %+v", p.Capability, p.Operation, r)
		}
	}
	model, _, _ := Find("model", "resolve")
	if r := Run(context.Background(), absent, model, "bad"); r.Outcome != "invalid" || !strings.Contains(r.Detail, "REGISTRY:REPO") {
		t.Fatalf("invalid model reference %+v", r)
	}
}
