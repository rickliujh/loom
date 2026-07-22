package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/action"
	"github.com/rickliujh/loom/pkg/module"
	"github.com/spf13/cobra"
)

var (
	diffParams     []string
	diffParamsFile string
	diffTargetPath string
	diffAuthor     string
	diffEmail      string
	diffQuick      bool
)

var diffCmd = &cobra.Command{
	Use:   "diff [path]",
	Short: "Show the diffs a module run would produce",
	Long: `Show every change a loom module would make.

By default, diff runs the module in local mode — it clones each target, executes
all operations (including pure shell commands), commits locally, and skips push
and PR creation — then prints a git diff of each target against its base branch.
This is the complete, accurate picture, including files rewritten by shell ops.

With --quick, diff instead simulates the run (dry-run): it prints unified diffs
for newFiles and patch operations without executing anything. It is fast and has
no side effects, but cannot show changes made by shell commands.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().StringArrayVarP(&diffParams, "param", "p", nil, "Parameter in key=value format (can be repeated)")
	diffCmd.Flags().StringVar(&diffParamsFile, "params-file", "", "YAML file with parameters")
	diffCmd.Flags().StringVar(&diffTargetPath, "target-path", "", "Directory for target clones. When set, it is kept for inspection instead of a cleaned-up temp dir")
	diffCmd.Flags().StringVar(&diffAuthor, "author", "", "Default git author name for commitPush operations")
	diffCmd.Flags().StringVar(&diffEmail, "email", "", "Default git author email for commitPush operations")
	diffCmd.Flags().BoolVar(&diffQuick, "quick", false, "Simulate the run (dry-run) and show newFiles/patch diffs only, without executing anything")
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	logger := newLogger()

	source := "."
	if len(args) > 0 {
		source = args[0]
	}

	paramMap, err := parseParams(diffParams, diffParamsFile)
	if err != nil {
		return err
	}

	if diffQuick {
		return runDiffQuick(cmd.Context(), source, paramMap, logger)
	}
	return runDiffFull(cmd.Context(), source, paramMap, logger)
}

// runDiffQuick simulates the run and prints in-memory unified diffs for
// newFiles/patch operations, executing nothing (the old `run --diff`).
func runDiffQuick(ctx context.Context, source string, paramMap map[string]string, logger *slog.Logger) error {
	diffs := &action.DiffCollector{}
	opts := module.RunOptions{
		DryRun:     true,
		ShowDiff:   true,
		TargetPath: diffTargetPath,
		Diffs:      diffs,
	}

	mod, targetDir, cleanup, err := resolveModuleAndTarget(ctx, source, paramMap, &opts, logger)
	if err != nil {
		return err
	}
	defer cleanup()

	execErr := module.Execute(ctx, mod, targetDir, opts)
	diffs.Print(os.Stdout)
	if execErr == nil {
		fmt.Fprintln(os.Stderr)
		prettylog.Successf(os.Stderr, "diff of %q complete — no changes were made", mod.Config.Metadata.Name)
	}
	return execErr
}

// runDiffFull runs the module in local mode into a workspace, then prints a git
// diff of every cloned target against its base branch.
func runDiffFull(ctx context.Context, source string, paramMap map[string]string, logger *slog.Logger) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("loom diff needs the git CLI to compute diffs; install git or use --quick for a dry-run preview")
	}

	// Workspace: a caller-supplied --target-path is kept for inspection; otherwise
	// a temp dir that is cleaned up once the diff is printed.
	workspace := diffTargetPath
	if workspace == "" {
		tmp, err := os.MkdirTemp("", "loom-diff-*")
		if err != nil {
			return fmt.Errorf("creating diff workspace: %w", err)
		}
		workspace = tmp
		defer os.RemoveAll(tmp)
	}

	// Local mode gives us exactly what a diff wants: pure shell runs, remote-only
	// commands and PRs are skipped, and commits stay local. Default a git identity
	// so a module's commitPush can commit without a configured user.
	author := diffAuthor
	if author == "" {
		author = "loom-diff"
	}
	email := diffEmail
	if email == "" {
		email = "loom-diff@localhost"
	}
	opts := module.RunOptions{
		LocalRun:   true,
		TargetPath: workspace,
		GitAuthor:  author,
		GitEmail:   email,
	}

	mod, targetDir, cleanup, err := resolveModuleAndTarget(ctx, source, paramMap, &opts, logger)
	if err != nil {
		return err
	}
	defer cleanup()

	execErr := module.Execute(ctx, mod, targetDir, opts)

	// Print diffs even on a partial failure — the changes made before it are
	// exactly what the user is inspecting.
	n, diffErr := printTargetDiffs(workspace, os.Stdout)
	if diffErr != nil {
		if execErr != nil {
			return execErr
		}
		return diffErr
	}

	fmt.Fprintln(os.Stderr)
	if n == 0 {
		prettylog.Successf(os.Stderr, "diff of %q complete — no changes (local-only modules: try --quick)", mod.Config.Metadata.Name)
	} else {
		prettylog.Successf(os.Stderr, "diff of %q complete — %d target(s) changed", mod.Config.Metadata.Name, n)
	}
	return execErr
}

// printTargetDiffs writes a git diff for every git repo found directly under
// root (and root itself, if it is a repo), against each repo's base branch.
// It stages all changes first so newly created files appear in the diff.
// Returns the number of repos that had changes.
func printTargetDiffs(root string, w io.Writer) (int, error) {
	repos := gitRepoDirs(root)
	colorFlag := "--color=never"
	if isTerminalWriter(w) {
		colorFlag = "--color=always"
	}

	changed := 0
	for _, dir := range repos {
		if out, err := runGit(dir, "add", "-A"); err != nil {
			return changed, fmt.Errorf("staging changes in %s: %w\n%s", dir, err, out)
		}
		base := baseRef(dir)
		out, err := runGit(dir, "--no-pager", "diff", "--cached", colorFlag, base)
		if err != nil {
			return changed, fmt.Errorf("diffing %s: %w", dir, err)
		}
		if strings.TrimSpace(out) == "" {
			continue
		}
		changed++
		fmt.Fprintf(w, "\n# %s\n", filepath.Base(dir))
		fmt.Fprint(w, out)
		if !strings.HasSuffix(out, "\n") {
			fmt.Fprintln(w)
		}
	}
	return changed, nil
}

// gitRepoDirs returns root (if a git repo) followed by its immediate git-repo
// subdirectories, in directory-name order — matching the numbered subdirs that
// local mode clones into.
func gitRepoDirs(root string) []string {
	var dirs []string
	if isGitRepo(root) {
		dirs = append(dirs, root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return dirs
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name())
		if isGitRepo(p) {
			dirs = append(dirs, p)
		}
	}
	return dirs
}

func isGitRepo(dir string) bool {
	// .git is a directory for a normal clone (a file for worktrees/submodules).
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// baseRef returns the remote-tracking branch a single-branch clone was made
// from — the pristine baseline to diff against. Falls back to HEAD.
func baseRef(dir string) string {
	out, err := runGit(dir, "for-each-ref", "--format=%(refname)", "refs/remotes/origin/")
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
	}
	return "HEAD"
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

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

// resolveModuleAndTarget loads the module at source and resolves the target
// directory to run against per opts, returning a cleanup that tears down any
// temporary clones or sources it created.
func resolveModuleAndTarget(ctx context.Context, source string, paramMap map[string]string, opts *module.RunOptions, logger *slog.Logger) (*module.Module, string, func(), error) {
	var cleanups []func()
	runCleanups := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	moduleDir, srcCleanup, err := module.ResolveSource(source, ".", logger)
	if err != nil {
		return nil, "", nil, err
	}
	if srcCleanup != nil {
		cleanups = append(cleanups, srcCleanup)
	}

	mod, err := module.Load(moduleDir, paramMap, logger)
	if err != nil {
		runCleanups()
		return nil, "", nil, err
	}

	var targetDir string
	if mod.Config.Spec.Target != nil {
		cloneDir, cloneCleanup, err := cloneTarget(ctx, mod, mod.Params, opts, logger)
		if err != nil {
			runCleanups()
			return nil, "", nil, err
		}
		if cloneCleanup != nil {
			cleanups = append(cleanups, cloneCleanup)
		}
		targetDir = cloneDir
	}
	if targetDir == "" && opts.TargetPath != "" {
		targetDir = opts.TargetPath
	}
	if targetDir == "" {
		targetDir = moduleDir
	}

	return mod, targetDir, runCleanups, nil
}
