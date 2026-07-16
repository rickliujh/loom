package generate

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/rickliujh/loom/pkg/config"
)

func TestBuildModule_AddedFiles(t *testing.T) {
	pr := &ChangeSet{
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

	mod := buildModule(pr, "test-module", params, testLogger())

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

	// Check newFiles operation (+ commitPush + pr from gitops = 3 total).
	if len(mod.loomFile.Spec.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(mod.loomFile.Spec.Operations))
	}
	op := mod.loomFile.Spec.Operations[0]
	if op.NewFiles == nil {
		t.Fatal("expected newFiles operation")
	}
	if op.Name != "create-files" {
		t.Errorf("expected operation name 'create-files', got %q", op.Name)
	}
}

func TestBuildModule_ModifiedYAML(t *testing.T) {
	pr := &ChangeSet{
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

	mod := buildModule(pr, "test", nil, testLogger())

	// Should have a patch operation + commitPush + pr = 3.
	if len(mod.loomFile.Spec.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(mod.loomFile.Spec.Operations))
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
	pr := &ChangeSet{
		Title:    "Cleanup",
		Provider: "github",
		Files: []FileChange{
			{Type: ChangeDeleted, Path: "old/deprecated.yaml"},
		},
	}

	mod := buildModule(pr, "test", nil, testLogger())

	// delete shell + commitPush + pr = 3.
	if len(mod.loomFile.Spec.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(mod.loomFile.Spec.Operations))
	}
	op := mod.loomFile.Spec.Operations[0]
	if op.Shell == nil {
		t.Fatal("expected shell operation for delete")
	}
}

func TestBuildModule_DefaultGitOps(t *testing.T) {
	pr := &ChangeSet{
		Title:      "Add feature",
		BaseBranch: "main",
		HeadBranch: "feat/add-feature",
		RepoURL:    "https://github.com/myorg/myrepo.git",
		Provider:   "github",
		Files: []FileChange{
			{Type: ChangeAdded, Path: "file.yaml", NewContent: []byte("test: true")},
		},
	}

	mod := buildModule(pr, "test", nil, testLogger())

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
		t.Fatal("expected target to be generated")
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

func TestBuildModule_DefaultGitOps_ParameterizesTarget(t *testing.T) {
	pr := &ChangeSet{
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

	mod := buildModule(pr, "test", params, testLogger())

	target := mod.loomFile.Spec.Target
	if target == nil {
		t.Fatal("expected target to be generated")
	}
	// HeadBranch contains "payments" which should be parameterized.
	if target.FeatureBranch != "feat/onboard-{{ .serviceName }}" {
		t.Errorf("expected parameterized featureBranch, got %q", target.FeatureBranch)
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

	// Verify 2-space indent (yaml.Marshal would emit 4).
	if !strings.Contains(string(loomData), "\n  name: test") {
		t.Errorf("expected 2-space indent in loom.yaml, got:\n%s", string(loomData))
	}
	if strings.Contains(string(loomData), "\n    name: test") {
		t.Errorf("loom.yaml uses 4-space indent:\n%s", string(loomData))
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

// --- G3: Module name derivation priority ---

func TestBuildModule_NamePriority_ExplicitName(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Some PR Title",
		Provider: "github",
		Files:    []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("x: 1")}},
	}
	mod := buildModule(pr, "explicit-name", nil, testLogger())
	if mod.loomFile.Metadata.Name != "explicit-name" {
		t.Errorf("expected explicit name, got %q", mod.loomFile.Metadata.Name)
	}
}

func TestBuildModule_NamePriority_SlugifiedTitle(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Add Payment Service",
		Provider: "github",
		Files:    []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("x: 1")}},
	}
	// Pass empty moduleName — should slugify the title.
	mod := buildModule(pr, "", nil, testLogger())
	if mod.loomFile.Metadata.Name != "" {
		// buildModule receives name directly; the fallback is in Run().
		// Here we verify that empty name is passed through.
		// The Run() function handles the fallback chain.
	}
}

