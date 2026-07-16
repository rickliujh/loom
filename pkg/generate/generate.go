package generate

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/rickliujh/loom/internal/util"
	"github.com/rickliujh/loom/pkg/config"
	"gopkg.in/yaml.v3"
)

// Options configures module generation.
type Options struct {
	// Refs are the source references (PR/MR, commit, or local path), applied
	// in the order given (oldest first).
	Refs []string
	// Params maps param names to concrete values to parameterize.
	Params map[string]string
	// OutputDir is the directory to write the generated module.
	OutputDir string
	// ModuleName overrides the auto-derived module name.
	ModuleName string
	// TokenEnv is the name of the environment variable that holds the
	// GitHub personal access token or GitLab private token used to
	// authenticate API requests when fetching PR/MR data. Commit and
	// snapshot sources are git-native and do not use it.
	TokenEnv string
	// Include, Exclude, and Base configure snapshot (local path) sources.
	Include []string
	Exclude []string
	Base    string
}

// Run generates a loom module from one or more sources.
func Run(ctx context.Context, opts Options, logger *slog.Logger) error {
	if len(opts.Refs) == 0 {
		return fmt.Errorf("at least one source reference is required")
	}

	// 1. Parse all references.
	snap := SnapshotOptions{Include: opts.Include, Exclude: opts.Exclude, Base: opts.Base}
	sources := make([]*Source, 0, len(opts.Refs))
	hasSnapshot := false
	allSnapshots := true
	for _, ref := range opts.Refs {
		src, err := ParseSourceRef(ref, snap, logger)
		if err != nil {
			return err
		}
		if src.Kind == KindSnapshot {
			hasSnapshot = true
		} else {
			allSnapshots = false
		}
		sources = append(sources, src)
	}
	if !hasSnapshot && (len(opts.Include) > 0 || len(opts.Exclude) > 0 || opts.Base != "") {
		return fmt.Errorf("--include/--exclude/--base require a local path source")
	}

	// 2. Fetch each source's changes, in the order given.
	sets := make([]*ChangeSet, 0, len(sources))
	for i, src := range sources {
		token := ""
		if src.Kind == KindPR {
			token = tokenFromEnv(opts.TokenEnv, src.Provider, logger)
		}
		logger.Info("fetching source", "ref", opts.Refs[i], "kind", src.Kind.String())
		cs, err := src.ChangeSource.Fetch(ctx, opts.Refs[i], token, logger)
		if err != nil {
			return fmt.Errorf("fetching diff: %w", err)
		}
		if len(cs.Files) == 0 {
			return fmt.Errorf("source %q has no file changes", opts.Refs[i])
		}
		logger.Info("found file changes", "ref", opts.Refs[i], "count", len(cs.Files))
		sets = append(sets, cs)
	}

	// 3. Compose into a single net changeset.
	merged, err := Compose(sets, logger)
	if err != nil {
		return err
	}
	if len(sets) > 1 {
		logger.Info("composed sources", "sources", len(sets), "netChanges", len(merged.Files))
	}

	// 4. Derive module name.
	moduleName := opts.ModuleName
	if moduleName == "" {
		moduleName = slugify(merged.Title)
	}
	if moduleName == "" {
		if allSnapshots {
			return fmt.Errorf("--name is required when generating from local files")
		}
		moduleName = "generated-module"
	}

	// 5. Classify files and build module structure.
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = "."
	}

	module := buildModule(merged, moduleName, opts.Params, logger)

	// 6. Emit the module.
	return emitModule(outputDir, module, logger)
}

// generatedModule holds the complete generated module data.
type generatedModule struct {
	loomFile      config.LoomFile
	templateFiles map[string][]byte // relative path -> content
	patchFiles    map[string][]byte // relative path (under __functions/patches/) -> content
}

