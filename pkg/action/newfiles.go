package action

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickliujh/loom/internal/util"
	"github.com/rickliujh/loom/pkg/config"
	tmpl "github.com/rickliujh/loom/pkg/template"
)

// NewFilesAction renders template files from the module directory
// and writes them to the target directory.
type NewFilesAction struct {
	Config config.NewFiles
}

func (a *NewFilesAction) Execute(ctx context.Context, execCtx *ExecutionContext) error {
	source, err := tmpl.RenderString(a.Config.Source, execCtx.Params)
	if err != nil {
		return actionError("newFiles", fmt.Errorf("rendering source: %w", err))
	}
	dest, err := tmpl.RenderString(a.Config.Dest, execCtx.Params)
	if err != nil {
		return actionError("newFiles", fmt.Errorf("rendering dest: %w", err))
	}
	// Checked before the walk so a bad dest fails even when nothing is rendered.
	if _, err := resolveTargetPath(execCtx.TargetDir, "newFiles dest", dest); err != nil {
		return actionError("newFiles", err)
	}

	sourceDir := util.ExpandPath(execCtx.ModuleDir, source)

	opts := &util.FilterOptions{
		Excludes: execCtx.Excludes,
		Includes: execCtx.Includes,
	}
	files, err := util.WalkTemplateFiles(sourceDir, opts)
	if err != nil {
		return actionError("newFiles", err)
	}

	for _, relPath := range files {
		srcPath := filepath.Join(sourceDir, relPath)
		content, err := os.ReadFile(srcPath)
		if err != nil {
			return actionError("newFiles", err)
		}

		rendered, err := tmpl.RenderFile(content, execCtx.Params)
		if err != nil {
			// Name the file: a module renders many, and a missing-value or
			// required failure is only actionable once you know which.
			return actionError("newFiles", fmt.Errorf("rendering template file %q: %w", relPath, err))
		}

		// Convert filesystem-friendly __param__ placeholders, then render.
		destRel, err := tmpl.RenderString(tmpl.ConvertPathTemplate(relPath), execCtx.Params)
		if err != nil {
			return actionError("newFiles", err)
		}

		// File and directory names are templates too, so each rendered path is
		// checked, not just dest.
		displayPath := filepath.Join(dest, destRel)
		destPath, err := resolveTargetPath(execCtx.TargetDir, "newFiles destination", displayPath)
		if err != nil {
			return actionError("newFiles", err)
		}

		if execCtx.DryRun {
			if _, err := os.Stat(destPath); err == nil {
				execCtx.Logger.Warn("destination file already exists, a real run would fail", "path", displayPath)
			}
			execCtx.Logger.Info("dry-run: would write file", "path", displayPath, "bytes", len(rendered))
			if execCtx.ShowDiff {
				// Named from the target's root, dest included — the path the
				// file lands at, and the one a full-mode git diff shows.
				printDiff(execCtx, filepath.ToSlash(displayPath), "", string(rendered))
			}
			continue
		}

		execCtx.Logger.Info("writing file", "path", displayPath)

		if _, err := os.Stat(destPath); err == nil {
			return actionError("newFiles", fmt.Errorf("destination file already exists: %s", destPath))
		}

		info, err := os.Stat(srcPath)
		if err != nil {
			return actionError("newFiles", err)
		}

		if err := util.WriteFile(destPath, rendered, info.Mode()); err != nil {
			return actionError("newFiles", err)
		}
	}

	return nil
}
