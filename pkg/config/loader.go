package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/go-jsonnet"
	"gopkg.in/yaml.v3"
)

// Load reads and parses the module config from the given module directory.
// A module is defined by exactly one of loom.yaml or loom.jsonnet. A jsonnet
// config is evaluated first (imports resolve relative to the module directory)
// and must produce an object with the same schema as loom.yaml.
func Load(moduleDir string) (*LoomFile, error) {
	yamlPath := filepath.Join(moduleDir, "loom.yaml")
	jsonnetPath := filepath.Join(moduleDir, "loom.jsonnet")

	yamlExists := fileExists(yamlPath)
	jsonnetExists := fileExists(jsonnetPath)

	switch {
	case yamlExists && jsonnetExists:
		return nil, fmt.Errorf("both loom.yaml and loom.jsonnet found in %s: a module must have exactly one config file", moduleDir)
	case jsonnetExists:
		return loadJsonnet(jsonnetPath)
	default:
		return loadYAML(yamlPath)
	}
}

func loadYAML(path string) (*LoomFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading loom.yaml: %w", err)
	}

	var lf LoomFile
	if err := strictUnmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing loom.yaml: %w", err)
	}

	return &lf, nil
}

func loadJsonnet(path string) (*LoomFile, error) {
	vm := jsonnet.MakeVM()
	vm.Importer(&jsonnet.FileImporter{JPaths: []string{filepath.Dir(path)}})

	jsonStr, err := vm.EvaluateFile(path)
	if err != nil {
		return nil, fmt.Errorf("evaluating loom.jsonnet: %w", err)
	}

	// JSON is a subset of YAML, so the evaluated output unmarshals through
	// the same yaml tags as loom.yaml.
	var lf LoomFile
	if err := strictUnmarshal([]byte(jsonStr), &lf); err != nil {
		return nil, fmt.Errorf("parsing loom.jsonnet output: %w", err)
	}

	return &lf, nil
}

// strictUnmarshal decodes YAML into lf, rejecting fields that don't exist in
// the schema so typos surface as parse errors instead of being silently dropped.
func strictUnmarshal(data []byte, lf *LoomFile) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(lf); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
