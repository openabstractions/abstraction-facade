package client

import (
	"path/filepath"
	"strings"
	"testing"
)

func manifestProgram() string {
	return filepath.Join(filepath.VolumeName(`C:\`)+string(filepath.Separator), "apps", "example", "example.exe")
}

func manifestJSON(extra string) string {
	program := strings.ReplaceAll(manifestProgram(), `\`, `\\`)
	return `{"name":"example","program":"` + program + `","title":"Example","start_guidance":"Starts the example."` + extra + `}`
}

func TestParseApplicationDescriptorReadsTheWireForm(t *testing.T) {
	got, err := ParseApplicationDescriptor([]byte(manifestJSON(`,"activation":{"arguments":["--serve"],"readiness":{"name":"ready","protocol":"example","contract":"example/ready@1"},"readiness_timeout_ms":30000}`)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "example" || got.Program != manifestProgram() || got.Title != "Example" || got.StartGuidance != "Starts the example." ||
		got.Activation == nil || got.Activation.Arguments[0] != "--serve" || got.Activation.Readiness.Contract != "example/ready@1" || got.Activation.ReadinessTimeoutMs != 30000 {
		t.Fatalf("parsed %+v", got)
	}
	if _, err := ParseApplicationDescriptor([]byte(manifestJSON(""))); err != nil {
		t.Fatalf("a manifest without an activation recipe: %v", err)
	}
	guidance := strings.Replace(manifestJSON(""), `Starts the example.`, `Starts the example.\n\tThen it waits — «quietly».`, 1)
	if _, err := ParseApplicationDescriptor([]byte(guidance)); err != nil {
		t.Fatalf("start guidance with line breaks, a tab and ordinary punctuation: %v", err)
	}
}

func TestParseApplicationDescriptorRefusesMalformedManifests(t *testing.T) {
	for name, text := range map[string]string{
		"unknown field":                       manifestJSON(`,"icon":"x"`),
		"unknown nested field":                manifestJSON(`,"activation":{"arguments":[],"readiness":{"name":"r","protocol":"p","contract":"c","port":1},"readiness_timeout_ms":100}`),
		"duplicate field":                     manifestJSON(`,"title":"Again"`),
		"missing program":                     `{"name":"example","title":"Example"}`,
		"null activation":                     manifestJSON(`,"activation":null`),
		"trailing data":                       manifestJSON("") + `{}`,
		"relative program":                    `{"name":"example","program":"example.exe","title":"Example"}`,
		"upper-case name":                     strings.Replace(manifestJSON(""), `"example"`, `"Example"`, 1),
		"empty title":                         strings.Replace(manifestJSON(""), `"title":"Example"`, `"title":""`, 1),
		"short readiness":                     manifestJSON(`,"activation":{"arguments":[],"readiness":{"name":"r","protocol":"p","contract":"c"},"readiness_timeout_ms":99}`),
		"empty argument":                      manifestJSON(`,"activation":{"arguments":[""],"readiness":{"name":"r","protocol":"p","contract":"c"},"readiness_timeout_ms":100}`),
		"not an object":                       `[]`,
		"oversized":                           manifestJSON(`,"x":"` + strings.Repeat("a", MaxApplicationDescriptorBytes) + `"`),
		"wrong type for a name":               `{"name":1,"program":"C:\\x.exe","title":"t"}`,
		"right-to-left override in the title": strings.Replace(manifestJSON(""), `"title":"Example"`, `"title":"Example\u202Eexe.txt"`, 1),
		"zero-width space in the title":       strings.Replace(manifestJSON(""), `"title":"Example"`, `"title":"Exa\u200Bmple"`, 1),
		"left-to-right mark in the program":   strings.Replace(manifestJSON(""), `example.exe`, `exa\u200Emple.exe`, 1),
		"line break in the title":             strings.Replace(manifestJSON(""), `"title":"Example"`, `"title":"Example\nTrusted"`, 1),
		"bidi isolate in an argument":         manifestJSON(`,"activation":{"arguments":["--a\u2066b"],"readiness":{"name":"r","protocol":"p","contract":"c"},"readiness_timeout_ms":100}`),
		"escape in an argument":               manifestJSON(`,"activation":{"arguments":["\u001b[2J"],"readiness":{"name":"r","protocol":"p","contract":"c"},"readiness_timeout_ms":100}`),
		"byte order mark in the guidance":     strings.Replace(manifestJSON(""), `Starts the example.`, `Starts\uFEFF the example.`, 1),
		"line separator in the title":         strings.Replace(manifestJSON(""), `"title":"Example"`, `"title":"Example\u2028Trusted"`, 1),
		"paragraph separator in the guidance": strings.Replace(manifestJSON(""), `Starts the example.`, `Starts\u2029the example.`, 1),
	} {
		if _, err := ParseApplicationDescriptor([]byte(text)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
