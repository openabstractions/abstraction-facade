package bootstrap

import "runtime"

// UnsupportedPlatformError is installed-runtime selection on a platform the
// runtime's platform declaration lists as unsupported. errors.Is matches it
// with ErrUnsupportedSelection.
type UnsupportedPlatformError struct {
	// Platform is the declaration's name for the platform: "android" or "macos".
	Platform string
}

func (e *UnsupportedPlatformError) Error() string {
	return "no supported OpenAbstractions runtime exists for " + e.Platform
}

// Is reports the error as ErrUnsupportedSelection.
func (e *UnsupportedPlatformError) Is(target error) bool { return target == ErrUnsupportedSelection }

// UnsupportedPlatform returns the declared unsupported platform a GOOS value
// names, or "" when the declaration does not list it as unsupported.
func UnsupportedPlatform(goos string) string {
	switch goos {
	case "android":
		return "android"
	case "darwin":
		return "macos"
	}
	return ""
}

// unsupportedPlatform refuses selection on this process's platform when the
// declaration lists it as unsupported.
func unsupportedPlatform() error {
	if platform := UnsupportedPlatform(runtime.GOOS); platform != "" {
		return &UnsupportedPlatformError{Platform: platform}
	}
	return nil
}
