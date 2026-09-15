package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	download "github.com/openabstractions/abstraction-download/go"
	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	logging "github.com/openabstractions/abstraction-logging/go"
	logwire "github.com/openabstractions/abstraction-logging/go/abstraction/logging"
	model "github.com/openabstractions/abstraction-model/go"
	modelclient "github.com/openabstractions/abstraction-model/go/client"
	rights "github.com/openabstractions/abstraction-rights/go"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
	router "github.com/openabstractions/abstraction-router/go"
	routerclient "github.com/openabstractions/abstraction-router/go/client"
	routerservice "github.com/openabstractions/abstraction-router/go/service"
)

type rightsRegistry struct{}

func (rightsRegistry) Registry() string { return "fixture" }
func (rightsRegistry) Resolve(ctx context.Context, ref model.Ref) (download.Spec, error) {
	return download.Spec{Artifact: download.Artifact{Digest: "sha256:" + strings.Repeat("a", 64), Size: 5},
		Sources: []download.Source{{Scheme: "http", Locator: "http://127.0.0.1/weights"}}}, ctx.Err()
}

func serviceCode(err error) string {
	var logErr *logwire.ServiceError
	var routeErr *routerclient.ServiceError
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &logErr):
		return logErr.Code
	case errors.As(err, &routeErr):
		return routeErr.Code
	}
	return "error: " + err.Error()
}

func TestResolvedRightsEnforceHistoryModelAndRouter(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	const model_ = "qwen2.5"
	actions := []string{LogHistoryAction, ModelLookupAction, routerservice.ActionInventory, routerservice.ActionRoute}
	policy, err := rights.LoadDecisionPolicy(filepath.Join(t.TempDir(), "policy.json"), actions)
	if err != nil {
		t.Fatal(err)
	}
	sink, err := logging.OpenFileSink(filepath.Join(t.TempDir(), "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	o := jobOptions(t)
	o.Sink = sink
	o.RightsPolicy, o.RightsEndpoint = policy, o.JobEndpoint+"-rights"
	// This isolated fixture designates its own process as the enforcer for these actions.
	o.RightsEnforcer = func(ctx context.Context, peer *identity.Peer, a, r string) bool {
		process, e := peer.Process.AtLeast(listen.Program.Process)
		if e != nil || process.PID != os.Getpid() {
			return false
		}
		for _, allowed := range actions {
			if a == allowed {
				return true
			}
		}
		return false
	}
	decisions := rightsclient.New(o.RightsEndpoint)
	subjects := make(chan rightsclient.Subject, 64)
	capture := func(peer *identity.Peer) {
		if subject, e := rightsclient.SubjectFromPeer(peer); e == nil {
			select {
			case subjects <- subject:
			default:
			}
		}
	}
	history := HistoryPolicyFromRights(decisions)
	o.LogHistoryPolicy = func(ctx context.Context, peer *identity.Peer) error { capture(peer); return history(ctx, peer) }
	o.ModelRegistry, o.ModelEndpoint = model.NewServiceRegistry(rightsRegistry{}), o.JobEndpoint+"-model"
	o.ModelPolicy = ModelPolicyFromRights(decisions)
	o.Router, o.RouterEndpoint = router.New(), o.JobEndpoint+"-router"
	o.RouterPolicy = RouterPolicyFromRights(decisions)
	h, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	reader, err := m.ResolveLogReader(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := m.ResolveModel(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	routes, err := m.ResolveRouter(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	ref := modelclient.Ref{Registry: "fixture", Repo: "weights"}
	read := func() string { _, e := reader.ReadContext(ctx, "", 16, 65536); return serviceCode(e) }
	resolve := func() string {
		r, e := lookup.ResolveContext(ctx, ref)
		if e != nil {
			return "error: " + e.Error()
		}
		return r.Outcome
	}
	inventory := func() string { _, e := routes.ModelsContext(ctx, false); return serviceCode(e) }
	route := func() string {
		_, e := routes.PickContext(ctx, routerclient.PickRequest{Model: model_})
		return serviceCode(e)
	}
	expect := func(stage string, got, want map[string]string) {
		t.Helper()
		for name, value := range want {
			if got[name] != value {
				t.Fatalf("%s: %s = %q, want %q", stage, name, got[name], value)
			}
		}
	}
	observe := func() map[string]string {
		return map[string]string{"history": read(), "model": resolve(), "inventory": inventory(), "route": route()}
	}

	expect("ungranted", observe(), map[string]string{"history": "forbidden", "model": "forbidden", "inventory": "forbidden", "route": "forbidden"})
	var subject rightsclient.Subject
	select {
	case subject = <-subjects:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	rules := [][2]string{{LogHistoryAction, LogHistoryResource}, {ModelLookupAction, "fixture"}, {routerservice.ActionInventory, routerservice.ResourceInventory}}
	for _, r := range rules {
		if err := policy.Set(subject, r[0], r[1], true); err != nil {
			t.Fatal(err)
		}
	}
	expect("inventory granted without route", observe(), map[string]string{"history": "ok", "model": "resolved", "inventory": "ok", "route": "forbidden"})
	if other := func() string {
		r, e := lookup.ResolveContext(ctx, modelclient.Ref{Registry: "other", Repo: "weights"})
		if e != nil {
			return e.Error()
		}
		return r.Outcome
	}(); other != "forbidden" {
		t.Fatalf("registry grant leaked to another registry: %s", other)
	}
	if err := policy.Set(subject, routerservice.ActionRoute, model_, true); err != nil {
		t.Fatal(err)
	}
	expect("all granted", observe(), map[string]string{"history": "ok", "model": "resolved", "inventory": "ok", "route": "ok"})
	if _, err := routes.PickContext(ctx, routerclient.PickRequest{Model: "other-model"}); serviceCode(err) != "forbidden" {
		t.Fatalf("route grant leaked to another model: %v", err)
	}
	observer, err := m.ResolveLogObserver(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := observer.ObserveContext(ctx, "", 16, 65536, 0); serviceCode(err) != "ok" {
		t.Fatalf("granted observation: %v", err)
	}

	for _, r := range append(rules, [2]string{routerservice.ActionRoute, model_}) {
		if err := policy.Revoke(subject, r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
	expect("revoked", observe(), map[string]string{"history": "forbidden", "model": "forbidden", "inventory": "forbidden", "route": "forbidden"})
	if _, err := observer.ObserveContext(ctx, "", 16, 65536, 0); serviceCode(err) != "forbidden" {
		t.Fatalf("revoked observation: %v", err)
	}

	for _, r := range append(rules, [2]string{routerservice.ActionRoute, model_}) {
		if err := policy.Set(subject, r[0], r[1], true); err != nil {
			t.Fatal(err)
		}
	}
	h.rights.Close()
	expect("decision outage", observe(), map[string]string{"history": "policy_unavailable", "model": "unavailable", "inventory": "policy_unavailable", "route": "policy_unavailable"})
	if _, err := m.ResolveConfig(ctx, client.Requirements{}); err != nil {
		t.Fatal("rights outage disabled config", err)
	}
}
