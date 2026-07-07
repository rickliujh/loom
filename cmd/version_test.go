package cmd

import "testing"

func TestResolveVersion_LdflagsWins(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()

	Version = "v1.2.3"
	if got := resolveVersion(); got != "v1.2.3" {
		t.Errorf("expected ldflags version, got %q", got)
	}
}

func TestResolveVersion_DevFallback(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()

	// Under `go test` the main module version is "(devel)", so the
	// fallback chain ends at the default.
	Version = "dev"
	if got := resolveVersion(); got != "dev" {
		t.Errorf("expected dev fallback, got %q", got)
	}
}
