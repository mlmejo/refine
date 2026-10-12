// Package worktree checks a branch out into a refine-owned git worktree, so
// Claude reads that branch without touching the developer's checkout.
package worktree

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Checkout puts ref into a detached worktree of repo at dest and returns its commit.
// With fetch it refreshes origin first and prefers origin/<ref> over the local branch.
func Checkout(ctx context.Context, repo, ref, dest string, fetch bool) (string, error) {
	if fetch {
		// A failed fetch (offline, no origin) falls back to what is already there.
		_, _ = git(ctx, repo, "fetch", "--quiet", "origin", ref)
	}

	commit, err := resolve(ctx, repo, ref)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		if _, err := git(ctx, dest, "checkout", "--quiet", "--force", "--detach", commit); err != nil {
			return "", err
		}
		return commit, nil
	}

	if _, err := git(ctx, repo, "worktree", "prune"); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	if _, err := git(ctx, repo, "worktree", "add", "--quiet", "--detach", dest, commit); err != nil {
		return "", err
	}
	return commit, nil
}

func resolve(ctx context.Context, repo, ref string) (string, error) {
	for _, candidate := range []string{"refs/remotes/origin/" + ref, "refs/heads/" + ref} {
		if commit, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", candidate+"^{commit}"); err == nil {
			return commit, nil
		}
	}
	return "", fmt.Errorf("%s has no %s branch", filepath.Base(repo), ref)
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s in %s: %w: %s", args[0], filepath.Base(dir), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
