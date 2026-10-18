// Command rights-catalogue projects the existing compiled permission catalogue
// into per-layer reference files. Generated files are never authority inputs.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/openabstractions/abstraction-facade/go/grants"
	wire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

const format = "openabstractions-rights@1"
const begin = "<!-- BEGIN GENERATED PERMISSION DEFINITIONS -->"
const end = "<!-- END GENERATED PERMISSION DEFINITIONS -->"

var owners = []string{"asks", "cas", "config", "credentials", "download", "facade", "identity", "inference", "job", "logging", "model", "resource", "rights", "router", "storage", "watch"}

var emptyCoverage = map[string]string{
	"cas":      "This projection has no named action definition for CAS (compare-and-set). Atomic shared-state updates and the composing service's admission remain with their existing owners.",
	"download": "This projection has no named action definition for Download. Delegated jobs and caller/service ownership retain their existing admission; job, credential and storage actions are listed under their defining layers.",
	"identity": "This projection has no named action definition for Identity. Receiving peer proof and program comparison retain their existing contract.",
	"rights":   "This projection lists no action owned by Rights. Its generic seed names actions defined by other layers. Authorization, operator designation and legacy awake admission retain their existing contracts.",
	"watch":    "This projection has no named action definition for Watch. Debounce and settle snapshots remain provider utilities; cursor/gap observation and admission belong to each capability service.",
}

type catalogue struct {
	Format       string            `json:"format"`
	Owner        string            `json:"owner"`
	Generated    string            `json:"generated"`
	Source       string            `json:"source"`
	Availability string            `json:"availability"`
	Receivers    receiverContext   `json:"compiled_receivers"`
	Coverage     string            `json:"coverage,omitempty"`
	Definitions  []json.RawMessage `json:"definitions"`
	Bundles      []json.RawMessage `json:"bundles"`
}

