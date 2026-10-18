package runtime

import (
	"context"
	"errors"
	"fmt"
	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	logging "github.com/openabstractions/abstraction-logging/go"
	logwire "github.com/openabstractions/abstraction-logging/go/abstraction/logging"
	logservice "github.com/openabstractions/abstraction-logging/go/service"
	rwire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
	"path/filepath"
	"strings"
	"testing"
)

type legacyOnlyLogDecider struct{}

func (legacyOnlyLogDecider) Require(context.Context, *identity.Peer, string, string) error {
	return nil
}

func TestApplicationLogResolverKeepsV2UnavailableWithoutSharedPolicyOrOwnedProvider(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprint(owned), func(t *testing.T) {
			o := jobOptions(t)
			o.JobRoot = ""
			o.JobOwner = ""
			o.JobEndpoint = ""
			if owned {
				out, err := logging.OpenRetainedFileSink(filepath.Join(t.TempDir(), "rows"), logging.LogsRetention{MaxUse: 1 << 20})
				if err != nil {
					t.Fatal(err)
				}
				defer out.Close()
				o.Sink = out
			}
			o.LogApplicationHistoryPolicy = ApplicationHistoryPolicyFromRights(legacyOnlyLogDecider{})
			if assertDarwinNativeSocketRefusal(t, o) {
				return
			}
			_, stop := runJobRuntime(t, o)
			defer stop()
			m := client.New(o.Endpoint)
			reader, err := m.ResolveLogApplicationReader(context.Background(), client.Requirements{})
			if !owned {
				var unavailable *client.ResolutionError
				if !errors.As(err, &unavailable) || unavailable.Status != "not_ready" {
					t.Fatalf("unowned provider advertised @2: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			page, err := reader.ReadContext(context.Background(), logwire.ApplicationLogTarget{Selection: logwire.ApplicationLogSelectionAllApps}, "", 1, 512)
			if err != nil || page.Outcome != logwire.HistoryPageOutcomeUnavailable || len(page.Records) != 0 || page.Next != "" {
				t.Fatalf("legacy Require became @2 permit: %+v %v", page, err)
			}
		})
	}
}

func TestApplicationLogBatchBudgetsSelectedTargetAndWholeEncodedFrame(t *testing.T) {
	program := filepath.Join(t.TempDir(), "selected")
	one := logservice.ApplicationHistoryQuery{Selection: logwire.ApplicationLogSelectionOneApp, Target: logwire.ApplicationLogResource{Program: program}}
	request, err := applicationLogBatch(one)
	if err != nil || len(request.Resources) != 1 || request.CollectionAdmission != nil {
		t.Fatalf("selected admission: %+v %v", request, err)
	}
	all := logservice.ApplicationHistoryQuery{Selection: logwire.ApplicationLogSelectionAllApps}
	request, err = applicationLogBatch(all)
	if err != nil || len(request.Resources) != 0 || request.CollectionAdmission == nil {
		t.Fatalf("collection admission: %+v %v", request, err)
	}
	subject := rwire.Subject{Account: "native-account", Program: program}
	for i := 0; i < 256; i++ {
		emitter := logwire.ApplicationLogResource{Program: filepath.Join(filepath.Dir(program), strings.Repeat("p", 3800)+fmt.Sprint(i)), CapturedFileKey: strings.Repeat("key", 150)}
		if !logging.ValidApplicationResource(emitter) {
			t.Fatal("fixture invalid")
		}
		all.Emitters = append(all.Emitters, emitter)
	}
	request, err = applicationLogBatch(all)
	if err != nil {
		t.Fatal(err)
	}
	if applicationLogFrameFits(subject, request) {
		t.Fatal("256 individually bounded targets overflow actual binary/frame encoding")
	}
	// The same packet builder admits a smaller page; it retains caller and
	// collection overhead instead of estimating resource bytes in isolation.
	all.Emitters = all.Emitters[:1]
	request, err = applicationLogBatch(all)
	if err != nil || !applicationLogFrameFits(subject, request) {
		t.Fatalf("one actual frame: %v", err)
	}
	one.Emitters = make([]logwire.ApplicationLogResource, 256)
	for i := range one.Emitters {
		one.Emitters[i] = one.Target
	}
	if _, err = applicationLogBatch(one); err == nil {
		t.Fatal("selected target overhead exceeded 256 resources")
	}
}

func TestApplicationLogFinalBatchRefusesInvalidPartialAndMissingAdmission(t *testing.T) {
	q := logservice.ApplicationHistoryQuery{Selection: logwire.ApplicationLogSelectionAllApps, Emitters: []logwire.ApplicationLogResource{{Program: filepath.Join(t.TempDir(), "a")}, {Program: filepath.Join(t.TempDir(), "b")}}}
	permit := rwire.DecisionOutcomePermitted
	good := rwire.BatchDecision{Outcome: rwire.BatchCheckOutcomeChecked, Revision: "snapshot", CollectionOutcome: &permit, Outcomes: []rwire.DecisionOutcome{rwire.DecisionOutcomePermitted, rwire.DecisionOutcomeDenied}}
	allowed, err := checkedApplicationLogBatch(good, q)
	if err != nil || len(allowed) != 2 || !allowed[0] || allowed[1] {
		t.Fatalf("one shared revision: %v %v", allowed, err)
	}
	for _, edit := range []func(*rwire.BatchDecision){
		func(d *rwire.BatchDecision) { d.Revision = "" },
		func(d *rwire.BatchDecision) { d.CollectionOutcome = nil },
		func(d *rwire.BatchDecision) { d.Outcome = rwire.BatchCheckOutcomeConflict },
		func(d *rwire.BatchDecision) { d.Outcome = rwire.BatchCheckOutcomeUnavailable },
		func(d *rwire.BatchDecision) { d.Outcomes = d.Outcomes[:1] },
		func(d *rwire.BatchDecision) {
			d.Outcomes = []rwire.DecisionOutcome{rwire.DecisionOutcomePermitted, rwire.DecisionOutcomeUnavailable}
		},
	} {
		refused := good
		edit(&refused)
		allowed, err = checkedApplicationLogBatch(refused, q)
		if len(allowed) != 0 || !errors.Is(err, logservice.ErrHistoryPolicyUnavailable) {
			t.Fatalf("partial authority escaped: %+v -> %v %v", refused, allowed, err)
		}
	}
}
