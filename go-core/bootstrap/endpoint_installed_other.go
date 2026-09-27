//go:build !darwin

package bootstrap

// InstalledEndpoint returns the endpoint published by the installed runtime.
// Non-Darwin installations use the ordinary per-user transport convention.
func InstalledEndpoint(service string) (string, error) { return Endpoint(service) }
