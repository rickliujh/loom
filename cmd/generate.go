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
	genInclude  []string
	genExclude  []string
	genBase     string
)

var generateCmd = &cobra.Command{
	Use:   "generate <ref> [<ref>...]",
	Short: "Generate a loom module from PRs/MRs, commits, or local files",
	Long: `Generate a reusable loom module by analyzing file changes from one or more
sources. Added files become templates, modified YAML files become strategic
merge patches, and concrete values you specify with -p are replaced with
template parameters.

When multiple references are given they are composed in order (oldest first)
into a single net changeset — useful when the desired state was reached over
several PRs or follow-up commits.

Supported references:
  PR/MR (provider API):
    https://github.com/owner/repo/pull/123
    https://gitlab.com/group/repo/-/merge_requests/123
    github:owner/repo#123
    gitlab:group/repo!123
  Commit or commit range (git-native, any host):
    github:owner/repo@abc1234
    github:owner/repo@abc1234...def5678
    git@bitbucket.org:owner/repo.git@abc1234...def5678
    https://github.com/owner/repo/commit/abc1234
    ./checkout@abc1234...def5678
  Current state of local files (with --include, optionally --base):
    ./checkout    /abs/path    file:relative/path

By default, files are generated into the current directory. Use -o to specify
a different output directory.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runGenerate,
}

func init() {
	generateCmd.Flags().StringArrayVarP(&genParams, "param", "p", nil, "Value to parameterize: key=value (can be repeated)")
	generateCmd.Flags().StringVarP(&genOutput, "output", "o", "", "Output directory (default: current directory)")
	generateCmd.Flags().StringVarP(&genName, "name", "n", "", "Module name (default: derived from PR title or commit subject)")
	generateCmd.Flags().StringVar(&genTokenEnv, "token-env", "", "Env var holding the API token for PR/MR sources (default: GITHUB_TOKEN or GITLAB_TOKEN)")
	generateCmd.Flags().StringArrayVar(&genInclude, "include", nil, "Glob of files to capture from a local path source (can be repeated; ** matches directories)")
	generateCmd.Flags().StringArrayVar(&genExclude, "exclude", nil, "Glob of files to skip from a local path source (can be repeated)")
	generateCmd.Flags().StringVar(&genBase, "base", "", "Git ref to diff a local path source against (default: capture files as-is)")
	rootCmd.AddCommand(generateCmd)
}

func runGenerate(cmd *cobra.Command, args []string) error {
	logger := newLogger()

	paramMap, err := parseParams(genParams, "")
	if err != nil {
		return err
	}

	opts := generate.Options{
		Refs:       args,
		Params:     paramMap,
		OutputDir:  genOutput,
		ModuleName: genName,
		TokenEnv:   genTokenEnv,
		Include:    genInclude,
		Exclude:    genExclude,
		Base:       genBase,
	}

	return generate.Run(cmd.Context(), opts, logger)
}
