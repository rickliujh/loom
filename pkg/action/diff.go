package action

import (
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// printDiff outputs a unified diff between old and new content to the logger.
func printDiff(execCtx *ExecutionContext, path, oldContent, newContent string) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(oldContent, newContent, true)
	diffs = dmp.DiffCleanupSemantic(diffs)

	if dmp.DiffLevenshtein(diffs) == 0 {
		execCtx.Logger.Info("diff: no changes", "path", path)
		return
	}

	patches := dmp.PatchMake(oldContent, diffs)
	out := dmp.PatchToText(patches)

	// Log the diff header and content.
	var sb strings.Builder
	if oldContent == "" {
		fmt.Fprintf(&sb, "--- /dev/null\n+++ %s\n", path)
	} else {
		fmt.Fprintf(&sb, "--- %s\n+++ %s\n", path, path)
	}
	sb.WriteString(out)

	execCtx.Logger.Info("diff", "path", path, "content", "\n"+sb.String())
}
