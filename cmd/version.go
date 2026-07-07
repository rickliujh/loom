package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version is set via ldflags at build time (release builds). When left at
// its default (e.g. installed via `go install`), the semver module version
// embedded by the Go toolchain is used instead.
var Version = "dev"

// resolveVersion returns the ldflags-injected version, falling back to the
// module version from build info for `go install` builds.
func resolveVersion() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return Version
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of loom",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("loom version %s\n", resolveVersion())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
