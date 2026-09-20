package abstraction_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	config "github.com/openabstractions/abstraction-config/go/abstraction/config"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	jobs "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	logging "github.com/openabstractions/abstraction-logging/go/abstraction/logging"
)

// exchange is a transport that hands each frame to answer.
type exchange func([]byte) ([]byte, error)

func (e exchange) ExchangeFrame(frame []byte) ([]byte, error) { return e(frame) }

// notReady is a history reader whose readiness hook reports it is not ready.
type notReady struct{ logging.HistoryReader }

func (notReady) Ready() (bool, string) { return false, "journal:unreadable" }

// A generated logging dispatcher answers endpoint@1 Describe with sink@1 ready.
func TestALoggingDispatcherDescribesSinkReady(t *testing.T) {
	sink := &logging.SinkDispatcher{}
	description, err := wire.NewEndpointClient(exchange(func(frame []byte) ([]byte, error) {
		return logging.DescribeEndpoint(frame, "fixture", "1.0", sink)
	})).Describe()
	if err != nil {
		t.Fatal(err)
	}
	want := []wire.ServiceState{{Contract: "abstraction.logging/sink@1", Readiness: wire.ServiceReadinessReady, Why: "", Guarantees: []string{}, Capabilities: map[string]string{}}}
	if description.Outcome != wire.DescriptionOutcomeDescribed || description.Program != "fixture" || description.Version != "1.0" ||
		len(description.Services) != 1 || description.Services[0].Contract != want[0].Contract || description.Services[0].Readiness != want[0].Readiness {
		t.Fatalf("description %+v", description)
	}
	// A dispatcher with replies answers Describe itself, for its own service.
	reader := &logging.HistoryReaderDispatcher{}
	own, err := wire.NewEndpointClient(reader).Describe()
	if err != nil || len(own.Services) != 1 || own.Services[0].Contract != "abstraction.logging/reader@1" || own.Services[0].Readiness != wire.ServiceReadinessReady {
		t.Fatalf("reader description %+v %v", own, err)
	}
}

// A Ready hook returning false reads not_ready with its reason.
func TestAReadyHookReturningFalseReadsNotReady(t *testing.T) {
	description, err := wire.NewEndpointClient(&logging.HistoryReaderDispatcher{Handler: notReady{}}).Describe()
	if err != nil {
		t.Fatal(err)
	}
	if len(description.Services) != 1 || description.Services[0].Readiness != wire.ServiceReadinessNotReady || description.Services[0].Why != "journal:unreadable" {
		t.Fatalf("description %+v", description)
	}
}

// An endpoint hosting the five default runtime contracts, from three generated
// packages, lists all five in order.
func TestAnEndpointWithFiveContractsListsAllFive(t *testing.T) {
	hosted := []logging.DescribedService{&logging.SinkDispatcher{}, &config.ConfigReaderDispatcher{}, &jobs.RecoverableAcceptanceDispatcher{},
		&jobs.OperationControlDispatcher{}, &config.ConfigEditorDispatcher{}}
	description, err := wire.NewEndpointClient(exchange(func(frame []byte) ([]byte, error) {
		return logging.DescribeEndpoint(frame, "", "", hosted...)
	})).Describe()
	if err != nil {
		t.Fatal(err)
	}
	var contracts []string
	for _, s := range description.Services {
		if s.Readiness != wire.ServiceReadinessReady {
			t.Fatalf("service %+v", s)
		}
		contracts = append(contracts, s.Contract)
	}
	if !slices.Equal(contracts, wire.DefaultRuntimeContracts) {
		t.Fatalf("listed %v, want %v", contracts, wire.DefaultRuntimeContracts)
	}
}

// unknown_service still answers a frame for a service nobody declared, from a
// dispatcher and from DescribeEndpoint.
func TestUnknownServiceStillAnswersForUndeclaredServices(t *testing.T) {
	reply, err := (&logging.HistoryReaderDispatcher{}).ExchangeFrame([]byte(`{"version":1,"service":"abstraction.example/none@1","method":"Read","arguments":{}}`))
	if err != nil || !strings.Contains(string(reply), `"unknown_service"`) {
		t.Fatalf("dispatcher reply %s %v", reply, err)
	}
	var refused *wire.ServiceError
	_, err = wire.NewResolverClient(exchange(func(frame []byte) ([]byte, error) {
		return logging.DescribeEndpoint(frame, "", "", &logging.SinkDispatcher{})
	})).Resolve(wire.ResolveRequest{Capability: "x", Contracts: []string{"x/y@1"}, Guarantees: []string{}, Scope: wire.ScopeAny})
	if !errors.As(err, &refused) || refused.Code != "unknown_service" {
		t.Fatalf("DescribeEndpoint given a resolver frame: %v", err)
	}
}

// ServeEndpoint describes every hosted service, routes other frames by the
// service they name, reads wrong_mode for a one-way-only service and
// unknown_service for a service it does not host.
func TestServeEndpointDescribesAllAndRoutesByServiceName(t *testing.T) {
	hosted := []logging.ServedService{&logging.SinkDispatcher{}, &logging.HistoryReaderDispatcher{Handler: notReady{}}, &config.ConfigEditorDispatcher{}}
	serve := exchange(func(frame []byte) ([]byte, error) { return logging.ServeEndpoint(frame, "fixture", "", hosted...) })
	description, err := wire.NewEndpointClient(serve).Describe()
	if err != nil {
		t.Fatal(err)
	}
	var contracts []string
	for _, s := range description.Services {
		contracts = append(contracts, s.Contract)
	}
	if want := []string{"abstraction.logging/sink@1", "abstraction.logging/reader@1", "abstraction.config/editor@1"}; !slices.Equal(contracts, want) ||
		description.Program != "fixture" || description.Services[1].Readiness != wire.ServiceReadinessNotReady {
		t.Fatalf("description %+v", description)
	}
	for frame, code := range map[string]string{
		`{"version":1,"service":"abstraction.config/editor@1","method":"None","arguments":{}}`: `"unknown_method"`,
		`{"version":1,"service":"abstraction.logging/sink@1","method":"Write","arguments":{}}`: `"wrong_mode"`,
		`{"version":1,"service":"abstraction.example/none@1","method":"Read","arguments":{}}`:  `"unknown_service"`,
	} {
		reply, err := serve([]byte(frame))
		if err != nil || !strings.Contains(string(reply), code) {
			t.Fatalf("frame %s: reply %s %v, want %s", frame, reply, err, code)
		}
	}
}
