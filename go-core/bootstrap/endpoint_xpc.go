package bootstrap

import "fmt"

const installedXPCPrefix = "com.openabstractions."

var installedXPCServices = map[string]struct{}{
	"runtime-v1": {}, "logging-v1": {}, "config-v1": {},
	"job-acceptance-v1": {}, "router-v1": {}, "model-v1": {},
	"storage-content-v1": {}, "asks-application-v1": {},
	"rights-authorization-v1": {}, "credentials-v1": {},
	"inference-v1": {}, "inference-remote-v1": {},
	"registry-v1": {}, "applications-v1": {},
	"resource-table-v1": {}, "lend-v1": {},
}

func validXPCService(service string) error {
	if err := validService(service); err != nil {
		return err
	}
	if _, ok := installedXPCServices[service]; !ok {
		return fmt.Errorf("bootstrap: service %q is not published by the macOS runtime", service)
	}
	return nil
}

// IsolatedXPCEndpoint names one runtime service in a disjoint development
// namespace. The caller registers these MachServices in its own user launchd
// domain. Naming an endpoint neither registers it nor establishes server trust.
// The native transport remains responsible for request-bound peer evidence.
func IsolatedXPCEndpoint(name, service string) (string, error) {
	if len(name) > 64 {
		return "", fmt.Errorf("bootstrap: isolated name must be 1-64 characters from a-z, 0-9 and -")
	}
	if err := validService(name); err != nil {
		return "", fmt.Errorf("bootstrap: invalid isolated name: %w", err)
	}
	if err := validXPCService(service); err != nil {
		return "", err
	}
	return "xpc:" + installedXPCPrefix + "isolated." + name + "." + service, nil
}
