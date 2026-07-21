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
			return actionError("newFiles", err)
		}

		// Convert filesystem-friendly __param__ placeholders, then render.
		destRel, err := tmpl.RenderString(tmpl.ConvertPathTemplate(relPath), execCtx.Params)
		if err != nil {
			return actionError("newFiles", err)
		}

		destPath := filepath.Join(execCtx.TargetDir, dest, destRel)
		displayPath := filepath.Join(dest, destRel)

		if execCtx.DryRun {
			if _, err := os.Stat(destPath); err == nil {
				execCtx.Logger.Warn("destination file already exists, a real run would fail", "path", displayPath)
			}
			execCtx.Logger.Info("dry-run: would write file", "path", displayPath, "bytes", len(rendered))
			if execCtx.ShowDiff {
				printDiff(execCtx, destRel, "", string(rendered))
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
