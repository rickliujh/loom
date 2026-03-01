package git

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitcfg "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Repo wraps a go-git repository for loom operations.
type Repo struct {
	repo   *gogit.Repository
	dir    string
	logger *slog.Logger
}

// Clone clones a git repository to the given directory.
func Clone(ctx context.Context, url, dir, branch string, logger *slog.Logger) (*Repo, error) {
	opts := &gogit.CloneOptions{
		URL:      url,
		Progress: nil,
	}
	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		opts.SingleBranch = true
	}

	logger.Info("cloning repository", "url", url, "dir", dir, "branch", branch)
	r, err := gogit.PlainCloneContext(ctx, dir, false, opts)
	if err != nil {
		return nil, fmt.Errorf("cloning %s: %w", url, err)
	}

	return &Repo{repo: r, dir: dir, logger: logger}, nil
}

// Open opens an existing git repository.
func Open(dir string, logger *slog.Logger) (*Repo, error) {
	r, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("opening repo at %s: %w", dir, err)
	}
	return &Repo{repo: r, dir: dir, logger: logger}, nil
}

// Dir returns the repository working directory.
func (r *Repo) Dir() string {
	return r.dir
}

// CreateBranch creates and checks out a new branch.
func (r *Repo) CreateBranch(name string) error {
	wt, err := r.repo.Worktree()
	if err != nil {
		return err
	}

	head, err := r.repo.Head()
	if err != nil {
		return err
	}

	ref := plumbing.NewBranchReferenceName(name)
	err = r.repo.Storer.SetReference(plumbing.NewHashReference(ref, head.Hash()))
	if err != nil {
		return err
	}

	return wt.Checkout(&gogit.CheckoutOptions{
		Branch: ref,
	})
}

// AddAll stages all changes.
func (r *Repo) AddAll() error {
	wt, err := r.repo.Worktree()
	if err != nil {
		return err
	}

	// Add will stage all changes recursively.
	err = wt.AddWithOptions(&gogit.AddOptions{All: true})
	if err != nil {
		return err
	}
	return nil
}

// Commit creates a commit with the given message and author info.
func (r *Repo) Commit(message, author, email string) error {
	wt, err := r.repo.Worktree()
	if err != nil {
		return err
	}

	_, err = wt.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  author,
			Email: email,
			When:  time.Now(),
		},
	})
	return err
}

// Push pushes commits to the remote.
func (r *Repo) Push(ctx context.Context, token string) error {
	opts := &gogit.PushOptions{}
	if token != "" {
		opts.Auth = &http.BasicAuth{
			Username: "loom", // username is ignored for token auth
			Password: token,
		}
	}
	return r.repo.PushContext(ctx, opts)
}

// CurrentBranch returns the name of the current branch.
func (r *Repo) CurrentBranch() (string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return "", err
	}
	return head.Name().Short(), nil
}

// RemoteURL returns the URL of the "origin" remote.
func (r *Repo) RemoteURL() (string, error) {
	remote, err := r.repo.Remote("origin")
	if err != nil {
		return "", err
	}
	cfg := remote.Config()
	if len(cfg.URLs) == 0 {
		return "", fmt.Errorf("no URLs configured for origin remote")
	}
	return cfg.URLs[0], nil
}

// SetRemoteURL updates the origin remote URL.
func (r *Repo) SetRemoteURL(url string) error {
	err := r.repo.DeleteRemote("origin")
	if err != nil {
		return err
	}
	_, err = r.repo.CreateRemote(&gogitcfg.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})
	return err
}
