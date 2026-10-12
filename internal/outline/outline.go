// Package outline sums up a repo as its directories and how many files each
// holds, cached per commit so Claude starts with the layout instead of
// listing directories turn by turn.
package outline

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Cache keeps outlines by repo and commit.
type Cache interface {
	Outline(repo, commit string) (string, bool, error)
	SaveOutline(repo, commit, outline string) error
}

// Outline is a repo's layout at a commit.
type Outline struct {
	Commit string
	Text   string
	// Cached is true when the outline came from the cache.
	Cached bool
	// Dirty is true when added, deleted or untracked files make the checkout
	// differ from its commit; such an outline is built fresh and not cached.
	Dirty bool
}

var skipExtensions = []string{
	".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".webp",
	".woff", ".woff2", ".ttf", ".eot", ".map", ".lock",
}

// Get returns the outline of the repo checked out at dir, named name in the cache.
func Get(ctx context.Context, cache Cache, name, dir string) (Outline, error) {
	commit, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return Outline{}, err
	}
	status, err := git(ctx, dir, "status", "--porcelain")
	if err != nil {
		return Outline{}, err
	}

	if changesTree(status) {
		files, err := git(ctx, dir, "ls-files", "--cached", "--others", "--exclude-standard")
		if err != nil {
			return Outline{}, err
		}
		deleted, err := git(ctx, dir, "ls-files", "--deleted")
		if err != nil {
			return Outline{}, err
		}
		gone := strings.Split(deleted, "\n")
		kept := slices.DeleteFunc(strings.Split(files, "\n"), func(file string) bool { return slices.Contains(gone, file) })
		return Outline{Commit: commit, Text: Summarise(kept), Dirty: true}, nil
	}

	if text, ok, err := cache.Outline(name, commit); err != nil || ok {
		return Outline{Commit: commit, Text: text, Cached: ok}, err
	}
	files, err := git(ctx, dir, "ls-files")
	if err != nil {
		return Outline{}, err
	}
	text := Summarise(strings.Split(files, "\n"))
	return Outline{Commit: commit, Text: text}, cache.SaveOutline(name, commit, text)
}

// changesTree reports whether git status shows files added, deleted, renamed
// or untracked; edits to tracked files leave the outline as it is.
func changesTree(status string) bool {
	for _, line := range strings.Split(status, "\n") {
		if len(line) >= 2 && strings.ContainsAny(line[:2], "?ADRC") {
			return true
		}
	}
	return false
}

// Summarise lists each directory with its file count, leaving out dependency
// folders, tool folders other than .github, and images, fonts and lock files.
func Summarise(files []string) string {
	counts := map[string]int{}
	for _, file := range files {
		if file == "" || skip(file) {
			continue
		}
		counts[path.Dir(file)]++
	}
	dirs := make([]string, 0, len(counts))
	for dir := range counts {
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)

	var out strings.Builder
	for _, dir := range dirs {
		fmt.Fprintf(&out, "%s/ %d\n", dir, counts[dir])
	}
	return out.String()
}

func skip(file string) bool {
	if slices.Contains(skipExtensions, strings.ToLower(path.Ext(file))) {
		return true
	}
	parts := strings.Split(file, "/")
	if len(parts) > 1 && strings.HasPrefix(parts[0], ".") && parts[0] != ".github" {
		return true
	}
	return slices.Contains(parts, "node_modules") || slices.Contains(parts, "vendor")
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s in %s: %w: %s", args[0], filepath.Base(dir), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}
