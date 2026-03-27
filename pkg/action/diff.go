package action

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// ANSI color codes for diff output.
const (
	diffColorReset  = "\033[0m"
	diffColorRed    = "\033[31m"
	diffColorGreen  = "\033[32m"
	diffColorCyan   = "\033[36m"
)

// printDiff writes a colored unified diff between old and new content
// directly to execCtx.DiffWriter, bypassing the structured logger.
func printDiff(execCtx *ExecutionContext, path, oldContent, newContent string) {
	if execCtx.DiffWriter == nil {
		return
	}

	if oldContent == newContent {
		execCtx.Logger.Info("diff: no changes", "path", path)
		return
	}

	fromName := path
	if oldContent == "" {
		fromName = "/dev/null"
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldContent),
		B:        difflib.SplitLines(newContent),
		FromFile: fromName,
		ToFile:   path,
		Context:  3,
	}

	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		execCtx.Logger.Warn("diff: failed to generate", "path", path, "error", err)
		return
	}

	if text == "" {
		execCtx.Logger.Info("diff: no changes", "path", path)
		return
	}

	colored := colorizeDiff(text, isTerminalWriter(execCtx.DiffWriter))
	fmt.Fprint(execCtx.DiffWriter, colored)
}

// colorizeDiff applies ANSI colors to unified diff lines.
func colorizeDiff(text string, color bool) string {
	if !color {
		return text
	}

	var sb strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"):
			sb.WriteString(diffColorCyan)
			sb.WriteString(line)
			sb.WriteString(diffColorReset)
		case strings.HasPrefix(line, "@@"):
			sb.WriteString(diffColorCyan)
			sb.WriteString(line)
			sb.WriteString(diffColorReset)
		case strings.HasPrefix(line, "+"):
			sb.WriteString(diffColorGreen)
			sb.WriteString(line)
			sb.WriteString(diffColorReset)
		case strings.HasPrefix(line, "-"):
			sb.WriteString(diffColorRed)
			sb.WriteString(line)
			sb.WriteString(diffColorReset)
		default:
			sb.WriteString(line)
		}
	}
	return sb.String()
}

// isTerminalWriter checks if the writer is a terminal for color support.
func isTerminalWriter(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		fi, err := f.Stat()
		if err != nil {
			return false
		}
		return (fi.Mode() & os.ModeCharDevice) != 0
	}
	return false
}