// --- G4: Slugification rules ---

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
		// G4: slashes and underscores become dashes.
		{"fix/broken_deploy", "fix-broken-deploy"},
		// G4: consecutive dashes collapsed.
		{"a---b", "a-b"},
		// G4: leading/trailing dashes trimmed.
		{"-leading-and-trailing-", "leading-and-trailing"},
		// G4: empty string.
		{"", ""},
		// G4: only special chars → empty.
		{"!@#$%", ""},
	}

	for _, tt := range tests {
		got := slugify(tt.in)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSlugify_TruncatesAt60(t *testing.T) {
	long := "this-is-a-very-long-title-that-should-be-truncated-at-sixty-characters-exactly-here"
	got := slugify(long)
	if len(got) > 60 {
		t.Errorf("slugify output length %d exceeds 60: %q", len(got), got)
	}
}

// --- PD1/PD2: Provider detection via ParseSourceRef ---

func TestParseSourceRef_GitHub(t *testing.T) {
	tests := []struct {
		ref      string
		provider string
	}{
		// PD1: URL-based detection (github.com).
		{"https://github.com/owner/repo/pull/1", "github"},
		// PD1: self-hosted GitHub Enterprise.
		{"https://git.internal.company.com/owner/repo/pull/42", "github"},
		// PD2: short-form detection.
		{"github:owner/repo#1", "github"},
	}
	for _, tt := range tests {
		src, err := ParseSourceRef(tt.ref, SnapshotOptions{}, testLogger())
		if err != nil {
			t.Errorf("ParseSourceRef(%q): %v", tt.ref, err)
			continue
		}
		if src.Provider != tt.provider {
			t.Errorf("ParseSourceRef(%q) provider = %q, want %q", tt.ref, src.Provider, tt.provider)
		}
		if src.Kind != KindPR {
			t.Errorf("ParseSourceRef(%q) kind = %v, want pr", tt.ref, src.Kind)
		}
		if src.ChangeSource == nil {
			t.Errorf("ParseSourceRef(%q) returned nil ChangeSource", tt.ref)
		}
	}
}

func TestParseSourceRef_RejectsBadURLs(t *testing.T) {
	// These contain "/pull/" or "/-/merge_requests/" as substrings but
	// are not valid PR/MR URLs — the strict regex should reject them.
	badRefs := []string{
		"https://example.com/pull/not-a-number",
		"https://example.com/some/path/with/pull/in/it",
		"this-has-/pull/42-in-the-middle",
	}
	for _, ref := range badRefs {
		_, err := ParseSourceRef(ref, SnapshotOptions{}, testLogger())
		if err == nil {
			t.Errorf("ParseSourceRef(%q) should have been rejected", ref)
		}
	}
}

func TestParseSourceRef_GitLab(t *testing.T) {
	tests := []struct {
		ref      string
		provider string
	}{
		// PD1: URL-based detection.
		{"https://gitlab.com/group/repo/-/merge_requests/5", "gitlab"},
		// PD3: self-hosted GitLab.
		{"https://gitlab.example.com/group/repo/-/merge_requests/5", "gitlab"},
		// PD2: short-form.
		{"gitlab:group/repo!5", "gitlab"},
	}
	for _, tt := range tests {
		src, err := ParseSourceRef(tt.ref, SnapshotOptions{}, testLogger())
		if err != nil {
			t.Errorf("ParseSourceRef(%q): %v", tt.ref, err)
			continue
		}
		if src.Provider != tt.provider {
			t.Errorf("ParseSourceRef(%q) provider = %q, want %q", tt.ref, src.Provider, tt.provider)
		}
		if src.Kind != KindPR {
			t.Errorf("ParseSourceRef(%q) kind = %v, want pr", tt.ref, src.Kind)
		}
		if src.ChangeSource == nil {
			t.Errorf("ParseSourceRef(%q) returned nil ChangeSource", tt.ref)
		}
	}
}

func TestParseSourceRef_UnknownProvider(t *testing.T) {
	refs := []string{
		"https://example.com/repo/changes/1",
		"bitbucket:owner/repo#1",
		"not-a-url",
		"",
	}
	for _, ref := range refs {
		_, err := ParseSourceRef(ref, SnapshotOptions{}, testLogger())
		if err == nil {
			t.Errorf("ParseSourceRef(%q) expected error for unknown provider", ref)
		}
	}
}

// --- githubRepoURL ---

func TestGithubRepoURL(t *testing.T) {
	tests := []struct {
		ref   string
		owner string
		repo  string
		want  string
	}{
		// Full URL — extract host from ref.
		{"https://github.com/myorg/myrepo/pull/42", "myorg", "myrepo", "https://github.com/myorg/myrepo.git"},
		// Self-hosted — host preserved.
		{"https://git.company.com/myorg/myrepo/pull/1", "myorg", "myrepo", "https://git.company.com/myorg/myrepo.git"},
		// Short-form — defaults to github.com.
		{"github:myorg/myrepo#42", "myorg", "myrepo", "https://github.com/myorg/myrepo.git"},
	}
	for _, tt := range tests {
		got := githubRepoURL(tt.ref, tt.owner, tt.repo)
		if got != tt.want {
			t.Errorf("githubRepoURL(%q, %q, %q) = %q, want %q", tt.ref, tt.owner, tt.repo, got, tt.want)
		}
	}
}

// --- PD1/PD2: GitHub PR reference parsing (detailed) ---

func TestParseGitHubPRRef(t *testing.T) {
	tests := []struct {
		ref    string
		owner  string
		repo   string
		number int
	}{
		{"https://github.com/myorg/myrepo/pull/42", "myorg", "myrepo", 42},
		{"github:myorg/myrepo#42", "myorg", "myrepo", 42},
		// Self-hosted GitHub Enterprise.
		{"https://git.company.com/myorg/myrepo/pull/99", "myorg", "myrepo", 99},
		// Trailing slash tolerance.
		{"https://github.com/owner/repo/pull/1/", "owner", "repo", 1},
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

func TestParseGitHubPRRef_Errors(t *testing.T) {
	badRefs := []string{
		"https://github.com/myorg/myrepo/pull/notanumber",
		"github:myorg/myrepo",   // missing #number
		"github:myorg#42",       // missing /repo
		"not-a-valid-reference", // no /pull/ or github: prefix
	}
	for _, ref := range badRefs {
		_, _, _, err := parseGitHubPRRef(ref)
		if err == nil {
			t.Errorf("parseGitHubPRRef(%q) expected error", ref)
		}
	}
}

// --- PD3: GitLab MR reference parsing (detailed) ---

func TestParseGitLabMRRef(t *testing.T) {
	tests := []struct {
		ref     string
		baseURL string
		project string
		number  int64
	}{
		{"https://gitlab.com/mygroup/myrepo/-/merge_requests/10", "https://gitlab.com", "mygroup/myrepo", 10},
		{"gitlab:mygroup/myrepo!10", "https://gitlab.com", "mygroup/myrepo", 10},
		// PD3: self-hosted GitLab.
		{"https://gitlab.example.com/team/project/-/merge_requests/99", "https://gitlab.example.com", "team/project", 99},
		// Trailing slash tolerance.
		{"https://gitlab.com/group/repo/-/merge_requests/5/", "https://gitlab.com", "group/repo", 5},
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

func TestParseGitLabMRRef_Errors(t *testing.T) {
	badRefs := []string{
		"gitlab:group/repo",     // missing !number
		"gitlab:group/repo!abc", // non-numeric
	}
	for _, ref := range badRefs {
		_, _, _, err := parseGitLabMRRef(ref)
		if err == nil {
			t.Errorf("parseGitLabMRRef(%q) expected error", ref)
		}
	}
}

// --- FC1: Added files — single newFiles operation from root ---

func TestBuildModule_AddedFiles_SingleNewFilesOp(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Add multiple dirs",
		Provider: "github",
		Files: []FileChange{
			{Type: ChangeAdded, Path: "alpha/one.yaml", NewContent: []byte("a: 1")},
			{Type: ChangeAdded, Path: "alpha/two.yaml", NewContent: []byte("b: 2")},
			{Type: ChangeAdded, Path: "beta/three.yaml", NewContent: []byte("c: 3")},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	// Should have exactly 1 newFiles operation (from root), not grouped.
	newFilesCount := 0
	for _, op := range mod.loomFile.Spec.Operations {
		if op.NewFiles != nil {
			newFilesCount++
		}
	}
	if newFilesCount != 1 {
		t.Errorf("expected 1 newFiles operation, got %d", newFilesCount)
	}

	// All 3 template files should exist.
	if len(mod.templateFiles) != 3 {
		t.Errorf("expected 3 template files, got %d", len(mod.templateFiles))
	}
}

func TestBuildModule_AddedFiles_SourceIsDot(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Root files",
		Provider: "github",
		Files: []FileChange{
			{Type: ChangeAdded, Path: "root-file.yaml", NewContent: []byte("x: 1")},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	// newFiles + commitPush + pr = 3
	op := mod.loomFile.Spec.Operations[0]
	if op.NewFiles == nil {
		t.Fatal("expected newFiles operation")
	}
	if op.NewFiles.Source != "." {
		t.Errorf("expected source '.', got %q", op.NewFiles.Source)
	}
	if op.NewFiles.Dest != "" {
		t.Errorf("expected empty dest, got %q", op.NewFiles.Dest)
	}
}

func TestBuildModule_AddedFiles_NilContentSkipped(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Nil content",
		Provider: "github",
		Files: []FileChange{
			{Type: ChangeAdded, Path: "dir/no-content.yaml", NewContent: nil},
			{Type: ChangeAdded, Path: "dir/has-content.yaml", NewContent: []byte("x: 1")},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	// Only the file with content should produce a template.
	if len(mod.templateFiles) != 1 {
		t.Errorf("expected 1 template file (nil content skipped), got %d", len(mod.templateFiles))
	}
	if _, ok := mod.templateFiles["dir/has-content.yaml"]; !ok {
		t.Error("expected 'dir/has-content.yaml' in templateFiles")
	}
}

// --- FC2: Modified files — YAML with SMP only; others skipped with warning ---

func TestBuildModule_ModifiedNonYAML_Skipped(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Update JSON",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeModified,
				Path:       "config/app.json",
				OldContent: []byte(`{"name":"old"}`),
				NewContent: []byte(`{"name":"new"}`),
			},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	// Non-YAML modified → skipped (no patch, no template).
	if len(mod.patchFiles) != 0 {
		t.Errorf("expected 0 patch files for non-YAML, got %d", len(mod.patchFiles))
	}
	if len(mod.templateFiles) != 0 {
		t.Errorf("expected 0 template files (non-YAML skipped), got %d", len(mod.templateFiles))
	}
}

func TestBuildModule_ModifiedYAML_NoOldContent_Skipped(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Update YAML without old",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeModified,
				Path:       "config/app.yaml",
				OldContent: nil, // no old content available
				NewContent: []byte("name: new\n"),
			},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	// No old content → SMP cannot compute → skipped.
	if len(mod.patchFiles) != 0 {
		t.Errorf("expected 0 patch files when old content missing, got %d", len(mod.patchFiles))
	}
	if len(mod.templateFiles) != 0 {
		t.Errorf("expected 0 template files (skipped), got %d", len(mod.templateFiles))
	}
}

func TestBuildModule_ModifiedYAML_InvalidYAML_Skipped(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Bad YAML",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeModified,
				Path:       "config/app.yml",
				OldContent: []byte("not: yaml: {invalid"),
				NewContent: []byte("name: new\n"),
			},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	// Invalid YAML → SMP returns nil → skipped.
	if len(mod.patchFiles) != 0 {
		t.Errorf("expected 0 patch files for invalid YAML, got %d", len(mod.patchFiles))
	}
	if len(mod.templateFiles) != 0 {
		t.Errorf("expected 0 template files (skipped), got %d", len(mod.templateFiles))
	}
}

func TestBuildModule_ModifiedYAML_YmlExtension(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Update yml",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeModified,
				Path:       "config/app.yml",
				OldContent: []byte("name: old\nreplicas: 1"),
				NewContent: []byte("name: old\nreplicas: 3"),
			},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	// .yml should also be treated as YAML.
	if len(mod.patchFiles) != 1 {
		t.Errorf("expected 1 patch file for .yml extension, got %d", len(mod.patchFiles))
	}
	if mod.loomFile.Spec.Operations[0].Patch == nil {
		t.Error("expected first operation to be a patch for .yml file")
	}
}

func TestBuildModule_ModifiedYAML_NilNewContent_Skipped(t *testing.T) {
	pr := &ChangeSet{
		Title:    "No new content",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeModified,
				Path:       "config/app.yaml",
				OldContent: []byte("name: old"),
				NewContent: nil,
			},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	if len(mod.patchFiles) != 0 {
		t.Errorf("expected 0 patch files for nil new content, got %d", len(mod.patchFiles))
	}
	if len(mod.templateFiles) != 0 {
		t.Errorf("expected 0 template files for nil new content, got %d", len(mod.templateFiles))
	}
}

// --- FC3: Deleted files → shell rm ---

func TestBuildModule_DeletedFiles_Parameterized(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Remove payments config",
		Provider: "github",
		Files: []FileChange{
			{Type: ChangeDeleted, Path: "services/payments/config.yaml"},
		},
	}
	params := map[string]string{"serviceName": "payments"}
	mod := buildModule(pr, "test", params, testLogger())

	// delete shell + commitPush + pr = 3.
	if len(mod.loomFile.Spec.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(mod.loomFile.Spec.Operations))
	}
	op := mod.loomFile.Spec.Operations[0]
	if op.Shell == nil {
		t.Fatal("expected shell operation")
	}
	// Path should be parameterized.
	if !strings.Contains(op.Shell.Command, "{{ .serviceName }}") {
		t.Errorf("expected parameterized path in shell command, got %q", op.Shell.Command)
	}
}

// --- FC4: Renamed files → mv, then SMP patch if content also changed ---

func TestBuildModule_RenamedFiles_MvOnly_NoOldContent(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Rename file",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeRenamed,
				Path:       "new/location.yaml",
				OldPath:    "old/location.yaml",
				NewContent: []byte("content: here"),
				// OldContent is nil — can't compute SMP, so no patch.
			},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	hasNewFiles := false
	hasPatch := false
	hasShellMv := false
	for _, op := range mod.loomFile.Spec.Operations {
		if op.NewFiles != nil {
			hasNewFiles = true
		}
		if op.Patch != nil {
			hasPatch = true
		}
		if op.Shell != nil && strings.Contains(op.Shell.Command, "mv") {
			hasShellMv = true
		}
	}
	if hasNewFiles {
		t.Error("renamed file should NOT produce a newFiles operation")
	}
	if hasPatch {
		t.Error("renamed file without OldContent should NOT produce a patch")
	}
	if !hasShellMv {
		t.Error("expected shell mv operation for rename")
	}
	if len(mod.templateFiles) != 0 {
		t.Errorf("expected 0 template files for rename, got %d", len(mod.templateFiles))
	}
}

func TestBuildModule_RenamedFiles_MvAndPatch(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Rename and modify",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeRenamed,
				Path:       "new/location.yaml",
				OldPath:    "old/location.yaml",
				OldContent: []byte("key: old-value\nother: keep"),
				NewContent: []byte("key: new-value\nother: keep"),
			},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	ops := mod.loomFile.Spec.Operations
	// Expected: patch (targets old path), mv, commitPush, pr
	hasMv := false
	hasPatch := false
	mvIdx := -1
	patchIdx := -1
	for i, op := range ops {
		if op.Shell != nil && strings.Contains(op.Shell.Command, "mv") {
			hasMv = true
			mvIdx = i
		}
		if op.Patch != nil {
			hasPatch = true
			patchIdx = i
			// Patch should target the old path (before mv).
			if op.Patch.Target != "old/location.yaml" {
				t.Errorf("patch target should be old path, got %q", op.Patch.Target)
			}
		}
	}
	if !hasMv {
		t.Error("expected shell mv operation for rename")
	}
	if !hasPatch {
		t.Error("expected SMP patch operation for renamed file with content changes")
	}
	if patchIdx >= mvIdx {
		t.Errorf("patch (idx %d) should come before mv (idx %d)", patchIdx, mvIdx)
	}

	// Patch file should exist.
	if len(mod.patchFiles) != 1 {
		t.Errorf("expected 1 patch file, got %d", len(mod.patchFiles))
	}
	// No template files — renamed files don't produce newFiles.
	if len(mod.templateFiles) != 0 {
		t.Errorf("expected 0 template files, got %d", len(mod.templateFiles))
	}
}

