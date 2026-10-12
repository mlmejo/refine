package reveal

import (
	"slices"
	"testing"
)

func TestCommand(t *testing.T) {
	for goos, want := range map[string][]string{
		"darwin":  {"open", "-R", "/c/exports/GEEK-7.md"},
		"linux":   {"xdg-open", "/c/exports"},
		"freebsd": {"xdg-open", "/c/exports"},
		"windows": {"explorer", "/select,/c/exports/GEEK-7.md"},
	} {
		name, args := Command(goos, "/c/exports/GEEK-7.md")
		if got := append([]string{name}, args...); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", goos, got, want)
		}
	}
}