func buildModule(pr *ChangeSet, name string, params map[string]string, logger *slog.Logger) *generatedModule {
	mod := &generatedModule{
		loomFile: config.LoomFile{
			APIVersion: "loom.rickliujh.github.io/v1beta1",
			Kind:       "Loom",
			Metadata:   config.Metadata{Name: name},
			Spec: config.Spec{
				Params: buildParamDefs(params),
			},
		},
		templateFiles: make(map[string][]byte),
		patchFiles:    make(map[string][]byte),
	}

	// Classify file changes.
	var addedFiles []FileChange
	var modifiedFiles []FileChange
	var deletedFiles []FileChange
	var renamedFiles []FileChange

	for _, f := range pr.Files {
		switch f.Type {
		case ChangeAdded:
			addedFiles = append(addedFiles, f)
		case ChangeModified:
			modifiedFiles = append(modifiedFiles, f)
		case ChangeDeleted:
			deletedFiles = append(deletedFiles, f)
		case ChangeRenamed:
			renamedFiles = append(renamedFiles, f)
		}
	}

	// Process added files — single newFiles operation from root.
	hasTemplateFiles := false
	for _, f := range addedFiles {
		if f.NewContent == nil {
			continue
		}
		hasTemplateFiles = true
		content := Parameterize(string(f.NewContent), params)
		destPath := ParameterizePath(f.Path, params)
		mod.templateFiles[destPath] = []byte(content)
	}
	if hasTemplateFiles {
		mod.loomFile.Spec.Operations = append(mod.loomFile.Spec.Operations, config.Operation{
			Name: "create-files",
			NewFiles: &config.NewFiles{
				Source: ".",
				Dest:   "",
			},
		})
	}

	// Process modified files — YAML with SMP only; skip others with warning.
	for i, f := range modifiedFiles {
		if f.NewContent == nil {
			continue
		}

		isYAML := strings.HasSuffix(f.Path, ".yaml") || strings.HasSuffix(f.Path, ".yml")

		if !isYAML {
			logger.Warn("modified non-YAML file skipped (manual review needed)", "file", f.Path)
			continue
		}

		if f.OldContent == nil {
			logger.Warn("modified YAML file skipped (old content unavailable for SMP)", "file", f.Path)
			continue
		}

		smpContent := ComputeSMP(f.OldContent, f.NewContent)
		if smpContent == nil {
			logger.Warn("SMP computation failed or no changes detected (manual review needed)", "file", f.Path)
			continue
		}

		// Parameterize the SMP patch.
		patchStr := Parameterize(string(smpContent), params)

		patchName := sanitizeFilename(f.Path) + ".patch.yaml"
		mod.patchFiles[patchName] = []byte(patchStr)

		patchTarget := ParameterizePath(f.Path, params)

		mod.loomFile.Spec.Operations = append(mod.loomFile.Spec.Operations, config.Operation{
			Name: fmt.Sprintf("patch-%d", i),
			Patch: &config.Patch{
				Engine: "smp",
				Path:   filepath.Join(util.FunctionsDir, "patches", patchName),
				Target: patchTarget,
			},
		})
	}

	// Process deleted files.
	for i, f := range deletedFiles {
		target := ParameterizePath(f.Path, params)
		mod.loomFile.Spec.Operations = append(mod.loomFile.Spec.Operations, config.Operation{
			Name: fmt.Sprintf("delete-%d", i),
			Shell: &config.Shell{
				Command: fmt.Sprintf("rm -f %q", target),
			},
		})
	}

	// Process renamed files — patch first (built-in, more stable), then mv.
	for i, f := range renamedFiles {
		oldPath := ParameterizePath(f.OldPath, params)
		newPath := ParameterizePath(f.Path, params)

		// If content also changed, produce an SMP patch targeting the old path (before mv).
		if f.NewContent != nil && f.OldContent != nil {
			isYAML := strings.HasSuffix(f.Path, ".yaml") || strings.HasSuffix(f.Path, ".yml")
			if !isYAML {
				logger.Warn("renamed non-YAML file has content changes (manual review needed)", "file", f.Path, "oldPath", f.OldPath)
			} else {
				smpContent := ComputeSMP(f.OldContent, f.NewContent)
				if smpContent != nil {
					patchStr := Parameterize(string(smpContent), params)
					patchName := sanitizeFilename(f.OldPath) + ".patch.yaml"
					mod.patchFiles[patchName] = []byte(patchStr)

					mod.loomFile.Spec.Operations = append(mod.loomFile.Spec.Operations, config.Operation{
						Name: fmt.Sprintf("rename-patch-%d", i),
						Patch: &config.Patch{
							Engine: "smp",
							Path:   filepath.Join(util.FunctionsDir, "patches", patchName),
							Target: oldPath,
						},
					})
				}
			}
		}

		mod.loomFile.Spec.Operations = append(mod.loomFile.Spec.Operations, config.Operation{
			Name: fmt.Sprintf("rename-%d", i),
			Shell: &config.Shell{
				Command: fmt.Sprintf("mv %q %q", oldPath, newPath),
			},
		})
	}

	// Add target and gitops operations. Sources without PR metadata (commit
	// ranges, local snapshots) get synthesized fallbacks; sources without a
	// recognizable remote degrade by omitting the target and/or pr operation.
	title := pr.Title
	if title == "" {
		title = "apply " + name
	}
	featureBranch := pr.HeadBranch
	if featureBranch == "" {
		featureBranch = "loom/" + name
	}

	if pr.RepoURL != "" {
		mod.loomFile.Spec.Target = &config.TargetSpec{
			URL:           toSSHURL(pr.RepoURL),
			Branch:        pr.BaseBranch,
			FeatureBranch: Parameterize(featureBranch, params),
		}
	} else {
		logger.Warn("no repository URL detected; spec.target omitted (fill in manually before running)")
	}

	mod.loomFile.Spec.Operations = append(mod.loomFile.Spec.Operations,
		config.Operation{
			Name: "commit",
			CommitPush: &config.CommitPush{
				Message: Parameterize(title, params),
			},
		},
	)

	if pr.Provider != "" {
		mod.loomFile.Spec.Operations = append(mod.loomFile.Spec.Operations,
			config.Operation{
				Name: "open-pr",
				PR: &config.PR{
					Provider:   pr.Provider,
					Title:      Parameterize(title, params),
					Body:       Parameterize(pr.Body, params),
					BaseBranch: pr.BaseBranch,
				},
			},
		)
	} else {
		logger.Warn("provider unknown; pr operation omitted (add manually if needed)")
	}

	return mod
}