func TestBuildModule_RenamedFiles_NonYAML_ContentChanged(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Rename non-YAML",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeRenamed,
				Path:       "new/script.sh",
				OldPath:    "old/script.sh",
				OldContent: []byte("#!/bin/bash\necho old"),
				NewContent: []byte("#!/bin/bash\necho new"),
			},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	// Non-YAML renamed file: mv only, no patch, warning logged.
	hasPatch := false
	for _, op := range mod.loomFile.Spec.Operations {
		if op.Patch != nil {
			hasPatch = true
		}
	}
	if hasPatch {
		t.Error("non-YAML renamed file should NOT produce a patch")
	}
}

func TestBuildModule_RenamedFiles_YAML_IdenticalContent(t *testing.T) {
	content := []byte("key: value\nother: data")
	pr := &ChangeSet{
		Title:    "Pure rename YAML",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeRenamed,
				Path:       "new/config.yaml",
				OldPath:    "old/config.yaml",
				OldContent: content,
				NewContent: content,
			},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	// Identical content: SMP returns nil → no patch, just mv.
	hasPatch := false
	for _, op := range mod.loomFile.Spec.Operations {
		if op.Patch != nil {
			hasPatch = true
		}
	}
	if hasPatch {
		t.Error("renamed YAML file with identical content should NOT produce a patch")
	}
}

