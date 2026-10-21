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

var sampleTriage = draft.SavedTriage{
	Key:     "GEEK-1",
	CostUSD: 0.04,
	Created: time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC),
	Triage: draft.Triage{
		Problem:        "Counts only update on /messages.",
		Solution:       "Counts update live on every page.",
		Blockers:       []string{"GEEK-5 (To Do) blocks the badge reset"},
		ScopeQuestions: []draft.ScopeQuestion{{Question: "Pop-ups too?", Options: []string{"counts only", "pop-ups too"}, Recommended: "counts only", Why: "smaller ticket"}},
		Relations:      []draft.Relation{{Key: "GEEK-6", Kind: "needs", Why: "the badge criterion"}},
	},
}

// questionsDraft is sample as the questions step leaves it: no fields for Jira yet.
func questionsDraft() draft.Saved {
	saved := sample
	saved.Step = draft.StepQuestions
	saved.Draft = sampleTriage.Triage.Apply(draft.Draft{
		Findings:       []string{"echo.client.ts:73 connects on login"},
		ApproachReview: sample.Draft.ApproachReview,
		Deployment:     sample.Draft.Deployment,
		OpenQuestions:  sample.Draft.OpenQuestions,
	})
	return saved
}

// awaitMsg runs a command batch until a message of type T arrives.
func awaitMsg[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	results := make(chan tea.Msg, 4)
	for _, c := range cmd().(tea.BatchMsg) {
		go func() { results <- c() }()
	}
	timeout := time.After(3 * time.Second)
	for {
		select {
		case msg := <-results:
			if got, ok := msg.(T); ok {
				return got
			}
		case <-timeout:
			var zero T
			t.Fatalf("no %T", zero)
			return zero
		}
	}
}

func TestTriageFeedsTheApproachAndTheQuestions(t *testing.T) {
	var got draft.Request
	m := refining(t, Refiner{
		Triage: func(_ context.Context, issue jira.Issue) (draft.SavedTriage, error) {
			if issue.Key != "GEEK-1" {
				t.Errorf("triaged %s", issue.Key)
			}
			return sampleTriage, nil
		},
		Draft: func(_ context.Context, _ jira.Issue, req draft.Request) (draft.Saved, error) {
			got = req
			return questionsDraft(), nil
		},
	})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 120})
	m = press(t, next.(Model), "enter")
	if !strings.Contains(m.View(), "t triage ·") {
		t.Fatalf("detail help:\n%s", m.View())
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = next.(Model)
	if m.screen != triaging || !strings.Contains(m.View(), "Reads Jira only") {
		t.Fatalf("screen %v view:\n%s", m.screen, m.View())
	}
	next, _ = m.Update(awaitMsg[triagedMsg](t, cmd))
	m = next.(Model)
	view := m.View()
	for _, want := range []string{"triage · $0.04", "Counts only update on /messages.", "⛔ GEEK-5 (To Do) blocks the badge reset", "? Pop-ups too?", "counts only ★", "needs GEEK-6"} {
		if !strings.Contains(view, want) {
			t.Errorf("triage view missing %q:\n%s", want, view)
		}
	}

	m = press(t, m, "a")
	if m.screen != approach || !strings.Contains(m.View(), "Counts update live on every page.") {
		t.Fatalf("approach without the triage beside it:\n%s", m.View())
	}
	// tab moves to the scope questions; l picks the first option.
	m = press(t, m, "tab", "l")
	if !strings.Contains(m.View(), "(•) counts only") || !strings.Contains(m.View(), "Scope · 1/1 answered") {
		t.Fatalf("scope not picked:\n%s", m.View())
	}
	// l cycles through the options and back to unanswered; h goes back; r
	// takes the recommendation; enter answers in words.
	for _, step := range []struct{ key, want string }{
		{"l", "pop-ups too"}, {"l", ""}, {"h", "pop-ups too"}, {"x", ""}, {"r", "counts only"},
	} {
		if m = press(t, m, step.key); m.scope["Pop-ups too?"] != step.want {
			t.Fatalf("after %s: scope = %q, want %q", step.key, m.scope["Pop-ups too?"], step.want)
		}
	}
	m = press(t, m, "enter", "popups later", "enter")
	if m.scope["Pop-ups too?"] != "popups later" || !strings.Contains(m.View(), "↳ popups later") {
		t.Fatalf("own answer: %q\n%s", m.scope["Pop-ups too?"], m.View())
	}
	m = press(t, m, "r", "tab", "Counts only.")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	next, _ = next.(Model).Update(awaitDraft(t, cmd))
	m = next.(Model)
	if got.Triage == nil || got.Triage.Triage.Problem != "Counts only update on /messages." || got.Approach != "Counts only." || got.Scope["Pop-ups too?"] != "counts only" {
		t.Errorf("request = %+v", got)
	}
	if m.screen != drafted {
		t.Fatalf("screen = %v", m.screen)
	}
}

