package gitsync

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

// cloned returns an origin whose default branch is main, and a clone of it.
func cloned(t *testing.T) (origin, clone string) {
	t.Helper()
	origin = t.TempDir()
	run(t, origin, "init", "--quiet", "-b", "main")
	commit(t, origin, "app.txt", "v1")
	clone = filepath.Join(t.TempDir(), "app")
	run(t, origin, "clone", "--quiet", origin, clone)
	return origin, clone
}

func TestFetchPrefersDevelop(t *testing.T) {
	origin, clone := cloned(t)
	run(t, origin, "branch", "develop")
	s, err := Fetch(context.Background(), "app", clone)
	if err != nil || s.Error != "" || s.Branch != "develop" || s.Commit != run(t, origin, "rev-parse", "develop") {
		t.Fatalf("status = %+v", s)
	}
}

func TestFetchFallsBackToTheDefaultBranchAndCountsNewCommits(t *testing.T) {
	ctx := context.Background()
	origin, clone := cloned(t)
	if s, err := Fetch(ctx, "app", clone); err != nil || s.Branch != "main" || s.New != 0 {
		t.Fatalf("first fetch = %+v %v", s, err)
	}
	commit(t, origin, "app.txt", "v2")
	commit(t, origin, "app.txt", "v3")
	s, err := Fetch(ctx, "app", clone)
	if err != nil || s.New != 2 || s.Commit != run(t, origin, "rev-parse", "main") {
		t.Fatalf("second fetch = %+v %v", s, err)
	}
	// The checkout is not touched.
	if got, _ := os.ReadFile(filepath.Join(clone, "app.txt")); string(got) != "v1" {
		t.Errorf("app.txt = %q", got)
	}
}

func TestFetchFailsWithoutOrigin(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "--quiet")
	s, err := Fetch(context.Background(), "app", dir)
	if err == nil || s.Error != "" || s.Name != "app" || s.Commit != "" {
		t.Fatalf("status = %+v %v", s, err)
	}
}
