package generate

// ChangeType classifies what happened to a file in a PR/MR.
type ChangeType int

const (
	ChangeAdded ChangeType = iota
	ChangeModified
	ChangeDeleted
	ChangeRenamed
)

func (c ChangeType) String() string {
	switch c {
	case ChangeAdded:
		return "added"
	case ChangeModified:
		return "modified"
	case ChangeDeleted:
		return "deleted"
	case ChangeRenamed:
		return "renamed"
	default:
		return "unknown"
	}
}

// FileChange represents a single file change from a PR/MR.
type FileChange struct {
	Type       ChangeType
	Path       string // file path relative to repo root
	OldPath    string // previous path (only for renames)
	NewContent []byte // full file content after the change (for added/modified)
	OldContent []byte // full file content before the change (for modified)
}

// PRInfo holds metadata about a PR/MR.
type PRInfo struct {
	Title      string
	Body       string
	BaseBranch string
	HeadBranch string
	RepoURL    string
	Provider   string // "github" or "gitlab"
	Files      []FileChange
}
