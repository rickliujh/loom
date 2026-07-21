package cmd

import (
	"log/slog"
	"os"

	prettylog "github.com/rickliujh/loom/internal/log"
	"github.com/spf13/cobra"
)

var (
	verbose   bool
	dryRun    bool
	localRun bool
	showDiff  bool
	logLevel  string
	logFormat string
)

var rootCmd = &cobra.Command{
	Use:   "loom",
	Short: "Loom automates the last mile of your GitOps",
	Long:  "Loom is a CLI tool that automates GitOps workflows via declarative YAML modules.",
	// Silence cobra's own usage/error output so the top-level error is printed
	// once, in our own colored style, rather than as a plain "Error: …" line.
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		prettylog.Failuref(os.Stderr, "%v", err)
		os.Exit(1)
	}
}

func init() {
	// SilenceUsage suppresses help text on runtime errors; still show it for flag misuse.
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		cmd.PrintErrln(cmd.UsageString())
		return err
	})
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Simulate operations without making changes")
	rootCmd.PersistentFlags().BoolVar(&localRun, "local-run", false, "Run all operations locally but skip remote push and PR creation")
	rootCmd.PersistentFlags().BoolVar(&showDiff, "diff", false, "Show file diffs during dry-run (implies --dry-run)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "pretty", "Log format (pretty, text, json)")
}

// newLogger creates a structured logger based on CLI flags.
func newLogger() *slog.Logger {
	var level slog.Level
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	if verbose {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch logFormat {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, opts)
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	default:
		handler = prettylog.NewPrettyHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}
