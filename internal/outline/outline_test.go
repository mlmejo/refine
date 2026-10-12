package outline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type memory struct {
	entries map[string]string
	saves   int
}

func (m *memory) Outline(repo, commit string) (string, bool, error) {
	text, ok := m.entries[repo+"@"+commit]
	return text, ok, nil
}

func (m *memory) SaveOutline(repo, commit, outline string) error {
	m.entries[repo+"@"+commit] = outline
	m.saves++
	return nil
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, repo, file string) {
	t.Helper()
	full := filepath.Join(repo, file)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSummariseCountsDirectoriesAndSkipsNoise(t *testing.T) {
	got := Summarise([]string{
		"composer.json", "app/Models/User.php", "app/Models/Book.php", "app/Http/Kernel.php",
		".github/workflows/test.yml", ".claude/settings.json", "vendor/x/y.php",
		"assets/logo.png", "package-lock.json", "yarn.lock", "",
	})
	want := "./ 2\n.github/workflows/ 1\napp/Http/ 1\napp/Models/ 2\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGetCachesCleanCheckoutsByCommit(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	run(t, repo, "init", "--quiet")
	write(t, repo, "app/Models/User.php")
	run(t, repo, "add", ".")
	run(t, repo, "commit", "--quiet", "-m", "one")
	cache := &memory{entries: map[string]string{}}

	first, err := Get(ctx, cache, "api", repo)
	if err != nil || first.Cached || first.Dirty || first.Text != "app/Models/ 1\n" || cache.saves != 1 {
		t.Fatalf("first = %+v err %v saves %d", first, err, cache.saves)
	}

	os.WriteFile(filepath.Join(repo, "app/Models/User.php"), []byte("edited"), 0o600)
	again, err := Get(ctx, cache, "api", repo)
	if err != nil || !again.Cached || again.Commit != first.Commit {
		t.Errorf("edit to a tracked file should hit the cache: %+v err %v", again, err)
	}

	write(t, repo, "app/Models/Book.php")
	dirty, err := Get(ctx, cache, "api", repo)
	if err != nil || !dirty.Dirty || dirty.Cached || !strings.Contains(dirty.Text, "app/Models/ 2") || cache.saves != 1 {
		t.Errorf("untracked file should rebuild without caching: %+v err %v saves %d", dirty, err, cache.saves)
	}

	run(t, repo, "add", ".")
	run(t, repo, "commit", "--quiet", "-am", "two")
	next, err := Get(ctx, cache, "api", repo)
	if err != nil || next.Cached || next.Commit == first.Commit || next.Text != "app/Models/ 2\n" || cache.saves != 2 {
		t.Errorf("new commit should build and cache: %+v err %v saves %d", next, err, cache.saves)
	}
}
