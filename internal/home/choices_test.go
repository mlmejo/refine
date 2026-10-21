package home

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mlmejo/refine/internal/draft"
)

// TestDraftQuestionsArePickedByNumber answers a question with options by its
// number, cycles with space, clears with 0, and saves each change.
func TestDraftQuestionsArePickedByNumber(t *testing.T) {
	saved := questionsDraft()
	saved.Draft.Deployment.Blockers = nil
	saved.Draft.OpenQuestions = []draft.Choice{{
		Question: "Rename the mobile data-sv-name?", Options: []string{"rename", "keep"}, Recommended: "rename", Why: "cross-brand consistency",
	}}
	var kept []map[string]string
	m := onDraft(t, Refiner{
		Load:        func(key string) (draft.Saved, bool, error) { return saved, key == "GEEK-1", nil },
		SaveAnswers: func(s draft.Saved) error { kept = append(kept, s.Answers); return nil },
	}, 120)

	view := m.View()
	for _, want := range []string{"○ Rename the mobile data-sv-name?", "1 ( ) rename ★", "2 ( ) keep", "★ cross-brand consistency", "1-2 pick"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}

	question := "Rename the mobile data-sv-name?"
	for _, step := range []struct{ key, want string }{{"2", "keep"}, {" ", "rename"}, {" ", "keep"}, {"0", ""}, {"1", "rename"}} {
		m = run(t, m, keyMsg(step.key))
		if got := m.current().Answers[question]; got != step.want {
			t.Fatalf("after %q: answer %q, want %q", step.key, got, step.want)
		}
	}
	if len(kept) != 5 || kept[4][question] != "rename" {
		t.Errorf("saved %v", kept)
	}
	if view := m.View(); !strings.Contains(view, "✓ Rename") || !strings.Contains(view, "(•) rename") || strings.Contains(view, "needs an answer") {
		t.Errorf("answered view:\n%s", view)
	}
}

func keyMsg(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
