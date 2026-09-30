package bulk

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rickliujh/loom/internal/util"
	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/module"
	"github.com/rickliujh/loom/pkg/params"
	tmpl "github.com/rickliujh/loom/pkg/template"
	"gopkg.in/yaml.v3"
)

// Options configures bulk wrapper generation.
type Options struct {
	// ModuleRef is the child module source: local path or git URL,
	// same forms as `loom run`.
	ModuleRef string
	// OutputDir is the directory to write the generated loom.jsonnet.
	OutputDir string
	// Name overrides the wrapper module name (default: bulk-<childName>).
	Name string
	// ItemsFile is an optional YAML file with a list of param sets.
	ItemsFile string
	// Items are param sets given directly, as `loom serve` receives them;
	// when non-nil they are used instead of ItemsFile and checked the same
	// way. A top-level scalar is expected as its text, as a file gives it,
	// and a string given for a list or map param is parsed as YAML
	// (params.Coerce), so the wrapper holds the structure, not its text.
	Items []map[string]any
	// NameParam is an optional child param whose value names each entry
	// (default: the item index).
	NameParam string
}

// Run generates a bulk wrapper module from an existing module's config.
// It is RunContext under context.Background().
func Run(opts Options, logger *slog.Logger) error {
	return RunContext(context.Background(), opts, logger)
}

// RunContext is Run with the clone of a git-URL module bound by ctx.
func RunContext(ctx context.Context, opts Options, logger *slog.Logger) error {
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = "."
	}

	// B6: never clobber an existing module config.
	for _, f := range []string{"loom.jsonnet", "loom.yaml"} {
		p := filepath.Join(outputDir, f)
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("refusing to overwrite existing %s", p)
		}
	}

	// B1: load and validate the child module.
	moduleDir, cleanup, err := module.ResolveSourceContext(ctx, opts.ModuleRef, ".", logger)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	cfg, err := config.Load(moduleDir)
	if err != nil {
		return err
	}
	if err := config.ValidateInDir(cfg, moduleDir); err != nil {
		return fmt.Errorf("validating %s: %w", opts.ModuleRef, err)
	}

	declared := make(map[string]bool, len(cfg.Spec.Params)+len(cfg.Spec.DynamicParams))
	for _, p := range cfg.Spec.Params {
		declared[p.Name] = true
	}
	for _, dp := range cfg.Spec.DynamicParams {
		declared[dp.Name] = true
	}

	if opts.NameParam != "" && !declared[opts.NameParam] {
		return fmt.Errorf("--name-param %q is not a declared parameter of %s", opts.NameParam, cfg.Metadata.Name)
	}
	// The entry name is built by string concatenation in jsonnet, which a
	// list or map cannot take part in.
	if t := declaredType(cfg, opts.NameParam); opts.NameParam != "" && t != config.ParamString {
		return fmt.Errorf("--name-param %q is a %s parameter; it must be a string", opts.NameParam, t)
	}

	// B2: seed items from file, or B1: a single placeholder item.
	items, err := loadItems(opts, cfg)
	if err != nil {
		return err
	}

	wrapperName := opts.Name
	if wrapperName == "" {
		wrapperName = "bulk-" + cfg.Metadata.Name
	}

	source, err := emittedSource(opts.ModuleRef, outputDir)
	if err != nil {
		return err
	}

	content := render(cfg, wrapperName, source, opts, items)

	path := filepath.Join(outputDir, "loom.jsonnet")
	logger.Info("writing", "path", path)
	if err := util.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing loom.jsonnet: %w", err)
	}

	// B7: the generated wrapper must load and validate through the
	// standard pipeline.
	wrapper, err := config.Load(outputDir)
	if err != nil {
		return fmt.Errorf("generated wrapper failed validation: %w", err)
	}
	if err := config.Validate(wrapper); err != nil {
		return fmt.Errorf("generated wrapper failed validation: %w", err)
	}

	logger.Info("bulk wrapper generated", "name", wrapperName, "child", cfg.Metadata.Name, "items", len(items))
	if !opts.seeded() {
		logger.Info("edit the items list in loom.jsonnet, then run it", "path", path)
	}
	return nil
}

// item is one param set: strings, lists and maps. Fields are emitted in the
// child's param declaration order for deterministic output.
type item map[string]any

// declaredType is the effective type of a declared param, static or dynamic.
func declaredType(cfg *config.LoomFile, name string) config.ParamType {
	for _, p := range cfg.Spec.Params {
		if p.Name == name {
			return p.Type.Effective()
		}
	}
	for _, dp := range cfg.Spec.DynamicParams {
		if dp.Name == name {
			return dp.Type.Effective()
		}
	}
	return config.ParamString
}

