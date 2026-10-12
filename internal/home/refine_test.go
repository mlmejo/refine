package home

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/jira"
)

var sample = draft.Saved{
	Key:      "GEEK-1",
	Approach: "Use the SDK.",
	CostUSD:  0.87,
	Created:  time.Date(2026, 10, 7, 18, 0, 0, 0, time.UTC),
	Draft: draft.Draft{
		AcceptanceCriteria: []string{"QA sees the purchase on the card."},
		TechnicalImplementation: []draft.RepoFiles{{Repo: "dating-backend", Files: []draft.File{
			{Path: "app/Listeners/TrackPurchase.php", New: true},
		}}},
		StoryPointCriteria: draft.StoryPointCriteria{Novelty: "Medium", Risk: "High", Total: "High"},
		Points:             5,
		Split: []draft.Child{{Title: "Purchase Tracking - Session Header", Points: 1,
			AcceptanceCriteria:      []string{"As an analyst, I want purchases tied to the session, so that funnels add up."},
			TechnicalImplementation: []draft.RepoFiles{{Repo: "dating", Files: []draft.File{{Path: "plugins/axios.js"}}}},
		}},
		ApproachReview: draft.ApproachReview{Misses: []string{"The dating routes skip the api middleware."}},
		Deployment:     draft.Deployment{Platform: "Laravel Forge", Blockers: []string{"Forge needs a deploy key for the private SDK."}},
		OpenQuestions:  []string{"How does dating-backend deploy?"},
	},
}

func refining(t *testing.T, refiner Refiner) Model {
	t.Helper()
	m := New("Refinement Ready", Source{List: func(context.Context) (Page, error) { return Page{Issues: issues}, nil }}, refiner)
	next, _ := m.Update(m.Init()())
	return next.(Model)
}

// awaitDraft runs a startDraft command batch until the draft result arrives.
func awaitDraft(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	results := make(chan tea.Msg, 4)
	for _, c := range cmd().(tea.BatchMsg) {
		go func() { results <- c() }()
	}
	timeout := time.After(3 * time.Second)
	for {
		select {
		case msg := <-results:
			if _, ok := msg.(draftedMsg); ok {
				return msg
			}
		case <-timeout:
			t.Fatal("no draft result")
		}
	}
}

func TestApproachThenDraftShowsAllSections(t *testing.T) {
	var gotIssue jira.Issue
	var gotApproach string
	m := refining(t, Refiner{Draft: func(_ context.Context, issue jira.Issue, req draft.Request) (draft.Saved, error) {
		gotIssue, gotApproach = issue, req.Approach
		return sample, nil
	}})

	m = press(t, m, "enter", "a")
	if m.screen != approach || !strings.Contains(m.View(), "ctrl+s draft with Claude") {
		t.Fatalf("screen = %v view:\n%s", m.screen, m.View())
	}
	m = press(t, m, "Use the SDK.")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	if m.screen != drafting || !strings.Contains(m.View(), "esc cancel") {
		t.Fatalf("screen = %v view:\n%s", m.screen, m.View())
	}

	next, _ = m.Update(awaitDraft(t, cmd))
	m = next.(Model)
	if gotIssue.Key != "GEEK-1" || gotApproach != "Use the SDK." {
		t.Errorf("drafted %q with %q", gotIssue.Key, gotApproach)
	}
	if m.screen != drafted {
		t.Fatalf("screen = %v", m.screen)
	}

	next, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 200})
	view := next.(Model).View()
	for _, want := range []string{
		"draft · 5 pts · $0.87",
		"Your approach", "Use the SDK.",
		"Not covered yet", "The dating routes skip the api middleware.",
		"╭─ Acceptance Criteria · 1 ", "* QA sees the purchase on the card.",
		"dating-backend", "app/Listeners/TrackPurchase.php (new)",
		"Risk & Feature Impact", "High", "Points",
		"╭─ Split · 1 tickets ", "↗ Purchase Tracking - Session Header · 1 pt",
		"Platform: Laravel Forge", "○ Forge needs a deploy key for the private SDK.",
		"○ How does dating-backend deploy?", "↳ needs an answer",
		"╭─ Open questions · 0/2 answered ", "0/2 answered",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("draft view missing %q:\n%s", want, view)
		}
	}
}

