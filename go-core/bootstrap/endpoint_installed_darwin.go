package bootstrap

// InstalledEndpoint returns one of the fixed Mach services published by the
// installed LaunchAgent. Generic Endpoint remains the Unix-socket convention
// used by explicit and isolated runtimes without --xpc.
func InstalledEndpoint(service string) (string, error) {
	if err := validXPCService(service); err != nil {
		return "", err
	}
	return "xpc:" + installedXPCPrefix + service, nil
}