// seeded reports whether the items list was supplied rather than derived.
func (o Options) seeded() bool {
	return o.Items != nil || o.ItemsFile != ""
}

// loadItems returns the items list: Items or ItemsFile if given (B2),
// otherwise a single placeholder derived from the declared params (B1).
func loadItems(opts Options, cfg *config.LoomFile) ([]item, error) {
	if opts.Items != nil {
		if len(opts.Items) == 0 {
			return nil, fmt.Errorf("items list contains no items")
		}
		items := make([]item, len(opts.Items))
		for i, it := range opts.Items {
			items[i] = make(item, len(it))
			for k, v := range it {
				if s, ok := v.(string); ok && declaredType(cfg, k) != config.ParamString {
					if parsed, err := params.Coerce(k, declaredType(cfg, k), s); err == nil {
						v = parsed
					}
				}
				items[i][k] = v
			}
		}
		return checkItems(items, cfg)
	}
	itemsFile := opts.ItemsFile
	if itemsFile == "" {
		placeholder := make(item, len(cfg.Spec.Params))
		for _, p := range cfg.Spec.Params {
			switch {
			case p.HasDefault():
				placeholder[p.Name] = p.Default
			default:
				placeholder[p.Name] = placeholderValue(p)
			}
		}
		return []item{placeholder}, nil
	}

	data, err := os.ReadFile(itemsFile)
	if err != nil {
		return nil, fmt.Errorf("reading items file: %w", err)
	}
	// Each item decodes like a params file: top-level scalars as the text
	// written, nested values keeping their scalars' written form.
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing items file: %w", err)
	}
	var list []*yaml.Node
	if len(doc.Content) > 0 {
		root := doc.Content[0]
		if root.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("parsing items file: expected a list of param sets")
		}
		list = root.Content
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("items file %s contains no items", itemsFile)
	}
	items := make([]item, len(list))
	for i, n := range list {
		m, err := config.DecodeParamValues(n)
		if err != nil {
			return nil, fmt.Errorf("parsing items file: item %d: %w", i, err)
		}
		items[i] = m
	}
	return checkItems(items, cfg)
}

// checkItems rejects an item a run of the child could not take: an
// undeclared param, a value of the wrong type, a required param left out.
func checkItems(items []item, cfg *config.LoomFile) ([]item, error) {
	declared := make(map[string]bool)
	for _, p := range cfg.Spec.Params {
		declared[p.Name] = true
	}
	for _, dp := range cfg.Spec.DynamicParams {
		declared[dp.Name] = true
	}

	for i, it := range items {
		for k := range it {
			if !declared[k] {
				return nil, fmt.Errorf("item %d: undeclared parameter %q", i, k)
			}
			// A value the child could not take as its declared type would
			// fail every run of that item; say so now.
			if _, err := params.Coerce(k, declaredType(cfg, k), it[k]); err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
		}
		for _, p := range cfg.Spec.Params {
			if p.Required && !p.HasDefault() {
				if _, ok := it[p.Name]; !ok {
					return nil, fmt.Errorf("item %d: required parameter %q not provided", i, p.Name)
				}
			}
		}
	}
	return items, nil
}

// placeholderValue is what an item holds for a param with no default: a
// marker for a required one — a string even for a list or map param, so a run
// left unedited fails on it instead of proceeding with an empty value — and the
// empty value of the param's type otherwise.
func placeholderValue(p config.ParamDef) any {
	if p.Required {
		return "CHANGEME"
	}
	return params.Zero(p.Type)
}

// emittedSource derives the child source to write into the wrapper (B4):
// local paths become relative to the output dir; git URLs pass through.
func emittedSource(moduleRef, outputDir string) (string, error) {
	if !strings.HasPrefix(moduleRef, ".") && !strings.HasPrefix(moduleRef, "/") {
		return moduleRef, nil
	}

	absModule, err := filepath.Abs(moduleRef)
	if err != nil {
		return "", fmt.Errorf("resolving module path: %w", err)
	}
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return "", fmt.Errorf("resolving output path: %w", err)
	}
	rel, err := filepath.Rel(absOutput, absModule)
	if err != nil {
		// No relative path between the two — fall back to absolute.
		return absModule, nil
	}
	if !strings.HasPrefix(rel, ".") {
		// Child source resolution requires a "." or "/" prefix.
		rel = "./" + rel
	}
	return rel, nil
}

