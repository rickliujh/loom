package cmd

import (
	"fmt"
	"io/fs"

	"github.com/rickliujh/loom/internal/skill"
	"github.com/spf13/cobra"
)

// Docs is the documentation tree served by `loom skill`: docs/guide,
// docs/reference and specs, as they sit in the repository. main embeds it and
// sets it before Execute — go:embed can only reach files beneath the package
// that declares it, and the documentation lives at the repository root.
var Docs fs.FS

var skillCmd = &cobra.Command{
	Use:   "skill [topic]",
	Short: "Print the guide for AI agents working with loom",
	Long: `Print loom's guide for AI agents, straight from the binary.

Nothing needs to be installed and nothing is fetched: the documentation is
embedded in loom itself, so what an agent reads always matches the version it
is about to run.

  loom skill            the agent guide, followed by the list of topics
  loom skill list       the topics alone
  loom skill <topic>    one topic, e.g. "reference/op-patch" or just "op-patch"

An agent should run "loom skill" before authoring, validating or running a
module, and read further topics as the task calls for them.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSkill,
}

func init() {
	rootCmd.AddCommand(skillCmd)
}

func runSkill(cmd *cobra.Command, args []string) error {
	if Docs == nil {
		return fmt.Errorf("this build of loom does not include its documentation")
	}
	lib, err := skill.Load(Docs)
	if err != nil {
		return err
	}

	var out string
	switch {
	case len(args) == 0:
		out, err = lib.Guide(resolveVersion())
	case args[0] == "list":
		out = lib.Index("")
	default:
		out, err = lib.Read(args[0])
	}
	if err != nil {
		return err
	}

	// stdout, and nothing but the document: the output is meant to be read
	// into a model's context as is.
	_, err = fmt.Fprintln(cmd.OutOrStdout(), trimTrailingNewlines(out))
	return err
}

func trimTrailingNewlines(s string) string {
	for len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	return s
}
