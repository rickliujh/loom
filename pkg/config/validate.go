package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	ttparse "text/template/parse"
	"time"

	"github.com/rickliujh/loom/internal/util"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

const (
	ExpectedAPIVersion = "loom.rickliujh.github.io/v1beta1"
	ExpectedKind       = "Loom"
)

// Validate checks that a LoomFile has required fields and valid structure.
// All violations are collected and returned as a single joined error so a
// config can be fixed in one pass.
func Validate(lf *LoomFile) error {
	_, err := validate(lf, "")
	return err
}

// ValidateInDir runs all Validate checks plus filesystem checks that need
// the module directory: newFiles.source must be an existing directory and
// patch.path an existing file (both resolved like at run time, skipped when
// templated). Warnings are discarded; use ValidateInDirWithWarnings to see
// them.
func ValidateInDir(lf *LoomFile, moduleDir string) error {
	_, err := validate(lf, moduleDir)
	return err
}

// ValidateInDirWithWarnings is ValidateInDir plus the warnings it collected:
// findings that do not make the config invalid — the run proceeds and does
// exactly what the config says — but that are near-certainly not what was
// meant. They are advisory, so a caller that only cares whether the config
// loads can keep ignoring them.
func ValidateInDirWithWarnings(lf *LoomFile, moduleDir string) (warnings []string, err error) {
	return validate(lf, moduleDir)
}

