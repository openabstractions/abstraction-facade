package bootstrap

import (
	"errors"
	"fmt"
	"testing"
)

func TestDeclaredUnsupportedPlatforms(t *testing.T) {
	for goos, want := range map[string]string{
		"android": "android", "darwin": "", "linux": "", "windows": "", "freebsd": "",
	} {
		if got := UnsupportedPlatform(goos); got != want {
			t.Errorf("UnsupportedPlatform(%q) = %q, want %q", goos, got, want)
		}
	}
}

func TestUnsupportedPlatformErrorNamesThePlatform(t *testing.T) {
	err := fmt.Errorf("select installed runtime: %w", &UnsupportedPlatformError{Platform: "android"})
	var platform *UnsupportedPlatformError
	if !errors.As(err, &platform) || platform.Platform != "android" {
		t.Fatalf("platform lost: %v", err)
	}
	if !errors.Is(err, ErrUnsupportedSelection) {
		t.Fatalf("not an unsupported selection: %v", err)
	}
	if got := platform.Error(); got != "no supported OpenAbstractions runtime exists for android" {
		t.Fatalf("message %q", got)
	}
}
