package cmd

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickliujh/loom/pkg/module"
)

// aliasConfigDir points the alias file at a temp dir for the duration of a test.
func aliasConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOOM_CONFIG_DIR", dir)
	return dir
}

func writeAliasFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "aliases.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newFilesModule writes a module that copies templates/ into the target dir,
// naming the emitted file after the "greeting" param so a test can read back
// which value won.
func newFilesModule(t *testing.T, dir string) {
	t.Helper()
	srcDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "out.txt"), []byte("{{ .greeting }}"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLoomYAML(t, dir, `
apiVersion: loom.rickliujh.github.io/v1beta1
kind: Loom
metadata:
  name: alias-test
spec:
  params:
    - name: greeting
      default: from-module
  operations:
    - name: write-files
      newFiles:
        source: "templates"
        dest: ""
`)
}

// AL6: `loom :bar` rewrites to `loom run :bar`; anything else is untouched, so
// unknown subcommands still reach cobra's own dispatch.
func TestAlias_AL6_DispatchArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"bare alias defaults to run", []string{":bar"}, []string{"run", ":bar"}},
		{"alias keeps trailing flags", []string{":bar", "-p", "a=b"}, []string{"run", ":bar", "-p", "a=b"}},
		{"explicit run is untouched", []string{"run", ":bar"}, []string{"run", ":bar"}},
		{"other subcommands untouched", []string{"diff", ":bar"}, []string{"diff", ":bar"}},
		{"typo untouched", []string{"rnu"}, []string{"rnu"}},
		{"path untouched", []string{"run", "./mod"}, []string{"run", "./mod"}},
		{"no args", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dispatchArgs(tt.in)
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("dispatchArgs(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// AL6: a typo is still reported as an unknown command rather than swallowed by
// alias handling — the property the ":" sigil buys.
func TestAlias_AL6_TypoIsUnknownCommand(t *testing.T) {
	resetFlags()
	rootCmd.SetArgs(dispatchArgs([]string{"rnu"}))
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected an unknown-command error, got %v", err)
	}
}

// AL7: `loom run :bar` resolves the alias to its source and runs it.
func TestAlias_AL7_RunAcceptsAliasRef(t *testing.T) {
	resetFlags()
	cfgDir := aliasConfigDir(t)

	moduleDir := t.TempDir()
	targetDir := t.TempDir()
	newFilesModule(t, moduleDir)
	writeAliasFile(t, cfgDir, "aliases:\n  bar:\n    source: "+moduleDir+"\n")

	rootCmd.SetArgs(dispatchArgs([]string{":bar", "--target-path", targetDir}))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "out.txt")); err != nil {
		t.Error("expected the aliased module to run and write out.txt")
	}
}

// AL5/AL7: an unknown alias reports the alias, and never falls through to a
// clone attempt.
func TestAlias_AL5_RunUnknownAliasErrors(t *testing.T) {
	resetFlags()
	aliasConfigDir(t)

	rootCmd.SetArgs(dispatchArgs([]string{":nope", "--target-path", t.TempDir()}))
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), `unknown alias "nope"`) {
		t.Errorf("expected an unknown-alias error, got %q", err)
	}
	if strings.Contains(err.Error(), "cloning") {
		t.Errorf("alias miss fell through to a clone: %q", err)
	}
}

// AL8: the module executor does not consult the alias file, so a module's
// child source can never depend on the operator's local aliases.
func TestAlias_AL8_ExecutorDoesNotResolveAliases(t *testing.T) {
	cfgDir := aliasConfigDir(t)
	moduleDir := t.TempDir()
	newFilesModule(t, moduleDir)
	writeAliasFile(t, cfgDir, "aliases:\n  bar:\n    source: "+moduleDir+"\n")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	_, cleanup, err := module.ResolveSource(":bar", ".", logger)
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("ResolveSource should not resolve alias references")
	}
	if strings.Contains(err.Error(), "unknown alias") {
		t.Errorf("ResolveSource consulted the alias file: %q", err)
	}
}