func validate(lf *LoomFile, moduleDir string) ([]string, error) {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}
	var warnings []string
	warn := func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}
	// paramNames is consulted by checkTmpl while still being built: dynamic
	// param templates may only reference params declared before them, while
	// everything after the param loops sees the complete set.
	paramNames := make(map[string]bool)
	// usedParams records every name any template references, and usageComplete
	// tracks whether that record is exhaustive. Together they drive the
	// declared-but-never-referenced check, which must stay silent the moment
	// any template could have referenced a param out of sight.
	usedParams := make(map[string]bool)
	// Without the module directory the rendered files cannot be read at all,
	// so an in-memory Validate never has a complete record.
	usageComplete := moduleDir != ""
	// structured holds the list and map params. Printing one bare is valid —
	// the run does exactly what the template says — but is never what was
	// meant, so it is reported as a warning.
	structured := make(map[string]ParamType)
	// checkTmplRefs reports references to undeclared params and folds the rest
	// into usedParams. Templates whose references cannot be resolved statically
	// are skipped, and mark the usage record incomplete.
	checkTmplRefs := func(field string, root *ttparse.ListNode) {
		refs, ok := templateParamRefs(root)
		if !ok {
			usageComplete = false
			return
		}
		seen := make(map[string]bool)
		for _, r := range refs {
			usedParams[r] = true
			if !paramNames[r] && !seen[r] {
				seen[r] = true
				fail("%s: references undeclared param %q", field, r)
			}
		}
	}
	// checkTmpl parses value as a Go template with the loom function map and
	// records a violation on syntax errors or references to undeclared
	// params. Values without template expressions always pass, so they are
	// skipped.
	checkTmpl := func(field, value string) {
		if !isTemplated(value) {
			return
		}
		t, err := template.New("").Funcs(tmpl.FuncMap()).Parse(value)
		if err != nil {
			fail("%s: invalid template: %v", field, err)
			return
		}
		checkTmplRefs(field, t.Tree.Root)
		for _, u := range printedStructured(t.Tree.Root, structured) {
			if u.fn == "" {
				warn("%s: prints %s param %q directly, which writes Go's formatting (like [map[k:v]]); render it with toYaml, toJson, join or range",
					field, structured[u.name], u.name)
				continue
			}
			warn("%s: passes %s param %q to %s, which takes text; render it with toYaml, toJson, join or range",
				field, structured[u.name], u.name, u.fn)
		}
	}

	if lf.APIVersion != ExpectedAPIVersion {
		fail("unsupported apiVersion %q, expected %q", lf.APIVersion, ExpectedAPIVersion)
	}
	if lf.Kind != ExpectedKind {
		fail("unsupported kind %q, expected %q", lf.Kind, ExpectedKind)
	}
	if lf.Metadata.Name == "" {
		fail("metadata.name is required")
	}

	for _, p := range lf.Spec.Params {
		if p.Name == "" {
			fail("param name cannot be empty")
			continue
		}
		if paramNames[p.Name] {
			fail("duplicate param name %q", p.Name)
		}
		paramNames[p.Name] = true
		if p.Type == ParamList || p.Type == ParamMap {
			structured[p.Name] = p.Type
		}
		if !p.Type.Valid() {
			fail("param %q: unknown type %q (supported: string, list, map)", p.Name, p.Type)
		} else if p.Default != nil {
			if shape := ShapeOf(p.Default); shape != string(p.Type.Effective()) {
				fail("param %q: default is a %s, but the param's type is %s", p.Name, shape, p.Type.Effective())
			}
		}
	}

	for _, dp := range lf.Spec.DynamicParams {
		if dp.Name == "" {
			fail("dynamicParam name cannot be empty")
			continue
		}
		if paramNames[dp.Name] {
			fail("duplicate param name %q (declared in both params and dynamicParams)", dp.Name)
		}
		if dp.Command == "" {
			fail("dynamicParam %q: command is required", dp.Name)
		}
		if !dp.Type.Valid() {
			fail("dynamicParam %q: unknown type %q (supported: string, list, map)", dp.Name, dp.Type)
		}
		// Checked before the name is registered: dynamic params are evaluated
		// in declaration order, so a command may only reference static params
		// and earlier dynamic params, never itself or later ones.
		checkTmpl(fmt.Sprintf("dynamicParam %q command", dp.Name), dp.Command)
		checkTmpl(fmt.Sprintf("dynamicParam %q default", dp.Name), dp.Default)
		paramNames[dp.Name] = true
		if dp.Type == ParamList || dp.Type == ParamMap {
			structured[dp.Name] = dp.Type
		}
	}

	for i, e := range lf.Spec.Excludes {
		field := fmt.Sprintf("spec.excludes[%d]", i)
		checkTmpl(field, e)
		checkGlob(fail, field, e)
	}
	for i, e := range lf.Spec.Includes {
		field := fmt.Sprintf("spec.includes[%d]", i)
		checkTmpl(field, e)
		checkGlob(fail, field, e)
	}

	if lf.Spec.Target != nil {
		if lf.Spec.Target.URL == "" {
			fail("spec.target.url is required")
		}
		checkTmpl("spec.target.url", lf.Spec.Target.URL)
		checkTmpl("spec.target.branch", lf.Spec.Target.Branch)
		checkTmpl("spec.target.featureBranch", lf.Spec.Target.FeatureBranch)
	}

	moduleNames := make(map[string]bool)
	for _, m := range lf.Spec.Modules {
		if m.Name == "" {
			fail("module name cannot be empty")
		} else {
			if moduleNames[m.Name] {
				fail("duplicate module name %q", m.Name)
			}
			moduleNames[m.Name] = true
		}
		if m.Source == "" {
			fail("module %q: source is required", m.Name)
		}
		checkTmpl(fmt.Sprintf("module %q name", m.Name), m.Name)
		checkTmpl(fmt.Sprintf("module %q source", m.Name), m.Source)
		checkTmpl(fmt.Sprintf("module %q if", m.Name), m.If)
		paramKeys := make([]string, 0, len(m.Params))
		for k := range m.Params {
			paramKeys = append(paramKeys, k)
		}
		sort.Strings(paramKeys)
		for _, k := range paramKeys {
			// Every string leaf of a structured value is a template (T4).
			_, _ = tmpl.MapStrings(m.Params[k], k, func(path, v string) (string, error) {
				checkTmpl(fmt.Sprintf("module %q param %q", m.Name, path), v)
				return v, nil
			})
		}
	}

	// Non-templated newFiles sources and patch paths, paired with their
	// operation names, for the cross-operation check after the loop.
	type opPath struct{ op, path string }
	var newFilesSources, patchPaths []opPath

	// Directories standing in for an asset path that is only resolved at run
	// time. They are read for param references but never for violations: which
	// of their files a run actually renders is not known here.
	var usageOnlyDirs []string

	opNames := make(map[string]bool)
	for _, op := range lf.Spec.Operations {
		if op.Name == "" {
			fail("operation name cannot be empty")
		} else {
			if opNames[op.Name] {
				fail("duplicate operation name %q", op.Name)
			}
			opNames[op.Name] = true
		}

		checkTmpl(fmt.Sprintf("operation %q if", op.Name), op.If)

		count := 0
		if op.NewFiles != nil {
			count++
		}
		if op.Patch != nil {
			count++
		}
		if op.Shell != nil {
			count++
		}
		if op.CommitPush != nil {
			count++
		}
		if op.PR != nil {
			count++
		}
		if op.LLM != nil {
			count++
		}
		if count != 1 {
			fail("operation %q must have exactly one action type, got %d", op.Name, count)
		}

		if op.NewFiles != nil {
			if op.NewFiles.Source == "" {
				fail("operation %q: newFiles source is required", op.Name)
			} else if moduleDir != "" && !isTemplated(op.NewFiles.Source) {
				src := util.ExpandPath(moduleDir, op.NewFiles.Source)
				if info, err := os.Stat(src); err != nil {
					fail("operation %q: newFiles source %q not found in module directory", op.Name, op.NewFiles.Source)
				} else if !info.IsDir() {
					fail("operation %q: newFiles source %q is not a directory", op.Name, op.NewFiles.Source)
				} else {
					newFilesSources = append(newFilesSources, opPath{op.Name, op.NewFiles.Source})
				}
			} else if moduleDir != "" {
				// Which directory this renders is only known at run time, so
				// the fixed part of the path stands in for it: every file under
				// it is read for references, and none of it is reported as a
				// violation, since these files may not be rendered at all.
				usageOnlyDirs = append(usageOnlyDirs, staticPrefixDir(op.NewFiles.Source))
			}
			checkTmpl(fmt.Sprintf("operation %q newFiles.source", op.Name), op.NewFiles.Source)
			checkTmpl(fmt.Sprintf("operation %q newFiles.dest", op.Name), op.NewFiles.Dest)
			checkTargetPath(fail, op.Name, "newFiles dest", op.NewFiles.Dest)
		}

		if op.Patch != nil {
			if op.Patch.Path == "" {
				fail("operation %q: patch path is required", op.Name)
			} else if moduleDir != "" && !isTemplated(op.Patch.Path) {
				p := util.ExpandPath(moduleDir, op.Patch.Path)
				if info, err := os.Stat(p); err != nil {
					fail("operation %q: patch file %q not found in module directory", op.Name, op.Patch.Path)
				} else if info.IsDir() {
					fail("operation %q: patch path %q is a directory, expected a file", op.Name, op.Patch.Path)
				} else {
					patchPaths = append(patchPaths, opPath{op.Name, op.Patch.Path})
				}
			} else if moduleDir != "" {
				// Same as newFiles above: the fixed part of the path stands in
				// for the file, contributing references but no violations.
				usageOnlyDirs = append(usageOnlyDirs, staticPrefixDir(op.Patch.Path))
			}
			if op.Patch.Target == "" {
				fail("operation %q: patch target is required", op.Name)
			}
			// Skip enum validation for templated values (resolved at run time).
			if op.Patch.Engine != "" && !isTemplated(op.Patch.Engine) {
				switch op.Patch.Engine {
				case "smp", "json6902":
					// valid
				default:
					fail("operation %q: unknown patch engine %q (supported: smp, json6902)", op.Name, op.Patch.Engine)
				}
			}
			if op.Patch.PreserveComments != "" && !isTemplated(op.Patch.PreserveComments) {
				switch op.Patch.PreserveComments {
				case "true", "false":
					// valid
				default:
					fail("operation %q: invalid patch preserveComments %q (supported: true, false)", op.Name, op.Patch.PreserveComments)
				}
			}
			checkTmpl(fmt.Sprintf("operation %q patch.engine", op.Name), op.Patch.Engine)
			checkTmpl(fmt.Sprintf("operation %q patch.path", op.Name), op.Patch.Path)
			checkTmpl(fmt.Sprintf("operation %q patch.target", op.Name), op.Patch.Target)
			checkTmpl(fmt.Sprintf("operation %q patch.preserveComments", op.Name), op.Patch.PreserveComments)
			checkTargetPath(fail, op.Name, "patch target", op.Patch.Target)
		}

		if op.Shell != nil {
			if op.Shell.Command == "" {
				fail("operation %q: shell command is required", op.Name)
			}
			// Skip duration validation for templated values (resolved at run time).
			if op.Shell.Timeout != "" && !isTemplated(op.Shell.Timeout) {
				if _, err := time.ParseDuration(op.Shell.Timeout); err != nil {
					fail("operation %q: invalid shell timeout %q: %v", op.Name, op.Shell.Timeout, err)
				}
			}
			checkTmpl(fmt.Sprintf("operation %q shell.command", op.Name), op.Shell.Command)
			checkTmpl(fmt.Sprintf("operation %q shell.timeout", op.Name), op.Shell.Timeout)
		}

		if op.CommitPush != nil {
			if op.CommitPush.Message == "" {
				fail("operation %q: commitPush message is required", op.Name)
			}
			checkTmpl(fmt.Sprintf("operation %q commitPush.message", op.Name), op.CommitPush.Message)
			checkTmpl(fmt.Sprintf("operation %q commitPush.author", op.Name), op.CommitPush.Author)
			checkTmpl(fmt.Sprintf("operation %q commitPush.email", op.Name), op.CommitPush.Email)
		}

		if op.PR != nil {
			if op.PR.Provider == "" {
				fail("operation %q: pr provider is required", op.Name)
			} else if !isTemplated(op.PR.Provider) {
				switch op.PR.Provider {
				case "github", "gitlab":
					// valid
				default:
					fail("operation %q: unknown pr provider %q (supported: github, gitlab)", op.Name, op.PR.Provider)
				}
			}
			if op.PR.Title == "" {
				fail("operation %q: pr title is required", op.Name)
			}
			checkTmpl(fmt.Sprintf("operation %q pr.provider", op.Name), op.PR.Provider)
			checkTmpl(fmt.Sprintf("operation %q pr.title", op.Name), op.PR.Title)
			checkTmpl(fmt.Sprintf("operation %q pr.body", op.Name), op.PR.Body)
			checkTmpl(fmt.Sprintf("operation %q pr.baseBranch", op.Name), op.PR.BaseBranch)
			checkTmpl(fmt.Sprintf("operation %q pr.tokenEnv", op.Name), op.PR.TokenEnv)
			for i, l := range op.PR.Labels {
				checkTmpl(fmt.Sprintf("operation %q pr.labels[%d]", op.Name, i), l)
			}
		}

		if op.LLM != nil {
			// Skip enum validation for templated values (resolved at run time).
			if !isTemplated(op.LLM.Provider) {
				switch op.LLM.Provider {
				case "openai", "anthropic", "vertex", "gemini", "openrouter", "bedrock":
					// valid
				default:
					fail("operation %q: unknown llm provider %q (supported: openai, anthropic, vertex, gemini, openrouter, bedrock)", op.Name, op.LLM.Provider)
				}
			}
			if op.LLM.Model == "" {
				fail("operation %q: llm model is required", op.Name)
			}
			if op.LLM.Prompt == "" {
				fail("operation %q: llm prompt is required", op.Name)
			}
			if op.LLM.Target == "" {
				fail("operation %q: llm target is required", op.Name)
			}
			if op.LLM.Mode != "" && !isTemplated(op.LLM.Mode) && op.LLM.Mode != "generate" && op.LLM.Mode != "modify" {
				fail("operation %q: unknown llm mode %q (supported: generate, modify)", op.Name, op.LLM.Mode)
			}
			if op.LLM.Retries < 0 {
				fail("operation %q: llm retries must be >= 0", op.Name)
			}
			// A negative limit is silently dropped at call time (only > 0 is
			// forwarded to the provider), so the run would quietly ignore it.
			if op.LLM.MaxTokens < 0 {
				fail("operation %q: llm maxTokens must be >= 0", op.Name)
			}
			if op.LLM.RetryDelay != "" && !isTemplated(op.LLM.RetryDelay) {
				if _, err := time.ParseDuration(op.LLM.RetryDelay); err != nil {
					fail("operation %q: invalid llm retryDelay %q: %v", op.Name, op.LLM.RetryDelay, err)
				}
			}
			if !isTemplated(op.LLM.Provider) && op.LLM.Provider == "vertex" {
				if op.LLM.ProviderConfig == nil || op.LLM.ProviderConfig.Project == "" {
					fail("operation %q: llm providerConfig.project is required for vertex provider", op.Name)
				}
			}
			checkTmpl(fmt.Sprintf("operation %q llm.provider", op.Name), op.LLM.Provider)
			checkTmpl(fmt.Sprintf("operation %q llm.model", op.Name), op.LLM.Model)
			checkTmpl(fmt.Sprintf("operation %q llm.prompt", op.Name), op.LLM.Prompt)
			checkTmpl(fmt.Sprintf("operation %q llm.systemPrompt", op.Name), op.LLM.SystemPrompt)
			checkTmpl(fmt.Sprintf("operation %q llm.target", op.Name), op.LLM.Target)
			checkTargetPath(fail, op.Name, "llm target", op.LLM.Target)
			checkTmpl(fmt.Sprintf("operation %q llm.mode", op.Name), op.LLM.Mode)
			checkTmpl(fmt.Sprintf("operation %q llm.retryDelay", op.Name), op.LLM.RetryDelay)
			if pc := op.LLM.ProviderConfig; pc != nil {
				checkTmpl(fmt.Sprintf("operation %q llm.providerConfig.tokenEnv", op.Name), pc.TokenEnv)
				checkTmpl(fmt.Sprintf("operation %q llm.providerConfig.project", op.Name), pc.Project)
				checkTmpl(fmt.Sprintf("operation %q llm.providerConfig.location", op.Name), pc.Location)
			}
		}
	}

	// The files a run renders — newFiles bodies and their path names, patch
	// bodies — are templates too (T4), and their param references are checked
	// the same way loom.yaml fields are. Nothing else catches a typo there: a
	// name that is not a declared param is not an error at run time, it renders
	// as the literal "<no value>" into the target. Walking them here also
	// completes the usage record the unused-param check needs.
	//
	// Skipped when any filter pattern is templated, since the run-time walk
	// would then cover a different set of files than this one.
	if !anyTemplated(lf.Spec.Excludes, lf.Spec.Includes) {
		filter := &util.FilterOptions{Excludes: lf.Spec.Excludes, Includes: lf.Spec.Includes}
		for _, src := range newFilesSources {
			srcDir := util.ExpandPath(moduleDir, src.path)
			rendered, err := util.WalkTemplateFiles(srcDir, filter)
			if err != nil {
				usageComplete = false
				continue
			}
			renderedSet := make(map[string]bool, len(rendered))
			for _, rel := range rendered {
				renderedSet[rel] = true

				content, err := os.ReadFile(filepath.Join(srcDir, rel))
				if err != nil {
					usageComplete = false
					continue
				}
				checkTmpl(fmt.Sprintf("operation %q: template file %q", src.op, rel), string(content))
				// T3: __param__ placeholders in the path become {{ .param }}
				// before the destination path is rendered.
				checkTmpl(fmt.Sprintf("operation %q: template file path %q", src.op, rel), tmpl.ConvertPathTemplate(rel))
			}

			// A patch file that also survives a newFiles walk is rendered into
			// the target as module output — the classic symptom of forgetting
			// to list the utility directory in spec.excludes.
			for _, p := range patchPaths {
				rel, err := filepath.Rel(srcDir, util.ExpandPath(moduleDir, p.path))
				if err != nil || !renderedSet[rel] {
					continue
				}
				fail("operation %q: patch file %q is also rendered into the target by newFiles operation %q — exclude it via spec.excludes", p.op, p.path, src.op)
			}
		}

		for _, p := range patchPaths {
			content, err := os.ReadFile(util.ExpandPath(moduleDir, p.path))
			if err != nil {
				usageComplete = false
				continue
			}
			checkTmpl(fmt.Sprintf("operation %q: patch file %q", p.op, p.path), string(content))
		}
	} else {
		usageComplete = false
	}

	// Stand-in directories for run-time-resolved asset paths. Only the usage
	// record is updated: a reference here proves a param is read somewhere, but
	// a bad reference cannot be blamed on a file that may never be rendered.
	for _, dir := range usageOnlyDirs {
		root := util.ExpandPath(moduleDir, dir)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil || !isTemplated(string(content)) {
				return nil
			}
			t, err := template.New("").Funcs(tmpl.FuncMap()).Parse(string(content))
			if err != nil {
				return nil
			}
			refs, ok := templateParamRefs(t.Tree.Root)
			if !ok {
				usageComplete = false
				return nil
			}
			for _, r := range refs {
				usedParams[r] = true
			}
			return nil
		})
		if err != nil {
			usageComplete = false
		}
	}

	// A declared param that nothing references is dead config. It is a warning
	// rather than a violation: the run is correct, it just ignores the value —
	// but nothing reports that, so a param left behind by a rename looks
	// identical to one that is deliberately optional, and a value passed on the
	// command line is silently discarded.
	//
	// Only reported once every template has been accounted for: a source that
	// could not be walked, a body that could not be read, or a template that
	// rebinds dot all leave references unseen, and a param used only there must
	// not be called unused.
	if usageComplete {
		for _, p := range lf.Spec.Params {
			if p.Name != "" && !usedParams[p.Name] {
				warn("param %q is declared but never referenced by any template", p.Name)
			}
		}
		for _, dp := range lf.Spec.DynamicParams {
			if dp.Name != "" && !usedParams[dp.Name] {
				warn("dynamicParam %q is declared but never referenced by any template", dp.Name)
			}
		}
	}

	return warnings, errors.Join(errs...)
}