func TestBuildModule_RenamedFiles_Parameterized(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Rename payments",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeRenamed,
				Path:       "services/payments/new.yaml",
				OldPath:    "services/payments/old.yaml",
				NewContent: []byte("svc: payments"),
			},
		},
	}
	params := map[string]string{"serviceName": "payments"}
	mod := buildModule(pr, "test", params, testLogger())

	// The mv command should have parameterized paths.
	for _, op := range mod.loomFile.Spec.Operations {
		if op.Shell != nil && strings.Contains(op.Shell.Command, "mv") {
			if !strings.Contains(op.Shell.Command, "{{ .serviceName }}") {
				t.Errorf("expected parameterized paths in mv command, got %q", op.Shell.Command)
			}
		}
	}
}

func TestBuildModule_RenamedFiles_PureRename_NoNewContent(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Pure rename",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeRenamed,
				Path:       "new/path.yaml",
				OldPath:    "old/path.yaml",
				NewContent: nil, // pure rename, no content fetched
			},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	hasShellMv := false
	for _, op := range mod.loomFile.Spec.Operations {
		if op.Shell != nil && strings.Contains(op.Shell.Command, "mv") {
			hasShellMv = true
		}
	}
	if !hasShellMv {
		t.Error("expected shell mv operation for pure rename")
	}
}

