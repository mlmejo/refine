package home

import "testing"

func TestInlineDropsMarkers(t *testing.T) {
	for in, want := range map[string]string{
		"reads `navbar.vue:169` first": "reads navbar.vue:169 first",
		"a **big** change":             "a big change",
		"one ` stray backtick":         "one ` stray backtick",
		"`a` and `b`":                  "a and b",
		"2 * 3 ** 4":                   "2 * 3 ** 4",
	} {
		// Tests render without colour, so only the markers show a change.
		if got := inline(in); got != want {
			t.Errorf("inline(%q) = %q, want %q", in, got, want)
		}
	}
}
