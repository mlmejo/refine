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

// cloned returns an origin with a develop branch and a clone of it on develop.
func cloned(t *testing.T) (origin, clone string) {
	t.Helper()
	origin = t.TempDir()
	run(t, origin, "init", "--quiet", "-b", "develop")
	commit(t, origin, "app.txt", "v1")
	clone = filepath.Join(t.TempDir(), "app")
	run(t, origin, "clone", "--quiet", origin, clone)
	return origin, clone
}

func TestCheckUpToDate(t *testing.T) {
	_, clone := cloned(t)
	s := Check(context.Background(), "app", clone, "develop")
	if s.Behind != 0 || s.Skip != "" {
		t.Errorf("status = %+v", s)
	}
}

func TestCheckBehindThenPull(t *testing.T) {
	ctx := context.Background()
	origin, clone := cloned(t)
	commit(t, origin, "app.txt", "v2")
	commit(t, origin, "app.txt", "v3")
	// Untracked files do not stop the pull.
	os.WriteFile(filepath.Join(clone, "notes.txt"), []byte("mine"), 0o600)

	s := Check(ctx, "app", clone, "develop")
	if s.Behind != 2 || s.Skip != "" || s.Origin != run(t, origin, "rev-parse", "HEAD") {
		t.Fatalf("status = %+v", s)
	}
	if err := Pull(ctx, s); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(clone, "app.txt")); string(got) != "v3" {
		t.Errorf("app.txt = %q", got)
	}
	if s := Check(ctx, "app", clone, "develop"); s.Behind != 0 || s.Skip != "" {
		t.Errorf("after pull: %+v", s)
	}
}

func TestCheckSkips(t *testing.T) {
	ctx := context.Background()

	t.Run("dirty", func(t *testing.T) {
		origin, clone := cloned(t)
		commit(t, origin, "app.txt", "v2")
		os.WriteFile(filepath.Join(clone, "app.txt"), []byte("editing"), 0o600)
		s := Check(ctx, "app", clone, "develop")
		if s.Behind != 0 || !strings.Contains(s.Skip, "uncommitted changes") {
			t.Errorf("status = %+v", s)
		}
	})

	t.Run("diverged", func(t *testing.T) {
		origin, clone := cloned(t)
		commit(t, origin, "app.txt", "v2")
		commit(t, clone, "local.txt", "local")
		s := Check(ctx, "app", clone, "develop")
		if s.Behind != 0 || !strings.Contains(s.Skip, "cannot fast-forward") {
			t.Errorf("status = %+v", s)
		}
	})

	t.Run("offline", func(t *testing.T) {
		origin, clone := cloned(t)
		os.RemoveAll(origin)
		s := Check(ctx, "app", clone, "develop")
		if s.Behind != 0 || !strings.Contains(s.Skip, "could not fetch origin") {
			t.Errorf("status = %+v", s)
		}
	})

	t.Run("other branch", func(t *testing.T) {
		origin, clone := cloned(t)
		commit(t, origin, "app.txt", "v2")
		run(t, clone, "checkout", "--quiet", "-b", "feature/x")
		if s := Check(ctx, "app", clone, "develop"); s.Behind != 0 || s.Skip != "" {
			t.Errorf("status = %+v", s)
		}
	})
}