type receiverContext struct {
	Credentials     bool `json:"credentials"`
	ApplicationLogs bool `json:"application_logs"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("rights-catalogue", flag.ContinueOnError)
	fs.SetOutput(errOut)
	root := fs.String("root", "", "monorepo root containing authored Thrift, Go sources and layer READMEs")
	write := fs.Bool("write", false, "write deterministic RIGHTS.json files and marked README sections")
	check := fs.Bool("check", false, "check current source coverage and generated-file freshness")
	fs.Usage = func() {
		fmt.Fprintln(out, "rights-catalogue --root <repository> --write|--check")
		fmt.Fprintln(out, "Project existing compiled permission definitions and finite bundles. No runtime or policy is contacted.")
		fmt.Fprintln(out, "--check also checks current action declarations and owning IDL method references.")
	}
	if len(args) == 0 {
		fs.Usage()
		return 0
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *root == "" || *write == *check || fs.NArg() != 0 {
		fmt.Fprintln(errOut, "choose --root and exactly one of --write or --check")
		return 2
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	files, err := generate(absolute)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		if *write {
			if err = os.WriteFile(filepath.Join(absolute, path), files[path], 0644); err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
		} else if err = fresh(absolute, path, files[path]); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
	}
	fmt.Fprintf(out, "permission catalogue: %d layer projections and README sections %s\n", len(owners), map[bool]string{true: "written", false: "current"}[*write])
	return 0
}

func compiled() ([]wire.SelectorPermissionDefinition, []wire.PermissionBundleDefinition, error) {
	// This is the existing known-receiver catalogue, rather than any installed
	// instance. Runtime composition independently supplies its actual flags.
	c, err := grants.SelectorPermissionDefinitions(grants.SelectorServices{Credentials: true, ApplicationLogs: true})
	if err != nil {
		return nil, nil, err
	}
	return c.SelectorDefinitions(), c.PermissionBundles(), nil
}

func generate(root string) (map[string][]byte, error) {
	definitions, bundles, err := compiled()
	if err != nil {
		return nil, err
	}
	if err = validateSources(root, definitions, bundles); err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, owner := range owners {
		layer := "abstraction-" + owner
		c := catalogue{Format: format, Owner: "abstraction." + owner, Generated: "Do not edit. Run rights-catalogue --write from the existing compiled catalogue.", Source: "github.com/openabstractions/abstraction-facade/go/grants", Availability: "Compiled catalogue reference: served and selector support use the named receiving-service context. Connected-runtime discovery supplies current availability. Facade Applications is experimental. Definitions grant no access.", Receivers: receiverContext{Credentials: true, ApplicationLogs: true}, Coverage: emptyCoverage[owner], Definitions: []json.RawMessage{}, Bundles: []json.RawMessage{}}
		var ownDefs []wire.SelectorPermissionDefinition
		var ownBundles []wire.PermissionBundleDefinition
		for _, d := range definitions {
			if strings.HasPrefix(d.Definition.Action, c.Owner+"/") {
				// Named codecs enforce the common owning Thrift record shape and
				// preserve wire field/enum names for every language consumer.
				raw := wire.EncodeSelectorPermissionDefinition(&d)
				if _, err := wire.DecodeSelectorPermissionDefinition(raw); err != nil {
					return nil, err
				}
				c.Definitions = append(c.Definitions, json.RawMessage(raw))
				ownDefs = append(ownDefs, d)
			}
		}
		for _, b := range bundles {
			if strings.HasPrefix(b.BundleRef, c.Owner+"/") {
				raw := wire.EncodePermissionBundleDefinition(&b)
				if _, err := wire.DecodePermissionBundleDefinition(raw); err != nil {
					return nil, err
				}
				c.Bundles = append(c.Bundles, json.RawMessage(raw))
				ownBundles = append(ownBundles, b)
			}
		}
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return nil, err
		}
		base := filepath.Join("openabstractions-flat", layer)
		files[filepath.Join(base, "RIGHTS.json")] = append(data, '\n')
		readmePath := filepath.Join(base, "README.md")
		old, err := os.ReadFile(filepath.Join(root, readmePath))
		if err != nil {
			return nil, err
		}
		text, err := replaceSection(string(old), render(owner, ownDefs, ownBundles))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", readmePath, err)
		}
		files[readmePath] = []byte(text)
	}
	return files, nil
}

func fresh(root, path string, want []byte) error {
	got, err := os.ReadFile(filepath.Join(root, path))
	if err != nil || !bytes.Equal([]byte(strings.ReplaceAll(string(got), "\r\n", "\n")), want) {
		return fmt.Errorf("%s: generated permission reference is stale; run rights-catalogue --write", path)
	}
	return nil
}

func replaceSection(text, section string) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.Count(text, begin) == 0 && strings.Count(text, end) == 0 {
		return strings.TrimRight(text, "\n") + "\n\n" + section, nil
	}
	if strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 {
		return "", fmt.Errorf("permission section markers must form one complete pair")
	}
	a, z := strings.Index(text, begin), strings.Index(text, end)
	if z < a {
		return "", fmt.Errorf("permission section markers are reversed")
	}
	z += len(end)
	if z < len(text) && text[z] == '\n' {
		z++
	}
	return text[:a] + section + text[z:], nil
}

func cell(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ") }
func code(s string) string { return "`" + cell(s) + "`" }

func firstUseTargets(d wire.SelectorPermissionDefinition) string {
	if len(d.FirstUseSelectors) != 0 {
		kinds := make([]string, len(d.FirstUseSelectors))
		for i, kind := range d.FirstUseSelectors {
			kinds[i] = map[wire.ResourceSelectorKind]string{wire.ResourceSelectorKindOne: "One", wire.ResourceSelectorKindAll: "All"}[kind]
		}
		return strings.Join(kinds, ", ")
	}
	if d.Definition.FirstUse != wire.PermissionFirstUseNone {
		return "Legacy " + code(d.Definition.FirstUse.String())
	}
	return code(d.Definition.FirstUse.String())
}

func render(owner string, definitions []wire.SelectorPermissionDefinition, bundles []wire.PermissionBundleDefinition) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n## Permission definitions\n\n", begin)
	b.WriteString("Generated from the existing compiled catalogue. [RIGHTS.json](RIGHTS.json) contains the complete action/profile records, disclosure, lifecycle, first-use behavior, selector support and finite bundles.\n\n")
	if len(definitions) == 0 {
		b.WriteString(emptyCoverage[owner] + "\n\n")
	} else {
		b.WriteString("This reference records compiled receiving support; RIGHTS.json names the compilation context. A connected runtime advertises current availability and selector support. These definitions supply no grant.\n\n")
		if owner == "facade" {
			b.WriteString("The Applications directory and its receiving operations are experimental.\n\n")
		}
		b.WriteString("| Action | Resource profile | Owning operations | First-use targets | Compiled receiver |\n|---|---|---|---|---|\n")
		for _, d := range definitions {
			ops := make([]string, len(d.Definition.Operations))
			for i, op := range d.Definition.Operations {
				ops[i] = code(op)
			}
			status := "defined, unserved"
			if d.Definition.Served {
				status = "served in compiled context"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", code(d.Definition.Action), code(d.Definition.ResourceProfile), strings.Join(ops, ", "), firstUseTargets(d), status)
		}
		b.WriteByte('\n')
	}
	for _, bundle := range bundles {
		fmt.Fprintf(&b, "The finite %s bundle contains these exact action/profile pairs:\n\n", code(bundle.BundleRef))
		for _, member := range bundle.Members {
			fmt.Fprintf(&b, "- %s on %s\n", code(member.Action), code(member.ResourceProfile))
		}
		fmt.Fprintf(&b, "\n%s\n\n", bundle.Disclosure)
	}
	fmt.Fprintf(&b, "%s\n", end)
	return b.String()
}

// This bounded declaration scan reuses the reviewed inventory extractor's
// service/method shape. It checks references, not Thrift grammar or semantics.
// The IDL compiler owns those. Unsupported layouts fail explicitly here.
var serviceStart = regexp.MustCompile(`^\s*service\s+(\w+)\s*\{\s*$`)
var wireName = regexp.MustCompile(`wire_name\s*=\s*"([^"]+)"`)
var methodStart = regexp.MustCompile(`^\s*[\w.<>, ]+\s+(\w+)\s*\(`)
var actionStart = regexp.MustCompile(`^\s*const\s+list<string>\s+(resource_actions|registry_actions)\b`)
var actionArray = regexp.MustCompile(`^\s*const\s+list<string>\s+(resource_actions|registry_actions)\s*=\s*\[([^]]*)\]`)
var quoted = regexp.MustCompile(`"([^"\\]*)"`)

