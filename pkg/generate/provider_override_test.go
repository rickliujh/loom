package generate

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickliujh/loom/pkg/config"
)

type stubProvider struct{ ref string }

func (s *stubProvider) FetchDiff(_ context.Context, ref, _ string, _ *slog.Logger) (*PRInfo, error) {
	s.ref = ref
	return &PRInfo{Title: "Add app", Files: []FileChange{{Type: ChangeAdded, Path: "app.yaml", NewContent: []byte("a: 1\n")}}}, nil
}

// Options.Provider fetches in place of the provider the ref names.
func TestRun_ProviderOverride(t *testing.T) {
	out := t.TempDir()
	stub := &stubProvider{}
	if err := Run(context.Background(), Options{Ref: "github:o/r#5", OutputDir: out, Provider: stub}, testLogger()); err != nil {
		t.Fatal(err)
	}
	if stub.ref != "github:o/r#5" {
		t.Errorf("provider got ref %q", stub.ref)
	}
	lf, err := config.Load(out)
	if err != nil || lf.Metadata.Name != "add-app" {
		t.Errorf("generated %+v, %v", lf, err)
	}
	if !fileExistsT(filepath.Join(out, "app.yaml")) {
		t.Error("template not written")
	}
}

func fileExistsT(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