func TestDraftErrorKeepsApproach(t *testing.T) {
	m := refining(t, Refiner{Draft: func(context.Context, jira.Issue, draft.Request) (draft.Saved, error) {
		return draft.Saved{}, errors.New("claude error_max_turns: Reached max turns")
	}})
	m = press(t, m, "enter", "a", "my plan")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	next, _ = next.(Model).Update(awaitDraft(t, cmd))
	m = next.(Model)

	if m.screen != approach || m.input.Value() != "my plan" || !strings.Contains(m.View(), "Reached max turns") {
		t.Errorf("screen = %v value = %q view:\n%s", m.screen, m.input.Value(), m.View())
	}
}

func TestEscCancelsDraftAndIgnoresLateResult(t *testing.T) {
	cancelled := make(chan struct{})
	m := refining(t, Refiner{Draft: func(ctx context.Context, _ jira.Issue, _ draft.Request) (draft.Saved, error) {
		<-ctx.Done()
		close(cancelled)
		return draft.Saved{}, ctx.Err()
	}})
	m = press(t, m, "enter", "a")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = press(t, next.(Model), "esc")

	if m.screen != approach || !strings.Contains(m.View(), "cancelled") {
		t.Errorf("screen = %v view:\n%s", m.screen, m.View())
	}
	late := awaitDraft(t, cmd)
	<-cancelled
	if next, _ := m.Update(late); next.(Model).screen != approach {
		t.Error("late result changed the screen")
	}
}

func TestSavedDraftOpensFromDetailAndPrefillsApproach(t *testing.T) {
	m := refining(t, Refiner{Load: func(key string) (draft.Saved, bool, error) {
		return sample, key == "GEEK-1", nil
	}})

	m = press(t, m, "enter")
	if !strings.Contains(m.View(), "d saved draft") {
		t.Errorf("detail view:\n%s", m.View())
	}
	if m = press(t, m, "d"); m.screen != drafted {
		t.Fatalf("screen = %v", m.screen)
	}
	if m = press(t, m, "a"); m.screen != approach || m.input.Value() != "Use the SDK." {
		t.Errorf("screen = %v value = %q", m.screen, m.input.Value())
	}

	m = press(t, m, "esc", "esc", "j", "enter", "d")
	if m.screen != detail || strings.Contains(m.View(), "d saved draft") {
		t.Errorf("GEEK-22 has no draft; screen = %v", m.screen)
	}
}

func TestHangIndentsWrappedLines(t *testing.T) {
	got := hang("  * ", "one two three four five six", 20)
	lines := strings.Split(got, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "  * one") || !strings.HasPrefix(lines[1], "    ") {
		t.Errorf("hang:\n%s", got)
	}
}

func TestRevealFromDraftScreen(t *testing.T) {
	stored := sample
	stored.Markdown = "/c/exports/GEEK-1/20261007-180000.md"
	var revealed string
	var revealErr error
	m := refining(t, Refiner{
		Load: func(key string) (draft.Saved, bool, error) { return stored, key == "GEEK-1", nil },
		Reveal: func(saved draft.Saved) error {
			revealed = saved.Markdown
			return revealErr
		},
	})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = press(t, next.(Model), "enter", "d")
	if !strings.Contains(m.View(), "s markdown") {
		t.Fatalf("footer:\n%s", m.View())
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	next, _ = next.(Model).Update(cmd())
	m = next.(Model)
	if revealed != stored.Markdown || !strings.Contains(m.View(), stored.Markdown+" · esc back") {
		t.Errorf("revealed %q view:\n%s", revealed, m.View())
	}

	revealErr = errors.New("no xdg-open")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	next, _ = next.(Model).Update(cmd())
	if view := next.(Model).View(); !strings.Contains(view, ": no xdg-open") {
		t.Errorf("view:\n%s", view)
	}

	m = press(t, next.(Model), "esc", "d")
	if strings.Contains(m.View(), stored.Markdown) {
		t.Error("reveal notice kept after reopening")
	}
}
