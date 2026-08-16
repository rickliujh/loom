package bulk

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rickliujh/loom/internal/util"
	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/module"
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
	// NameParam is an optional child param whose value names each entry
	// (default: the item index).
	NameParam string
}

// Run generates a bulk wrapper module from an existing module's config.
func Run(opts Options, logger *slog.Logger) error {
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
	moduleDir, cleanup, err := module.ResolveSource(opts.ModuleRef, "", ".", logger)
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

	// B2: seed items from file, or B1: a single placeholder item.
	items, err := loadItems(opts.ItemsFile, cfg)
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
	if opts.ItemsFile == "" {
		logger.Info("edit the items list in loom.jsonnet, then run it", "path", path)
	}
	return nil
}

// item is one param set. Fields are emitted in the child's param
// declaration order for deterministic output.
type item map[string]string

// loadItems returns the items list: parsed from ItemsFile if given (B2),
// otherwise a single placeholder derived from the declared params (B1).
func loadItems(itemsFile string, cfg *config.LoomFile) ([]item, error) {
	if itemsFile == "" {
		placeholder := make(item, len(cfg.Spec.Params))
		for _, p := range cfg.Spec.Params {
			switch {
			case p.Default != "":
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
	var items []item
	if err := yaml.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parsing items file: %w", err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("items file %s contains no items", itemsFile)
	}

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
		}
		for _, p := range cfg.Spec.Params {
			if p.Required && p.Default == "" {
				if _, ok := it[p.Name]; !ok {
					return nil, fmt.Errorf("item %d: required parameter %q not provided", i, p.Name)
				}
			}
		}
	}
	return items, nil
}

func placeholderValue(p config.ParamDef) string {
	if p.Required {
		return "CHANGEME"
	}
	return ""
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
			if opts.ItemsFile == "" && p.Required && p.Default == "" {
				comment = "  // required"
			}
			fmt.Fprintf(&b, "    %s: %s,%s\n", jsonnetField(p.Name), jsonnetString(v), comment)
		}
		// Keys of dynamic params (only possible via --items) come after
		// the static ones.
		for _, dp := range cfg.Spec.DynamicParams {
			if v, ok := it[dp.Name]; ok {
				fmt.Fprintf(&b, "    %s: %s,\n", jsonnetField(dp.Name), jsonnetString(v))
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