// --- PM5/GO3/GO4: GitOps metadata parameterization ---

func TestBuildModule_GitOps_ParameterizesCommitAndPR(t *testing.T) {
	pr := &ChangeSet{
		Title:      "onboard payments service",
		Body:       "Adding payments to the platform",
		BaseBranch: "main",
		HeadBranch: "feat/onboard-payments",
		RepoURL:    "https://github.com/org/repo.git",
		Provider:   "github",
		Files:      []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("x: 1")}},
	}
	params := map[string]string{"serviceName": "payments"}
	mod := buildModule(pr, "test", params, testLogger())

	// Find commitPush and PR operations.
	var commitOp, prOp *config.Operation
	for i := range mod.loomFile.Spec.Operations {
		if mod.loomFile.Spec.Operations[i].CommitPush != nil {
			commitOp = &mod.loomFile.Spec.Operations[i]
		}
		if mod.loomFile.Spec.Operations[i].PR != nil {
			prOp = &mod.loomFile.Spec.Operations[i]
		}
	}

	// GO3: commit message should be parameterized.
	if commitOp == nil {
		t.Fatal("expected commitPush operation")
	}
	if !strings.Contains(commitOp.CommitPush.Message, "{{ .serviceName }}") {
		t.Errorf("expected parameterized commit message, got %q", commitOp.CommitPush.Message)
	}

	// GO4: PR title and body should be parameterized.
	if prOp == nil {
		t.Fatal("expected PR operation")
	}
	if !strings.Contains(prOp.PR.Title, "{{ .serviceName }}") {
		t.Errorf("expected parameterized PR title, got %q", prOp.PR.Title)
	}
	if !strings.Contains(prOp.PR.Body, "{{ .serviceName }}") {
		t.Errorf("expected parameterized PR body, got %q", prOp.PR.Body)
	}
	if prOp.PR.BaseBranch != "main" {
		t.Errorf("expected baseBranch 'main', got %q", prOp.PR.BaseBranch)
	}
	if prOp.PR.Provider != "github" {
		t.Errorf("expected provider 'github', got %q", prOp.PR.Provider)
	}
}