func declarations(path string, data []byte, actions, services, methods map[string]bool) error {
	var pending []string
	service := ""
	for index, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if service == "" {
			if a := actionArray.FindStringSubmatch(line); a != nil {
				for _, word := range quoted.FindAllStringSubmatch(a[2], -1) {
					actions[word[1]] = true
				}
			} else if actionStart.MatchString(line) {
				return fmt.Errorf("%s:%d: action coverage scan requires the current single-line string-list declaration", path, index+1)
			}
			if s := serviceStart.FindStringSubmatch(line); s != nil {
				service, pending = s[1], nil
			} else if strings.HasPrefix(trim, "service ") {
				return fmt.Errorf("%s:%d: unsupported service declaration layout in coverage scan", path, index+1)
			}
			continue
		}
		if trim == "" || strings.HasPrefix(trim, "//") {
			continue
		}
		if strings.HasPrefix(trim, "}") {
			name := wireName.FindStringSubmatch(line)
			if name == nil {
				return fmt.Errorf("%s:%d: service %s has no single-line wire_name", path, index+1, service)
			}
			services[name[1]] = true
			for _, method := range pending {
				methods[name[1]+"#"+method] = true
			}
			service = ""
			continue
		}
		if m := methodStart.FindStringSubmatch(line); m != nil && strings.Contains(line, ")") {
			pending = append(pending, m[1])
		} else {
			return fmt.Errorf("%s:%d: unsupported service method layout in coverage scan", path, index+1)
		}
	}
	if service != "" {
		return fmt.Errorf("%s: unfinished service %s in coverage scan", path, service)
	}
	return nil
}

