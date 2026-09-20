package runtime

import (
	"fmt"
	"maps"
	"slices"

	jobapi "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	rightspolicy "github.com/openabstractions/abstraction-rights/go"
	routerservice "github.com/openabstractions/abstraction-router/go/service"
)

// Rights actions enforced by the storage content profiles this runtime serves.
const (
	ContentReadAction    = "abstraction.storage/content.read"
	ContentWriteAction   = "abstraction.storage/content.write"
	ContentObserveAction = "abstraction.storage/content.observe"
)

// ResourceRightsActions lists every action a resource service composed by this
// runtime enforces, sorted. A host registers them into its decision policy
// through Options.RightsActions. The job definition declares its own
// (resource_actions, JOB-A14); the storage, config, logging, model and router
// definitions do not yet, and their names are listed here.
func ResourceRightsActions() []string {
	actions := []string{ContentReadAction, ContentWriteAction, ContentObserveAction, ConfigEditAction,
		LogHistoryAction, ModelLookupAction, routerservice.ActionInventory, routerservice.ActionRoute}
	actions = append(actions, jobapi.ResourceActions...)
	actions = append(actions, slices.Collect(maps.Values(JobRightsActions))...)
	slices.Sort(actions)
	return slices.Compact(actions)
}

// registerRightsActions registers each action with the host's own subject as
// registrant. An action already in the catalogue writes nothing.
func registerRightsActions(policy *rightspolicy.DecisionPolicy, actions []string) error {
	for _, action := range actions {
		if err := policy.RegisterAction(action); err != nil {
			return fmt.Errorf("register %s: %w", action, err)
		}
	}
	return nil
}
