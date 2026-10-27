// Package hidden remembers tickets removed from the home list.
package hidden

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
)

// Store keeps hidden ticket keys in one JSON file.
type Store struct {
	Path string
}

// Load returns the hidden keys; a missing file means none.
func (s Store) Load() ([]string, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("decode hidden keys %s: %w", s.Path, err)
	}
	return keys, nil
}

// Save replaces the hidden keys.
func (s Store) Save(keys []string) error {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// Set is the hidden ticket keys, kept in a Store. It is safe for concurrent
// use.
type Set struct {
	mu    sync.Mutex
	keys  map[string]bool
	store Store
}

// Open loads the set kept in the file at path.
func Open(path string) (*Set, error) {
	store := Store{Path: path}
	keys, err := store.Load()
	if err != nil {
		return nil, err
	}
	s := &Set{keys: map[string]bool{}, store: store}
	for _, key := range keys {
		s.keys[key] = true
	}
	return s, nil
}

// Has reports whether key is hidden.
func (s *Set) Has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keys[key]
}

// Set hides or unhides key and saves the set when it changes.
func (s *Set) Set(key string, hide bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keys[key] == hide {
		return nil
	}
	if hide {
		s.keys[key] = true
	} else {
		delete(s.keys, key)
	}
	return s.store.Save(slices.Collect(maps.Keys(s.keys)))
}
