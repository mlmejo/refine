// Package hidden remembers tickets removed from the home list.
package hidden

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
