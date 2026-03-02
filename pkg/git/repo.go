package git

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Repository is the interface for git operations.
// All methods try the go-git library first and fall back to the git CLI.
type Repository interface {
	Dir() string
	CreateBranch(name string) error
	AddAll() error
	Commit(message, author, email string) error
	Push(ctx context.Context, token string) error
	CurrentBranch() (string, error)
	RemoteURL() (string, error)
}

// Repo implements Repository using go-git with CLI fallback.
type Repo struct {
	gg     *gogit.Repository // nil when go-git is unavailable
	dir    string
	logger *slog.Logger
}

// Clone clones a repository. It tries go-git first, then falls back to git CLI.
func Clone(ctx context.Context, url, dir, branch string, logger *slog.Logger) (Repository, error) {
	logger.Info("cloning repository", "url", url, "dir", dir, "branch", branch)

	// Try go-git.
	opts := &gogit.CloneOptions{URL: url}
	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		opts.SingleBranch = true
	}

	r, err := gogit.PlainCloneContext(ctx, dir, false, opts)
	if err == nil {
		return &Repo{gg: r, dir: dir, logger: logger}, nil
	}
	libErr := err

	// Fallback to git CLI.
	if !hasBinary("git") {
		return nil, fmt.Errorf("go-git clone failed: %w (git CLI not available for fallback)", libErr)
	}

	logger.Info("go-git clone failed, falling back to git CLI", "error", libErr)
	if err := cliClone(ctx, url, dir, branch); err != nil {
		return nil, fmt.Errorf("clone failed — go-git: %v, git CLI: %w", libErr, err)
	}

	// Try to open with go-git so local operations can use the library.
	gg, _ := gogit.PlainOpen(dir)
	return &Repo{gg: gg, dir: dir, logger: logger}, nil
}

// Open opens an existing repository. go-git handles this reliably for any
// valid repo, so there is no CLI fallback needed here.
func Open(dir string, logger *slog.Logger) (Repository, error) {
	r, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("opening repo at %s: %w", dir, err)
	}
	return &Repo{gg: r, dir: dir, logger: logger}, nil
}

func (r *Repo) Dir() string { return r.dir }

// ---------------------------------------------------------------------------
// CreateBranch
// ---------------------------------------------------------------------------

func (r *Repo) CreateBranch(name string) error {
	if r.gg != nil {
		err := r.createBranchLib(name)
		if err == nil {
			return nil
		}
		if !hasBinary("git") {
			return err
		}
		r.logger.Debug("go-git CreateBranch failed, falling back to git CLI", "error", err)
	}
	return cliCreateBranch(r.dir, name)
}

func (r *Repo) createBranchLib(name string) error {
	wt, err := r.gg.Worktree()
	if err != nil {
		return err
	}
	head, err := r.gg.Head()
	if err != nil {
		return err
	}
	ref := plumbing.NewBranchReferenceName(name)
	if err := r.gg.Storer.SetReference(plumbing.NewHashReference(ref, head.Hash())); err != nil {
		return err
	}
	return wt.Checkout(&gogit.CheckoutOptions{Branch: ref})
}

// ---------------------------------------------------------------------------
// AddAll
// ---------------------------------------------------------------------------

func (r *Repo) AddAll() error {
	if r.gg != nil {
		wt, err := r.gg.Worktree()
		if err == nil {
			if err := wt.AddWithOptions(&gogit.AddOptions{All: true}); err == nil {
				return nil
			} else if !hasBinary("git") {
				return err
			} else {
				r.logger.Debug("go-git AddAll failed, falling back to git CLI", "error", err)
			}
		}
	}
	return cliAddAll(r.dir)
}

// ---------------------------------------------------------------------------
// Commit
// ---------------------------------------------------------------------------

func (r *Repo) Commit(message, author, email string) error {
	if r.gg != nil {
		wt, err := r.gg.Worktree()
		if err == nil {
			_, cerr := wt.Commit(message, &gogit.CommitOptions{
				Author: &object.Signature{
					Name:  author,
					Email: email,
					When:  time.Now(),
				},
			})
			if cerr == nil {
				return nil
			}
			if !hasBinary("git") {
				return cerr
			}
			r.logger.Debug("go-git Commit failed, falling back to git CLI", "error", cerr)
		}
	}
	return cliCommit(r.dir, message, author, email)
}

// ---------------------------------------------------------------------------
// Push
// ---------------------------------------------------------------------------

func (r *Repo) Push(ctx context.Context, token string) error {
	if r.gg != nil {
		opts := &gogit.PushOptions{}
		if token != "" {
			opts.Auth = &http.BasicAuth{
				Username: "loom",
				Password: token,
			}
		}
		err := r.gg.PushContext(ctx, opts)
		if err == nil {
			return nil
		}
		if !hasBinary("git") {
			return err
		}
		r.logger.Info("go-git push failed, falling back to git CLI", "error", err)
	}
	return cliPush(ctx, r.dir)
}

// ---------------------------------------------------------------------------
// CurrentBranch
// ---------------------------------------------------------------------------

func (r *Repo) CurrentBranch() (string, error) {
	if r.gg != nil {
		head, err := r.gg.Head()
		if err == nil {
			return head.Name().Short(), nil
		}
		if !hasBinary("git") {
			return "", err
		}
		r.logger.Debug("go-git CurrentBranch failed, falling back to git CLI", "error", err)
	}
	return cliCurrentBranch(r.dir)
}

// ---------------------------------------------------------------------------
// RemoteURL
// ---------------------------------------------------------------------------

func (r *Repo) RemoteURL() (string, error) {
	if r.gg != nil {
		remote, err := r.gg.Remote("origin")
		if err == nil {
			cfg := remote.Config()
			if len(cfg.URLs) > 0 {
				return cfg.URLs[0], nil
			}
		}
		if !hasBinary("git") {
			return "", err
		}
		r.logger.Debug("go-git RemoteURL failed, falling back to git CLI", "error", err)
	}
	return cliRemoteURL(r.dir)
}

// hasBinary reports whether a binary is available on PATH.
func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