// checkGlob records a violation for exclude/include patterns that can never
// match at run time. Filtering matches base names with filepath.Match and
// swallows its error, so a malformed pattern or one carrying a path separator
// silently matches nothing instead of failing the run. Templated patterns are
// resolved at run time and skipped.
func checkGlob(fail func(string, ...any), field, pattern string) {
	if isTemplated(pattern) {
		return
	}
	if pattern == "" {
		fail("%s: pattern cannot be empty", field)
		return
	}
	if strings.Contains(pattern, "/") {
		fail("%s: pattern %q contains a path separator, but patterns match base names only", field, pattern)
		return
	}
	if _, err := filepath.Match(pattern, "x"); err != nil {
		fail("%s: invalid glob pattern %q: %v", field, pattern, err)
	}
}

// checkTargetPath records a violation for a destination that would resolve
// outside the target directory once joined to it at run time. Templated values
// are resolved at run time and skipped.
func checkTargetPath(fail func(string, ...any), opName, field, value string) {
	if value == "" || isTemplated(value) {
		return
	}
	if clean := filepath.Clean(value); clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		fail("operation %q: %s %q escapes the target directory", opName, field, value)
	}
}

// staticPrefixDir returns the deepest directory of a path that is fixed before
// templating: "__functions/patches/{{ .kind }}.yaml" → "__functions/patches".
// A path templated from its first segment yields ".", the module root.
func staticPrefixDir(path string) string {
	dir := filepath.Dir(path)
	for dir != "." && dir != string(filepath.Separator) {
		if !isTemplated(dir) {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return "."
}

// anyTemplated reports whether any string in the given lists is templated.
func anyTemplated(lists ...[]string) bool {
	for _, l := range lists {
		for _, s := range l {
			if isTemplated(s) {
				return true
			}
		}
	}
	return false
}

// isTemplated returns true if the string contains Go template expressions.
func isTemplated(s string) bool {
	return strings.Contains(s, "{{")
}

// textFuncs take text. Handed a list or map, the loom ones fail the render
// and the print builtins write Go's own formatting.
var textFuncs = map[string]bool{
	"upper": true, "lower": true, "quote": true, "indent": true, "nindent": true, "split": true,
	"print": true, "printf": true, "println": true,
}

// structuredUse is one place a template treats a list or map param as text.
type structuredUse struct {
	name string
	fn   string // the text function it reaches; empty when printed bare
}

// printedStructured finds the list and map params a template treats as text:
// printed bare — {{ .sources }} or {{ $.sources }} as a whole action — or
// handed to a function that takes text, as an argument ({{ nindent 2 .cfg }})
// or through a pipe ({{ .cfg | nindent 2 }}). Each param and function pair is
// reported once.
func printedStructured(root *ttparse.ListNode, structured map[string]ParamType) []structuredUse {
	var uses []structuredUse
	seen := make(map[structuredUse]bool)
	report := func(u structuredUse) {
		if _, ok := structured[u.name]; ok && !seen[u] {
			seen[u] = true
			uses = append(uses, u)
		}
	}
	// paramOf names the param an argument reads whole, if any.
	paramOf := func(a ttparse.Node, top bool) string {
		switch a := a.(type) {
		case *ttparse.FieldNode:
			if top && len(a.Ident) == 1 {
				return a.Ident[0]
			}
		case *ttparse.VariableNode:
			if len(a.Ident) == 2 && a.Ident[0] == "$" {
				return a.Ident[1]
			}
		}
		return ""
	}
	var checkPipe func(p *ttparse.PipeNode, top bool)
	checkPipe = func(p *ttparse.PipeNode, top bool) {
		if p == nil {
			return
		}
		carried := "" // the param flowing into the next command of the pipe
		for _, c := range p.Cmds {
			if id, ok := c.Args[0].(*ttparse.IdentifierNode); ok && textFuncs[id.Ident] {
				for _, a := range c.Args[1:] {
					if name := paramOf(a, top); name != "" {
						report(structuredUse{name, id.Ident})
					}
				}
				if carried != "" {
					report(structuredUse{carried, id.Ident})
				}
			}
			for _, a := range c.Args {
				if sub, ok := a.(*ttparse.PipeNode); ok {
					checkPipe(sub, top)
				}
			}
			carried = ""
			if len(c.Args) == 1 {
				carried = paramOf(c.Args[0], top)
			}
		}
	}
	var walk func(n ttparse.Node, top bool)
	walk = func(n ttparse.Node, top bool) {
		switch n := n.(type) {
		case *ttparse.ListNode:
			if n == nil {
				return
			}
			for _, item := range n.Nodes {
				walk(item, top)
			}
		case *ttparse.ActionNode:
			p := n.Pipe
			if len(p.Decl) == 0 && len(p.Cmds) == 1 && len(p.Cmds[0].Args) == 1 {
				if name := paramOf(p.Cmds[0].Args[0], top); name != "" {
					report(structuredUse{name: name})
				}
			}
			checkPipe(p, top)
		case *ttparse.IfNode:
			walk(n.List, top)
			walk(n.ElseList, top)
		case *ttparse.RangeNode:
			walk(n.List, false)
			walk(n.ElseList, top)
		case *ttparse.WithNode:
			walk(n.List, false)
			walk(n.ElseList, top)
		}
	}
	walk(root, true)
	return uses
}

// TemplateRefs parses src with the loom function map and returns the params
// it references, read exactly as reference checking reads them (see
// templateParamRefs). ok is false when the template reaches the param map in a
// way whose keys cannot be known — a computed index key, or dot or $ handed
// whole to a function — and refs is then incomplete.
func TemplateRefs(src string) (refs []string, ok bool, err error) {
	t, err := template.New("").Funcs(tmpl.FuncMap()).Parse(src)
	if err != nil {
		return nil, false, err
	}
	refs, ok = templateParamRefs(t.Tree.Root)
	return refs, ok, nil
}

// templateParamRefs collects the params a parsed template references:
// {{ .name }}, {{ .name.sub }} and {{ index .name "k" }} all name "name", as
// does {{ $.name }} anywhere in the template.
//
// Params hold strings, lists and maps, and dot is the param map only at a
// template's top level and in the else branch of a range or with. Inside a
// range or with body dot is rebound to the item — a list element, a nested
// map, a string — so a field reference there reads the item, never a param;
// only the pipeline being ranged over names one. $ is the exception: it is
// the param map everywhere, so {{ $.env }} inside a range body is a reference
// to env and is read as one. {{ index . "name" }} (or $ in place of dot), the
// one way to reach a name that is not a valid template identifier, states its
// key literally and is read as a reference to it.
//
// ok is false only when the param map is reached in a way whose keys cannot
// be known: a computed index key, or dot or $ handed whole to a function.
// Reference checking is skipped for such templates.
func templateParamRefs(root *ttparse.ListNode) (refs []string, ok bool) {
	ok = true
	// isParamMap reports whether n evaluates to the param map itself: dot
	// where dot has not been rebound, or a bare $.
	isParamMap := func(n ttparse.Node, top bool) bool {
		switch n := n.(type) {
		case *ttparse.DotNode:
			return top
		case *ttparse.VariableNode:
			return len(n.Ident) == 1 && n.Ident[0] == "$"
		}
		return false
	}
	var walkNode func(n ttparse.Node, top bool)
	walkPipe := func(p *ttparse.PipeNode, top bool) {
		if p == nil {
			return
		}
		for _, c := range p.Cmds {
			// `index <param map> "key"` is recognised before the arguments are
			// walked, so its dot (or $) does not read as opaque.
			if len(c.Args) == 3 {
				if id, isID := c.Args[0].(*ttparse.IdentifierNode); isID && id.Ident == "index" && isParamMap(c.Args[1], top) {
					if key, isStr := c.Args[2].(*ttparse.StringNode); isStr {
						refs = append(refs, key.Text)
						continue
					}
				}
			}
			for _, arg := range c.Args {
				walkNode(arg, top)
			}
		}
	}
	walkNode = func(n ttparse.Node, top bool) {
		if n == nil || !ok {
			return
		}
		switch n := n.(type) {
		case *ttparse.ListNode:
			if n == nil {
				return
			}
			for _, item := range n.Nodes {
				walkNode(item, top)
			}
		case *ttparse.ActionNode:
			walkPipe(n.Pipe, top)
		case *ttparse.IfNode:
			walkPipe(n.Pipe, top)
			walkNode(n.List, top)
			walkNode(n.ElseList, top)
		case *ttparse.RangeNode:
			// The body sees an element as dot; the else branch runs when
			// there are none, with dot unchanged.
			walkPipe(n.Pipe, top)
			walkNode(n.List, false)
			walkNode(n.ElseList, top)
		case *ttparse.WithNode:
			walkPipe(n.Pipe, top)
			walkNode(n.List, false)
			walkNode(n.ElseList, top)
		case *ttparse.DotNode:
			// Dot reaching here as the param map is used opaquely — a
			// computed index key, or dot passed to a function. Which names
			// it reads is not visible.
			if top {
				ok = false
			}
		case *ttparse.VariableNode:
			if n.Ident[0] != "$" {
				return
			}
			if len(n.Ident) > 1 {
				refs = append(refs, n.Ident[1])
				return
			}
			// A bare $ is the whole param map, used opaquely.
			ok = false
		case *ttparse.TemplateNode:
			walkPipe(n.Pipe, top)
		case *ttparse.PipeNode:
			walkPipe(n, top)
		case *ttparse.FieldNode:
			if top {
				refs = append(refs, n.Ident[0])
			}
		case *ttparse.ChainNode:
			walkNode(n.Node, top)
		}
	}
	walkNode(root, true)
	return refs, ok
}
