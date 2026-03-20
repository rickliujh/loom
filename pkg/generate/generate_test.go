package generate

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/rickliujh/loom/pkg/config"
)

func TestBuildModule_AddedFiles(t *testing.T) {
	pr := &PRInfo{
		Title:      "Onboard payments",
		BaseBranch: "main",
		HeadBranch: "feat/payments",
		Provider:   "github",
		Files: []FileChange{
			{
				Type:       ChangeAdded,
				Path:       "argocd/application-payments.yaml",
				NewContent: []byte("name: payments\nnamespace: fintech"),
			},
		},
	}

	params := map[string]string{
		"serviceName": "payments",
		"namespace":   "fintech",
	}

	mod := buildModule(pr, "test-module", params, false, testLogger())

	// Check params are declared.
	if len(mod.loomFile.Spec.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(mod.loomFile.Spec.Params))
	}

	// Check template file was parameterized.
	content, ok := mod.templateFiles["argocd/application-{{ .serviceName }}.yaml"]
	if !ok {
		t.Fatal("expected parameterized template file path")
	}
	if string(content) != "name: {{ .serviceName }}\nnamespace: {{ .namespace }}" {
		t.Errorf("unexpected content: %s", string(content))
	}

	// Check newFiles operation.
	if len(mod.loomFile.Spec.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(mod.loomFile.Spec.Operations))
	}
	op := mod.loomFile.Spec.Operations[0]
	if op.NewFiles == nil {
		t.Fatal("expected newFiles operation")
	}
}

func TestBuildModule_ModifiedYAML(t *testing.T) {
	pr := &PRInfo{
		Title:    "Update config",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeModified,
				Path:       "config/app.yaml",
				OldContent: []byte("name: old\nreplicas: 1"),
				NewContent: []byte("name: old\nreplicas: 3"),
			},
		},
	}

	mod := buildModule(pr, "test", nil, false, testLogger())

	// Should have a patch operation.
	if len(mod.loomFile.Spec.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(mod.loomFile.Spec.Operations))
	}
	op := mod.loomFile.Spec.Operations[0]
	if op.Patch == nil {
		t.Fatal("expected patch operation")
	}
	if op.Patch.Engine != "smp" {
		t.Errorf("expected smp engine, got %s", op.Patch.Engine)
	}

	// Check patch content.
	if len(mod.patchFiles) != 1 {
		t.Fatalf("expected 1 patch file, got %d", len(mod.patchFiles))
	}
}

func TestBuildModule_DeletedFiles(t *testing.T) {
	pr := &PRInfo{
		Title:    "Cleanup",
		Provider: "github",
		Files: []FileChange{
			{Type: ChangeDeleted, Path: "old/deprecated.yaml"},
		},
	}

	mod := buildModule(pr, "test", nil, false, testLogger())

	if len(mod.loomFile.Spec.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(mod.loomFile.Spec.Operations))
	}
	op := mod.loomFile.Spec.Operations[0]
	if op.Shell == nil {
		t.Fatal("expected shell operation for delete")
	}
}

func TestBuildModule_IncludeGitOps(t *testing.T) {
	pr := &PRInfo{
		Title:      "Add feature",
		BaseBranch: "main",
		HeadBranch: "feat/add-feature",
		RepoURL:    "https://github.com/myorg/myrepo.git",
		Provider:   "github",
		Files: []FileChange{
			{Type: ChangeAdded, Path: "file.yaml", NewContent: []byte("test: true")},
		},
	}

	mod := buildModule(pr, "test", nil, true, testLogger())

	// Should have newFiles + commitPush + pr = 3 operations.
	if len(mod.loomFile.Spec.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(mod.loomFile.Spec.Operations))
	}
	if mod.loomFile.Spec.Operations[1].CommitPush == nil {
		t.Error("expected commitPush operation")
	}
	if mod.loomFile.Spec.Operations[2].PR == nil {
		t.Error("expected pr operation")
	}

	// Target should be populated from PR info.
	target := mod.loomFile.Spec.Target
	if target == nil {
		t.Fatal("expected target to be generated when includeGitOps is true")
	}
	if target.URL != "git@github.com:myorg/myrepo.git" {
		t.Errorf("expected target URL from PR, got %q", target.URL)
	}
	if target.Branch != "main" {
		t.Errorf("expected target branch 'main', got %q", target.Branch)
	}
	if target.FeatureBranch != "feat/add-feature" {
		t.Errorf("expected target featureBranch 'feat/add-feature', got %q", target.FeatureBranch)
	}
}

func TestBuildModule_IncludeGitOps_ParameterizesTarget(t *testing.T) {
	pr := &PRInfo{
		Title:      "Onboard payments",
		BaseBranch: "main",
		HeadBranch: "feat/onboard-payments",
		RepoURL:    "https://github.com/myorg/myrepo.git",
		Provider:   "github",
		Files: []FileChange{
			{Type: ChangeAdded, Path: "app/payments.yaml", NewContent: []byte("name: payments")},
		},
	}
	params := map[string]string{"serviceName": "payments"}

	mod := buildModule(pr, "test", params, true, testLogger())

	target := mod.loomFile.Spec.Target
	if target == nil {
		t.Fatal("expected target to be generated")
	}
	// HeadBranch contains "payments" which should be parameterized.
	if target.FeatureBranch != "feat/onboard-{{ .serviceName }}" {
		t.Errorf("expected parameterized featureBranch, got %q", target.FeatureBranch)
	}
}

