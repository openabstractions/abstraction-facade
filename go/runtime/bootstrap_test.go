package runtime

import (
	"testing"

	"github.com/openabstractions/abstraction-facade/go/bootstrap"
	"github.com/openabstractions/abstraction-facade/go/resolution"
)

func TestRuntimeAndResolvingCallerShareBootstrap(t *testing.T) {
	t.Setenv("ABSTRACTION_RUNTIME_ENDPOINT", "")
	o, err := configureEndpoints(Options{JobRoot: "configured", JobOwner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	caller, err := resolution.CheckedDefaultEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	if o.Endpoint != caller || caller != resolution.DefaultEndpoint() {
		t.Fatal("host and caller bootstrap differ")
	}
	for _, test := range []struct{ actual, service string }{{o.Endpoint, "runtime-v1"}, {o.LogEndpoint, "logging-v1"}, {o.ConfigEndpoint, "config-v1"}, {o.JobEndpoint, "job-acceptance-v1"}} {
		want, err := bootstrap.Endpoint(test.service)
		if err != nil || test.actual != want {
			t.Fatalf("%s: %q != %q (%v)", test.service, test.actual, want, err)
		}
	}
	absent, err := configureEndpoints(Options{})
	if err != nil || absent.JobEndpoint != "" {
		t.Fatal("unconfigured jobs received endpoint")
	}
}

func TestExplicitRuntimeEndpointsRemainVerbatim(t *testing.T) {
	t.Setenv("ABSTRACTION_RUNTIME_ENDPOINT", "environment-override")
	o := Options{Endpoint: "explicit-runtime", LogEndpoint: "explicit-log", ConfigEndpoint: "explicit-config", JobEndpoint: "explicit-job", JobRoot: "configured", JobOwner: "owner"}
	actual, err := configureEndpoints(o)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Endpoint != o.Endpoint || actual.LogEndpoint != o.LogEndpoint || actual.ConfigEndpoint != o.ConfigEndpoint || actual.JobEndpoint != o.JobEndpoint {
		t.Fatal("explicit endpoint changed")
	}
	actual, err = configureEndpoints(Options{})
	if err != nil || actual.Endpoint != "environment-override" {
		t.Fatalf("environment override: %+v %v", actual, err)
	}
	if value, err := resolution.CheckedDefaultEndpoint(); err != nil || value != "environment-override" {
		t.Fatalf("caller environment override: %q %v", value, err)
	}
}
