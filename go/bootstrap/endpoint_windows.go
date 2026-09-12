//go:build windows

package bootstrap

import (
	"fmt"

	"github.com/openabstractions/abstraction-identity/listen"
	"golang.org/x/sys/windows"
)

// Endpoint derives the Windows pipe namespace from the current process token.
// Its format is shared with the C++ resolving facade. Identity failure returns
// an error before a host listens or a resolving client connects.
func Endpoint(service string) (string, error) { return endpoint(service, currentUserSID) }

func currentUserSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("bootstrap: process user identity unavailable: %w", err)
	}
	return user.User.Sid.String(), nil
}

func endpoint(service string, processUser func() (string, error)) (string, error) {
	if err := validService(service); err != nil {
		return "", err
	}
	sid, err := processUser()
	if err != nil {
		return "", err
	}
	parsed, err := windows.StringToSid(sid)
	if err != nil {
		return "", fmt.Errorf("bootstrap: invalid process user SID: %w", err)
	}
	return listen.Endpoint("user-" + parsed.String() + "-" + service), nil
}