func TestBuildModule_NoGitOps_NoTarget(t *testing.T) {
	pr := &PRInfo{
		Title:      "Add feature",
		BaseBranch: "main",
		HeadBranch: "feat/add-feature",
		RepoURL:    "https://github.com/myorg/myrepo.git",
		Provider:   "github",
		Files: []FileChange{
			{Type: ChangeAdded, Path: "file.yaml", NewContent: []byte("test: true")},
		},
	}

	mod := buildModule(pr, "test", nil, false, testLogger())

	if mod.loomFile.Spec.Target != nil {
		t.Error("expected no target when includeGitOps is false")
	}
}

func TestEmitModule(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "test-module")

	mod := &generatedModule{
		loomFile: config.LoomFile{
			APIVersion: "loom.rickliujh.github.io/v1beta1",
			Kind:       "Loom",
			Metadata:   config.Metadata{Name: "test"},
			Spec: config.Spec{
				Params: []config.ParamDef{{Name: "svc", Required: true}},
				Operations: []config.Operation{
					{Name: "create", NewFiles: &config.NewFiles{Source: ".", Dest: ""}},
				},
			},
		},
		templateFiles: map[string][]byte{
			"app/{{ .svc }}.yaml": []byte("name: {{ .svc }}"),
		},
		patchFiles: map[string][]byte{
			"config.patch.yaml": []byte("replicas: 3"),
		},
	}

	if err := emitModule(outputDir, mod, testLogger()); err != nil {
		t.Fatal(err)
	}

	// Verify loom.yaml exists and is valid.
	loomData, err := os.ReadFile(filepath.Join(outputDir, "loom.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var lf config.LoomFile
	if err := yaml.Unmarshal(loomData, &lf); err != nil {
		t.Fatalf("invalid loom.yaml: %v", err)
	}
	if lf.Metadata.Name != "test" {
		t.Errorf("expected name 'test', got %q", lf.Metadata.Name)
	}

	// Verify template file.
	tmplData, err := os.ReadFile(filepath.Join(outputDir, "app", "{{ .svc }}.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(tmplData) != "name: {{ .svc }}" {
		t.Errorf("unexpected template content: %s", string(tmplData))
	}

	// Verify patch file.
	patchData, err := os.ReadFile(filepath.Join(outputDir, "__functions", "patches", "config.patch.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(patchData) != "replicas: 3" {
		t.Errorf("unexpected patch content: %s", string(patchData))
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Onboard payments service", "onboard-payments-service"},
		{"feat: add new config", "feat-add-new-config"},
		{"  multiple   spaces  ", "multiple-spaces"},
		{"UPPERCASE", "uppercase"},
		{"special!@#chars", "specialchars"},
	}

	for _, tt := range tests {
		got := slugify(tt.in)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseGitHubPRRef(t *testing.T) {
	tests := []struct {
		ref    string
		owner  string
		repo   string
		number int
	}{
		{"https://github.com/myorg/myrepo/pull/42", "myorg", "myrepo", 42},
		{"github:myorg/myrepo#42", "myorg", "myrepo", 42},
	}

	for _, tt := range tests {
		owner, repo, num, err := parseGitHubPRRef(tt.ref)
		if err != nil {
			t.Errorf("parseGitHubPRRef(%q): %v", tt.ref, err)
			continue
		}
		if owner != tt.owner || repo != tt.repo || num != tt.number {
			t.Errorf("parseGitHubPRRef(%q) = (%q, %q, %d), want (%q, %q, %d)",
				tt.ref, owner, repo, num, tt.owner, tt.repo, tt.number)
		}
	}
}

func TestParseGitLabMRRef(t *testing.T) {
	tests := []struct {
		ref     string
		baseURL string
		project string
		number  int64
	}{
		{"https://gitlab.com/mygroup/myrepo/-/merge_requests/10", "https://gitlab.com", "mygroup/myrepo", 10},
		{"gitlab:mygroup/myrepo!10", "https://gitlab.com", "mygroup/myrepo", 10},
	}

	for _, tt := range tests {
		baseURL, project, num, err := parseGitLabMRRef(tt.ref)
		if err != nil {
			t.Errorf("parseGitLabMRRef(%q): %v", tt.ref, err)
			continue
		}
		if baseURL != tt.baseURL || project != tt.project || num != tt.number {
			t.Errorf("parseGitLabMRRef(%q) = (%q, %q, %d), want (%q, %q, %d)",
				tt.ref, baseURL, project, num, tt.baseURL, tt.project, tt.number)
		}
	}
}

func TestToSSHURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://github.com/myorg/myrepo.git", "git@github.com:myorg/myrepo.git"},
		{"https://gitlab.com/mygroup/myrepo.git", "git@gitlab.com:mygroup/myrepo.git"},
		{"https://gitlab.example.com/nested/group/repo.git", "git@gitlab.example.com:nested/group/repo.git"},
		{"http://github.com/myorg/myrepo.git", "git@github.com:myorg/myrepo.git"},
		// Already SSH — returned as-is.
		{"git@github.com:myorg/myrepo.git", "git@github.com:myorg/myrepo.git"},
		// Empty or unparseable — returned as-is.
		{"", ""},
	}

	for _, tt := range tests {
		got := toSSHURL(tt.in)
		if got != tt.want {
			t.Errorf("toSSHURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
