package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	wire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

func repository(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "scripts", "split.manifest")); os.IsNotExist(err) {
		t.Skip("private monorepo source integration; standalone Facade checkout has no source layout")
	} else if err != nil {
		t.Fatal(err)
	}
	return root
}

// Portable rendering/codec/freshness tests use declaration fixtures. Actual
// authored-source coverage remains the separate integration test and --check.
func fixtureRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	definitions, bundles, err := compiled()
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range append([]string{"serve"}, ownerGoPaths()...) {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range owners {
		path := filepath.Join(root, "openabstractions-flat", "abstraction-"+owner, "README.md")
		if err := os.WriteFile(path, []byte("# "+owner+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	actions, services := map[string]bool{}, map[string]map[string]bool{}
	service := func(name string) {
		if services[name] == nil {
			services[name] = map[string]bool{}
		}
	}
	for _, d := range definitions {
		actions[d.Definition.Action] = true
		service(d.Definition.OwnerContract)
		for _, name := range d.Definition.EnforcedBy {
			service(name)
		}
		for _, operation := range d.Definition.Operations {
			name, method, ok := strings.Cut(operation, "#")
			if !ok {
				t.Fatal("invalid compiled operation", operation)
			}
			service(name)
			services[name][method] = true
		}
	}
	for _, b := range bundles {
		service(b.OwnerContract)
	}
	var declaration strings.Builder
	var words []string
	for action := range actions {
		words = append(words, strconv.Quote(action))
	}
	slices.Sort(words)
	fmt.Fprintf(&declaration, "const list<string> resource_actions = [%s]\n", strings.Join(words, ","))
	var names []string
	for name := range services {
		names = append(names, name)
	}
	slices.Sort(names)
	for i, name := range names {
		fmt.Fprintf(&declaration, "service Fixture%d {\n", i)
		var methods []string
		for method := range services[name] {
			methods = append(methods, method)
		}
		slices.Sort(methods)
		for _, method := range methods {
			fmt.Fprintf(&declaration, "string %s()\n", method)
		}
		fmt.Fprintf(&declaration, "}(wire_name=%s)\n", strconv.Quote(name))
	}
	path := filepath.Join(root, "openabstractions-flat", "abstraction-facade", "fixture.thrift")
	if err := os.WriteFile(path, []byte(declaration.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestProjectionPreservesFullKeysCodecsAndFiniteBundle(t *testing.T) {
	files, err := generate(fixtureRepository(t))
	if err != nil {
		t.Fatal(err)
	}
	var c catalogue
	if err := json.Unmarshal(files[filepath.Join("openabstractions-flat", "abstraction-credentials", "RIGHTS.json")], &c); err != nil {
		t.Fatal(err)
	}
	if c.Format != format || !strings.Contains(c.Availability, "Connected-runtime") || !strings.Contains(c.Generated, "Do not edit") || !c.Receivers.Credentials || !c.Receivers.ApplicationLogs {
		t.Fatal("projection omitted its format/source/availability qualification")
	}
	apply := map[string]bool{}
	for _, raw := range c.Definitions {
		d, err := wire.DecodeSelectorPermissionDefinition(raw)
		if err != nil {
			t.Fatal(err)
		}
		if d.Definition.Action == "abstraction.credentials/apply" {
			apply[d.Definition.ResourceProfile] = true
		}
	}
	if len(apply) != 2 || !apply["abstraction.credentials/named-use@1"] || !apply["abstraction.credentials/named-use@2"] {
		t.Fatal("projection reduced action/profile pairs to an action name")
	}
	if len(c.Bundles) != 1 {
		t.Fatal("missing finite credential bundle")
	}
	bundle, err := wire.DecodePermissionBundleDefinition(c.Bundles[0])
	if err != nil || len(bundle.Members) != 6 || bundle.BundleRef != "abstraction.credentials/full-control@1" || bundle.DefinitionDigest == "" {
		t.Fatalf("finite bundle changed: %+v, %v", bundle, err)
	}
	for _, owner := range []string{"cas", "identity", "watch", "download", "rights"} {
		var empty catalogue
		base := filepath.Join("openabstractions-flat", "abstraction-"+owner)
		if err := json.Unmarshal(files[filepath.Join(base, "RIGHTS.json")], &empty); err != nil {
			t.Fatal(err)
		}
		if len(empty.Definitions) != 0 || len(empty.Bundles) != 0 || empty.Coverage == "" || !bytes.Contains(files[filepath.Join(base, "README.md")], []byte("[RIGHTS.json]")) {
			t.Fatalf("%s projection hides its coverage or invents actions", owner)
		}
	}
}

func TestTypedReadFirstUseRendersItsOfferedSelectors(t *testing.T) {
	definitions, _, err := compiled()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range definitions {
		if d.Definition.Action == "abstraction.credentials/secret.read" {
			if d.Definition.FirstUse != wire.PermissionFirstUseNone || firstUseTargets(d) != "One, All" {
				t.Fatal("typed first-use support was reduced to the legacy projection")
			}
			text := render("credentials", []wire.SelectorPermissionDefinition{d}, nil)
			if !strings.Contains(text, "| One, All |") || strings.Contains(text, "| `none` |") {
				t.Fatal("README says none despite the offered One/All selectors")
			}
			return
		}
	}
	t.Fatal("typed Read definition missing")
}

func TestCurrentSourceCoverageExposesRemovedDefinitionsAndUnknownMethods(t *testing.T) {
	definitions, bundles, err := compiled()
	if err != nil {
		t.Fatal(err)
	}
	root := repository(t)
	if err := validateSources(root, definitions, bundles); err != nil {
		t.Fatal(err)
	}
	missing := slices.DeleteFunc(slices.Clone(definitions), func(d wire.SelectorPermissionDefinition) bool {
		return d.Definition.Action == "abstraction.facade/application.activate"
	})
	if err := validateSources(root, missing, bundles); err == nil || !strings.Contains(err.Error(), "lacks a compiled definition") {
		t.Fatalf("source declaration removal stayed green: %v", err)
	}
	bad := slices.Clone(definitions)
	bad[0].Definition.Operations = []string{bad[0].Definition.OwnerContract + "#NoSuchMethod"}
	if err := validateSources(root, bad, bundles); err == nil || !strings.Contains(err.Error(), "undeclared operation") {
		t.Fatalf("invalid owning operation stayed green: %v", err)
	}
	badBundle := slices.Clone(bundles)
	badBundle[0].Members = []wire.PermissionKey{{Action: "abstraction.credentials/apply", ResourceProfile: "abstraction.credentials/unknown@1"}}
	if err := validateSources(root, definitions, badBundle); err == nil || !strings.Contains(err.Error(), "absent action/profile") {
		t.Fatalf("invalid finite bundle key stayed green: %v", err)
	}
}

func TestFreshnessDetectsProfileAndREADMEChanges(t *testing.T) {
	files, err := generate(fixtureRepository(t))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, name := range []string{"RIGHTS.json", "README.md"} {
		path := filepath.Join("openabstractions-flat", "abstraction-credentials", name)
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), files[path], 0644); err != nil {
			t.Fatal(err)
		}
		if err := fresh(root, path, files[path]); err != nil {
			t.Fatal(err)
		}
		changed := bytes.Replace(files[path], []byte("named-use@2"), []byte("named-use@9"), 1)
		if bytes.Equal(changed, files[path]) {
			t.Fatal("mutation did not reach the generated profile")
		}
		if err := os.WriteFile(filepath.Join(root, path), changed, 0644); err != nil {
			t.Fatal(err)
		}
		if err := fresh(root, path, files[path]); err == nil {
			t.Fatalf("%s profile/documentation drift stayed green", name)
		}
	}
}

func TestCoverageScanRefusesUnsupportedLayouts(t *testing.T) {
	for _, text := range []string{
		"const list<string> resource_actions = [\n\"abstraction.test/read\"]\n",
		"const   list<string>   resource_actions = [\n\"abstraction.test/read\"]\n",
		"service Example {\nstring Read(\n1:string x)\n}(wire_name=\"abstraction.test/reader@1\")\n",
		"service Example {\nstring Read()\n}\n",
	} {
		if err := declarations("test.thrift", []byte(text), map[string]bool{}, map[string]bool{}, map[string]bool{}); err == nil {
			t.Fatal("unsupported declaration layout escaped the bounded source-reference scan")
		}
	}
	section := render("rights", nil, nil)
	text, err := replaceSection("# Rights\r\n", section)
	if err != nil {
		t.Fatal(err)
	}
	second, err := replaceSection(text, section)
	if err != nil || second != text {
		t.Fatal("generated README section is not idempotent")
	}
	if _, err := replaceSection(text+end, section); err == nil {
		t.Fatal("duplicate README marker accepted")
	}
}

func TestHelpAndNoArgumentsDoNotWrite(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}} {
		var out, errors bytes.Buffer
		if code := run(args, &out, &errors); code != 0 || out.Len() == 0 || errors.Len() != 0 {
			t.Fatalf("help outcome %d, %s, %s", code, &out, &errors)
		}
	}
}

func TestExplicitCheckRequiresItsSourceRoot(t *testing.T) {
	var out, errors bytes.Buffer
	if code := run([]string{"--root", t.TempDir(), "--check"}, &out, &errors); code != 1 || !strings.Contains(errors.String(), "no owning Thrift definitions") {
		t.Fatalf("malformed explicit source root was skipped: %d, %s", code, &errors)
	}
}