func validateSources(root string, definitions []wire.SelectorPermissionDefinition, bundles []wire.PermissionBundleDefinition) error {
	actions, services, methods := map[string]bool{}, map[string]bool{}, map[string]bool{}
	paths, err := filepath.Glob(filepath.Join(root, "openabstractions-flat", "*", "*.thrift"))
	if err != nil || len(paths) == 0 {
		return fmt.Errorf("no owning Thrift definitions under %s", root)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err = declarations(path, data, actions, services, methods); err != nil {
			return err
		}
	}
	// Existing Action.../...Action constants supply source declarations for
	// composed actions (notably Facade Applications and Resource leases).
	for _, dir := range append([]string{"serve"}, ownerGoPaths()...) {
		err = filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			for _, decl := range file.Decls {
				group, ok := decl.(*ast.GenDecl)
				if !ok || group.Tok != token.CONST {
					continue
				}
				for _, spec := range group.Specs {
					value := spec.(*ast.ValueSpec)
					for i, name := range value.Names {
						if i >= len(value.Values) || !(strings.HasPrefix(name.Name, "Action") || strings.HasSuffix(name.Name, "Action")) {
							continue
						}
						literal, ok := value.Values[i].(*ast.BasicLit)
						if !ok || literal.Kind != token.STRING {
							continue
						}
						word, err := strconv.Unquote(literal.Value)
						if err == nil && strings.HasPrefix(word, "abstraction.") && strings.Contains(word, "/") {
							actions[word] = true
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return validateReferences(definitions, bundles, actions, services, methods)
}

func ownerGoPaths() []string {
	paths := make([]string, 0, len(owners))
	for _, owner := range owners {
		path := filepath.Join("openabstractions-flat", "abstraction-"+owner)
		if owner != "identity" {
			path = filepath.Join(path, "go")
		}
		paths = append(paths, path)
	}
	return paths
}

func validateReferences(definitions []wire.SelectorPermissionDefinition, bundles []wire.PermissionBundleDefinition, actions, services, methods map[string]bool) error {
	covered, keys := map[string]bool{}, map[wire.PermissionKey]bool{}
	for _, d := range definitions {
		key := wire.PermissionKey{Action: d.Definition.Action, ResourceProfile: d.Definition.ResourceProfile}
		namespace, _, _ := strings.Cut(key.Action, "/")
		owner := strings.TrimPrefix(namespace, "abstraction.")
		if !slices.Contains(owners, owner) {
			return fmt.Errorf("catalogue action %s has no selected layer projection", key.Action)
		}
		if keys[key] {
			return fmt.Errorf("duplicate action/profile definition %v", key)
		}
		keys[key], covered[key.Action] = true, true
		if !actions[key.Action] {
			return fmt.Errorf("catalogue action %s lacks a current owning declaration", key.Action)
		}
		if !services[d.Definition.OwnerContract] {
			return fmt.Errorf("definition %s has undeclared owning service %s", key.Action, d.Definition.OwnerContract)
		}
		for _, op := range d.Definition.Operations {
			if !methods[op] {
				return fmt.Errorf("definition %s references undeclared operation %s", key.Action, op)
			}
		}
		for _, enforcer := range d.Definition.EnforcedBy {
			if !services[enforcer] {
				return fmt.Errorf("definition %s references undeclared enforcer %s", key.Action, enforcer)
			}
		}
	}
	for action := range actions {
		if !covered[action] {
			return fmt.Errorf("declared action %s lacks a compiled definition", action)
		}
	}
	for _, bundle := range bundles {
		if !services[bundle.OwnerContract] {
			return fmt.Errorf("bundle %s references undeclared owner", bundle.BundleRef)
		}
		for _, member := range bundle.Members {
			if !keys[member] {
				return fmt.Errorf("bundle %s references absent action/profile %v", bundle.BundleRef, member)
			}
		}
	}
	return nil
}
