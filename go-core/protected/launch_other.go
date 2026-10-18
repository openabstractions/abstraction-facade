//go:build !windows

package protected

import "time"

// Launch is unavailable outside Windows in 0.4.0 (FAC-H4).
func Launch(owner uintptr, helper, operation, ticket string) (func(time.Duration) (uint32, error), error) {
	if _, err := HelperArguments(operation, ticket); err != nil {
		return nil, err
	}
	return nil, ErrUnavailable
}

// UACPolicy has no meaning outside Windows; Read is always false.
type UACPolicy struct {
	Read                       bool
	EnableLUA                  uint32
	ConsentPromptBehaviorAdmin uint32
	PromptOnSecureDesktop      uint32
}

// ReadUACPolicy returns an unread policy outside Windows.
func ReadUACPolicy() UACPolicy { return UACPolicy{} }

func (p UACPolicy) String() string { return "no UAC on this platform" }