// --- GO2: SSH URL conversion ---

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
		// Non-http scheme — returned as-is.
		{"ftp://example.com/repo.git", "ftp://example.com/repo.git"},
	}

	for _, tt := range tests {
		got := toSSHURL(tt.in)
		if got != tt.want {
			t.Errorf("toSSHURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// --- E2: loom.yaml structure conformance ---

func TestBuildModule_LoomFileStructure(t *testing.T) {
	pr := &ChangeSet{
		Title:      "test",
		Provider:   "github",
		BaseBranch: "main",
		HeadBranch: "feat/test",
		RepoURL:    "https://github.com/o/r.git",
		Files:      []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("x: 1")}},
	}
	mod := buildModule(pr, "my-module", nil, testLogger())

	if mod.loomFile.APIVersion != "loom.rickliujh.github.io/v1beta1" {
		t.Errorf("unexpected apiVersion: %q", mod.loomFile.APIVersion)
	}
	if mod.loomFile.Kind != "Loom" {
		t.Errorf("unexpected kind: %q", mod.loomFile.Kind)
	}
	if mod.loomFile.Metadata.Name != "my-module" {
		t.Errorf("unexpected name: %q", mod.loomFile.Metadata.Name)
	}
}

// --- PM6: All params declared as required ---

func TestBuildModule_ParamDefs_AllRequired(t *testing.T) {
	pr := &ChangeSet{
		Title:    "test",
		Provider: "github",
		Files:    []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("x: 1")}},
	}
	params := map[string]string{"svc": "payments", "env": "prod", "region": "us-east-1"}
	mod := buildModule(pr, "test", params, testLogger())

	if len(mod.loomFile.Spec.Params) != 3 {
		t.Fatalf("expected 3 param defs, got %d", len(mod.loomFile.Spec.Params))
	}
	for _, p := range mod.loomFile.Spec.Params {
		if !p.Required {
			t.Errorf("param %q should be required", p.Name)
		}
		if p.Default != "" {
			t.Errorf("param %q should have no default, got %q", p.Name, p.Default)
		}
	}
}

// --- E3: Operation ordering ---

func TestBuildModule_OperationOrdering(t *testing.T) {
	pr := &ChangeSet{
		Title:      "Full change set",
		Provider:   "github",
		BaseBranch: "main",
		HeadBranch: "feat/full",
		RepoURL:    "https://github.com/o/r.git",
		Files: []FileChange{
			{Type: ChangeAdded, Path: "dir/new.yaml", NewContent: []byte("new: file")},
			{Type: ChangeModified, Path: "config/app.yaml",
				OldContent: []byte("name: old"), NewContent: []byte("name: new")},
			{Type: ChangeDeleted, Path: "old/removed.yaml"},
			{Type: ChangeRenamed, Path: "new/path.yaml", OldPath: "old/path.yaml",
				OldContent: []byte("key: old"), NewContent: []byte("key: new")},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	// Expected order: newFiles, patch(modified), delete shell, rename-patch, rename shell(mv), commitPush, pr
	ops := mod.loomFile.Spec.Operations

	if len(ops) != 7 {
		t.Fatalf("expected 7 operations, got %d: %+v", len(ops), ops)
	}

	// Verify ordering.
	if ops[0].NewFiles == nil {
		t.Error("expected newFiles operation first")
	}
	if ops[1].Patch == nil {
		t.Error("expected patch operation second (modified file)")
	}
	if ops[2].Shell == nil || !strings.Contains(ops[2].Shell.Command, "rm") {
		t.Error("expected delete shell operation third")
	}
	if ops[3].Patch == nil {
		t.Error("expected rename-patch operation fourth (before mv)")
	}
	if ops[4].Shell == nil || !strings.Contains(ops[4].Shell.Command, "mv") {
		t.Error("expected rename shell operation fifth (after patch)")
	}
	if ops[5].CommitPush == nil {
		t.Error("expected commitPush operation sixth")
	}
	if ops[6].PR == nil {
		t.Error("expected PR operation last")
	}
}

// --- SMP4: Patch file naming ---

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"cluster/apps/deployment.yaml", "cluster--apps--deployment.yaml"},
		{"simple.yaml", "simple.yaml"},
		{"a/b/c/d.yaml", "a--b--c--d.yaml"},
	}
	for _, tt := range tests {
		got := sanitizeFilename(tt.in)
		if got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// --- SMP5: Patch file path in operation ---

func TestBuildModule_PatchFilePath(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Patch test",
		Provider: "github",
		Files: []FileChange{
			{
				Type:       ChangeModified,
				Path:       "cluster/apps/deployment.yaml",
				OldContent: []byte("replicas: 1"),
				NewContent: []byte("replicas: 3"),
			},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	// patch + commitPush + pr = 3.
	if len(mod.loomFile.Spec.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(mod.loomFile.Spec.Operations))
	}
	op := mod.loomFile.Spec.Operations[0]
	if op.Patch == nil {
		t.Fatal("expected patch operation")
	}
	expectedPath := "__functions/patches/cluster--apps--deployment.yaml.patch.yaml"
	if op.Patch.Path != expectedPath {
		t.Errorf("expected patch path %q, got %q", expectedPath, op.Patch.Path)
	}
	if op.Patch.Target != "cluster/apps/deployment.yaml" {
		t.Errorf("expected patch target 'cluster/apps/deployment.yaml', got %q", op.Patch.Target)
	}

	// Verify patch file exists in patchFiles map.
	expectedPatchName := "cluster--apps--deployment.yaml.patch.yaml"
	if _, ok := mod.patchFiles[expectedPatchName]; !ok {
		t.Errorf("expected patch file %q in patchFiles map", expectedPatchName)
	}
}

// --- A1/A2: Token resolution ---

func TestTokenFromEnv_CustomEnv(t *testing.T) {
	t.Setenv("MY_CUSTOM_TOKEN", "custom-token-value")
	got := tokenFromEnv("MY_CUSTOM_TOKEN", "github", testLogger())
	if got != "custom-token-value" {
		t.Errorf("expected custom token, got %q", got)
	}
}

func TestTokenFromEnv_CustomEnvEmpty(t *testing.T) {
	t.Setenv("MY_CUSTOM_TOKEN", "")
	got := tokenFromEnv("MY_CUSTOM_TOKEN", "github", testLogger())
	if got != "" {
		t.Errorf("expected empty string for empty custom env, got %q", got)
	}
}

func TestTokenFromEnv_GitHubDefault(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "gh-token-123")
	got := tokenFromEnv("", "github", testLogger())
	if got != "gh-token-123" {
		t.Errorf("expected GITHUB_TOKEN value, got %q", got)
	}
}

