//go:build !windows

package bootstrap

import "github.com/openabstractions/abstraction-identity/listen"

// Endpoint preserves the shared transport's existing Unix runtime convention.
func Endpoint(service string) (string, error) {
	if err := validService(service); err != nil {
		return "", err
	}
	return listen.Endpoint(service), nil
}
