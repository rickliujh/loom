package action

import (
	"context"
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
	sourceDir := util.ExpandPath(execCtx.ModuleDir, a.Config.Source)

	files, err := util.WalkTemplateFiles(sourceDir)
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

		// Render the destination path itself (may contain template expressions).
		destRel, err := tmpl.RenderString(relPath, execCtx.Params)
		if err != nil {
			return actionError("newFiles", err)
		}

		destPath := filepath.Join(execCtx.TargetDir, a.Config.Dest, destRel)

		execCtx.Logger.Info("writing file", "path", destPath)
		if execCtx.DryRun {
			execCtx.Logger.Info("dry-run: would write", "path", destPath, "size", len(rendered))
			continue
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
