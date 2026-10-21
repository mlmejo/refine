package home

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withRepos is a loaded home screen whose repos live in a map, under a
// folder holding two git checkouts and a plain folder.
func withRepos(t *testing.T) (Model, string, map[string]string) {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"dating/.git", "wannahookup/.git", "notes", ".hidden/.git"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	repos := map[string]string{"dating": filepath.Join(root, "dating")}
	source := Source{
		List:  func(context.Context) (Page, error) { return Page{Issues: issues}, nil },
		Repos: func() map[string]string { return repos },
		AddRepo: func(name, dir string) error {
			repos = maps.Clone(repos)
			repos[name] = dir
			return nil
		},
		RemoveRepo: func(name string) error {
			repos = maps.Clone(repos)
			delete(repos, name)
			return nil
		},
	}
	m := New("Refinement Ready", source, Refiner{})
	next, _ := m.Update(m.Init()())
	return next.(Model), root, repos
}

func TestReposScreenAddsAGitRepoFromTheBrowser(t *testing.T) {
	m, root, _ := withRepos(t)
	m = press(t, m, "R")
	if m.screen != repos || !strings.Contains(m.View(), "dating") {
		t.Fatalf("screen = %v:\n%s", m.screen, m.View())
	}

	m = press(t, m, "a")
	if m.screen != browse || m.dir != root {
		t.Fatalf("browser at %q, want %q", m.dir, root)
	}
	view := m.View()
	if strings.Contains(view, ".hidden") || !strings.Contains(view, "dating/ · added") || !strings.Contains(view, "wannahookup/ · git") {
		t.Errorf("browser view:\n%s", view)
	}

	// notes is not a git repo; dating is already listed.
	if m = press(t, m, "j", "a"); m.naming || m.repoErr == nil {
		t.Errorf("picked notes: naming %v, err %v", m.naming, m.repoErr)
	}
	m = press(t, m, "k", "k", "a")
	if m.naming || m.repoErr == nil || !strings.Contains(m.repoErr.Error(), "already listed") {
		t.Errorf("picked dating: naming %v, err %v", m.naming, m.repoErr)
	}

	m = press(t, m, "j", "j", "a")
	if !m.naming || m.nameIn.Value() != "wannahookup" {
		t.Fatalf("naming %v, name %q", m.naming, m.nameIn.Value())
	}
	m = press(t, m, "enter")
	if m.screen != repos || m.source.Repos()["wannahookup"] != filepath.Join(root, "wannahookup") {
		t.Errorf("screen %v, repos %v", m.screen, m.source.Repos())
	}
	if !strings.Contains(m.View(), "added wannahookup") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestBrowserGoesInAndBackUp(t *testing.T) {
	m, root, _ := withRepos(t)
	m = press(t, m, "R", "a", "j", "j", "l")
	if m.dir != filepath.Join(root, "wannahookup") {
		t.Fatalf("dir = %q", m.dir)
	}
	m = press(t, m, "h")
	if m.dir != root || m.folders[m.dirCursor].name != "wannahookup" {
		t.Errorf("dir %q, cursor on %v", m.dir, m.folders[m.dirCursor])
	}
	if m = press(t, m, "esc", "esc"); m.screen != list {
		t.Errorf("screen = %v", m.screen)
	}
}

func TestReposScreenRemovesAfterYes(t *testing.T) {
	m, _, _ := withRepos(t)
	m = press(t, m, "R", "x")
	if !strings.Contains(m.View(), "remove dating from the config? y/n") {
		t.Fatalf("view:\n%s", m.View())
	}
	if m = press(t, m, "n"); len(m.source.Repos()) != 1 {
		t.Fatal("n removed the repo")
	}
	if m = press(t, m, "x", "y"); len(m.source.Repos()) != 0 {
		t.Errorf("repos = %v", m.source.Repos())
	}
}
