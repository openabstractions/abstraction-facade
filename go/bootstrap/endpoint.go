// Package bootstrap names the current user's shared runtime endpoints.
// Transport access control and peer binding remain supplied by identity/listen.
package bootstrap

import "fmt"

func validService(service string) error {
	if service == "" {
		return fmt.Errorf("bootstrap: service name required")
	}
	for _, c := range service {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return fmt.Errorf("bootstrap: invalid service name")
		}
	}
	return nil
}
