package generate

import (
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
	// Ref is the PR/MR URL or short reference.
	Ref string
	// Params maps param names to concrete values to parameterize.
	Params map[string]string
	// OutputDir is the directory to write the generated module.
	OutputDir string
	// ModuleName overrides the auto-derived module name.
	ModuleName string
	// TokenEnv is the env var name for the API token.
	TokenEnv string
	// ExcludeGitOps skips generating target, commitPush, and pr operations.
	ExcludeGitOps bool
}

// Run generates a loom module from a PR/MR.
func Run(ctx context.Context, opts Options, logger *slog.Logger) error {
	// 1. Detect provider and parse reference.
	provider, diffProvider, err := ParsePRRef(opts.Ref, logger)
	if err != nil {
		return err
	}

	token := tokenFromEnv(opts.TokenEnv, provider, logger)

	// 2. Fetch PR/MR diff.
	logger.Info("fetching PR/MR data", "ref", opts.Ref, "provider", provider)
	prInfo, err := diffProvider.FetchDiff(ctx, opts.Ref, token, logger)
	if err != nil {
		return fmt.Errorf("fetching diff: %w", err)
	}

	if len(prInfo.Files) == 0 {
		return fmt.Errorf("PR/MR has no file changes")
	}

	logger.Info("found file changes", "count", len(prInfo.Files))

	// 3. Derive module name.
	moduleName := opts.ModuleName
	if moduleName == "" {
		moduleName = slugify(prInfo.Title)
	}
	if moduleName == "" {
		moduleName = "generated-module"
	}

	// 4. Classify files and build module structure.
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = "."
	}

	module := buildModule(prInfo, moduleName, opts.Params, !opts.ExcludeGitOps, logger)

	// 5. Emit the module.
	return emitModule(outputDir, module, logger)
}

// generatedModule holds the complete generated module data.
type generatedModule struct {
	loomFile      config.LoomFile
	templateFiles map[string][]byte // relative path -> content
	patchFiles    map[string][]byte // relative path (under __functions/patches/) -> content
}

func buildModule(pr *PRInfo, name string, params map[string]string, includeGitOps bool, logger *slog.Logger) *generatedModule {
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

	// Group added files by common parent directory for newFiles operations.
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
			// Also treat renamed files with new content like added files.
			addedFiles = append(addedFiles, FileChange{
				Type:       ChangeAdded,
				Path:       f.Path,
				NewContent: f.NewContent,
			})
		}
	}

	// Process added files.
	if len(addedFiles) > 0 {
		groups := groupByTopDir(addedFiles)
		opIdx := 0
		for topDir, files := range groups {
			for _, f := range files {
				if f.NewContent == nil {
					continue
				}
				content := string(f.NewContent)
				destPath := f.Path

				// Parameterize content and path.
				content = Parameterize(content, params)
				destPath = ParameterizePath(destPath, params)

				mod.templateFiles[destPath] = []byte(content)
			}

			opName := fmt.Sprintf("create-files-%d", opIdx)
			source := topDir
			dest := topDir
			if topDir == "." {
				source = "."
				dest = ""
			}
			mod.loomFile.Spec.Operations = append(mod.loomFile.Spec.Operations, config.Operation{
				Name: opName,
				NewFiles: &config.NewFiles{
					Source: source,
					Dest:   dest,
				},
			})
			opIdx++
		}
	}

	// Process modified YAML files as SMP patches.
	for i, f := range modifiedFiles {
		if f.NewContent == nil {
			continue
		}

		isYAML := strings.HasSuffix(f.Path, ".yaml") || strings.HasSuffix(f.Path, ".yml")

		if isYAML && f.OldContent != nil {
			smpContent := ComputeSMP(f.OldContent, f.NewContent)
			if smpContent != nil {
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
				continue
			}
		}

		// Non-YAML modified file or SMP computation failed: treat as full replacement.
		content := Parameterize(string(f.NewContent), params)
		destPath := ParameterizePath(f.Path, params)
		mod.templateFiles[destPath] = []byte(content)

		logger.Info("modified file treated as full replacement (not YAML or SMP failed)", "file", f.Path)
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

	// Process renamed files (the move part; content was handled above).
	for i, f := range renamedFiles {
		oldPath := ParameterizePath(f.OldPath, params)
		mod.loomFile.Spec.Operations = append(mod.loomFile.Spec.Operations, config.Operation{
			Name: fmt.Sprintf("rename-%d", i),
			Shell: &config.Shell{
				Command: fmt.Sprintf("mv %q %q", oldPath, ParameterizePath(f.Path, params)),
			},
		})
	}

	// Add target and gitops operations (default behavior, since we're generating from a PR/MR).
	if includeGitOps {
		mod.loomFile.Spec.Target = &config.TargetSpec{
			URL:           toSSHURL(pr.RepoURL),
			Branch:        pr.BaseBranch,
			FeatureBranch: Parameterize(pr.HeadBranch, params),
		}
		mod.loomFile.Spec.Operations = append(mod.loomFile.Spec.Operations,
			config.Operation{
				Name: "commit",
				CommitPush: &config.CommitPush{
					Message: Parameterize(pr.Title, params),
				},
			},
			config.Operation{
				Name: "open-pr",
				PR: &config.PR{
					Provider:   pr.Provider,
					Title:      Parameterize(pr.Title, params),
					Body:       Parameterize(pr.Body, params),
					BaseBranch: pr.BaseBranch,
				},
			},
		)
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
	// Write loom.yaml.
	loomYAML, err := yaml.Marshal(mod.loomFile)
	if err != nil {
		return fmt.Errorf("marshaling loom.yaml: %w", err)
	}

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

// groupByTopDir groups files by their top-level directory.
func groupByTopDir(files []FileChange) map[string][]FileChange {
	groups := make(map[string][]FileChange)
	for _, f := range files {
		dir := filepath.Dir(f.Path)
		topDir := strings.Split(dir, string(filepath.Separator))[0]
		if topDir == "" || topDir == "." {
			topDir = "."
		}
		groups[topDir] = append(groups[topDir], f)
	}
	return groups
}
