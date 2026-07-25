package cmd

import (
	"fmt"
	"os"
	"strings"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/rickliujh/loom/pkg/alias"
	"github.com/spf13/cobra"
)

var (
	aliasParams []string
	aliasForce  bool
)

var aliasCmd = &cobra.Command{
	Use:   "alias",
	Short: "Manage shorthand names for module sources",
	Long: `Manage user-level aliases for module sources.

An alias maps a short name to a module source and a set of default parameters,
so a long invocation:

  loom run git@github.com:foo/bar.git -p foo=bar -p something=anotherthing

becomes:

  loom :bar

Aliases are stored per-user and are never read from a repository, so switching
branches can never change what an alias resolves to.`,
}

var aliasAddCmd = &cobra.Command{
	Use:   "add <name> <source>",
	Short: "Create an alias for a module source",
	Long: `Create an alias for a module source.

The source accepts the same forms as ` + "`loom run`" + `: a local path, a git URL, or a
git URL with the //subdir suffix. Parameters given with -p become the alias's
default parameters, overridable at run time.`,
	Example: `  loom alias add bar git@github.com:foo/bar.git//modules/svc -p foo=bar
  loom :bar`,
	Args: cobra.ExactArgs(2),
	RunE: runAliasAdd,
}

var aliasRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm"},
	Short:   "Delete an alias",
	Args:    cobra.ExactArgs(1),
	RunE:    runAliasRemove,
}

var aliasListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List defined aliases",
	Args:    cobra.NoArgs,
	RunE:    runAliasList,
}

func init() {
	aliasAddCmd.Flags().StringArrayVarP(&aliasParams, "param", "p", nil, "Default parameter in key=value format (can be repeated)")
	aliasAddCmd.Flags().BoolVar(&aliasForce, "force", false, "Replace an existing alias of the same name")
	aliasCmd.AddCommand(aliasAddCmd, aliasRemoveCmd, aliasListCmd)
	rootCmd.AddCommand(aliasCmd)
}

func runAliasAdd(cmd *cobra.Command, args []string) error {
	name, source := args[0], args[1]

	params, err := parseParams(aliasParams, "")
	if err != nil {
		return err
	}

	f, err := alias.Load()
	if err != nil {
		return err
	}
	def := &alias.Def{Source: source}
	if len(params) > 0 {
		def.Params = params
	}
	if err := f.Add(name, def, aliasForce); err != nil {
		return err
	}
	if err := f.Save(); err != nil {
		return err
	}

	prettylog.Successf(os.Stderr, "alias %q → %s", strings.TrimPrefix(name, alias.Ref), source)
	fmt.Fprintf(os.Stderr, "  run it with: loom :%s\n", strings.TrimPrefix(name, alias.Ref))
	return nil
}

func runAliasRemove(cmd *cobra.Command, args []string) error {
	f, err := alias.Load()
	if err != nil {
		return err
	}
	if err := f.Remove(args[0]); err != nil {
		return err
	}
	if err := f.Save(); err != nil {
		return err
	}
	prettylog.Successf(os.Stderr, "removed alias %q", strings.TrimPrefix(args[0], alias.Ref))
	return nil
}

func runAliasList(cmd *cobra.Command, args []string) error {
	f, err := alias.Load()
	if err != nil {
		return err
	}

	names := f.Names()
	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "no aliases defined (%s)\n", f.Path())
		fmt.Fprintln(os.Stderr, "add one with: loom alias add <name> <source>")
		return nil
	}

	out := cmd.OutOrStdout()
	for _, name := range names {
		def := f.Aliases[name]
		fmt.Fprintf(out, ":%s\n", name)
		fmt.Fprintf(out, "  source: %s\n", def.Source)
		for _, k := range sortedKeys(def.Params) {
			fmt.Fprintf(out, "  param:  %s=%s\n", k, def.Params[k])
		}
	}
	return nil
}
