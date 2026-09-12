package bootstrap

import "testing"

func TestServiceNames(t *testing.T) {
	for _, name := range []string{"", `../runtime`, "with space", `other\pipe`} {
		if _, err := Endpoint(name); err == nil {
			t.Fatalf("invalid service accepted: %q", name)
		}
	}
}