func render(cfg *config.LoomFile, wrapperName, source string, opts Options, items []item) string {
	var b strings.Builder

	fmt.Fprintf(&b, "// Generated by loom bulk from %s.\n", opts.ModuleRef)
	b.WriteString("// One entry in `items` = one execution of the child module.\n")
	if cfg.Spec.Target != nil {
		// B8: state the resulting PR topology.
		fmt.Fprintf(&b, "//\n// NOTE: %s declares its own spec.target, so every item clones, branches,\n", cfg.Metadata.Name)
		b.WriteString("// and opens its own PR. For a single PR covering the whole batch, move\n")
		b.WriteString("// target/commitPush/pr onto this wrapper and remove them from the child.\n")
	}
	b.WriteString("local items = [\n")
	for _, it := range items {
		b.WriteString("  {\n")
		for _, p := range cfg.Spec.Params {
			v, ok := it[p.Name]
			if !ok {
				continue
			}
			comment := ""
			if !opts.seeded() && p.Required && !p.HasDefault() {
				comment = "  // required"
			}
			fmt.Fprintf(&b, "    %s: %s,%s\n", jsonnetField(p.Name), jsonnetValue(v, "    "), comment)
		}
		// Keys of dynamic params (only possible via --items) come after
		// the static ones.
		for _, dp := range cfg.Spec.DynamicParams {
			if v, ok := it[dp.Name]; ok {
				fmt.Fprintf(&b, "    %s: %s,\n", jsonnetField(dp.Name), jsonnetValue(v, "    "))
			}
		}
		b.WriteString("  },\n")
	}
	b.WriteString("];\n\n")

	nameExpr := "std.toString(i)"
	if opts.NameParam != "" {
		nameExpr = "items[i]" + jsonnetAccess(opts.NameParam)
	}

	b.WriteString("{\n")
	b.WriteString("  apiVersion: 'loom.rickliujh.github.io/v1beta1',\n")
	b.WriteString("  kind: 'Loom',\n")
	fmt.Fprintf(&b, "  metadata: { name: %s },\n", jsonnetString(wrapperName))
	b.WriteString("  spec: {\n")
	b.WriteString("    modules: [\n")
	b.WriteString("      {\n")
	fmt.Fprintf(&b, "        name: %s + '-' + %s,\n", jsonnetString(cfg.Metadata.Name), nameExpr)
	fmt.Fprintf(&b, "        source: %s,\n", jsonnetString(source))
	b.WriteString("        params: items[i],\n")
	b.WriteString("      }\n")
	b.WriteString("      for i in std.range(0, std.length(items) - 1)\n")
	b.WriteString("    ],\n")
	b.WriteString("  },\n")
	b.WriteString("}\n")

	return b.String()
}

var jsonnetIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// jsonnetKeywords are reserved words that cannot be bare field names.
var jsonnetKeywords = map[string]bool{
	"assert": true, "else": true, "error": true, "false": true,
	"for": true, "function": true, "if": true, "import": true,
	"importstr": true, "in": true, "local": true, "null": true,
	"self": true, "super": true, "tailstrict": true, "then": true,
	"true": true,
}

// jsonnetField renders a param name as an object field name, quoting it
// when it is not a valid identifier (B5).
func jsonnetField(name string) string {
	if jsonnetIdent.MatchString(name) && !jsonnetKeywords[name] {
		return name
	}
	return jsonnetString(name)
}

// jsonnetAccess renders field access on items[i] for a param name.
func jsonnetAccess(name string) string {
	if jsonnetIdent.MatchString(name) && !jsonnetKeywords[name] {
		return "." + name
	}
	return "[" + jsonnetString(name) + "]"
}

// jsonnetValue renders a param value as a jsonnet literal, nested values one
// level deeper than indent. A number kept as its written text (1.10) is
// emitted as a string: as a jsonnet number it would evaluate to 1.1.
func jsonnetValue(v any, indent string) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case string:
		return jsonnetString(v)
	case tmpl.Number:
		return jsonnetString(string(v))
	case bool, int, int64, uint64, float64:
		return fmt.Sprint(v)
	case []any:
		if len(v) == 0 {
			return "[]"
		}
		var b strings.Builder
		b.WriteString("[\n")
		for _, e := range v {
			fmt.Fprintf(&b, "%s  %s,\n", indent, jsonnetValue(e, indent+"  "))
		}
		b.WriteString(indent + "]")
		return b.String()
	case map[string]any:
		if len(v) == 0 {
			return "{}"
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("{\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "%s  %s: %s,\n", indent, jsonnetField(k), jsonnetValue(v[k], indent+"  "))
		}
		b.WriteString(indent + "}")
		return b.String()
	}
	return jsonnetString(fmt.Sprint(v))
}

// jsonnetString renders a single-quoted jsonnet string literal.
func jsonnetString(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		"\n", `\n`,
		"\t", `\t`,
		"\r", `\r`,
	)
	return "'" + r.Replace(s) + "'"
}
