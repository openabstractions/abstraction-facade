// Package client is the service-only facade. Unlike the legacy facade package,
// it imports no embedded store or file provider. Service absence is an error
// from the requested capability, never a request to create local state.
package client

import (
	config "github.com/openabstractions/abstraction-config/go/client"
	logging "github.com/openabstractions/abstraction-logging/go/client"
	router "github.com/openabstractions/abstraction-router/go/client"
)

type Machine struct{ endpoint string }

// Discover creates a facade using the runtime bootstrap convention.
// ResolveLog/ResolveConfig/ResolveRouter query its actual registrations.
func Discover() *Machine { return &Machine{} }

// Log is the legacy fixed-endpoint accessor. Use ResolveLog for runtime selection.
func (*Machine) Log() *logging.Client   { return logging.Discover() }
func (*Machine) Config() *config.Client { return config.Discover() }
func (*Machine) Router() *router.Client { return router.Discover() }
