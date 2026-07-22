package action

import (
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
	diffColorInvert = "\033[7m"
	diffColorMuted  = "\033[38;5;244m" // mid gray — matches the log handler's muted tone
)

// printDiff computes a unified diff between old and new content and hands it to
// execCtx.Diffs, which holds it until the end of the run. It does not write to
// the logger or stdout itself, so diffs stay out of the interleaved operation
// logs and print together once the run finishes.
func printDiff(execCtx *ExecutionContext, path, oldContent, newContent string) {
	if execCtx.Diffs == nil {
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

	execCtx.Diffs.Add(execCtx.ModuleName, execCtx.TargetLabel, text)
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
