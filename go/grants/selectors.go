package grants

import (
	"bytes"
	"slices"
	"strings"

	credentials "github.com/openabstractions/abstraction-credentials/go"
	logging "github.com/openabstractions/abstraction-logging/go"
	rights "github.com/openabstractions/abstraction-rights/go"
	wire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

// SelectorServices records actual receiving contracts installed by trusted
// runtime composition. Grammar remains registered when a receiver is unavailable.
// These flags describe presence; they confer no grant or OS privilege.
type SelectorServices struct {
	Credentials     bool
	ApplicationLogs bool
}

func savedSecretDefinitions(served bool) []wire.SelectorPermissionDefinition {
	rows := []struct{ action, label, contract, operation, disclosure string }{
		{credentials.ActionApply, "Use", "abstraction.credentials/applier@2", "Check,Apply", "Use through a designated service under current hosts and consumer restrictions. Gives headers to the designated service; supplies no raw value to the requesting app."},
		{credentials.ActionSecretRead, "Read secret value", "abstraction.credentials/reader@1", "Read", "Disclose the complete saved secret value to this requesting app. The app can export, copy, or use it elsewhere; current Use host and consumer restrictions do not constrain this disclosure."},
		{credentials.ActionSecretCreate, "Create", "abstraction.credentials/holder@2", "Store", "Store one saved app secret in a new name slot with its metadata and current restrictions. An existing slot conflicts; Rotate replaces the value."},
		{credentials.ActionSecretRotate, "Rotate", "abstraction.credentials/holder@2", "Rotate", "Replace one saved app secret value while retaining its current registration."},
		{credentials.ActionSecretRevoke, "Delete", "abstraction.credentials/holder@2", "Revoke", "Destroy one saved app secret value and retain revoked metadata as a tombstone until retention removes it."},
		{credentials.ActionSecretMetadataRead, "Read named metadata", "abstraction.credentials/holder@2", "Metadata", "Inspect metadata of one known saved app secret name. Does not enumerate unknown names or disclose its value; account List and Audit have separate legacy admission."},
	}
	result := make([]wire.SelectorPermissionDefinition, 0, len(rows))
	for _, r := range rows {
		profile := credentials.NamedSecretResourceProfile
		if r.action == credentials.ActionApply {
			profile = credentials.ScopedUseResourceProfile
		}
		operations := []string{}
		for _, op := range strings.Split(r.operation, ",") {
			operations = append(operations, r.contract+"#"+op)
		}
		d := wire.SelectorPermissionDefinition{Definition: wire.PermissionDefinition{Action: r.action, OwnerContract: r.contract, Operations: operations, ResourceProfile: profile, ResourceType: "abstraction.credentials.api.CredentialResource", EnforcedBy: []string{}, Disclosure: r.disclosure, Lifecycle: "One account-owned saved app secret name slot. Rotation and a recreated user-stored registration keep the target name. Runtime-managed local-key@1 records are excluded.", FirstUse: wire.PermissionFirstUseNone, Served: served}, Label: r.label, SupportedSelectors: []wire.ResourceSelectorKind{}, FirstUseSelectors: []wire.ResourceSelectorKind{}, Collection: credentials.SavedSecretCollection, RequestType: "abstraction.credentials.api.CredentialResource"}
		if served {
			d.Definition.EnforcedBy = []string{r.contract}
			d.SupportedSelectors = []wire.ResourceSelectorKind{wire.ResourceSelectorKindOne, wire.ResourceSelectorKindAll}
			d.FirstUseSelectors = slices.Clone(d.SupportedSelectors)
			d.AllEffect = "All current and future saved app secrets in this receiving account and runtime. Includes supported user-stored credentials; excludes runtime-managed local keys. Grants only this defined operation."
		}
		result = append(result, d)
	}
	return result
}

// SelectorPermissionDefinitions compiles the one owner catalogue for receiving
// services, public discovery, Asks and Panel. Capability codecs own target
// identity; compiled links import live explicit blocks in both directions only.
func SelectorPermissionDefinitions(services SelectorServices) (*rights.DefinitionCatalog, error) {
	defs := savedSecretDefinitions(services.Credentials)
	log := wire.SelectorPermissionDefinition{Definition: wire.PermissionDefinition{Action: logging.ApplicationLogReadAction, OwnerContract: "abstraction.logging/reader@2", Operations: []string{"abstraction.logging/reader@2#Read", "abstraction.logging/observer@2#Observe"}, ResourceProfile: logging.ApplicationLogResourceProfile, ResourceType: "abstraction.logging.ApplicationLogResource", EnforcedBy: []string{}, Disclosure: "Read or observe retained OA application events. One-app selection excludes unknown and unattributed records. All-app selection includes retained records under account binding and per-emitter blocks. Supplies no native OS source authority or helper launch permission.", Lifecycle: "Captures normalized program and file identity once; retained emitter identity is never reopened. Replaced executable follows the captured identity rules. Deleted targets remain removable using retained rule bytes.", FirstUse: wire.PermissionFirstUseNone, Served: services.ApplicationLogs}, Label: "Read application logs", SupportedSelectors: []wire.ResourceSelectorKind{}, FirstUseSelectors: []wire.ResourceSelectorKind{}, Collection: "abstraction.logging/applications@1", RequestType: "abstraction.logging.ApplicationLogResource"}
	if services.ApplicationLogs {
		log.Definition.EnforcedBy = []string{"abstraction.logging/reader@2", "abstraction.logging/observer@2"}
		log.SupportedSelectors = []wire.ResourceSelectorKind{wire.ResourceSelectorKindOne, wire.ResourceSelectorKindAll}
		log.FirstUseSelectors = slices.Clone(log.SupportedSelectors)
		log.AllEffect = "All current and future applications' retained OA logs in this receiving account and runtime. Explicit app blocks filter matching emitters; native protected OS channels remain separate."
	}
	defs = append(defs, log)
	credentialProfile := rights.ResourceProfile{AllowAll: true, Prepare: func(raw []byte) ([]byte, error) {
		v, err := credentials.DecodeCredentialTarget(raw)
		if err != nil {
			return nil, err
		}
		return credentials.PrepareCredentialTarget(v.Name)
	}, Validate: func(raw []byte) bool { _, err := credentials.DecodeCredentialTarget(raw); return err == nil }, Equal: bytes.Equal, Describe: func(raw []byte) (string, error) {
		v, err := credentials.DecodeCredentialTarget(raw)
		return v.Name, err
	}}
	logProfile := rights.ResourceProfile{AllowAll: true, Prepare: logging.PrepareApplicationLogTarget, Validate: logging.ValidateApplicationLogTarget, Equal: logging.EqualApplicationLogTargets, Describe: func(raw []byte) (string, error) {
		v, err := logging.DecodeApplicationResource(raw)
		return v.Program, err
	}}
	profiles := map[string]rights.ResourceProfile{credentials.ScopedUseResourceProfile: credentialProfile, credentials.NamedSecretResourceProfile: credentialProfile, logging.ApplicationLogResourceProfile: logProfile}
	members := make([]wire.PermissionKey, 0, 6)
	for _, d := range defs[:6] {
		members = append(members, wire.PermissionKey{Action: d.Definition.Action, ResourceProfile: d.Definition.ResourceProfile})
	}
	slices.SortFunc(members, func(a, b wire.PermissionKey) int {
		return strings.Compare(a.Action+"\x00"+a.ResourceProfile, b.Action+"\x00"+b.ResourceProfile)
	})
	bundle := wire.PermissionBundleDefinition{BundleRef: credentials.FullControlBundle, OwnerContract: "abstraction.credentials/holder@2", Label: "Full control v1", TargetType: "abstraction.credentials.api.CredentialResource", SupportedSelectors: []wire.ResourceSelectorKind{wire.ResourceSelectorKindOne, wire.ResourceSelectorKindAll}, Members: members, Disclosure: "Use, raw value Read, Create a new slot, Rotate its value, Delete its value with retained revoked metadata, and named metadata Read. Raw Read lets this app export, copy or use the secret elsewhere. This frozen six-operation list includes no enumeration, policy administration, future operation, profile version or runtime-managed local key."}
	key := func(action, profile string) wire.PermissionKey {
		return wire.PermissionKey{Action: action, ResourceProfile: profile}
	}
	links := []rights.LegacyEquivalence{{Action: credentials.ActionApply, Permission: key(credentials.ActionApply, credentials.ScopedUseResourceProfile), Mode: "saved-named-use@1", Concrete: func(resource string) ([]byte, bool) {
		name, ok := credentials.ParseResource(resource)
		if !ok {
			return nil, false
		}
		raw, err := credentials.PrepareCredentialTarget(name.Name)
		return raw, err == nil
	}},
		{Action: credentials.ActionRead, Resource: credentials.ResourceAccount, Permission: key(credentials.ActionSecretMetadataRead, credentials.NamedSecretResourceProfile), Mode: "saved-secret-metadata@1"},
		{Action: logging.ApplicationLogReadAction, Resource: "abstraction.logging/history", Permission: key(logging.ApplicationLogReadAction, logging.ApplicationLogResourceProfile), Mode: "retained-application-history@1"}}
	for _, action := range []string{credentials.ActionSecretCreate, credentials.ActionSecretRotate, credentials.ActionSecretRevoke} {
		links = append(links, rights.LegacyEquivalence{Action: credentials.ActionManage, Resource: credentials.ResourceAccount, Permission: key(action, credentials.NamedSecretResourceProfile), Mode: "saved-secret-management@1"})
	}
	return permissionDefinitions.WithSelectors(defs, profiles, []wire.PermissionBundleDefinition{bundle}, links)
}
