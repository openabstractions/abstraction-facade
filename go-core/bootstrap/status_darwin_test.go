package bootstrap

import "testing"

func TestLaunchdEvidence(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"service = {\n\tstate = running\n}", "running"},
		{"service = {\n\tstate = spawn scheduled\n}", "starting"},
		{"service = {\n\tstate = waiting\n}", "installed"},
		{"service = {\n\t\tstate = running\n}", "unknown"},
		{"garbled", "unknown"},
	} {
		if got := observeLaunchd(test.input); got.State != test.want {
			t.Fatal(got, test.want)
		}
	}
}
