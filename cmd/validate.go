package cmd

import (
	"os"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/engine"
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
	req := engine.ValidateRequest{
		Dir:       moduleDir,
		Recursive: validateRecursive,
		// Each warning prints as it is found, in step with any clone logs.
		OnWarning: func(f engine.Finding) { prettylog.Warningf(os.Stdout, "%s", f) },
	}
	if validateRecursive {
		req.Logger = newLogger()
	}
	res, err := engine.Validate(cmd.Context(), req)
	if err != nil {
		return err
	}

	if !validateRecursive {
		prettylog.Successf(os.Stdout, "module config in %s is valid", moduleDir)
		return nil
	}
	noun := "module configs"
	if res.Count == 1 {
		noun = "module config"
	}
	prettylog.Successf(os.Stdout, "%d %s valid, rooted at %s", res.Count, noun, moduleDir)
	return nil
}
