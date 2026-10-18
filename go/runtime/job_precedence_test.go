package runtime

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
)

// outageOn is a Decider whose decision on one resource cannot be obtained;
// every other decision is base's. It is a QuietDecider as the runtime's own
// decider is, and records each resource read quietly. A loud read of a kind's
// resource that reads not_granted is where the runtime admits a first-use
// question (the contract-wide resource asks nothing), so it records those as
// asked.
type outageOn struct {
	base  Decider
	mu    sync.Mutex
	out   map[string]bool
	read  []string
	asked []string
}

func (d *outageOn) set(resource string, out bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.out[resource] = out
}

func (d *outageOn) down(resource string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.out[resource]
}

func (d *outageOn) decide(ctx context.Context, peer *identity.Peer, action, resource string) error {
	if d.down(resource) {
		return errors.New("the decision service did not answer")
	}
	return d.base.Require(ctx, peer, action, resource)
}

func (d *outageOn) Require(ctx context.Context, peer *identity.Peer, action, resource string) error {
	err := d.decide(ctx, peer, action, resource)
	if resource != JobResource && refused(err, "not_granted") {
		d.mu.Lock()
		d.asked = append(d.asked, resource)
		d.mu.Unlock()
	}
	return err
}

func (d *outageOn) RequireQuietly(ctx context.Context, peer *identity.Peer, action, resource string) error {
	d.mu.Lock()
	d.read = append(d.read, resource)
	d.mu.Unlock()
	return d.decide(ctx, peer, action, resource)
}

// reset forgets what earlier submissions read and asked.
func (d *outageOn) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.read, d.asked = nil, nil
}

// seen is what the last submission read quietly and asked.
func (d *outageOn) seen() ([]string, []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.read), slices.Clone(d.asked)
}

// JOB-A13 (research/wake-and-inference-rights/REPORT.md recommendation 6):
// submission of one kind under each pair of the contract-wide rule and the
// kind's rule, each permitted, denied, absent or unavailable. An explicit deny
// on either refuses; otherwise a permit on either admits, unless the other
// decision could not be obtained, when the submission is unavailable: an
// unavailable kind read under a contract permit refuses, since the kind may be
// denied. A deny wins over an unavailable decision on the other resource.
// Only a submission with no contract-wide rule asks the person anything. A
// kind no rule can name is decided on the contract-wide rule alone.
func TestSubmitPrecedenceOfTheContractAndTheKindRule(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	eachDecider(t, []string{JobSubmitAction}, func(t *testing.T, f deciderFixture, o Options) {
		decider := &outageOn{base: f.decider, out: map[string]bool{}}
		enforce := JobPolicyFromRights(decider, JobRightsActions)
		subjects := make(chan rightsclient.Subject, 64)
		o.JobMethodPolicy = func(ctx context.Context, peer *identity.Peer, service, method string) error {
			if subject, e := rightsclient.SubjectFromPeer(peer); e == nil {
				select {
				case subjects <- subject:
				default:
				}
			}
			return enforce(ctx, peer, service, method)
		}
		_, stop := runJobRuntime(t, o)
		defer stop()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		jobs, err := client.New(o.Endpoint).ResolveJobs(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		window, err := jobs.GetHistoryWindow(ctx)
		if err != nil {
			t.Fatal(err)
		}
		submitKind := func(key, jobKind string) string {
			t.Helper()
			reply, err := jobs.Submit(ctx, api.Submission{Identity: api.RequestIdentity{Key: key, HistoryEpoch: window.HistoryEpoch},
				Kind: jobKind, Spec: []byte(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			return reply.Outcome.String()
		}
		submit := func(key string) string { t.Helper(); return submitKind(key, "download") }
		// The subject is the one the policy saw on a first submission.
		submit("precedence-subject")
		var subject rightsclient.Subject
		select {
		case subject = <-subjects:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		kind := JobKindResource("download")
		apply := func(resource, rule string) {
			t.Helper()
			decider.set(resource, rule == "unavailable")
			var err error
			switch rule {
			case "permit":
				err = f.policy.Set(subject, JobSubmitAction, resource, true)
			case "deny":
				err = f.policy.Set(subject, JobSubmitAction, resource, false)
			default:
				err = f.policy.Revoke(subject, JobSubmitAction, resource)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		want := map[[2]string]string{
			{"permit", "permit"}: "accepted", {"permit", "deny"}: "forbidden",
			{"permit", "absent"}: "accepted", {"permit", "unavailable"}: "unavailable",
			{"deny", "permit"}: "forbidden", {"deny", "deny"}: "forbidden",
			{"deny", "absent"}: "forbidden", {"deny", "unavailable"}: "forbidden",
			{"absent", "permit"}: "accepted", {"absent", "deny"}: "forbidden",
			{"absent", "absent"}: "forbidden", {"absent", "unavailable"}: "unavailable",
			{"unavailable", "permit"}: "unavailable", {"unavailable", "deny"}: "forbidden",
			{"unavailable", "absent"}: "unavailable", {"unavailable", "unavailable"}: "unavailable",
		}
		rules := []string{"permit", "deny", "absent", "unavailable"}
		for i, contract := range rules {
			for j, kindRule := range rules {
				apply(JobResource, contract)
				apply(kind, kindRule)
				decider.reset()
				if got, wanted := submit(fmt.Sprintf("precedence-%d-%d", i, j)), want[[2]string{contract, kindRule}]; got != wanted {
					t.Errorf("contract %s, kind %s: %s, want %s", contract, kindRule, got, wanted)
				}
				// Under a contract-wide permit the kind's rule is read
				// quietly; wherever a contract-wide rule exists, or its
				// decision is unavailable, no question is admitted.
				read, asked := decider.seen()
				if contract == "permit" && !slices.Equal(read, []string{kind}) {
					t.Errorf("contract permit, kind %s: the kind was read %v", kindRule, read)
				}
				if contract != "absent" && len(asked) != 0 {
					t.Errorf("contract %s, kind %s: asked %v", contract, kindRule, asked)
				}
				if contract == "absent" && kindRule == "absent" && !slices.Equal(asked, []string{kind}) {
					t.Errorf("no rule at all: asked %v, want the kind's question", asked)
				}
			}
		}
		// A kind no rule can name is decided on the contract-wide rule
		// alone: no kind's rule is read and nothing is asked.
		const unnamed = "Bulk Export"
		for i, contract := range rules {
			apply(JobResource, contract)
			decider.reset()
			wanted := map[string]string{"permit": "accepted", "deny": "forbidden", "absent": "forbidden", "unavailable": "unavailable"}[contract]
			if got := submitKind(fmt.Sprintf("precedence-unnamed-%d", i), unnamed); got != wanted {
				t.Errorf("contract %s, kind %q: %s, want %s", contract, unnamed, got, wanted)
			}
			if read, asked := decider.seen(); len(read) != 0 || len(asked) != 0 {
				t.Errorf("contract %s, kind %q: read %v, asked %v", contract, unnamed, read, asked)
			}
		}
	})
}
