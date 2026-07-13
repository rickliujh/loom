package cmd

import (
	"github.com/rickliujh/loom/pkg/bulk"
	"github.com/spf13/cobra"
)

var (
	bulkOutput    string
	bulkName      string
	bulkItems     string
	bulkNameParam string
)

var bulkCmd = &cobra.Command{
	Use:   "bulk <module>",
	Short: "Scaffold a bulk wrapper module from an existing module",
	Long: `Generate a loom.jsonnet wrapper that runs an existing module once per entry
in an items list. The wrapper's child entries are derived from the module's
declared parameters — edit the items list (or seed it with --items), then
execute the whole batch with a single loom run.

The module source may be a local path (./my-module) or a git URL, the same
forms accepted by loom run.`,
	Args: cobra.ExactArgs(1),
	RunE: runBulk,
}

func init() {
	bulkCmd.Flags().StringVarP(&bulkOutput, "output", "o", "", "Output directory (default: current directory)")
	bulkCmd.Flags().StringVarP(&bulkName, "name", "n", "", "Wrapper module name (default: bulk-<moduleName>)")
	bulkCmd.Flags().StringVar(&bulkItems, "items", "", "YAML file with a list of param sets to seed the items list")
	bulkCmd.Flags().StringVar(&bulkNameParam, "name-param", "", "Child param whose value names each entry (default: item index)")
	rootCmd.AddCommand(bulkCmd)
}

func runBulk(cmd *cobra.Command, args []string) error {
	logger := newLogger()

	opts := bulk.Options{
		ModuleRef: args[0],
		OutputDir: bulkOutput,
		Name:      bulkName,
		ItemsFile: bulkItems,
		NameParam: bulkNameParam,
	}

	return bulk.Run(opts, logger)
}
