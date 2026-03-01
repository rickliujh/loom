package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads and parses a loom.yaml file from the given module directory.
func Load(moduleDir string) (*LoomFile, error) {
	path := filepath.Join(moduleDir, "loom.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading loom.yaml: %w", err)
	}

	var lf LoomFile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing loom.yaml: %w", err)
	}

	return &lf, nil
}
