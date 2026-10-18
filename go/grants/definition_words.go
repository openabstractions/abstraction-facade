package grants

import (
	"strings"
	"time"
	"unicode"

	credentials "github.com/openabstractions/abstraction-credentials/go"
	rights "github.com/openabstractions/abstraction-rights/go"
	wire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

func definitionPlainName(value string) bool {
	if value == "" || len(value) > 64 || strings.ContainsAny(value, `/\`) {
		return false
	}
	hex := 0
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' {
			hex++
		} else {
			hex = 0
		}
		if hex >= 16 || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' || r == ':' || r == ' ') {
			return false
		}
	}
	return true
}

func definitionQuote(value string) string { return "“" + value + "”" }

// EnrollmentDisclosure is what the first-use question for the enrollment
// (asks ASK-R10) says about the right it asks for, beside the Panel's
// warning, which lists the classes it answers and those that stay with the
// person.
const EnrollmentDisclosure = "Passwords and keys, spending money, new apps, network sharing, deleting your files, OpenAbstractions itself and anything marked sensitive always come to you. An answer to an app's choice cannot be undone. It ends after 30 days without an answer; review it and stop it in the OpenAbstractions Panel."

// FirstUseRefuseInterval is how long a person's refusal of a first-use
// question stands before the same refusal asks again (asks ASK-R2). A plain
// Stop of an enrollment keeps the next enrollment question away as long
// (ASK-R10).
const FirstUseRefuseInterval = 10 * time.Minute

// The reasons the Panel's Stop records on the enrollment it replaces with a
// deny (ASK-R10): a plain Stop's deny lapses after FirstUseRefuseInterval;
// "Stop and don't ask again" stands until the person removes it.
const (
	EnrollmentStoppedWhy    = "Stopped answering for you; may ask again after 10 minutes"
	EnrollmentNeverAgainWhy = "Stopped answering for you; never ask again"
)

// EnrollmentWhy is the reason an enrollment records, as the Panel writes it.
func EnrollmentWhy(name string) string {
	return "Let " + name + " answer OpenAbstractions questions for you"
}

// FirstUseWords is the shared service-owned explanation of supported first-use
// definitions. Structured resource data supplies names; text supplies no target.
// Displaying an existing broad job record preserves its established caption;
// FirstUseMode separately refuses admission for that contract-wide resource.
func FirstUseWords(action, resource string) (verb, object string, ok bool) {
	if action == JobSubmitAction && resource == "abstraction.job/acceptance@1" {
		return "submit background work", "", true
	}
	if FirstUseMode(action, resource) == wire.PermissionFirstUseNone {
		return "", "", false
	}
	switch action {
	case rights.MetaAnswerAction:
		return "answer routine questions and the choices apps share with it", "for you", true
	case credentials.ActionApply:
		value, valid := credentials.ParseResource(resource)
		if !valid {
			return "", "", false
		}
		return "use the credential " + definitionQuote(value.Name), "through its registered services and websites", true
	case LendAction:
		if name, valid := strings.CutPrefix(resource, "engine:"); valid && definitionPlainName(name) {
			return "lend a model", "to the engine " + definitionQuote(name), true
		}
		return "lend a model", "to an engine", true
	case CompleteAction:
		if name, valid := strings.CutPrefix(resource, "host:"); valid && definitionPlainName(name) {
			return "use AI models", "on the model server " + definitionQuote(name), true
		}
		return "use AI models", "", true
	case RouterInventoryAction:
		return "see the models and servers available", "", true
	case ModelLookupAction:
		if definitionPlainName(resource) {
			return "look up models", "in the model registry " + definitionQuote(resource), true
		}
		return "look up models", "", true
	case JobSubmitAction:
		switch resource {
		case "job:inference":
			return "run AI work in the background", "", true
		case "job:download":
			return "download in the background", "", true
		}
		return "submit background work", "", true
	case ContentWriteAction:
		switch resource {
		case "input:image":
			return "send images for editing, vision or video", "", true
		case "input:audio":
			return "send audio for transcription", "", true
		case "output:image":
			return "save the images it makes and read them back", "", true
		case "output:speech":
			return "save the speech it makes and read it back", "", true
		case "output:video":
			return "save the videos it makes and read them back", "", true
		}
	}
	return "", "", false
}

// FirstUseDisclosure is concise user copy for shared first-use surfaces.
func FirstUseDisclosure(action, resource string) string {
	if action == rights.MetaAnswerAction && FirstUseMode(action, resource) != wire.PermissionFirstUseNone {
		return EnrollmentDisclosure
	}
	if action != credentials.ActionApply || FirstUseMode(action, resource) == wire.PermissionFirstUseNone {
		return ""
	}
	value, ok := credentials.ParseResource(resource)
	if !ok {
		return ""
	}
	return "Lets this app use " + definitionQuote(value.Name) + " through its registered services and websites. Access continues if the saved secret or its registration changes."
}

// FirstUseDefinitionDetails preserves the complete owning disclosure/lifecycle
// in the detailed view, alongside the exact structured permission target.
func FirstUseDefinitionDetails(action, resource string) string {
	if action != credentials.ActionApply || FirstUseMode(action, resource) == wire.PermissionFirstUseNone {
		return ""
	}
	definition, ok := PermissionDefinition(action)
	if !ok {
		return ""
	}
	return definition.Disclosure + " " + definition.Lifecycle
}