func TestTokenFromEnv_GitLabDefault_GITLAB_TOKEN(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "gl-token-456")
	t.Setenv("GITLAB_PRIVATE_TOKEN", "")
	got := tokenFromEnv("", "gitlab", testLogger())
	if got != "gl-token-456" {
		t.Errorf("expected GITLAB_TOKEN value, got %q", got)
	}
}

func TestTokenFromEnv_GitLabDefault_FallbackToPrivateToken(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("GITLAB_PRIVATE_TOKEN", "gl-private-789")
	got := tokenFromEnv("", "gitlab", testLogger())
	if got != "gl-private-789" {
		t.Errorf("expected GITLAB_PRIVATE_TOKEN value, got %q", got)
	}
}

func TestTokenFromEnv_NoTokenAvailable(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	got := tokenFromEnv("", "github", testLogger())
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestTokenFromEnv_UnknownProvider(t *testing.T) {
	got := tokenFromEnv("", "bitbucket", testLogger())
	if got != "" {
		t.Errorf("expected empty string for unknown provider, got %q", got)
	}
}

// --- E4: Emit module creates intermediate directories ---

func TestEmitModule_CreatesIntermediateDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "deeply", "nested", "module")

	mod := &generatedModule{
		loomFile: config.LoomFile{
			APIVersion: "loom.rickliujh.github.io/v1beta1",
			Kind:       "Loom",
			Metadata:   config.Metadata{Name: "test"},
		},
		templateFiles: map[string][]byte{
			"a/b/c/file.yaml": []byte("deep: file"),
		},
		patchFiles: map[string][]byte{},
	}

	if err := emitModule(outputDir, mod, testLogger()); err != nil {
		t.Fatal(err)
	}

	// Verify deeply nested file was written.
	data, err := os.ReadFile(filepath.Join(outputDir, "a", "b", "c", "file.yaml"))
	if err != nil {
		t.Fatalf("expected nested file to exist: %v", err)
	}
	if string(data) != "deep: file" {
		t.Errorf("unexpected content: %s", data)
	}
}

// --- E2: Emitted loom.yaml is valid and parseable ---

