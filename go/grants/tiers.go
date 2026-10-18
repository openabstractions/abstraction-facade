package grants

import (
	"slices"
	"strings"

	asks "github.com/openabstractions/abstraction-asks/go"
)

// The first-use permissions the tier table names, spelled as their owners
// spell them: inference.ActionComplete, the facade runtime's
// ModelLookupAction and JobSubmitAction, and the runtime's lend action. The
// runtime's tests hold each to its owner's constant.
const (
	CompleteAction    = "abstraction.inference/complete"
	ModelLookupAction = "abstraction.model/lookup"
	JobSubmitAction   = "abstraction.job/acceptance.submit"
	LendAction        = "abstraction.storage/lend"
	// RouterInventoryAction is the router's inventory read, asked at first use
	// (default-policy D5). No tier row names it, so it is sensitive.
	RouterInventoryAction = "abstraction.router/inventory.read"
	// completeResourcePrefix is inference.ResourceHost's prefix: the resource
	// of complete names one inference server.
	completeResourcePrefix = "host:"
)

// TierFacts are what the runtime knows about a first-use resource that its
// name does not say. A nil function knows of nothing, so its rows fall to the
// stricter tier.
type TierFacts struct {
	// LocalServer reports whether the named inference server runs on this
	// machine. A hosted server, a remote runtime and a server the runtime
	// does not list are not local.
	LocalServer func(name string) bool
	// ListedRegistry reports whether the runtime lists the named model
	// registry.
	ListedRegistry func(name string) bool
}

// TierClass is one row of the first-use tier table: which permissions it
// covers, their tier, and the words the Panel's enrollment warning names it
// with (completing "an app may ..."). The words are this table's; the Panel
// keeps no copy.
type TierClass struct {
	Tier  asks.Tier
	Words string
	match func(action, resource string, facts TierFacts) bool
}

func outputContent(action, resource string, _ TierFacts) bool {
	return (action == ContentWriteAction || action == ContentReadAction) && slices.Contains(OutputProfileResources, resource)
}

func localComplete(action, resource string, facts TierFacts) bool {
	name, ok := strings.CutPrefix(resource, completeResourcePrefix)
	return action == CompleteAction && ok && name != "" && facts.LocalServer != nil && facts.LocalServer(name)
}

func listedLookup(action, resource string, facts TierFacts) bool {
	return action == ModelLookupAction && facts.ListedRegistry != nil && facts.ListedRegistry(resource)
}

// FirstUseTierClasses is the one table of first-use tiers (asks ASK-R6;
// research/rights-model/PROPOSAL.md §7.1, §15 part 1; VISION 2026-10-03),
// read by the runtime's answerer check and by the Panel. The first row that
// matches decides; a permission no row names is sensitive.
var FirstUseTierClasses = []TierClass{
	{asks.TierAdministrative, "change what apps may do or who answers for you", func(action, _ string, _ TierFacts) bool {
		return strings.HasPrefix(action, "abstraction.asks/") || strings.HasPrefix(action, "abstraction.rights/")
	}},
	{asks.TierRoutine, "use a model on this device", localComplete},
	{asks.TierAdministrative, "use a model on a hosted server, which can cost money", func(action, _ string, _ TierFacts) bool {
		return action == CompleteAction
	}},
	{asks.TierRoutine, "look up a model in a registry OpenAbstractions lists", listedLookup},
	{asks.TierRoutine, "start a model job on this device", func(action, resource string, _ TierFacts) bool {
		return action == JobSubmitAction && resource == "job:inference"
	}},
	{asks.TierRoutine, "save and show the pictures, speech or video it made", outputContent},
	{asks.TierSensitive, "download a model", func(action, resource string, _ TierFacts) bool {
		return action == JobSubmitAction && resource == "job:download"
	}},
	{asks.TierSensitive, "lend your model files to another app", func(action, _ string, _ TierFacts) bool {
		return action == LendAction
	}},
}

// FirstUseTier is the tier of the first-use question on action and resource
// (asks ASK-R6). Only a routine one may be answered by an answerer.
func FirstUseTier(action, resource string, facts TierFacts) asks.Tier {
	for _, class := range FirstUseTierClasses {
		if class.match(action, resource, facts) {
			return class.Tier
		}
	}
	return asks.TierSensitive
}
