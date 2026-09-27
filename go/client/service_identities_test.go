package client

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go/resolution"
	identity "github.com/openabstractions/abstraction-identity"
)

// Each typed accessor must request the identity applications can use in
// diagnostics without copying a contract string from its implementation.
func TestAccessorRequestsExportedServiceIdentity(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Darwin sockets cannot prove the caller's Program to this resolver fixture")
	}
	services := []struct{ accessor, contract string }{
		{"ResolveApplications", ApplicationsContract},
		{"ResolveAsks", AsksContract},
		{"ResolveAsksOperator", AsksOperatorContract},
		{"ResolveConfig", ConfigContract},
		{"ResolveConfigEditor", ConfigEditorContract},
		{"ResolveConfigObserver", ConfigObserverContract},
		{"ResolveCredentials", CredentialsContract},
		{"ResolveCredentialsApplier", CredentialsApplierContract},
		{"ResolveEmbeddings", EmbeddingsContract},
		{"ResolveInference", InferenceContract},
		{"ResolveInferenceOperator", InferenceOperatorContract},
		{"ResolveJobInventory", JobInventoryContract},
		{"ResolveJobOperations", JobOperationsContract},
		{"ResolveJobOperator", JobOperatorContract},
		{"ResolveJobs", JobsContract},
		{"ResolveLending", LendingContract},
		{"ResolveLog", LogContract},
		{"ResolveLogObserver", LogObserverContract},
		{"ResolveLogReader", LogReaderContract},
		{"ResolveModel", ModelContract},
		{"ResolveRegistry", RegistryContract},
		{"ResolveResourceLeases", ResourceLeasesContract},
		{"ResolveResourceTable", ResourceTableContract},
		{"ResolveRights", RightsContract},
		{"ResolveRightsOperator", RightsOperatorContract},
		{"ResolveRouter", RouterContract},
		{"ResolveStorage", StorageContract},
		{"ResolveStorageChanges", StorageChangesContract},
		{"ResolveStorageInventory", StorageInventoryContract},
		{"ResolveStorageWriter", StorageWriterContract},
	}
	covered := make(map[string]bool, len(services))
	for _, service := range services {
		if covered[service.accessor] {
			t.Fatalf("duplicate accessor %s", service.accessor)
		}
		covered[service.accessor] = true
	}
	machineType := reflect.TypeOf((*Machine)(nil))
	for i := 0; i < machineType.NumMethod(); i++ {
		name := machineType.Method(i).Name
		if strings.HasPrefix(name, "Resolve") && name != "ResolveService" && !covered[name] {
			t.Errorf("%s has no exported service identity case", name)
		}
	}
	endpoint := unusedEndpoint(t, "identity-resolver")
	catalog, err := resolution.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	host, err := resolution.Listen(endpoint, catalog, func(*identity.Peer, ServiceReference) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan error, 1)
	go func() { done <- host.Serve(ctx) }()
	defer func() { cancel(); host.Close(); <-done }()
	machine := NewUnverified(endpoint)
	for _, service := range services {
		t.Run(service.accessor, func(t *testing.T) {
			capability, _, ok := strings.Cut(service.contract, "/")
			if !ok || capability == "" {
				t.Fatalf("invalid exported contract %q", service.contract)
			}
			method := reflect.ValueOf(machine).MethodByName(service.accessor)
			if !method.IsValid() {
				t.Fatalf("missing accessor %s", service.accessor)
			}
			results := method.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(Requirements{})})
			if len(results) != 2 || results[1].IsNil() {
				t.Fatalf("%s did not report absent service", service.accessor)
			}
			var refusal *ResolutionError
			if !errors.As(results[1].Interface().(error), &refusal) {
				t.Fatalf("%s returned %v", service.accessor, results[1].Interface())
			}
			if refusal.Capability != capability || refusal.Contract != service.contract {
				t.Fatalf("%s requested %s %s, export is %s %s", service.accessor, refusal.Capability, refusal.Contract, capability, service.contract)
			}
		})
	}
}
