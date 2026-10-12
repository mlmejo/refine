package home

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/jira"
)

var sampleBrief = draft.SavedBrief{
	Key:       "GEEK-1",
	SessionID: "b-1",
	CostUSD:   0.5,
	Created:   time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC),
	Brief: draft.Brief{
		Summary: "Purchases are tracked when the payment webhook lands.",
		Flow:    []string{"Payment provider calls app/Http/Controllers/WebhookController.php"},
		Parts:   []draft.Part{{Repo: "dating-backend", Path: "app/Listeners/TrackPurchase.php", Role: "Sends the purchase event."}},
		Terms:   []draft.Term{{Term: "credit", Meaning: "the paid unit spent on messages"}},
		AskTeam: []string{"Who owns the payment provider account? Likely the PM."},
	},
}

// await runs a command batch until a message of the wanted kind arrives.
func await[T tea.Msg](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	results := make(chan tea.Msg, 4)
	for _, c := range cmd().(tea.BatchMsg) {
		go func() { results <- c() }()
	}
	timeout := time.After(3 * time.Second)
	for {
		select {
		case msg := <-results:
			if want, ok := msg.(T); ok {
				return want
			}
		case <-timeout:
			t.Fatal("no result")
		}
	}
}

func TestBriefThenApproachDraftsFromTheBrief(t *testing.T) {
	var gotReq draft.Request
	m := refining(t, Refiner{
		Brief: func(context.Context, jira.Issue) (draft.SavedBrief, error) { return sampleBrief, nil },
		Draft: func(_ context.Context, _ jira.Issue, req draft.Request) (draft.Saved, error) {
			gotReq = req
			return sample, nil
		},
	})
	m.width, m.height = 120, 40

	m = press(t, m, "enter")
	if !strings.Contains(m.View(), "b brief me") {
		t.Fatalf("detail help:\n%s", m.View())
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = next.(Model)
	if m.screen != briefing || !strings.Contains(m.View(), "reading the code to brief you") {
		t.Fatalf("screen = %v view:\n%s", m.screen, m.View())
	}
	next, _ = m.Update(await[briefedMsg](t, cmd))
	m = next.(Model)

	view := m.View()
	for _, want := range []string{"What this area does", "payment webhook lands", "How it flows today", "1. Payment provider",
		"Key files", "TrackPurchase.php", "Sends the purchase event.", "credit", "Ask the team", "Who owns the payment", "a write your approach"} {
		if !strings.Contains(view, want) {
			t.Errorf("brief view missing %q:\n%s", want, view)
		}
	}

	m = press(t, m, "a")
	if m.screen != approach || !strings.Contains(m.View(), "pgup/pgdn scroll brief") || !strings.Contains(m.View(), "What this area does") {
		t.Fatalf("approach beside brief:\n%s", m.View())
	}
	m = press(t, m, "Reuse the listener? not sure")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	if !strings.Contains(m.View(), "drafting from the brief") {
		t.Errorf("drafting view:\n%s", m.View())
	}
	await[draftedMsg](t, cmd)
	if gotReq.Brief == nil || gotReq.Brief.SessionID != "b-1" || gotReq.Approach != "Reuse the listener? not sure" {
		t.Errorf("request = %+v", gotReq)
	}
}

func TestBriefFailureOffersRetryAndApproach(t *testing.T) {
	m := refining(t, Refiner{Brief: func(context.Context, jira.Issue) (draft.SavedBrief, error) {
		return draft.SavedBrief{}, context.DeadlineExceeded
	}})
	m = press(t, m, "enter")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m = next.(Model)
	next, _ = m.Update(await[briefedMsg](t, cmd))
	m = next.(Model)
	if view := m.View(); !strings.Contains(view, "deadline exceeded") || !strings.Contains(view, "b try again") {
		t.Errorf("failure view:\n%s", view)
	}
	if m = press(t, m, "a"); m.screen != approach {
		t.Errorf("a from a failed brief: screen %v", m.screen)
	}
}

func TestSavedBriefLoadsWithTheTicket(t *testing.T) {
	m := refining(t, Refiner{LoadBrief: func(key string) (draft.SavedBrief, bool, error) {
		return sampleBrief, key == "GEEK-1", nil
	}})
	m = press(t, m, "enter")
	if !strings.Contains(m.View(), "b brief ·") {
		t.Fatalf("detail help:\n%s", m.View())
	}
	if m = press(t, m, "b"); m.screen != briefed || !strings.Contains(m.View(), "payment webhook lands") {
		t.Errorf("screen %v view:\n%s", m.screen, m.View())
	}
}
