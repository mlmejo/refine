// Package reposcan finds the git checkouts under a folder, so a whole folder
// of repos can be added at once.
package reposcan

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// MaxDepth is how many folders down Scan looks below the one it is given.
const MaxDepth = 3

// skip are folders that hold dependencies, not the developer's repos.
var skip = map[string]bool{"node_modules": true, "vendor": true}

// Found is a git checkout under the scanned folder: the name it would be
// added as, and Listed, the name it is configured under already, if any.
type Found struct {
	Name   string `json:"name"`
	Dir    string `json:"dir"`
	Listed string `json:"listed,omitempty"`
}

// Scan lists the git checkouts at or under dir, in path order. It does not
// look inside a checkout, hidden folders or dependency folders. Each new one
// is named after its folder; when that name is taken, by a configured repo
// or another one found, the parent folder's name goes in front, e.g.
// "work-api".
func Scan(dir string, existing map[string]string) ([]Found, error) {
	dir = filepath.Clean(dir)
	if _, err := os.ReadDir(dir); err != nil {
		return nil, err
	}
	var dirs []string
	walk(dir, 0, &dirs)
	slices.Sort(dirs)

	listed := map[string]string{}
	taken := map[string]bool{}
	for name, d := range existing {
		listed[filepath.Clean(d)] = name
		taken[name] = true
	}
	base := map[string]int{}
	for _, d := range dirs {
		if listed[d] == "" {
			base[filepath.Base(d)]++
		}
	}

	found := make([]Found, 0, len(dirs))
	for _, d := range dirs {
		if name := listed[d]; name != "" {
			found = append(found, Found{Name: name, Dir: d, Listed: name})
			continue
		}
		name := filepath.Base(d)
		if taken[name] || base[name] > 1 {
			name = filepath.Base(filepath.Dir(d)) + "-" + name
		}
		unique := name
		for n := 2; taken[unique]; n++ {
			unique = name + "-" + strconv.Itoa(n)
		}
		taken[unique] = true
		found = append(found, Found{Name: unique, Dir: d})
	}
	return found, nil
}

// IsRepo reports whether dir is a git checkout. A worktree's .git is a file.
func IsRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func walk(dir string, depth int, out *[]string) {
	if IsRepo(dir) {
		*out = append(*out, dir)
		return
	}
	if depth >= MaxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") || skip[name] {
			continue
		}
		walk(filepath.Join(dir, name), depth+1, out)
	}
}
