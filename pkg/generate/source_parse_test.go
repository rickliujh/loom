package generate

import (
	"testing"
)

func parseCommitRef(t *testing.T, ref string) (*Source, *CommitSource) {
	t.Helper()
	src, err := ParseSourceRef(ref, SnapshotOptions{}, testLogger())
	if err != nil {
		t.Fatalf("ParseSourceRef(%q): %v", ref, err)
	}
	if src.Kind != KindCommit {
		t.Fatalf("ParseSourceRef(%q) kind = %v, want commit", ref, src.Kind)
	}
	cs, ok := src.ChangeSource.(*CommitSource)
	if !ok {
		t.Fatalf("ParseSourceRef(%q) source type = %T, want *CommitSource", ref, src.ChangeSource)
	}
	return src, cs
}

func TestParseSourceRef_CommitRefs(t *testing.T) {
	tests := []struct {
		ref      string
		repoURL  string
		base     string
		head     string
		provider string
	}{
		// Short-form sugar.
		{"github:o/r@abc1234", "git@github.com:o/r.git", "", "abc1234", "github"},
		{"github:o/r@abc1234...def5678", "git@github.com:o/r.git", "abc1234", "def5678", "github"},
		{"gitlab:g/r@abc1234", "git@gitlab.com:g/r.git", "", "abc1234", "gitlab"},
		// Tags allowed on unambiguous repos.
		{"github:o/r@v1.0.0...v1.1.0", "git@github.com:o/r.git", "v1.0.0", "v1.1.0", "github"},
		// Commit / compare URL sugar.
		{"https://github.com/o/r/commit/abc1234", "https://github.com/o/r.git", "", "abc1234", "github"},
		{"https://gitlab.com/g/r/-/commit/abc1234", "https://gitlab.com/g/r.git", "", "abc1234", "gitlab"},
		{"https://github.com/o/r/compare/abc1234...def5678", "https://github.com/o/r.git", "abc1234", "def5678", "github"},
		{"https://gitlab.com/g/r/-/compare/abc1234...def5678", "https://gitlab.com/g/r.git", "abc1234", "def5678", "gitlab"},
		// Arbitrary git URLs — platform-agnostic.
		{"git@bitbucket.org:o/r.git@abc1234...def5678", "git@bitbucket.org:o/r.git", "abc1234", "def5678", ""},
		{"https://gitea.example.com/o/r.git@abc1234", "https://gitea.example.com/o/r.git", "", "abc1234", ""},
		{"git@github.enterprise.io:o/r.git@abc1234", "git@github.enterprise.io:o/r.git", "", "abc1234", "github"},
	}

	for _, tt := range tests {
		src, cs := parseCommitRef(t, tt.ref)
		if cs.RepoURL != tt.repoURL {
			t.Errorf("%q: repoURL = %q, want %q", tt.ref, cs.RepoURL, tt.repoURL)
		}
		if cs.BaseRev != tt.base || cs.HeadRev != tt.head {
			t.Errorf("%q: range = %q...%q, want %q...%q", tt.ref, cs.BaseRev, cs.HeadRev, tt.base, tt.head)
		}
		if src.Provider != tt.provider {
			t.Errorf("%q: provider = %q, want %q", tt.ref, src.Provider, tt.provider)
		}
	}
}

func TestParseSourceRef_LocalCommitRef(t *testing.T) {
	_, cs := parseCommitRef(t, "./checkout@a1b2c3d...f6e5d4c")
	if cs.LocalPath != "./checkout" {
		t.Errorf("localPath = %q, want ./checkout", cs.LocalPath)
	}
	if cs.BaseRev != "a1b2c3d" || cs.HeadRev != "f6e5d4c" {
		t.Errorf("range = %q...%q", cs.BaseRev, cs.HeadRev)
	}
}

func TestParseSourceRef_SnapshotRefs(t *testing.T) {
	snap := SnapshotOptions{Include: []string{"a/**"}, Exclude: []string{"b/**"}, Base: "main"}
	tests := []struct {
		ref  string
		path string
	}{
		{"./checkout", "./checkout"},
		{"/abs/path", "/abs/path"},
		{"file:relative/dir", "relative/dir"},
		// Non-SHA suffix after @ on a path stays a snapshot path.
		{"./release@v2", "./release@v2"},
	}
	for _, tt := range tests {
		src, err := ParseSourceRef(tt.ref, snap, testLogger())
		if err != nil {
			t.Errorf("ParseSourceRef(%q): %v", tt.ref, err)
			continue
		}
		if src.Kind != KindSnapshot {
			t.Errorf("ParseSourceRef(%q) kind = %v, want snapshot", tt.ref, src.Kind)
			continue
		}
		ss := src.ChangeSource.(*SnapshotSource)
		if ss.Path != tt.path {
			t.Errorf("ParseSourceRef(%q) path = %q, want %q", tt.ref, ss.Path, tt.path)
		}
		if len(ss.Include) != 1 || len(ss.Exclude) != 1 || ss.Base != "main" {
			t.Errorf("ParseSourceRef(%q) snapshot options not threaded: %+v", tt.ref, ss)
		}
	}
}

func TestParseSourceRef_PRShortFormStillWins(t *testing.T) {
	// A '#' short-form must remain a PR source even though it contains no rev.
	src, err := ParseSourceRef("github:owner/repo#123", SnapshotOptions{}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if src.Kind != KindPR {
		t.Errorf("kind = %v, want pr", src.Kind)
	}
}

func TestParseSourceRef_BareGitURLWithoutRevRejected(t *testing.T) {
	// A git URL without @rev is not a valid source (nothing to diff).
	refs := []string{
		"git@github.com:o/r.git",
		"https://github.com/o/r.git",
	}
	for _, ref := range refs {
		if _, err := ParseSourceRef(ref, SnapshotOptions{}, testLogger()); err == nil {
			t.Errorf("ParseSourceRef(%q) expected error", ref)
		}
	}
}

func TestInferProviderFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"git@github.com:o/r.git", "github"},
		{"https://github.enterprise.io/o/r.git", "github"},
		{"git@gitlab.com:g/r.git", "gitlab"},
		{"https://gitlab.example.com/g/r.git", "gitlab"},
		{"git@bitbucket.org:o/r.git", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := inferProviderFromURL(tt.url); got != tt.want {
			t.Errorf("inferProviderFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"services/payments/**", "services/payments/deploy.yaml", true},
		{"services/payments/**", "services/payments/sub/dir/file.yaml", true},
		{"services/payments/**", "services/billing/deploy.yaml", false},
		{"**/*.yaml", "a/b/c.yaml", true},
		{"**/*.yaml", "top.yaml", true},
		{"**/*.yaml", "a/b/c.json", false},
		{"*.yaml", "top.yaml", true},
		{"*.yaml", "a/top.yaml", false},
		// No wildcards → directory prefix convenience.
		{"services/payments", "services/payments/deploy.yaml", true},
		{"services/payments", "services/payments", true},
		{"services/payments", "services/payments-v2/deploy.yaml", false},
	}
	for _, tt := range tests {
		if got := matchGlob(tt.pattern, tt.path); got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}
