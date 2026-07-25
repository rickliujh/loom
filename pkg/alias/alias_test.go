package alias

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfigDir points the alias file at a temp dir for the duration of a test.
func withConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOOM_CONFIG_DIR", dir)
	return dir
}

func writeAliases(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "aliases.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// AL1: LOOM_CONFIG_DIR overrides the location; otherwise it is
// <user config dir>/loom/aliases.yaml.
func TestAlias_AL1_Path(t *testing.T) {
	dir := withConfigDir(t)
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "aliases.yaml"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}

	t.Setenv("LOOM_CONFIG_DIR", "")
	got, err = Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("loom", "aliases.yaml"); !strings.HasSuffix(got, want) {
		t.Errorf("Path() = %q, want suffix %q", got, want)
	}
}

// AL2: a missing alias file is an empty set, not an error.
func TestAlias_AL2_MissingFileIsEmpty(t *testing.T) {
	withConfigDir(t) // nothing written

	f, err := Load()
	if err != nil {
		t.Fatalf("missing alias file should not error, got %v", err)
	}
	if len(f.Names()) != 0 {
		t.Errorf("expected no aliases, got %v", f.Names())
	}
}

// AL3: names may not contain ":", "/", "=", or whitespace — the property that
// keeps them unambiguous against paths, git URLs, and key=value args.
func TestAlias_AL3_NameGrammar(t *testing.T) {
	withConfigDir(t)

	valid := []string{"bar", "my-module", "svc.onboard", "a_b", "x1"}
	invalid := []string{"", "-lead", "has space", "a/b", "a:b", "a=b", "git@x.com:y.git"}

	for _, name := range valid {
		f, _ := Load()
		if err := f.Add(name, &Def{Source: "./x"}, false); err != nil {
			t.Errorf("Add(%q) rejected a valid name: %v", name, err)
		}
	}
	for _, name := range invalid {
		f, _ := Load()
		if err := f.Add(name, &Def{Source: "./x"}, false); err == nil {
			t.Errorf("Add(%q) accepted an invalid name", name)
		}
	}

	// An invalid name in the file is rejected at load time, not at use time.
	dir := os.Getenv("LOOM_CONFIG_DIR")
	writeAliases(t, dir, "aliases:\n  \"a/b\":\n    source: ./x\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "invalid alias name") {
		t.Errorf("expected invalid-name load error, got %v", err)
	}
}

// AL4: a leading ":" marks an alias reference; a bare ":" is an error.
func TestAlias_AL4_RefSyntax(t *testing.T) {
	for _, arg := range []string{":bar", ":"} {
		if !IsRef(arg) {
			t.Errorf("IsRef(%q) = false, want true", arg)
		}
	}
	for _, arg := range []string{"bar", "./bar", "git@github.com:foo/bar.git", "https://x/y//sub"} {
		if IsRef(arg) {
			t.Errorf("IsRef(%q) = true, want false", arg)
		}
	}

	name, err := ParseRef(":bar")
	if err != nil || name != "bar" {
		t.Errorf("ParseRef(\":bar\") = (%q, %v), want (\"bar\", nil)", name, err)
	}
	if _, err := ParseRef(":"); err == nil {
		t.Error("ParseRef(\":\") should error on an empty name")
	}
}

// AL5: an unknown alias errors naming the file — it is never retried as a git
// URL, so a typo reports a typo rather than a failed clone.
func TestAlias_AL5_UnknownAlias(t *testing.T) {
	dir := withConfigDir(t)
	path := writeAliases(t, dir, "aliases:\n  bar:\n    source: ./bar\n")

	f, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Resolve("bra")
	if err == nil {
		t.Fatal("expected unknown-alias error")
	}
	if !strings.Contains(err.Error(), `unknown alias "bra"`) || !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the alias and the file, got %q", err)
	}
}

// AL10: add refuses to silently replace an existing alias.
func TestAlias_AL10_AddNoClobber(t *testing.T) {
	withConfigDir(t)

	f, _ := Load()
	if err := f.Add("bar", &Def{Source: "./one"}, false); err != nil {
		t.Fatal(err)
	}
	if err := f.Add("bar", &Def{Source: "./two"}, false); err == nil {
		t.Fatal("expected an already-exists error")
	}
	if f.Aliases["bar"].Source != "./one" {
		t.Errorf("source changed despite the error: %q", f.Aliases["bar"].Source)
	}
	if err := f.Add("bar", &Def{Source: "./two"}, true); err != nil {
		t.Fatalf("--force should replace: %v", err)
	}
	if f.Aliases["bar"].Source != "./two" {
		t.Errorf("force did not replace, got %q", f.Aliases["bar"].Source)
	}

	// A leading ":" on the name is accepted and normalized away.
	if err := f.Add(":colon", &Def{Source: "./x"}, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Aliases["colon"]; !ok {
		t.Errorf("name not normalized, got %v", f.Names())
	}
}

// AL11: remove deletes an entry; removing an unknown alias errors.
func TestAlias_AL11_Remove(t *testing.T) {
	dir := withConfigDir(t)
	writeAliases(t, dir, "aliases:\n  bar:\n    source: ./bar\n")

	f, _ := Load()
	if err := f.Remove("bar"); err != nil {
		t.Fatal(err)
	}
	if len(f.Names()) != 0 {
		t.Errorf("expected empty after remove, got %v", f.Names())
	}
	if err := f.Remove("bar"); err == nil {
		t.Error("removing an unknown alias should error")
	}
}

// AL13: a save round-trips through the file and leaves no temp files behind.
func TestAlias_AL13_SaveRoundTrip(t *testing.T) {
	dir := withConfigDir(t)

	f, _ := Load()
	if err := f.Add("bar", &Def{
		Source: "git@github.com:foo/bar.git//modules/svc",
		Params: map[string]string{"foo": "bar", "something": "anotherthing"},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	def, err := reloaded.Resolve("bar")
	if err != nil {
		t.Fatal(err)
	}
	if def.Source != "git@github.com:foo/bar.git//modules/svc" {
		t.Errorf("source did not round-trip: %q", def.Source)
	}
	if def.Params["foo"] != "bar" || def.Params["something"] != "anotherthing" {
		t.Errorf("params did not round-trip: %v", def.Params)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "aliases.yaml" {
			t.Errorf("save left a stray file behind: %s", e.Name())
		}
	}
}

// An entry without a source is rejected at load time rather than producing a
// confusing empty-source clone later.
func TestAlias_SourceRequired(t *testing.T) {
	dir := withConfigDir(t)
	writeAliases(t, dir, "aliases:\n  bar:\n    params:\n      a: b\n")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "has no source") {
		t.Errorf("expected a missing-source error, got %v", err)
	}
}

// Unknown fields in the alias file are rejected, so a typo is reported at the
// field rather than silently ignored.
func TestAlias_UnknownFieldRejected(t *testing.T) {
	dir := withConfigDir(t)
	writeAliases(t, dir, "aliases:\n  bar:\n    source: ./bar\n    sorce: ./typo\n")

	if _, err := Load(); err == nil {
		t.Error("expected an unknown-field error")
	}
}