func buildParamDefs(params map[string]string) []config.ParamDef {
	defs := make([]config.ParamDef, 0, len(params))
	for k := range params {
		defs = append(defs, config.ParamDef{
			Name:     k,
			Required: true,
		})
	}
	return defs
}

// emitModule writes the generated module to disk.
func emitModule(outputDir string, mod *generatedModule, logger *slog.Logger) error {
	// Write loom.yaml with 2-space indent, matching the patch files emitted
	// by ComputeSMP (yaml.Marshal defaults to 4).
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(mod.loomFile); err != nil {
		return fmt.Errorf("marshaling loom.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("marshaling loom.yaml: %w", err)
	}
	loomYAML := buf.Bytes()

	loomPath := filepath.Join(outputDir, "loom.yaml")
	logger.Info("writing", "path", loomPath)
	if err := util.WriteFile(loomPath, loomYAML, 0o644); err != nil {
		return fmt.Errorf("writing loom.yaml: %w", err)
	}

	// Write template files.
	for relPath, content := range mod.templateFiles {
		destPath := filepath.Join(outputDir, relPath)
		logger.Info("writing template", "path", destPath)
		if err := util.WriteFile(destPath, content, 0o644); err != nil {
			return fmt.Errorf("writing template %s: %w", relPath, err)
		}
	}

	// Write patch files.
	for name, content := range mod.patchFiles {
		destPath := filepath.Join(outputDir, util.FunctionsDir, "patches", name)
		logger.Info("writing patch", "path", destPath)
		if err := util.WriteFile(destPath, content, 0o644); err != nil {
			return fmt.Errorf("writing patch %s: %w", name, err)
		}
	}

	return nil
}

// slugify converts a string to a URL/path-friendly slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r == ' ' || r == '_' || r == '/' {
			return '-'
		}
		return -1
	}, s)
	// Collapse multiple dashes.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// sanitizeFilename converts a path to a safe flat filename by replacing / with --.
func sanitizeFilename(path string) string {
	return strings.ReplaceAll(path, "/", "--")
}

// toSSHURL converts an HTTPS git URL to SSH format.
// e.g. "https://github.com/myorg/myrepo.git" → "git@github.com:myorg/myrepo.git"
// URLs that are already SSH or cannot be parsed are returned as-is.
func toSSHURL(repoURL string) string {
	parsed, err := url.Parse(repoURL)
	if err != nil || parsed.Scheme == "" {
		return repoURL
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return repoURL
	}
	host := parsed.Hostname()
	path := strings.TrimPrefix(parsed.Path, "/")
	return fmt.Sprintf("git@%s:%s", host, path)
}
