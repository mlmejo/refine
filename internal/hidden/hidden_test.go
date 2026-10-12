package hidden

import (
	"path/filepath"
	"slices"
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
