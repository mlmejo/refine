// Package worktree checks a branch out into a refine-owned git worktree, so
// Claude reads that branch without touching the developer's checkout.
package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mlmejo/refine/internal/gitcmd"
)

// Checkout puts ref, a branch or a commit, into a detached worktree of repo
// at dest. It prefers origin/<ref> over the local branch.
func Checkout(ctx context.Context, repo, ref, dest string) error {
	commit, err := resolve(ctx, repo, ref)
	if err != nil {
		return err
	}

	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		if same(ctx, repo, dest) {
			_, err := gitcmd.Run(ctx, dest, "checkout", "--quiet", "--force", "--detach", commit)
			return err
		}
		// A worktree of another clone, say from before the repo moved: it
		// cannot read this repo's commits, so it is made again.
		if err := os.RemoveAll(dest); err != nil {
			return err
		}
	}

	if _, err := gitcmd.Run(ctx, repo, "worktree", "prune"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	_, err = gitcmd.Run(ctx, repo, "worktree", "add", "--quiet", "--detach", dest, commit)
	return err
}

// same reports whether the worktree at dest shares repo's object store.
func same(ctx context.Context, repo, dest string) bool {
	common := func(dir string) string {
		out, err := gitcmd.Run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			return ""
		}
		if real, err := filepath.EvalSymlinks(out); err == nil {
			return real
		}
		return out
	}
	want := common(repo)
	return want != "" && common(dest) == want
}

func resolve(ctx context.Context, repo, ref string) (string, error) {
	for _, candidate := range []string{"refs/remotes/origin/" + ref, "refs/heads/" + ref, ref} {
		if commit, err := gitcmd.Run(ctx, repo, "rev-parse", "--verify", "--quiet", candidate+"^{commit}"); err == nil {
			return commit, nil
		}
	}
	return "", fmt.Errorf("%s has no %s branch", filepath.Base(repo), ref)
}
