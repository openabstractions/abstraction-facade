package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	configwire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	download "github.com/openabstractions/abstraction-download/go"
	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	logging "github.com/openabstractions/abstraction-logging/go"
	logwire "github.com/openabstractions/abstraction-logging/go/abstraction/logging"
	logclient "github.com/openabstractions/abstraction-logging/go/client"
	model "github.com/openabstractions/abstraction-model/go"
	modelclient "github.com/openabstractions/abstraction-model/go/client"
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
		return string(logErr.Code)
	case errors.As(err, &routeErr):
		return string(routeErr.Code)
	}
	return "error: " + err.Error()
}

func TestResolvedRightsEnforceHistoryModelAndRouter(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	home := t.TempDir()
	for _, key := range []string{"HOME", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(key, home)
	}
	t.Setenv("ProgramData", filepath.Join(home, "machine"))
	const model_ = "qwen2.5"
	actions := []string{LogHistoryAction, ModelLookupAction, routerservice.ActionInventory, routerservice.ActionRoute}
	eachDecider(t, actions, func(t *testing.T, f deciderFixture, o Options) {
		policy := f.policy
		sink, err := logging.OpenFileSink(filepath.Join(t.TempDir(), "history.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		defer sink.Close()
		o.Sink = sink
		subjects := make(chan rightsclient.Subject, 64)
		capture := func(peer *identity.Peer) {
			if subject, e := rightsclient.SubjectFromPeer(peer); e == nil {
				select {
				case subjects <- subject:
				default:
				}
			}
		}
		history := HistoryPolicyFromRights(f.decider)
		o.LogHistoryPolicy = func(ctx context.Context, peer *identity.Peer) error { capture(peer); return history(ctx, peer) }
		o.ModelRegistry, o.ModelEndpoint = model.NewServiceRegistry(rightsRegistry{}), o.JobEndpoint+"-model"
		o.ModelPolicy = ModelPolicyFromRights(f.decider)
		o.Router, o.RouterEndpoint = router.New(), o.JobEndpoint+"-router"
		o.RouterPolicy = RouterPolicyFromRights(f.decider)
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
			return r.Outcome.String()
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
			return r.Outcome.String()
		}(); other != "forbidden" {
			t.Fatalf("registry grant leaked to another registry: %s", other)
		}
		if err := policy.Set(subject, routerservice.ActionRoute, RoutesResource, true); err != nil {
			t.Fatal(err)
		}
		expect("all granted", observe(), map[string]string{"history": "ok", "model": "resolved", "inventory": "ok", "route": "ok"})
		// One rule covers every route; the per-host switch is inference complete.
		if _, err := routes.PickContext(ctx, routerclient.PickRequest{Model: "other-model"}); serviceCode(err) == "forbidden" {
			t.Fatalf("the routes rule did not cover another model: %v", err)
		}
		observer, err := m.ResolveLogObserver(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := observer.ObserveContext(ctx, "", 16, 65536, 0); serviceCode(err) != "ok" {
			t.Fatalf("granted observation: %v", err)
		}

		for _, r := range append(rules, [2]string{routerservice.ActionRoute, RoutesResource}) {
			if err := policy.Revoke(subject, r[0], r[1]); err != nil {
				t.Fatal(err)
			}
		}
		expect("revoked", observe(), map[string]string{"history": "forbidden", "model": "forbidden", "inventory": "forbidden", "route": "forbidden"})
		if _, err := observer.ObserveContext(ctx, "", 16, 65536, 0); serviceCode(err) != "forbidden" {
			t.Fatalf("revoked observation: %v", err)
		}
		for _, r := range append(rules, [2]string{routerservice.ActionRoute, RoutesResource}) {
			if err := policy.Set(subject, r[0], r[1], false); err != nil {
				t.Fatal(err)
			}
		}
		expect("denied", observe(), map[string]string{"history": "forbidden", "model": "forbidden", "inventory": "forbidden", "route": "forbidden"})

		for _, r := range append(rules, [2]string{routerservice.ActionRoute, RoutesResource}) {
			if err := policy.Set(subject, r[0], r[1], true); err != nil {
				t.Fatal(err)
			}
		}
		f.outage(t, h)
		expect("decision outage", observe(), map[string]string{"history": "policy_unavailable", "model": "unavailable", "inventory": "unavailable", "route": "unavailable"})
		// Open calls continue through the outage: a log write and a config read.
		log, err := m.ResolveLog(ctx, client.Requirements{})
		if err != nil {
			t.Fatal("rights outage disabled logging", err)
		}
		if err := log.Log(0, "written during a rights outage", nil); err != nil {
			t.Fatal("log write during a rights outage", err)
		}
		config, err := m.ResolveConfig(ctx, client.Requirements{})
		if err != nil {
			t.Fatal("rights outage disabled config", err)
		}
		if _, err := config.ReadWithOverridesContext(ctx, configwire.RunOverrides{}); err != nil {
			t.Fatal("config read during a rights outage", err)
		}
	})
}

func TestResolvedRightsScopeNativeLogSourcesIndependently(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	sourceID := "windows/application"
	if runtime.GOOS == "linux" {
		sourceID = "linux/system-journal"
	}
	eachDecider(t, []string{LogSourceAction}, func(t *testing.T, f deciderFixture, o Options) {
		policy := SourcePolicyFromRights(f.decider)
		subjects := make(chan rightsclient.Subject, 32)
		o.LogSourcePolicy = func(ctx context.Context, peer *identity.Peer, action, resource string) error {
			if subject, e := rightsclient.SubjectFromPeer(peer); e == nil {
				select {
				case subjects <- subject:
				default:
				}
			}
			return policy(ctx, peer, action, resource)
		}
		_, stop := runJobRuntime(t, o)
		defer stop()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		reader, err := client.New(o.Endpoint).ResolveLogSourceReader(ctx, client.Requirements{Scope: client.ScopeLocal})
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := reader.SourcesContext(ctx)
		if err != nil || catalog.Outcome != logwire.SourceCatalogOutcomeCatalog {
			t.Fatalf("source catalog: %+v %v", catalog, err)
		}
		if sourceAvailability(catalog, sourceID) != logwire.SourceAvailabilityPermissionDenied {
			t.Fatalf("initial source was not denied: %+v", catalog)
		}
		if page, err := reader.ReadContext(ctx, sourceID, "retained:cursor", 1, 512); err != nil || page.Outcome != logwire.HistoryPageOutcomeNotPermitted || len(page.Records) != 0 || page.Next != "retained:cursor" {
			t.Fatalf("ungranted source read: %+v %v", page, err)
		}
		if page, err := reader.StartAtEndContext(ctx, sourceID); err != nil || page.Outcome != logwire.HistoryPageOutcomeNotPermitted || len(page.Records) != 0 || page.Next != "" {
			t.Fatalf("ungranted source baseline: %+v %v", page, err)
		}
		var subject rightsclient.Subject
		select {
		case subject = <-subjects:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		resource := LogSourceResource(sourceID)
		if resource != "source:"+sourceID {
			t.Fatalf("source resource %q", resource)
		}
		if err := f.policy.Set(subject, LogSourceAction, resource, true); err != nil {
			t.Fatal(err)
		}
		catalog, err = reader.SourcesContext(ctx)
		if err != nil || catalog.Outcome != logwire.SourceCatalogOutcomeCatalog {
			t.Fatalf("granted source catalog: %+v %v", catalog, err)
		}
		availability := sourceAvailability(catalog, sourceID)
		if availability == logwire.SourceAvailabilityPermissionDenied {
			t.Fatalf("source grant did not reach this fixed resource: %+v", catalog)
		}
		otherID := "windows/system"
		if sourceID == otherID {
			otherID = "linux/system-journal"
		}
		if sourceAvailability(catalog, otherID) != logwire.SourceAvailabilityPermissionDenied {
			t.Fatalf("source grant widened to %s: %+v", otherID, catalog)
		}
		if err := f.policy.Revoke(subject, LogSourceAction, resource); err != nil {
			t.Fatal(err)
		}
		catalog, err = reader.SourcesContext(ctx)
		if err != nil || catalog.Outcome != logwire.SourceCatalogOutcomeCatalog || sourceAvailability(catalog, sourceID) != logwire.SourceAvailabilityPermissionDenied {
			t.Fatalf("revoked source right returned %+v, %v", catalog, err)
		}
		if page, err := reader.ReadContext(ctx, sourceID, "retained:cursor", 1, 512); err != nil || page.Outcome != logwire.HistoryPageOutcomeNotPermitted || len(page.Records) != 0 || page.Next != "retained:cursor" {
			t.Fatalf("revoked source read: %+v %v", page, err)
		}
	})
}

func sourceAvailability(catalog logclient.SourceCatalog, id string) logwire.SourceAvailability {
	for _, descriptor := range catalog.Sources {
		if descriptor.ID == id {
			return descriptor.Availability
		}
	}
	return 0
}
