package bootstrap

import "testing"

func TestSystemdEvidence(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"LoadState=loaded\nActiveState=active\nSubState=running", "running"},
		{"LoadState=loaded\nActiveState=activating\nSubState=auto-restart", "starting"},
		{"LoadState=loaded\nActiveState=failed\nSubState=failed", "installed"},
		{"LoadState=loaded\nActiveState=inactive\nSubState=dead", "installed"},
		{"LoadState=not-found\nActiveState=inactive", "unknown"},
		{"LoadState=loaded\nLoadState=not-found\nActiveState=active", "unknown"},
	} {
		if got := observeSystemd(test.input); got.State != test.want {
			t.Fatal(got, test.want)
		}
	}
}
