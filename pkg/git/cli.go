package git

import (
	"context"
	"fmt"
	"strings"

	"github.com/rickliujh/loom/internal/proc"
)

// CLI fallback implementations for git operations.
// These rely on the system git binary and its configured credential helpers,
// SSH agent, etc. — making loom work naturally on a DevOps laptop.
//
// Every helper takes a context and starts git through proc.Command, so a
// cancelled run stops the subprocess and, under `loom serve`, the whole
// process tree git spawned (ssh, credential helpers).

func cliClone(ctx context.Context, url, dir, branch string) error {
	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch, "--single-branch")
	}
	args = append(args, url, dir)

	cmd := proc.Command(ctx, "git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w\n%s", err, output)
	}
	return nil
}

func cliCreateBranch(ctx context.Context, dir, name string) error {
	cmd := proc.Command(ctx, "git", "checkout", "-b", name)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout -b: %w\n%s", err, output)
	}
	return nil
}

func cliAddAll(ctx context.Context, dir string) error {
	cmd := proc.Command(ctx, "git", "add", "-A")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git add -A: %w\n%s", err, output)
	}
	return nil
}

func cliCommit(ctx context.Context, dir, message, author, email string) error {
	args := []string{}
	if author != "" {
		args = append(args, "-c", "user.name="+author)
	}
	if email != "" {
		args = append(args, "-c", "user.email="+email)
	}
	args = append(args, "commit", "-m", message)

	cmd := proc.Command(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, output)
	}
	return nil
}

func cliPush(ctx context.Context, dir, branch string) error {
	args := []string{"push", "-u", "origin"}
	if branch != "" {
		args = append(args, fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
	} else {
		args = append(args, "HEAD")
	}

	cmd := proc.Command(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push: %w\n%s", err, output)
	}
	return nil
}

func cliCurrentBranch(ctx context.Context, dir string) (string, error) {
	cmd := proc.Command(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w\n%s", err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

func cliRemoteURL(ctx context.Context, dir string) (string, error) {
	cmd := proc.Command(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git remote get-url: %w\n%s", err, output)
	}
	return strings.TrimSpace(string(output)), nil
}
