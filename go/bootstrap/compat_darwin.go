package bootstrap

import core "github.com/openabstractions/abstraction-facade/go-core/bootstrap"

func ValidateRuntimeAgent(raw []byte, executable string) error {
	return core.ValidateRuntimeAgent(raw, executable)
}
