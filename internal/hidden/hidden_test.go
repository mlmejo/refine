package hidden

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "state", "hidden.json")}
	if keys, err := store.Load(); err != nil || keys != nil {
		t.Fatalf("empty load = %v, %v", keys, err)
	}
	if err := store.Save([]string{"GEEK-9", "GEEK-1"}); err != nil {
		t.Fatal(err)
	}
	if keys, err := store.Load(); err != nil || !slices.Equal(keys, []string{"GEEK-1", "GEEK-9"}) {
		t.Errorf("load = %v, %v", keys, err)
	}
}

func TestCorruptFileNamesItsPath(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "hidden.json")}
	if err := os.WriteFile(store.Path, []byte(`["GEEK-1", 2]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if keys, err := store.Load(); err == nil || keys != nil || !strings.Contains(err.Error(), store.Path) {
		t.Errorf("load = %v, %v", keys, err)
	}
}

func TestSetKeepsKeysAcrossOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "hidden.json")
	set, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if set.Has("GEEK-1") {
		t.Fatal("empty set has GEEK-1")
	}
	for _, key := range []string{"GEEK-9", "GEEK-1", "GEEK-5"} {
		if err := set.Set(key, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Set("GEEK-5", false); err != nil {
		t.Fatal(err)
	}
	if !set.Has("GEEK-1") || set.Has("GEEK-5") {
		t.Errorf("Has = %v %v", set.Has("GEEK-1"), set.Has("GEEK-5"))
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.Has("GEEK-9") || !reopened.Has("GEEK-1") || reopened.Has("GEEK-5") {
		t.Error("reopened set lost keys")
	}
	if keys, _ := (Store{Path: path}).Load(); !slices.Equal(keys, []string{"GEEK-1", "GEEK-9"}) {
		t.Errorf("file = %v", keys)
	}
}

func TestSetSkipsTheFileWhenNothingChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hidden.json")
	set, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Set("GEEK-1", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("unhiding an unknown key wrote the file: %v", err)
	}
}
