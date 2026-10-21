package reposcan

import (
	"os"
	"path/filepath"
	"testing"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanFindsCheckoutsBelow(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root,
		"dating/.git",
		"dating/packages/inner/.git", // inside a checkout: not looked at
		"work/api/.git",
		"play/api/.git", // same folder name as work/api
		"notes",
		".hidden/x/.git",
		"web/node_modules/dep/.git",
		"a/b/c/deep/.git", // four down: too deep
		"a/b/shallow/.git",
	)
	// A worktree's .git is a file.
	mkdirs(t, root, "tree")
	if err := os.WriteFile(filepath.Join(root, "tree", ".git"), []byte("gitdir: x"), 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := Scan(root, map[string]string{"dating": filepath.Join(root, "dating") + "/"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Found{
		{Name: "shallow", Dir: filepath.Join(root, "a/b/shallow")},
		{Name: "dating", Dir: filepath.Join(root, "dating"), Listed: "dating"},
		{Name: "play-api", Dir: filepath.Join(root, "play/api")},
		{Name: "tree", Dir: filepath.Join(root, "tree")},
		{Name: "work-api", Dir: filepath.Join(root, "work/api")},
	}
	if len(found) != len(want) {
		t.Fatalf("found %+v", found)
	}
	for i := range want {
		if found[i] != want[i] {
			t.Errorf("found[%d] = %+v, want %+v", i, found[i], want[i])
		}
	}
}

func TestScanOfACheckoutIsJustIt(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "api/.git", "api/sub/.git")
	found, err := Scan(filepath.Join(root, "api"), nil)
	if err != nil || len(found) != 1 || found[0].Name != "api" {
		t.Fatalf("found %+v, err %v", found, err)
	}
}

func TestScanAvoidsTakenNames(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "work/api/.git")
	// "api" and "work-api" are both configured for other folders.
	found, err := Scan(root, map[string]string{"api": "/elsewhere/api", "work-api": "/elsewhere/work-api"})
	if err != nil || len(found) != 1 || found[0].Name != "work-api-2" || found[0].Listed != "" {
		t.Fatalf("found %+v, err %v", found, err)
	}
}

func TestScanOfAMissingFolderFails(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "gone"), nil); err == nil {
		t.Fatal("no error")
	}
}
