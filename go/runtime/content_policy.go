package runtime

import (
	"context"

	identity "github.com/openabstractions/abstraction-identity"
	storage "github.com/openabstractions/abstraction-storage/go/service"
)

// ContentPolicyFromRights decides before each content access. The action is a
// configured catalogue identifier: abstraction.storage/content.read for
// StoragePolicy and abstraction.storage/content.write for StorageWritePolicy.
// The resource is the one the storage service names. No positive decision is
// cached.
func ContentPolicyFromRights(decisions Decider, action string) storage.Policy {
	return func(ctx context.Context, peer *identity.Peer, resource string) error {
		return requireDecision(ctx, decisions, peer, action, resource, storage.ErrPolicyUnavailable)
	}
}
