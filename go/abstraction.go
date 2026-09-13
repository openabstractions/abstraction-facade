// Package abstraction supplies the service-first application entry point.
// Discover creates a resolver binding; Resolve* methods report availability and
// typed refusal for each capability. Discovery opens no local store and starts
// no provider. Existing embedded adopters explicitly import the legacy package.
package abstraction

import (
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/client"
)

type Machine = client.Machine
type Requirements = client.Requirements
type BindingError = client.BindingError
type JobsClient = client.JobsClient
type JobsOptions = client.JobsOptions

// DefaultStatusRequests supplies the generated baseline for Machine.Observe.
// Applications may instead request only the capabilities they use.
func DefaultStatusRequests() []wire.ResolveRequest { return client.DefaultStatusRequests() }

// Discover uses the installed runtime bootstrap. It performs no eager I/O.
func Discover() *Machine { return client.Discover() }

// New binds the trusted runtime endpoint supplied by installation or a host.
func New(endpoint string) *Machine { return client.New(endpoint) }

// NewJobs restores an explicitly retained endpoint and logical owner for recovery.
func NewJobs(endpoint string, options JobsOptions) (*JobsClient, error) {
	return client.NewJobs(endpoint, options)
}
