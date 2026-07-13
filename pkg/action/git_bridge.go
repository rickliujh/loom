package action

import (
	"context"
	"os"

	"github.com/rickliujh/loom/pkg/git"
)

// commitOnly stages all changes and commits without pushing.
func commitOnly(_ context.Context, execCtx *ExecutionContext, message, author, email string) error {
	repo, err := git.Open(execCtx.TargetDir, execCtx.Logger)
	if err != nil {
		return err
	}

	if err := repo.AddAll(); err != nil {
		return actionError("commitPush", err)
	}

	if err := repo.Commit(message, author, email); err != nil {
		return actionError("commitPush", err)
	}

	return nil
}

// commitAndPush stages all changes, commits, and pushes.
func commitAndPush(ctx context.Context, execCtx *ExecutionContext, message, author, email string) error {
	repo, err := git.Open(execCtx.TargetDir, execCtx.Logger)
	if err != nil {
		return err
	}

	if err := repo.AddAll(); err != nil {
		return actionError("commitPush", err)
	}

	if err := repo.Commit(message, author, email); err != nil {
		return actionError("commitPush", err)
	}

	token := os.Getenv("LOOM_GIT_TOKEN")
	if err := repo.Push(ctx, token); err != nil {
		return actionError("commitPush", err)
	}

	return nil
}

// openPR opens a pull request using the configured provider.
func openPR(ctx context.Context, execCtx *ExecutionContext, providerName, tokenEnv, baseBranch string, labels []string, title, body string) error {
	provider, err := git.NewProvider(providerName, tokenEnv, execCtx.Logger)
	if err != nil {
		return actionError("pr", err)
	}

	repo, err := git.Open(execCtx.TargetDir, execCtx.Logger)
	if err != nil {
		return actionError("pr", err)
	}

	headBranch, err := repo.CurrentBranch()
	if err != nil {
		return actionError("pr", err)
	}

	repoURL, err := repo.RemoteURL()
	if err != nil {
		return actionError("pr", err)
	}

	if baseBranch == "" {
		baseBranch = "main"
	}

	prURL, err := provider.CreatePR(ctx, git.PROptions{
		RepoURL:    repoURL,
		Title:      title,
		Body:       body,
		HeadBranch: headBranch,
		BaseBranch: baseBranch,
		Labels:     labels,
		WorkDir:    execCtx.TargetDir,
	})
	if err != nil {
		return actionError("pr", err)
	}

	execCtx.Logger.Info("PR created", "url", prURL)
	execCtx.Summary.AddPR(execCtx.ModuleName, title, prURL)
	return nil
}
