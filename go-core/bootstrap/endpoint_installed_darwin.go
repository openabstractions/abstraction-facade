package bootstrap

import "fmt"

const installedXPCPrefix = "com.openabstractions."

var installedXPCServices = map[string]struct{}{
	"runtime-v1":              {},
	"logging-v1":              {},
	"config-v1":               {},
	"job-acceptance-v1":       {},
	"router-v1":               {},
	"model-v1":                {},
	"storage-content-v1":      {},
	"asks-application-v1":     {},
	"rights-authorization-v1": {},
	"credentials-v1":          {},
	"inference-v1":            {},
	"inference-remote-v1":     {},
	"registry-v1":             {},
	"applications-v1":         {},
	"resource-table-v1":       {},
	"lend-v1":                 {},
}

// InstalledEndpoint returns one of the fixed Mach services published by the
// installed LaunchAgent. Generic Endpoint remains the Unix-socket convention
// used by explicit and isolated runtimes.
func InstalledEndpoint(service string) (string, error) {
	if err := validService(service); err != nil {
		return "", err
	}
	if _, ok := installedXPCServices[service]; !ok {
		return "", fmt.Errorf("bootstrap: service %q is not published by the installed macOS runtime", service)
	}
	return "xpc:" + installedXPCPrefix + service, nil
}
