// Package version says which build of refine is running.
package version

import "runtime/debug"

// Build is the commit refine was built from, with -dirty when the tree had
// uncommitted changes, from the VCS stamp go build embeds. It is "unknown"
// when there is none, as under go run or go test.
func Build() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	var revision string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if revision == "" {
		return "unknown"
	}
	revision = revision[:min(len(revision), 12)]
	if dirty {
		revision += "-dirty"
	}
	return revision
}