func TestQuestionsDraftHoldsNoJiraFieldsAndCannotBeWritten(t *testing.T) {
	m := onDraft(t, Refiner{
		Load:  func(string) (draft.Saved, bool, error) { return questionsDraft(), true, nil },
		Write: func(context.Context, string, draft.Draft) (jira.Issue, error) { t.Error("wrote a questions draft"); return jira.Issue{}, nil },
		Stamp: func(context.Context, jira.Issue, draft.Saved) (draft.Saved, error) { return draft.Saved{}, nil },
	}, 120)
	view := m.View()
	for _, want := range []string{"questions · $0.87", "╭─ Ticket ", "Problem: Counts only update on /messages.", "╭─ Findings · 1 ", "echo.client.ts:73", "S stamp"} {
		if !strings.Contains(view, want) {
			t.Errorf("questions view missing %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"Acceptance Criteria", "Story Point Criteria", "w write to Jira"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("questions view shows %q:\n%s", unwanted, view)
		}
	}

	m = press(t, m, "w")
	if !strings.Contains(m.View(), "stamp the ticket with S before writing to Jira") || m.confirming {
		t.Errorf("w on a questions draft:\n%s", m.View())
	}
}

func TestStampWithOpenQuestionsNeedsAcknowledging(t *testing.T) {
	var stamped draft.Saved
	m := onDraft(t, Refiner{
		Load: func(string) (draft.Saved, bool, error) { return questionsDraft(), true, nil },
		Stamp: func(_ context.Context, _ jira.Issue, saved draft.Saved) (draft.Saved, error) {
			stamped = saved
			out := saved
			out.Step = draft.StepStamp
			out.Draft = saved.Draft.WithStamp(draft.Stamp{AcceptanceCriteria: []string{"QA sees live counts."}, Points: 2})
			return out, nil
		},
		Write: func(context.Context, string, draft.Draft) (jira.Issue, error) { return jira.Issue{}, nil },
	}, 120)

	// Answer one of the two questions; the other stays open.
	m = press(t, m, "enter", "Forge")
	m = run(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	m = press(t, m, "S")
	if !m.stampAsk || !strings.Contains(m.View(), "1 question still open. Stamp anyway?") {
		t.Fatalf("no acknowledgement asked:\n%s", m.View())
	}
	m = press(t, m, "n")
	if m.stampAsk || m.screen != drafted {
		t.Fatalf("n did not cancel: screen %v", m.screen)
	}

	m = press(t, m, "S")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = next.(Model)
	if m.screen != drafting || !strings.Contains(m.View(), "stamping the ticket from your answers") {
		t.Fatalf("screen %v view:\n%s", m.screen, m.View())
	}
	next, _ = m.Update(awaitDraft(t, cmd))
	m = next.(Model)
	if stamped.Answers["Forge needs a deploy key for the private SDK."] != "Forge" {
		t.Errorf("stamped without the answers: %+v", stamped.Answers)
	}

	view := m.View()
	for _, want := range []string{"stamped · 2 pts", "* QA sees live counts.", "still open: stamped without an answer", "w write to Jira"} {
		if !strings.Contains(view, want) {
			t.Errorf("stamped view missing %q:\n%s", want, view)
		}
	}
	m = press(t, m, "w")
	if !m.confirming {
		t.Errorf("w on a stamped draft with an acknowledged open question did not ask to write:\n%s", m.View())
	}
}

func TestStampFailureReturnsToTheQuestions(t *testing.T) {
	m := onDraft(t, Refiner{
		Load: func(string) (draft.Saved, bool, error) {
			saved := questionsDraft()
			saved.Draft.OpenQuestions, saved.Draft.Deployment.Blockers = nil, nil
			return saved, true, nil
		},
		Stamp: func(context.Context, jira.Issue, draft.Saved) (draft.Saved, error) {
			return draft.Saved{}, errors.New("max turns")
		},
	}, 120)
	// Every question answered: S stamps without asking.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	m = next.(Model)
	if m.screen != drafting {
		t.Fatalf("screen = %v", m.screen)
	}
	next, _ = m.Update(awaitDraft(t, cmd))
	m = next.(Model)
	if m.screen != drafted || !strings.Contains(m.View(), "could not stamp: max turns") || m.current().Stamped() {
		t.Errorf("screen %v view:\n%s", m.screen, m.View())
	}
}
