package bootstrap

import "testing"

func TestInstalledDarwinEndpointsAreFixed(t *testing.T) {
	for _, service := range []string{"resource-table-v1", "lend-v1"} {
		if _, ok := installedXPCServices[service]; !ok {
			t.Fatalf("runtime service %q is absent from the installed XPC roster", service)
		}
	}
	for service := range installedXPCServices {
		got, err := InstalledEndpoint(service)
		if err != nil || got != "xpc:"+installedXPCPrefix+service {
			t.Fatalf("InstalledEndpoint(%q) = %q, %v", service, got, err)
		}
	}
	for _, service := range []string{"", "future-v1", "../runtime"} {
		if _, err := InstalledEndpoint(service); err == nil {
			t.Fatalf("unpublished service accepted: %q", service)
		}
	}
	ordinary, err := Endpoint("runtime-v1")
	if err != nil || ordinary == "xpc:"+installedXPCPrefix+"runtime-v1" {
		t.Fatalf("generic endpoint changed: %q, %v", ordinary, err)
	}
}
