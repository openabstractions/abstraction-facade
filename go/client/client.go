// Package client is the service-only facade. Unlike the legacy facade package,
// it imports no embedded store or file provider. Service absence is an error
// from the requested capability, never a request to create local state.
package client

import (
	config "github.com/openabstractions/abstraction-config/go/client"
	logging "github.com/openabstractions/abstraction-logging/go/client"
	router "github.com/openabstractions/abstraction-router/go/client"
)

type Machine struct{}

// Discover resolves capabilities lazily using each service's shared endpoint
// convention. It does not claim that every possible service is installed.
func Discover() *Machine { return &Machine{} }

func (*Machine) Log() *logging.Client   { return logging.Discover() }
func (*Machine) Config() *config.Client { return config.Discover() }
func (*Machine) Router() *router.Client { return router.Discover() }
