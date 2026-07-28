package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/config"
	"github.com/rickliujh/loom/pkg/module"
	"github.com/spf13/cobra"
)

var validateRecursive bool

var validateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "Validate a module config (loom.yaml or loom.jsonnet)",
	Long:  "Check that a module's loom.yaml or loom.jsonnet is syntactically and semantically valid.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  validateModule,
}

func init() {
	validateCmd.Flags().BoolVarP(&validateRecursive, "recursive", "r", false,
		"Also validate the modules referenced by spec.modules, and theirs in turn")
	rootCmd.AddCommand(validateCmd)
}

func validateModule(cmd *cobra.Command, args []string) error {
	moduleDir := "."
	if len(args) > 0 {
		moduleDir = args[0]
	}

	// By default only the module named on the command line is checked. A
	// referenced module is a separate config that a run resolves on its own —
	// fetching one may mean a clone, and it may be perfectly valid while being
	// none of this module's business.
	if !validateRecursive {
		if err := validateOne(moduleDir, ""); err != nil {
			return err
		}
		prettylog.Successf(os.Stdout, "module config in %s is valid", moduleDir)
		return nil
	}

	// visited is keyed by resolved directory, so a module reached twice — a
	// shared library, or a cycle — is checked once.
	visited := map[string]bool{}
	n, err := validateTree(moduleDir, "", visited)
	if err != nil {
		return err
	}
	noun := "module configs"
	if n == 1 {
		noun = "module config"
	}
	prettylog.Successf(os.Stdout, "%d %s valid, rooted at %s", n, noun, moduleDir)
	return nil
}

// validateOne loads and validates a single module directory, printing any
// warnings it produced. label names the module in output when it was reached
// from a parent, and is empty for the module named on the command line.
func validateOne(moduleDir, label string) error {
	lf, err := config.Load(moduleDir)
	if err != nil {
		return prefixed(label, err)
	}

	warnings, err := config.ValidateInDirWithWarnings(lf, moduleDir)
	// Warnings print even when the config is invalid: both are findings about
	// the same config, and reporting them together keeps one pass enough to
	// fix everything.
	for _, w := range warnings {
		if label != "" {
			w = label + ": " + w
		}
		prettylog.Warningf(os.Stdout, "%s", w)
	}
	return prefixed(label, err)
}

// validateTree validates moduleDir and every module it references, returning
// the number of configs checked. A templated source is skipped with a warning
// rather than failing the command — it has no value until a run resolves its
// params, so there is nothing to fetch yet.
func validateTree(moduleDir, label string, visited map[string]bool) (int, error) {
	abs, err := filepath.Abs(moduleDir)
	if err != nil {
		abs = moduleDir
	}
	if visited[abs] {
		return 0, nil
	}
	visited[abs] = true

	if err := validateOne(moduleDir, label); err != nil {
		return 0, err
	}
	count := 1

	lf, err := config.Load(moduleDir)
	if err != nil {
		return count, prefixed(label, err)
	}

	logger := newLogger()
	var errs []error
	for _, ref := range lf.Spec.Modules {
		childLabel := ref.Name
		if label != "" {
			childLabel = label + " › " + ref.Name
		}
		if strings.Contains(ref.Source, "{{") {
			prettylog.Warningf(os.Stdout, "%s: source %q is templated, not checked — its value is only known at run time",
				childLabel, ref.Source)
			continue
		}

		childDir, cleanup, err := module.ResolveSource(ref.Source, moduleDir, logger)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", childLabel, err))
			continue
		}
		n, err := validateTree(childDir, childLabel, visited)
		if cleanup != nil {
			cleanup()
		}
		count += n
		if err != nil {
			errs = append(errs, err)
		}
	}
	return count, errors.Join(errs...)
}

// prefixed names the module a nested failure came from, so a violation deep in
// a tree says which config to open.
func prefixed(label string, err error) error {
	if err == nil || label == "" {
		return err
	}
	return fmt.Errorf("%s: %w", label, err)
}
