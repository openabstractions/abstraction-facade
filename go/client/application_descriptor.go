package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
)

// MaxApplicationDescriptorBytes bounds one JSON-encoded application manifest
// (facade.thrift, ApplicationDescriptor).
const MaxApplicationDescriptorBytes = 8192

var applicationName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidApplicationDescriptor holds an application manifest to the bounds
// facade.thrift states for ApplicationDescriptor and its activation recipe.
// The applications service applies it at Register; a program that shows a
// manifest before registering it applies the same check first.
func ValidApplicationDescriptor(v wire.ApplicationDescriptor) error {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > MaxApplicationDescriptorBytes {
		return errors.New("the application manifest is larger than 8192 encoded bytes")
	}
	if !applicationName.MatchString(v.Name) {
		return errors.New("the application name must be 1 to 64 lowercase letters, digits, '-' or '_'")
	}
	if !filepath.IsAbs(v.Program) || filepath.Clean(v.Program) != v.Program || len(v.Program) > 4096 || strings.ContainsRune(v.Program, 0) {
		return errors.New("the program must be a clean absolute path")
	}
	if len(v.Title) == 0 || len(v.Title) > 256 || len(v.StartGuidance) > 4096 {
		return errors.New("the title must be 1 to 256 bytes and the start guidance at most 4096")
	}
	if v.Activation == nil {
		return nil
	}
	r := v.Activation
	if len(r.Arguments) > 64 || r.ReadinessTimeoutMs < 100 || r.ReadinessTimeoutMs > 30000 {
		return errors.New("the activation recipe needs at most 64 arguments and a 100 to 30000 ms readiness budget")
	}
	if !applicationName.MatchString(r.Readiness.Name) || len(r.Readiness.Protocol) == 0 || len(r.Readiness.Protocol) > 128 || len(r.Readiness.Contract) > 256 {
		return errors.New("the readiness interface is invalid")
	}
	for _, argument := range r.Arguments {
		if len(argument) == 0 || len(argument) > 4096 || strings.ContainsRune(argument, 0) {
			return errors.New("each activation argument must be 1 to 4096 bytes without NUL")
		}
	}
	return nil
}

// descriptorKeys is ApplicationDescriptor's wire form: each object's field
// names and, for a nested object, its own fields.
var descriptorKeys = keySchema{fields: map[string]*keySchema{
	"name": nil, "program": nil, "title": nil, "start_guidance": nil,
	"activation": {fields: map[string]*keySchema{
		"arguments": nil, "readiness_timeout_ms": nil,
		"readiness": {fields: map[string]*keySchema{"name": nil, "protocol": nil, "contract": nil}},
	}},
}}

type keySchema struct{ fields map[string]*keySchema }

type descriptorFile struct {
	Name          string        `json:"name"`
	Program       string        `json:"program"`
	Title         string        `json:"title"`
	StartGuidance string        `json:"start_guidance"`
	Activation    *activationIn `json:"activation"`
}

type activationIn struct {
	Arguments          []string    `json:"arguments"`
	Readiness          interfaceIn `json:"readiness"`
	ReadinessTimeoutMs int64       `json:"readiness_timeout_ms"`
}

type interfaceIn struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Contract string `json:"contract"`
}

// ParseApplicationDescriptor reads one application manifest in the Facade's
// wire form (facade.thrift: name, program, title, start_guidance and the
// optional activation recipe). Unknown, duplicate or missing required fields,
// trailing data and a manifest outside ValidApplicationDescriptor are refused.
func ParseApplicationDescriptor(data []byte) (wire.ApplicationDescriptor, error) {
	return ParseApplicationDescriptorResolving(data, nil)
}

