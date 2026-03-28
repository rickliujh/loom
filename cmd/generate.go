package cmd

import (
	"github.com/rickliujh/loom/pkg/generate"
	"github.com/spf13/cobra"
)

var (
	genParams   []string
	genOutput   string
	genName     string
	genTokenEnv string
)

var generateCmd = &cobra.Command{
	Use:   "generate <pr-url>",
	Short: "Generate a loom module from a GitHub PR or GitLab MR",
	Long: `Generate a reusable loom module by analyzing the file changes in an existing
pull request or merge request. Added files become templates, modified YAML files
become strategic merge patches, and concrete values you specify with -p are
replaced with template parameters.

By default, files are generated into the current directory. Use -o to specify
a different output directory.

Supported references:
  https://github.com/owner/repo/pull/123
  https://gitlab.com/group/repo/-/merge_requests/123
  github:owner/repo#123
  gitlab:group/repo!123`,
	Args: cobra.ExactArgs(1),
	RunE: runGenerate,
}

func init() {
	generateCmd.Flags().StringArrayVarP(&genParams, "param", "p", nil, "Value to parameterize: key=value (can be repeated)")
	generateCmd.Flags().StringVarP(&genOutput, "output", "o", "", "Output directory (default: current directory)")
	generateCmd.Flags().StringVarP(&genName, "name", "n", "", "Module name (default: derived from PR title)")
	generateCmd.Flags().StringVar(&genTokenEnv, "token-env", "", "Env var holding the API token (default: GITHUB_TOKEN or GITLAB_TOKEN)")
	rootCmd.AddCommand(generateCmd)
}

func runGenerate(cmd *cobra.Command, args []string) error {
	logger := newLogger()

	paramMap, err := parseParams(genParams, "")
	if err != nil {
		return err
	}

	opts := generate.Options{
		Ref:        args[0],
		Params:     paramMap,
		OutputDir:  genOutput,
		ModuleName: genName,
		TokenEnv:   genTokenEnv,
	}

	return generate.Run(cmd.Context(), opts, logger)
}
