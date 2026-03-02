package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CLI fallback implementations for git operations.
// These rely on the system git binary and its configured credential helpers,
// SSH agent, etc. — making loom work naturally on a DevOps laptop.

func cliClone(ctx context.Context, url, dir, branch string) error {
	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch, "--single-branch")
	}
	args = append(args, url, dir)

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w\n%s", err, output)
	}
	return nil
}

func cliCreateBranch(dir, name string) error {
	cmd := exec.Command("git", "checkout", "-b", name)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout -b: %w\n%s", err, output)
	}
	return nil
}

func cliAddAll(dir string) error {
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git add -A: %w\n%s", err, output)
	}
	return nil
}

func cliCommit(dir, message, author, email string) error {
	args := []string{}
	if author != "" {
		args = append(args, "-c", "user.name="+author)
	}
	if email != "" {
		args = append(args, "-c", "user.email="+email)
	}
	args = append(args, "commit", "-m", message)

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, output)
	}
	return nil
}

func cliPush(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "push", "origin", "HEAD")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push: %w\n%s", err, output)
	}
	return nil
}

func cliCurrentBranch(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w\n%s", err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

func cliRemoteURL(dir string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git remote get-url: %w\n%s", err, output)
	}
	return strings.TrimSpace(string(output)), nil
}
