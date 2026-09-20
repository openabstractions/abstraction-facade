package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

// A create that lands in root reads real; one redirected into a package's
// LocalCache reads virtualized with that family; neither copy survives.
func TestProfileProbeFindsThePackageCopy(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Packages", "Other_abc", "LocalCache", "Local"), 0o700); err != nil {
		t.Fatal(err)
	}
	real := func(path string) error { return os.WriteFile(path, nil, 0o600) }
	view, err := probeProfile(root, real)
	if err != nil || view.Virtualized || view.String() != "real" {
		t.Fatalf("real create: %+v %v", view, err)
	}
	redirected := func(path string) error {
		copy := filepath.Join(root, "Packages", "Claude_pzs8sxrjxfjjc", "LocalCache", "Local", filepath.Base(path))
		if err := os.MkdirAll(filepath.Dir(copy), 0o700); err != nil {
			return err
		}
		return os.WriteFile(copy, nil, 0o600)
	}
	view, err = probeProfile(root, redirected)
	if err != nil || !view.Virtualized || view.Family != "Claude_pzs8sxrjxfjjc" || view.String() != "virtualized(Claude_pzs8sxrjxfjjc)" {
		t.Fatalf("redirected create: %+v %v", view, err)
	}
	left, _ := filepath.Glob(filepath.Join(root, "Packages", "*", "LocalCache", "Local", ".oa-profile-*"))
	direct, _ := filepath.Glob(filepath.Join(root, ".oa-profile-*"))
	if len(left)+len(direct) != 0 {
		t.Fatalf("probe files left: %v %v", left, direct)
	}
}

// The live probe answers, and OA_EXPECT_PROFILE, when set, names the answer:
// a Claude-contained shell expects virtualized(Claude_pzs8sxrjxfjjc), a
// WMI-started one expects real.
func TestCurrentProfileViewAnswers(t *testing.T) {
	view, err := CurrentProfileView()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("profile view: %s", view)
	if want := os.Getenv("OA_EXPECT_PROFILE"); want != "" && view.String() != want {
		t.Fatalf("profile view %s, want %s", view, want)
	}
}
