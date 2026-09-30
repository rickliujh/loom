package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/rickliujh/loom/pkg/config"
)

// Discovery limits. A root is usually a checkout of a GitOps repo; these keep
// a root pointed at a home directory from walking forever.
const (
	discoveryMaxDepth      = 8
	discoveryMaxDirs       = 5000
	discoveryMaxModules    = 2000
	discoveryConfigYAML    = "loom.yaml"
	discoveryConfigJsonnet = "loom.jsonnet"
)

// moduleEntry is one discovered module.
type moduleEntry struct {
	Dir       string      `json:"dir"`
	Rel       string      `json:"rel"`
	Root      string      `json:"root"`
	Name      string      `json:"name"`
	Format    string      `json:"format"`
	Params    paramCounts `json:"params"`
	HasTarget bool        `json:"hasTarget"`
	Children  int         `json:"children"`
	LoadError string      `json:"loadError"`
}

type paramCounts struct {
	// Required counts the params a run must be given: required and without
	// a default.
	Required int `json:"required"`
	// Total counts every declared param, static and dynamic.
	Total int `json:"total"`
}

// discovery caches the module list until a refresh.
type discovery struct {
	mu        sync.Mutex
	modules   []moduleEntry
	truncated bool
	loaded    bool
}

// list returns the modules under the roots, walking them on first use or
// when refresh is set.
func (s *Server) listModules(refresh bool) ([]moduleEntry, bool) {
	d := &s.discovery
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.loaded || refresh {
		d.modules, d.truncated = discover(s.roots)
		d.loaded = true
	}
	return append([]moduleEntry(nil), d.modules...), d.truncated
}

// invalidateModules makes the next listing walk again.
func (s *Server) invalidateModules() {
	s.discovery.mu.Lock()
	s.discovery.loaded = false
	s.discovery.mu.Unlock()
}

// skipDir reports the directories discovery never enters: git internals,
// every other dot-directory, dependency trees, and the helper files a module
// keeps beside its templates.
func skipDir(name string) bool {
	switch name {
	case "node_modules", "vendor", "__functions":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// discover walks each root for directories holding a module config. It
// reads configs with config.Load only — jsonnet is evaluated, nothing is
// executed — and keeps descending below a module, since child modules nest.
// WalkDir does not follow symlinks.
func discover(roots []string) ([]moduleEntry, bool) {
	var out []moduleEntry
	truncated := false
	for _, root := range roots {
		dirs := 0
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() && p != root {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if p != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(root, p)
			if rel != "." && strings.Count(filepath.ToSlash(rel), "/")+1 > discoveryMaxDepth {
				return filepath.SkipDir
			}
			dirs++
			if dirs > discoveryMaxDirs || len(out) >= discoveryMaxModules {
				truncated = true
				return filepath.SkipAll
			}
			if e, ok := describeModuleDir(root, p); ok {
				out = append(out, e)
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, truncated
}

// describeModuleDir is the discovery entry for dir, if it holds a config.
func describeModuleDir(root, dir string) (moduleEntry, bool) {
	hasYAML := isFile(filepath.Join(dir, discoveryConfigYAML))
	hasJsonnet := isFile(filepath.Join(dir, discoveryConfigJsonnet))
	if !hasYAML && !hasJsonnet {
		return moduleEntry{}, false
	}
	rel, _ := filepath.Rel(root, dir)
	e := moduleEntry{Dir: dir, Rel: filepath.ToSlash(rel), Root: root, Format: "yaml"}
	if hasJsonnet && !hasYAML {
		e.Format = "jsonnet"
	}
	lf, err := config.Load(dir)
	if err != nil {
		e.LoadError = redact(err.Error())
		return e, true
	}
	e.Name = lf.Metadata.Name
	for _, p := range lf.Spec.Params {
		if p.Required && !p.HasDefault() {
			e.Params.Required++
		}
	}
	e.Params.Total = len(lf.Spec.Params) + len(lf.Spec.DynamicParams)
	e.HasTarget = lf.Spec.Target != nil
	e.Children = len(lf.Spec.Modules)
	return e, true
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}
