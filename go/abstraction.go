// Package abstraction supplies the service-first application entry point.
// Discover creates a resolver binding; Resolve* methods report availability and
// typed refusal for each capability. Discovery opens no local store and starts
// no provider.
package abstraction

import (
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/client"
)

type Machine = client.Machine
type Requirements = client.Requirements

// Scope selects where a capability may execute.
type Scope = wire.Scope

const (
	ScopeAny    = wire.ScopeAny
	ScopeLocal  = wire.ScopeLocal
	ScopeRemote = wire.ScopeRemote
)

// ScopeValues lists the choices declared by the facade schema.
func ScopeValues() []Scope { return wire.ScopeValues() }

type ResolutionError = client.ResolutionError
type ResolutionErrorStatus = client.ResolutionErrorStatus

const (
	RuntimeUnavailable   = client.RuntimeUnavailable
	InvalidResolution    = client.InvalidResolution
	UnsupportedTransport = client.UnsupportedTransport
)

type JobsClient = client.JobsClient
type JobsOptions = client.JobsOptions

// Contract identities requested by the typed Machine accessors.
const (
	ApplicationsContract       = client.ApplicationsContract
	AsksContract               = client.AsksContract
	AsksOperatorContract       = client.AsksOperatorContract
	ConfigContract             = client.ConfigContract
	ConfigEditorContract       = client.ConfigEditorContract
	ConfigObserverContract     = client.ConfigObserverContract
	CredentialsContract        = client.CredentialsContract
	CredentialsApplierContract = client.CredentialsApplierContract
	EmbeddingsContract         = client.EmbeddingsContract
	InferenceContract          = client.InferenceContract
	InferenceOperatorContract  = client.InferenceOperatorContract
	JobInventoryContract       = client.JobInventoryContract
	JobOperationsContract      = client.JobOperationsContract
	JobOperatorContract        = client.JobOperatorContract
	JobsContract               = client.JobsContract
	LendingContract            = client.LendingContract
	LogContract                = client.LogContract
	LogObserverContract        = client.LogObserverContract
	LogReaderContract          = client.LogReaderContract
	ModelContract              = client.ModelContract
	RegistryContract           = client.RegistryContract
	ResourceLeasesContract     = client.ResourceLeasesContract
	ResourceTableContract      = client.ResourceTableContract
	RightsContract             = client.RightsContract
	RightsOperatorContract     = client.RightsOperatorContract
	RouterContract             = client.RouterContract
	StorageContract            = client.StorageContract
	StorageChangesContract     = client.StorageChangesContract
	StorageInventoryContract   = client.StorageInventoryContract
	StorageWriterContract      = client.StorageWriterContract
)

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