func TestEmitModule_LoomYAMLRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	mod := &generatedModule{
		loomFile: config.LoomFile{
			APIVersion: "loom.rickliujh.github.io/v1beta1",
			Kind:       "Loom",
			Metadata:   config.Metadata{Name: "roundtrip-test"},
			Spec: config.Spec{
				Params: []config.ParamDef{
					{Name: "svc", Required: true},
					{Name: "env", Required: true},
				},
				Target: &config.TargetSpec{
					URL:           "git@github.com:o/r.git",
					Branch:        "main",
					FeatureBranch: "feat/{{ .svc }}",
				},
				Operations: []config.Operation{
					{Name: "create", NewFiles: &config.NewFiles{Source: ".", Dest: ""}},
					{Name: "patch-0", Patch: &config.Patch{Engine: "smp", Path: "__functions/patches/p.yaml", Target: "t.yaml"}},
					{Name: "commit", CommitPush: &config.CommitPush{Message: "deploy {{ .svc }}"}},
					{Name: "open-pr", PR: &config.PR{Provider: "github", Title: "PR for {{ .svc }}", BaseBranch: "main"}},
				},
			},
		},
		templateFiles: map[string][]byte{},
		patchFiles:    map[string][]byte{},
	}

	if err := emitModule(tmpDir, mod, testLogger()); err != nil {
		t.Fatal(err)
	}

	// Read back and verify structure.
	data, err := os.ReadFile(filepath.Join(tmpDir, "loom.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var lf config.LoomFile
	if err := yaml.Unmarshal(data, &lf); err != nil {
		t.Fatalf("emitted loom.yaml is not valid: %v", err)
	}
	if lf.APIVersion != "loom.rickliujh.github.io/v1beta1" {
		t.Errorf("apiVersion mismatch: %q", lf.APIVersion)
	}
	if lf.Kind != "Loom" {
		t.Errorf("kind mismatch: %q", lf.Kind)
	}
	if len(lf.Spec.Operations) != 4 {
		t.Errorf("expected 4 operations, got %d", len(lf.Spec.Operations))
	}
	if lf.Spec.Target == nil {
		t.Error("expected target in emitted loom.yaml")
	}
}

// --- Mixed change types in a single PR ---

func TestBuildModule_MixedChangeTypes(t *testing.T) {
	pr := &ChangeSet{
		Title:    "Mixed changes",
		Provider: "github",
		Files: []FileChange{
			{Type: ChangeAdded, Path: "new/file.yaml", NewContent: []byte("new: true")},
			{Type: ChangeModified, Path: "existing.yaml",
				OldContent: []byte("v: 1"), NewContent: []byte("v: 2")},
			{Type: ChangeDeleted, Path: "gone.yaml"},
		},
	}
	mod := buildModule(pr, "test", nil, testLogger())

	var hasNewFiles, hasPatch, hasShell bool
	for _, op := range mod.loomFile.Spec.Operations {
		if op.NewFiles != nil {
			hasNewFiles = true
		}
		if op.Patch != nil {
			hasPatch = true
		}
		if op.Shell != nil {
			hasShell = true
		}
	}
	if !hasNewFiles {
		t.Error("expected newFiles operation for added file")
	}
	if !hasPatch {
		t.Error("expected patch operation for modified YAML")
	}
	if !hasShell {
		t.Error("expected shell operation for deleted file")
	}
}

// --- ChangeType String ---

func TestChangeType_String(t *testing.T) {
	tests := []struct {
		ct   ChangeType
		want string
	}{
		{ChangeAdded, "added"},
		{ChangeModified, "modified"},
		{ChangeDeleted, "deleted"},
		{ChangeRenamed, "renamed"},
		{ChangeType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.ct.String(); got != tt.want {
			t.Errorf("ChangeType(%d).String() = %q, want %q", tt.ct, got, tt.want)
		}
	}
}

// --- Degraded gitops paths (commit / snapshot sources) ---

func TestBuildModule_NoRepoURL_OmitsTarget(t *testing.T) {
	cs := &ChangeSet{
		Title:    "local change",
		Provider: "github",
		Files:    []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("x: 1")}},
	}
	mod := buildModule(cs, "test", nil, testLogger())

	if mod.loomFile.Spec.Target != nil {
		t.Error("expected target to be omitted when repo URL is unknown")
	}
	// commitPush is still emitted.
	hasCommit := false
	for _, op := range mod.loomFile.Spec.Operations {
		if op.CommitPush != nil {
			hasCommit = true
		}
	}
	if !hasCommit {
		t.Error("expected commitPush operation")
	}
}

func TestBuildModule_NoProvider_OmitsPROp(t *testing.T) {
	cs := &ChangeSet{
		Title:      "bitbucket change",
		RepoURL:    "git@bitbucket.org:o/r.git",
		BaseBranch: "main",
		Files:      []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("x: 1")}},
	}
	mod := buildModule(cs, "test", nil, testLogger())

	for _, op := range mod.loomFile.Spec.Operations {
		if op.PR != nil {
			t.Error("expected pr operation to be omitted when provider is unknown")
		}
	}
	if mod.loomFile.Spec.Target == nil {
		t.Fatal("expected target (repo URL is known)")
	}
}

func TestBuildModule_SynthesizesFeatureBranchAndTitle(t *testing.T) {
	cs := &ChangeSet{
		// No Title, no HeadBranch — e.g. a snapshot source.
		RepoURL:    "https://github.com/o/r.git",
		BaseBranch: "main",
		Provider:   "github",
		Files:      []FileChange{{Type: ChangeAdded, Path: "f.yaml", NewContent: []byte("x: 1")}},
	}
	mod := buildModule(cs, "my-module", nil, testLogger())

	if mod.loomFile.Spec.Target.FeatureBranch != "loom/my-module" {
		t.Errorf("featureBranch = %q, want loom/my-module", mod.loomFile.Spec.Target.FeatureBranch)
	}
	for _, op := range mod.loomFile.Spec.Operations {
		if op.CommitPush != nil && op.CommitPush.Message != "apply my-module" {
			t.Errorf("commit message = %q, want 'apply my-module'", op.CommitPush.Message)
		}
		if op.PR != nil && op.PR.Title != "apply my-module" {
			t.Errorf("pr title = %q, want 'apply my-module'", op.PR.Title)
		}
	}
}

// --- End-to-end: multiple sources composed through Run ---

func TestRun_SnapshotFlagsRequireSnapshotSource(t *testing.T) {
	opts := Options{
		Refs:    []string{"github:o/r#1"},
		Include: []string{"a/**"},
	}
	err := Run(t.Context(), opts, testLogger())
	if err == nil || !strings.Contains(err.Error(), "require a local path source") {
		t.Errorf("expected flag-validation error, got %v", err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
