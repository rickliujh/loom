package cmd

import (
	"fmt"

	"github.com/rickliujh/loom/pkg/config"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate [path]",
	Short: "Validate a loom.yaml file",
	Long:  "Check that a loom.yaml file is syntactically and semantically valid.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  validateModule,
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

func validateModule(cmd *cobra.Command, args []string) error {
	moduleDir := "."
	if len(args) > 0 {
		moduleDir = args[0]
	}

	lf, err := config.Load(moduleDir)
	if err != nil {
		return err
	}

	if err := config.Validate(lf); err != nil {
		return err
	}

	fmt.Printf("loom.yaml in %s is valid\n", moduleDir)
	return nil
}
