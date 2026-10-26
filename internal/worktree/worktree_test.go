package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, repo, file, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, file), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "add", file)
	run(t, repo, "commit", "--quiet", "-m", body)
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCheckoutReadsBranchWithoutTouchingCheckout(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	run(t, repo, "init", "--quiet", "-b", "develop")
	commit(t, repo, "app.txt", "develop v1")
	run(t, repo, "checkout", "--quiet", "-b", "feature/x")
	commit(t, repo, "app.txt", "feature work")
	os.WriteFile(filepath.Join(repo, "app.txt"), []byte("uncommitted"), 0o600)

	dest := filepath.Join(t.TempDir(), "worktrees", "app")
	if _, err := Checkout(ctx, repo, "develop", dest); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dest, "app.txt")); got != "develop v1" {
		t.Errorf("worktree has %q", got)
	}
	if got := read(t, filepath.Join(repo, "app.txt")); got != "uncommitted" {
		t.Errorf("checkout changed to %q", got)
	}
	if got := run(t, repo, "branch", "--show-current"); got != "feature/x" {
		t.Errorf("branch changed to %q", got)
	}

	run(t, repo, "stash", "--quiet")
	run(t, repo, "checkout", "--quiet", "develop")
	commit(t, repo, "app.txt", "develop v2")
	run(t, repo, "checkout", "--quiet", "feature/x")

	os.WriteFile(filepath.Join(dest, "app.txt"), []byte("scribbled"), 0o600)
	if _, err := Checkout(ctx, repo, "develop", dest); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dest, "app.txt")); got != "develop v2" {
		t.Errorf("worktree not updated: %q", got)
	}
}

func TestCheckoutRecreatesDeletedWorktree(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	run(t, repo, "init", "--quiet", "-b", "develop")
	commit(t, repo, "app.txt", "v1")

	dest := filepath.Join(t.TempDir(), "app")
	if _, err := Checkout(ctx, repo, "develop", dest); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(dest)
	if _, err := Checkout(ctx, repo, "develop", dest); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dest, "app.txt")); got != "v1" {
		t.Errorf("worktree has %q", got)
	}
}

func TestCheckoutMissingBranch(t *testing.T) {
	repo := t.TempDir()
	run(t, repo, "init", "--quiet", "-b", "main")
	commit(t, repo, "app.txt", "v1")
	_, err := Checkout(context.Background(), repo, "develop", filepath.Join(t.TempDir(), "app"))
	if err == nil || !strings.Contains(err.Error(), "has no develop branch") {
		t.Errorf("err = %v", err)
	}
}