// ParseApplicationDescriptorResolving is ParseApplicationDescriptor for a
// manifest file whose program and activation arguments may name folders
// known only on the installing machine. resolve, when not nil, receives the
// program (program true) and then each argument, and returns the value the
// descriptor carries or refuses the manifest; the result is checked as
// ParseApplicationDescriptor checks it. The wire form is unchanged: a
// resolved descriptor holds literal paths.
func ParseApplicationDescriptorResolving(data []byte, resolve func(value string, program bool) (string, error)) (wire.ApplicationDescriptor, error) {
	if len(data) > MaxApplicationDescriptorBytes {
		return wire.ApplicationDescriptor{}, errors.New("the application manifest is larger than 8192 bytes")
	}
	keys := json.NewDecoder(bytes.NewReader(data))
	if err := checkKeys(keys, &descriptorKeys); err != nil {
		return wire.ApplicationDescriptor{}, fmt.Errorf("the application manifest is malformed: %w", err)
	}
	if _, err := keys.Token(); !errors.Is(err, io.EOF) {
		return wire.ApplicationDescriptor{}, errors.New("the application manifest has data after its object")
	}
	var in descriptorFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		return wire.ApplicationDescriptor{}, fmt.Errorf("the application manifest is malformed: %w", err)
	}
	out := wire.ApplicationDescriptor{Name: in.Name, Program: in.Program, Title: in.Title, StartGuidance: in.StartGuidance}
	if in.Activation != nil {
		if in.Activation.Arguments == nil {
			return wire.ApplicationDescriptor{}, errors.New("the activation recipe needs its arguments list")
		}
		out.Activation = &wire.ApplicationActivationRecipe{Arguments: in.Activation.Arguments,
			Readiness:          wire.ApplicationInterface{Name: in.Activation.Readiness.Name, Protocol: in.Activation.Readiness.Protocol, Contract: in.Activation.Readiness.Contract},
			ReadinessTimeoutMs: in.Activation.ReadinessTimeoutMs}
	}
	if resolve != nil {
		var err error
		if out.Program, err = resolve(out.Program, true); err != nil {
			return wire.ApplicationDescriptor{}, fmt.Errorf("the program: %w", err)
		}
		if out.Activation != nil {
			arguments := make([]string, len(out.Activation.Arguments))
			for i, argument := range out.Activation.Arguments {
				if arguments[i], err = resolve(argument, false); err != nil {
					return wire.ApplicationDescriptor{}, fmt.Errorf("activation argument %d: %w", i+1, err)
				}
			}
			out.Activation.Arguments = arguments
		}
	}
	if err := ValidApplicationDescriptor(out); err != nil {
		return wire.ApplicationDescriptor{}, err
	}
	if err := plainManifestText(out); err != nil {
		return wire.ApplicationDescriptor{}, err
	}
	return out, nil
}

// plainManifestText refuses a manifest whose shown text carries a control,
// format or separator character (Unicode Cc, Cf, Zl or Zp: bidirectional
// overrides such as U+202E, zero-width characters, NUL, U+2028 and U+2029)
// or invalid UTF-8. A person
// confirms the title, program and recipe as written; such a character could
// make them read as something else. The start guidance may hold line breaks
// and tabs.
type manifestText struct {
	name, value string
	breaks      bool // line breaks and tabs are allowed
}

func plainManifestText(d wire.ApplicationDescriptor) error {
	fields := []manifestText{{"title", d.Title, false}, {"program", d.Program, false}, {"start guidance", d.StartGuidance, true}}
	if d.Activation != nil {
		for _, argument := range d.Activation.Arguments {
			fields = append(fields, manifestText{"activation argument", argument, false})
		}
		fields = append(fields, manifestText{"readiness protocol", d.Activation.Readiness.Protocol, false},
			manifestText{"readiness contract", d.Activation.Readiness.Contract, false})
	}
	for _, field := range fields {
		if !utf8.ValidString(field.value) {
			return fmt.Errorf("the %s is not valid UTF-8", field.name)
		}
		for _, r := range field.value {
			if field.breaks && (r == '\n' || r == '\t') {
				continue
			}
			if r == utf8.RuneError || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
				return fmt.Errorf("the %s holds the control or format character U+%04X", field.name, r)
			}
		}
	}
	return nil
}

// checkKeys walks one JSON value and refuses an object key the schema does
// not name, a key given twice, or a missing required key. Every field of the
// wire form is required except start_guidance and activation.
func checkKeys(dec *json.Decoder, schema *keySchema) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		if schema == nil {
			return errors.New("an object where a value belongs")
		}
		seen := map[string]bool{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, _ := keyToken.(string)
			child, known := schema.fields[key]
			if !known || seen[key] {
				return fmt.Errorf("unknown or duplicate field %q", key)
			}
			seen[key] = true
			if err := checkKeys(dec, child); err != nil {
				return err
			}
		}
		for key := range schema.fields {
			if !seen[key] && key != "start_guidance" && key != "activation" {
				return fmt.Errorf("missing field %q", key)
			}
		}
		_, err = dec.Token()
		return err
	case json.Delim('['):
		for dec.More() {
			if err := checkKeys(dec, nil); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	default:
		if schema != nil {
			return errors.New("a value where an object belongs")
		}
		return nil
	}
}
