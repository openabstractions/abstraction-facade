package client

// Contract identities are the exact versioned services requested by Machine's
// typed Resolve* accessors. Applications can use them in status observations.
const (
	AsksContract               = "abstraction.asks/application@1"
	AsksOperatorContract       = "abstraction.asks/operator@1"
	ConfigContract             = "abstraction.config/reader@1"
	ConfigEditorContract       = "abstraction.config/editor@1"
	ConfigObserverContract     = "abstraction.config/observer@1"
	CredentialsContract        = "abstraction.credentials/holder@1"
	CredentialsApplierContract = "abstraction.credentials/applier@1"
	EmbeddingsContract         = "abstraction.inference/embed@1"
	InferenceContract          = "abstraction.inference/chat@1"
	InferenceOperatorContract  = "abstraction.inference/operator@1"
	JobInventoryContract       = "abstraction.job/inventory@1"
	JobOperationsContract      = "abstraction.job/operations@1"
	JobOperatorContract        = "abstraction.job/operator@1"
	JobsContract               = "abstraction.job/acceptance@1"
	LogContract                = "abstraction.logging/sink@1"
	LogObserverContract        = "abstraction.logging/observer@1"
	LogReaderContract          = "abstraction.logging/reader@1"
	ModelContract              = "abstraction.model/resolver@1"
	ResourceLeasesContract     = "abstraction.resource/leases@1"
	ResourceTableContract      = "abstraction.resource/table@1"
	RightsContract             = "abstraction.rights/authorization@1"
	RightsOperatorContract     = "abstraction.rights/operator@1"
	RouterContract             = "abstraction.router/router@1"
	StorageContract            = "abstraction.storage/content-reader@1"
	StorageChangesContract     = "abstraction.storage/content-changes@1"
	StorageInventoryContract   = "abstraction.storage/inventory@1"
	StorageWriterContract      = "abstraction.storage/content-writer@1"
)
