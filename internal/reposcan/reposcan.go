// Package reposcan finds the git checkouts under a folder, so a whole folder
// of repos can be added at once.
package reposcan

import (
	"fmt"
	"io/fs"
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
	dirs, err := repos(dir)
	if err != nil {
		return nil, err
	}
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

// repos lists the checkouts at or under dir, at most MaxDepth folders down.
// It walks dir through os.DirFS so a dir that is a link to a folder is
// followed; links below it are not.
func repos(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", dir)
	}
	var dirs []string
	err = fs.WalkDir(os.DirFS(dir), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			if rel == "." {
				return err
			}
			return nil // unreadable: skip
		}
		if !d.IsDir() {
			return nil
		}
		if rel != "." && (strings.HasPrefix(d.Name(), ".") || skip[d.Name()]) {
			return fs.SkipDir
		}
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if IsRepo(p) {
			dirs = append(dirs, p)
			return fs.SkipDir
		}
		if rel != "." && strings.Count(rel, "/")+1 >= MaxDepth {
			return fs.SkipDir
		}
		return nil
	})
	return dirs, err
}