// AL9: alias params sit beneath --params-file and -p.
func TestAlias_AL9_ParamPrecedence(t *testing.T) {
	cfgDir := aliasConfigDir(t)
	paramsFile := filepath.Join(t.TempDir(), "params.yaml")
	if err := os.WriteFile(paramsFile, []byte("greeting: from-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeAliasFile(t, cfgDir, "aliases:\n  bar:\n    source: ./x\n    params:\n      greeting: from-alias\n")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"alias only", nil, "from-alias"},
		{"params-file beats alias", []string{"--params-file", paramsFile}, "from-file"},
		{"-p beats alias", []string{"-p", "greeting=from-flag"}, "from-flag"},
		{"-p beats params-file and alias", []string{"--params-file", paramsFile, "-p", "greeting=from-flag"}, "from-flag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetFlags()
			moduleDir := t.TempDir()
			targetDir := t.TempDir()
			newFilesModule(t, moduleDir)
			writeAliasFile(t, cfgDir, "aliases:\n  bar:\n    source: "+moduleDir+"\n    params:\n      greeting: from-alias\n")

			args := append([]string{":bar", "--target-path", targetDir}, tt.args...)
			rootCmd.SetArgs(dispatchArgs(args))
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got, err := os.ReadFile(filepath.Join(targetDir, "out.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("greeting = %q, want %q", got, tt.want)
			}
		})
	}
}

// AL9: a module default still applies when no alias param supplies a value.
func TestAlias_AL9_ModuleDefaultBeneathAlias(t *testing.T) {
	resetFlags()
	cfgDir := aliasConfigDir(t)

	moduleDir := t.TempDir()
	targetDir := t.TempDir()
	newFilesModule(t, moduleDir)
	writeAliasFile(t, cfgDir, "aliases:\n  bar:\n    source: "+moduleDir+"\n")

	rootCmd.SetArgs(dispatchArgs([]string{":bar", "--target-path", targetDir}))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(targetDir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "from-module" {
		t.Errorf("greeting = %q, want %q", got, "from-module")
	}
}

// AL12: list reports every alias sorted by name, with source and params.
func TestAlias_AL12_List(t *testing.T) {
	resetFlags()
	cfgDir := aliasConfigDir(t)
	writeAliasFile(t, cfgDir, `aliases:
  zeta:
    source: ./z
  bar:
    source: git@github.com:foo/bar.git
    params:
      foo: bar
`)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"alias", "list"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, ":bar") || !strings.Contains(out, "git@github.com:foo/bar.git") {
		t.Errorf("list omitted the bar alias:\n%s", out)
	}
	if !strings.Contains(out, "foo=bar") {
		t.Errorf("list omitted params:\n%s", out)
	}
	if strings.Index(out, ":bar") > strings.Index(out, ":zeta") {
		t.Errorf("list is not sorted by name:\n%s", out)
	}
}

// AL10/AL12: add writes an entry that list then reports and run can resolve.
func TestAlias_AL10_AddThenList(t *testing.T) {
	resetFlags()
	aliasConfigDir(t)

	rootCmd.SetArgs([]string{"alias", "add", "bar", "git@github.com:foo/bar.git", "-p", "foo=bar"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"alias", "list"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, ":bar") || !strings.Contains(out, "foo=bar") {
		t.Errorf("added alias not listed:\n%s", out)
	}

	// A second add without --force is refused.
	rootCmd.SetArgs([]string{"alias", "add", "bar", "./other"})
	if err := rootCmd.Execute(); err == nil {
		t.Error("expected an already-exists error")
	}
}

// AL11: remove deletes the entry through the CLI.
func TestAlias_AL11_RemoveCmd(t *testing.T) {
	resetFlags()
	cfgDir := aliasConfigDir(t)
	writeAliasFile(t, cfgDir, "aliases:\n  bar:\n    source: ./bar\n")

	rootCmd.SetArgs([]string{"alias", "remove", "bar"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"alias", "list"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if strings.Contains(out, ":bar") {
		t.Errorf("alias still listed after remove:\n%s", out)
	}
}
