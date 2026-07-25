// Package alias implements user-level shorthand names for module sources.
//
// An alias maps a short name to a module source plus a set of default
// parameters, so `loom :bar` stands in for
// `loom run git@github.com:foo/bar.git -p foo=bar`.
//
// Aliases are user configuration, not module configuration: they are resolved
// at the CLI boundary only. The module executor never consults them (AL8), so
// a module stays portable between operators regardless of what either has
// defined locally.
package alias

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Ref is the prefix marking an argument as an alias reference (AL4).
const Ref = ":"

// nameRe is the permitted alias name grammar (AL3). Excluding ":", "/", "=",
// and whitespace is what keeps a name unambiguous against a path, a git URL,
// and a key=value argument.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// Def is a single alias entry.
type Def struct {
	Source string            `yaml:"source"`
	Params map[string]string `yaml:"params,omitempty"`
}

// File is the parsed alias file.
type File struct {
	Aliases map[string]*Def `yaml:"aliases"`

	// path records where this file was loaded from, so errors can name it.
	path string
}

// Path returns the alias file location (AL1). LOOM_CONFIG_DIR overrides the
// user config dir, which keeps the path injectable for tests.
func Path() (string, error) {
	if dir := os.Getenv("LOOM_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "aliases.yaml"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config dir: %w", err)
	}
	return filepath.Join(dir, "loom", "aliases.yaml"), nil
}

// IsRef reports whether arg is an alias reference (AL4).
func IsRef(arg string) bool {
	return strings.HasPrefix(arg, Ref)
}

// ParseRef returns the alias name from a reference argument.
func ParseRef(arg string) (string, error) {
	name := strings.TrimPrefix(arg, Ref)
	if name == "" {
		return "", fmt.Errorf("empty alias name: expected :<name>")
	}
	return name, nil
}

// Load reads the alias file. A missing file yields an empty set, not an
// error (AL2) — only referencing a specific alias can fail on that.
func Load() (*File, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom reads the alias file at an explicit path.
func LoadFrom(path string) (*File, error) {
	f := &File{Aliases: map[string]*Def{}, path: path}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading alias file: %w", err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if f.Aliases == nil {
		f.Aliases = map[string]*Def{}
	}
	f.path = path

	// Validate on the way in so a malformed file fails at the file, not later
	// at a confusing clone error.
	for name, def := range f.Aliases {
		if !nameRe.MatchString(name) {
			return nil, fmt.Errorf("invalid alias name %q in %s: must match [a-zA-Z0-9][a-zA-Z0-9._-]*", name, path)
		}
		if def == nil || def.Source == "" {
			return nil, fmt.Errorf("alias %q in %s has no source", name, path)
		}
	}

	return f, nil
}

// Path returns where this file was loaded from.
func (f *File) Path() string { return f.path }

// Resolve looks up an alias by name. An unknown alias is an error rather than
// a fallthrough to the git-URL path (AL5).
func (f *File) Resolve(name string) (*Def, error) {
	def, ok := f.Aliases[name]
	if !ok {
		return nil, fmt.Errorf("unknown alias %q (looked in %s)", name, f.path)
	}
	return def, nil
}

// Names returns every alias name in sorted order.
func (f *File) Names() []string {
	names := make([]string, 0, len(f.Aliases))
	for name := range f.Aliases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Add inserts an alias. Without force, an existing name is an error so an
// alias is never silently replaced (AL10).
func (f *File) Add(name string, def *Def, force bool) error {
	name = strings.TrimPrefix(name, Ref)
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid alias name %q: must match [a-zA-Z0-9][a-zA-Z0-9._-]*", name)
	}
	if def.Source == "" {
		return fmt.Errorf("alias %q has no source", name)
	}
	if _, exists := f.Aliases[name]; exists && !force {
		return fmt.Errorf("alias %q already exists (use --force to replace)", name)
	}
	f.Aliases[name] = def
	return nil
}

// Remove deletes an alias (AL11).
func (f *File) Remove(name string) error {
	name = strings.TrimPrefix(name, Ref)
	if _, ok := f.Aliases[name]; !ok {
		return fmt.Errorf("unknown alias %q (looked in %s)", name, f.path)
	}
	delete(f.Aliases, name)
	return nil
}

// Save writes the alias file, creating its parent directory as needed. The
// write goes to a temp file in the same directory and is renamed into place so
// a failure can never leave a partially written alias file (AL13).
func (f *File) Save() error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("# loom aliases — managed by `loom alias`.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("encoding alias file: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encoding alias file: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".aliases-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return fmt.Errorf("writing alias file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing alias file: %w", err)
	}
	if err := os.Rename(tmpName, f.path); err != nil {
		return fmt.Errorf("writing alias file: %w", err)
	}
	return nil
}
