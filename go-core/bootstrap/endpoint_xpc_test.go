package bootstrap

import (
	"strings"
	"testing"
)

func TestIsolatedXPCNamesCoverRuntimeServicesAndStayDisjoint(t *testing.T) {
	if len(installedXPCServices) != 16 {
		t.Fatalf("runtime service inventory changed: %d", len(installedXPCServices))
	}
	seen := map[string]bool{}
	for service := range installedXPCServices {
		for _, name := range []string{"permission-demo", "permission-demo-2", strings.Repeat("a", 64)} {
			endpoint, err := IsolatedXPCEndpoint(name, service)
			want := "xpc:com.openabstractions.isolated." + name + "." + service
			if err != nil || endpoint != want || len(strings.TrimPrefix(endpoint, "xpc:")) > 255 {
				t.Fatalf("%s/%s = %q, %v", name, service, endpoint, err)
			}
			if endpoint == "xpc:"+installedXPCPrefix+service || seen[endpoint] {
				t.Fatalf("namespace collision: %s", endpoint)
			}
			seen[endpoint] = true
		}
	}
}

func TestIsolatedXPCNamesRefuseInvalidAndUnownedServices(t *testing.T) {
	for _, name := range []string{"", "Mixed", "dot.name", "slash/name", "name\n", strings.Repeat("a", 65)} {
		if endpoint, err := IsolatedXPCEndpoint(name, "runtime-v1"); err == nil || endpoint != "" {
			t.Fatalf("invalid namespace %q produced %q: %v", name, endpoint, err)
		}
	}
	for _, service := range []string{"", "runtime", "unknown-v1", "runtime-v1\n", "../runtime-v1"} {
		if endpoint, err := IsolatedXPCEndpoint("demo", service); err == nil || endpoint != "" {
			t.Fatalf("unowned service %q produced %q: %v", service, endpoint, err)
		}
	}
}
