package grants

import (
	credentials "github.com/openabstractions/abstraction-credentials/go"
	rights "github.com/openabstractions/abstraction-rights/go"
	wire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

// The composition rows identify existing owning IDL operations and receiving
// boundaries. Reviewed inventory f279448463f91e92f06f00c184ebb7674bce88c4 is
// source evidence; resource owners still perform their actual domain checks.
// Legacy validators here check exact Rights grammar and preserve literal '*'.
func legacyResource(resource string) bool {
	return rights.ValidDecisionQuery("abstraction.rights/definition", resource)
}
func credentialResource(resource string) bool {
	_, ok := credentials.ParseResource(resource)
	return ok
}

// NotificationPostAction and NotificationPostResource are the rule that
// decides whether a program may post notifications; a person's block on it
// turns one program's notifications off (ASK-N1, ASK-N6).
const (
	NotificationPostAction   = "abstraction.asks/notification.post"
	NotificationPostResource = "account"
)

var permissionDefinitions = buildPermissionDefinitions()

func buildPermissionDefinitions() *rights.DefinitionCatalog {
	definitions := []wire.PermissionDefinition{
		{Action: NotificationPostAction, OwnerContract: "abstraction.asks/notification@1",
			Operations: []string{"abstraction.asks/notification@1#Post", "abstraction.asks/notification-operator@1#Act"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.asks/notification@1", "abstraction.asks/notification-operator@1"}, Disclosure: "Post the caller's own one-way notification with attributed text and up to two actions; an action records application intent only. A block also refuses actions on posted notifications.",
			Lifecycle: "Every notification of one program in the receiving account; within service rate and live bounds.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.asks/question.answer", OwnerContract: "abstraction.asks/responder@1",
			Operations: []string{"abstraction.asks/responder@1#ListAnswerable", "abstraction.asks/responder@1#Answer", "abstraction.asks/responder@1#ListChoices", "abstraction.asks/responder@1#AnswerChoice", "abstraction.asks/responder@1#RequestAnswering"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.asks/responder@1"}, Disclosure: "List answerable; apply attributed allow/refusal under enrollment and renew expiry; paired allows atomic. List shared ordinary choices and record a shared option; audited, no OA permission effect. The enrollment on apps is asked at first use and answered by the person only.",
			Lifecycle: "apps = every current and later app in receiving account; routine admitted first-use requests and the options apps share in ordinary choices only. Lapses 30 days after the last answer; Stop ends it.", FirstUse: wire.PermissionFirstUseExact, Served: true},
		{Action: "abstraction.asks/question.ask", OwnerContract: "abstraction.asks/application@1",
			Operations: []string{"abstraction.asks/application@1#Ask", "abstraction.asks/application@2#Ask", "abstraction.asks/application@2#ShareChoice"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.asks/application@1", "abstraction.asks/application@2"}, Disclosure: "Admit or replay caller-owned question; question admission itself grants no resource authority. Admit/replay choice; supplied option IDs confer no OA permission.",
			Lifecycle: "All catalogue questions; one receiving account. Ordinary app-owned intent; one receiving account.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.config/user.replace", OwnerContract: "abstraction.config/editor@1",
			Operations: []string{"abstraction.config/editor@1#ReplaceUser"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.config/editor@1"}, Disclosure: "Atomic user-settings replace.",
			Lifecycle: "All persisted user rung values in one replace.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.credentials/apply", OwnerContract: "abstraction.credentials/applier@1",
			Operations: []string{"abstraction.credentials/applier@1#Check", "abstraction.credentials/applier@1#Apply"}, ResourceProfile: credentials.NamedUseResourceProfile, ResourceType: "abstraction.credentials.api.CredentialResource",
			EnforcedBy: []string{"abstraction.credentials/applier@1"}, Disclosure: "Use a named credential for one request through a designated service to the currently registered hosts and descendant subdomains. Check reads no secret; Apply gives headers only to the designated service.",
			Lifecycle: "One account-owned credential name slot. The grant follows rotation and a recreated registration with its current hosts/consumers. It grants no holder management and freezes no current restrictions.", FirstUse: wire.PermissionFirstUseExact, Served: true},
		{Action: "abstraction.credentials/holder.manage", OwnerContract: "abstraction.credentials/holder@1",
			Operations: []string{"abstraction.credentials/holder@1#Store", "abstraction.credentials/holder@1#Rotate", "abstraction.credentials/holder@1#Revoke"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.credentials/holder@1"}, Disclosure: "Store/replace/destroy secure-store bytes; no bytes read back.",
			Lifecycle: "Full registration/rotation/revocation of account credentials, including future names.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.credentials/holder.read", OwnerContract: "abstraction.credentials/holder@1",
			Operations: []string{"abstraction.credentials/holder@1#List", "abstraction.credentials/holder@1#Audit"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.credentials/holder@1"}, Disclosure: "Enumerate metadata and audit; no secret bytes returned.",
			Lifecycle: "All receiving account credential metadata/audit; future registered names included.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.facade/application.activate", OwnerContract: "abstraction.facade/applications@2",
			Operations: []string{"abstraction.facade/applications@2#Activate"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.facade/applications@2"}, Disclosure: "Bring up registered app and report readiness.",
			Lifecycle: "One registered app; activation command comes from trusted descriptor.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.facade/application.announce", OwnerContract: "abstraction.facade/applications@2",
			Operations: []string{"abstraction.facade/applications@2#Announce", "abstraction.facade/applications@2#Withdraw"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.facade/applications@2"}, Disclosure: "Publish/withdraw app presence.",
			Lifecycle: "One registered application, own executable/session/presence.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.facade/application.manage", OwnerContract: "abstraction.facade/applications@2",
			Operations: []string{"abstraction.facade/applications@2#Register", "abstraction.facade/applications@2#Remove"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.facade/applications@2"}, Disclosure: "Register/remove application activation descriptor.",
			Lifecycle: "Whole experimental applications directory.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.facade/application.read", OwnerContract: "abstraction.facade/applications@2",
			Operations: []string{"abstraction.facade/applications@2#Observe"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.facade/applications@2"}, Disclosure: "Read filtered directory/presence.",
			Lifecycle: "One app per current filtering check; account directory response includes permitted apps.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.facade/application.register_self", OwnerContract: "abstraction.facade/applications@2",
			Operations: []string{"abstraction.facade/applications@2#Register"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.facade/applications@2"}, Disclosure: "Register the caller's own application descriptor; the registered program receives no read, announce or activation permission from it.",
			Lifecycle: "One account; the caller's own program only. The account-wide register default permits it unless the program's own rule or the switch refuses (rights RIGHTS-R22).", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.facade/provider.manage", OwnerContract: "abstraction.facade/registry@1",
			Operations: []string{"abstraction.facade/registry@1#Declarations", "abstraction.facade/registry@1#Declare", "abstraction.facade/registry@1#Withdraw", "abstraction.facade/registry@1#Observe", "abstraction.facade/registry@2#Declarations", "abstraction.facade/registry@2#Declare", "abstraction.facade/registry@2#Withdraw", "abstraction.facade/registry@2#Observe"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.facade/registry@1", "abstraction.facade/registry@2"}, Disclosure: "List/edit/withdraw provider declaration; trusted add can assign provider hold/inventory.provide and operator complete rights.",
			Lifecycle: "All provider/program declarations in receiving account, including future providers.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.inference/audit.read", OwnerContract: "abstraction.inference/operator@1",
			Operations: []string{"abstraction.inference/operator@1#Audit"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.inference/operator@1"}, Disclosure: "Read retained audit.",
			Lifecycle: "All receiving account inference audit records.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.inference/complete", OwnerContract: "abstraction.inference/chat@1",
			Operations: []string{"abstraction.inference/chat@1#Start", "abstraction.inference/chat@2#Start", "abstraction.inference/preload@1#Preload", "abstraction.inference/embed@1#Embed", "abstraction.inference/transcription@1#Start", "abstraction.inference/speech@1#Start", "abstraction.inference/live@1#Start", "abstraction.inference/image@1#Start", "abstraction.inference/image@2#Start", "abstraction.inference/video@1#Start"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.inference/chat@1", "abstraction.inference/chat@2", "abstraction.inference/embed@1", "abstraction.inference/preload@1", "abstraction.inference/transcription@1", "abstraction.inference/speech@1", "abstraction.inference/live@1", "abstraction.inference/image@1", "abstraction.inference/image@2", "abstraction.inference/video@1"}, Disclosure: "Start model call/preload/embed/video; pin accepted provider and attribution.",
			Lifecycle: "One model server, all supported models/modalities; excludes different hosts.", FirstUse: wire.PermissionFirstUseExact, Served: true},
		{Action: "abstraction.inference/host.manage", OwnerContract: "abstraction.inference/operator@1",
			Operations: []string{"abstraction.inference/operator@1#Hosts", "abstraction.inference/operator@1#AddHost", "abstraction.inference/operator@1#RemoveHost", "abstraction.inference/operator@1#Gateway", "abstraction.inference/operator@1#SetGateway"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.inference/operator@1"}, Disclosure: "Read/edit server registrations and gateway; host edits write limited operator/provider permits.",
			Lifecycle: "Full model server list/config and loopback gateway settings in receiving account.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.inference/key.issue", OwnerContract: "abstraction.inference/operator@1",
			Operations: []string{"abstraction.inference/operator@1#Keys", "abstraction.inference/operator@1#IssueKey", "abstraction.inference/operator@1#RevokeKey"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.inference/operator@1"}, Disclosure: "Mint/revoke per-program gateway key; key alone grants no completion authority.",
			Lifecycle: "All program gateway keys in receiving account.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.inference/selection.manage", OwnerContract: "abstraction.inference/selection-operator@2",
			Operations: []string{"abstraction.inference/selection-operator@2#Read", "abstraction.inference/selection-operator@2#Put", "abstraction.inference/selection-operator@2#DeletePolicy"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.inference/selection-operator@2"}, Disclosure: "Read/write/delete named selection policy.",
			Lifecycle: "Own program's named policies in the receiving account; future policy names included.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.inference/selection.manage_for", OwnerContract: "abstraction.inference/selection-operator@2",
			Operations: []string{"abstraction.inference/selection-operator@2#Read", "abstraction.inference/selection-operator@2#Put", "abstraction.inference/selection-operator@2#DeletePolicy"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.inference/selection-operator@2"}, Disclosure: "Read/write/delete another program's named selection policy.",
			Lifecycle: "Any target program's named policies within the receiving account; future policy names included.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.job/acceptance.cancel", OwnerContract: "abstraction.job/operator@1",
			Operations: []string{"abstraction.job/operator@1#CancelOperation"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.job/operator@1"}, Disclosure: "Request cross-program cancellation.",
			Lifecycle: "Any operation in receiving account.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.job/acceptance.submit", OwnerContract: "abstraction.job/acceptance@1",
			Operations: []string{"abstraction.job/acceptance@1#Submit"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.job/acceptance@1"}, Disclosure: "Accept durable work once; no automatic retry creates fresh work.",
			Lifecycle: "One valid job kind OR every kind, including future kind names; deny on either overrides permit. First-use asks only for job:<kind>; the contract-wide acceptance check stays quiet.", FirstUse: wire.PermissionFirstUseExact, Served: true},
		{Action: "abstraction.job/inventory.read", OwnerContract: "abstraction.job/operator@1",
			Operations: []string{"abstraction.job/operator@1#ListAccountWork"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.job/operator@1"}, Disclosure: "Read cross-program work records.",
			Lifecycle: "All programs durable work in receiving account.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.logging/history.read", OwnerContract: "abstraction.logging/reader@1",
			Operations: []string{"abstraction.logging/reader@1#Read", "abstraction.logging/observer@1#Observe", "abstraction.logging/source-reader@1#Sources", "abstraction.logging/source-reader@1#Read", "abstraction.logging/source-reader@1#StartAtEnd"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.logging/reader@1", "abstraction.logging/observer@1", "abstraction.logging/source-reader@1"}, Disclosure: "Read or long-poll retained account history. Sources checks each source right before probe; Read/StartAtEnd recheck and invoke native source read.",
			Lifecycle: "All retained OA events of receiving account; future writing apps included. One whole native log source; no provider/program/time-window right.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.model/lookup", OwnerContract: "abstraction.model/resolver@1",
			Operations: []string{"abstraction.model/resolver@1#Resolve"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.model/resolver@1"}, Disclosure: "Resolve portable requests/descriptor manifest. The model manifest@1 profile is defined and unserved.",
			Lifecycle: "All model lookups in one registry; network reach can occur; manifest moves no content bytes.", FirstUse: wire.PermissionFirstUseExact, Served: true},
		{Action: "abstraction.resource/hold", OwnerContract: "abstraction.resource/leases@1",
			Operations: []string{"abstraction.resource/leases@1#Acquire", "abstraction.resource/leases@1#AcquireFree"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.resource/leases@1"}, Disclosure: "Reserve lease; may ask holders to yield; wake inhibitor subject to platform.",
			Lifecycle: "One scarce resource; attached-holder yield separately service-authorized.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.resource/table.read", OwnerContract: "abstraction.resource/table@1",
			Operations: []string{"abstraction.resource/table@1#Holders"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.resource/table@1"}, Disclosure: "Read other programs' holder rows under the account-scoped table right.",
			Lifecycle: "One receiving account. Public capacities and own holder rows remain available without this right.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.resource/yield", OwnerContract: "abstraction.resource/leases@1",
			Operations: []string{"abstraction.resource/leases@1#Acquire"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.resource/leases@1"}, Disclosure: "Request attached model holders to unload under the runtime own program host-specific yield decision.",
			Lifecycle: "One attached host:<name>; runtime yieldPolicy is the receiving check. The app hold right supplies no runtime yield authority.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.router/inventory.read", OwnerContract: "abstraction.router/router@1",
			Operations: []string{"abstraction.router/router@1#Models", "abstraction.router/router@1#Hosts", "abstraction.router/router@1#Offer"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.router/router@1"}, Disclosure: "Read inventory/offer candidates; no loading/unloading.",
			Lifecycle: "Account-wide models/servers, credential names, routing observations. First use asks the person: an app that lists models has no other path (default-policy D5).", FirstUse: wire.PermissionFirstUseExact, Served: true},
		{Action: "abstraction.router/route", OwnerContract: "abstraction.router/router@1",
			Operations: []string{"abstraction.router/router@1#Pick"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.router/router@1"}, Disclosure: "Select server; no execution by router.",
			Lifecycle: "Routing across all models/servers; actual inference needs complete per selected host.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.storage/content.observe", OwnerContract: "abstraction.storage/content-changes@1",
			Operations: []string{"abstraction.storage/content-changes@1#Observe", "abstraction.storage/content-changes@1#List"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.storage/content-changes@1"}, Disclosure: "Read/list changes only for readable digests.",
			Lifecycle: "Account store changes, filtered by content.read per digest.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.storage/content.read", OwnerContract: "abstraction.storage/content-reader@1",
			Operations: []string{"abstraction.storage/content-reader@1#Open", "abstraction.storage/content-reader@1#Read", "abstraction.storage/inventory@1#List", "abstraction.storage/inventory@1#ListStore", "abstraction.storage/inventory@1#Holders", "abstraction.storage/inventory@1#Unheld", "abstraction.storage/inventory@1#Find"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.storage/content-reader@1", "abstraction.storage/inventory@1"}, Disclosure: "Open/read verified-by-consumer content through bounded chunks. List/find/holders/unheld composed inventory.",
			Lifecycle: "Exact digest OR caller-owned published profile bytes. Composed stores/files/holders; content.read filters objects/manifests. ListStore limits data source selection, not authority.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.storage/content.remove", OwnerContract: "abstraction.storage/content-remover@1",
			Operations: []string{"abstraction.storage/content-remover@1#Remove"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{}, Disclosure: "Defined removal of content; the content-remover@1 receiving service is unserved.",
			Lifecycle: "Defined exact content resource; no current receiving enforcement or first-use admission.", FirstUse: wire.PermissionFirstUseNone, Served: false},
		{Action: "abstraction.storage/content.write", OwnerContract: "abstraction.storage/content-writer@1",
			Operations: []string{"abstraction.storage/content-writer@1#Begin", "abstraction.storage/content-writer@1#Append", "abstraction.storage/content-writer@1#Commit"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.storage/content-writer@1"}, Disclosure: "Begin/append/commit verified bytes; Abort cleans caller upload.",
			Lifecycle: "One upload or typed profile; write right supplies no unrelated read permission.", FirstUse: wire.PermissionFirstUseConsumingPaired, Served: true},
		{Action: "abstraction.storage/inventory.provide", OwnerContract: "abstraction.storage/inventory-source@1",
			Operations: []string{"abstraction.storage/inventory-source@1#Describe", "abstraction.storage/inventory-source@1#Snapshot", "abstraction.storage/inventory-source@1#Observe", "abstraction.storage/inventory-source@1#Verify", "abstraction.storage/inventory-source@1#Remove"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.storage/inventory-source@1"}, Disclosure: "Describe/snapshot/observe/verify/remove source index/files under owned provider contract.",
			Lifecycle: "Provider-owned declared store; trusted runtime peer, no arbitrary app peer.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.storage/inventory.read", OwnerContract: "abstraction.storage/inventory@1",
			Operations: []string{"abstraction.storage/inventory@1#List", "abstraction.storage/inventory@1#ListStore", "abstraction.storage/inventory@1#Holders", "abstraction.storage/inventory@1#Unheld", "abstraction.storage/inventory@1#Find"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.storage/inventory@1"}, Disclosure: "List/find/holders/unheld composed inventory.",
			Lifecycle: "Composed stores/files/holders; content.read filters objects/manifests. ListStore limits data source selection, not authority.", FirstUse: wire.PermissionFirstUseNone, Served: true},
		{Action: "abstraction.storage/lend", OwnerContract: "abstraction.storage/lend@1",
			Operations: []string{"abstraction.storage/lend@1#Lend", "abstraction.storage/lend@1#Unlend", "abstraction.storage/lend@1#Lends"}, ResourceProfile: rights.LegacyExactResourceProfile, ResourceType: "string (legacy exact resource)",
			EnforcedBy: []string{"abstraction.storage/lend@1"}, Disclosure: "Lend/unlend authorized model link/copy; observe filtered lend ledger.",
			Lifecycle: "One engine; all existing lends to that engine; Lends returns permitted engines only.", FirstUse: wire.PermissionFirstUseExact, Served: true},
	}
	catalogue, err := rights.NewDefinitionCatalog(definitions, map[string]func(string) bool{rights.LegacyExactResourceProfile: legacyResource, credentials.NamedUseResourceProfile: credentialResource})
	if err != nil {
		panic(err)
	}
	return catalogue
}

// PermissionDefinitions returns immutable metadata copies for shared explanation.
func PermissionDefinitions() []wire.PermissionDefinition {
	rows := permissionDefinitions.Definitions()
	for _, d := range savedSecretDefinitions(false) {
		if d.Definition.Action != credentials.ActionApply {
			rows = append(rows, d.Definition)
		}
	}
	return rows
}
func PermissionDefinition(action string) (wire.PermissionDefinition, bool) {
	if d, ok := permissionDefinitions.Lookup(action); ok {
		return d, true
	}
	for _, d := range savedSecretDefinitions(false) {
		if d.Definition.Action == action {
			return d.Definition, true
		}
	}
	return wire.PermissionDefinition{}, false
}

// FirstUseMode includes the existing consuming paired profile restrictions.
func FirstUseMode(action, resource string) wire.PermissionFirstUse {
	if action == JobSubmitAction && resource == "abstraction.job/acceptance@1" {
		return wire.PermissionFirstUseNone
	}
	// Answering is asked at first use only as the enrollment, on apps (asks
	// ASK-R10); an exact delegation is an operator's rule, never a question.
	if action == rights.MetaAnswerAction && resource != rights.MetaScopeApps {
		return wire.PermissionFirstUseNone
	}
	mode := permissionDefinitions.FirstUse(action, resource)
	if mode == wire.PermissionFirstUseConsumingPaired && !Paired(action, resource) {
		return wire.PermissionFirstUseNone
	}
	return mode
}

func FirstUseActions() []string {
	var result []string
	for _, definition := range permissionDefinitions.Definitions() {
		if definition.Served && definition.FirstUse == wire.PermissionFirstUseExact {
			result = append(result, definition.Action)
		}
	}
	return result
}
