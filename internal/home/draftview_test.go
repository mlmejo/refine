package home

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/jira"
)

// onDraft opens the sample draft for GEEK-1 at the given size.
func onDraft(t *testing.T, refiner Refiner, width int) Model {
	t.Helper()
	if refiner.Load == nil {
		refiner.Load = func(key string) (draft.Saved, bool, error) { return sample, key == "GEEK-1", nil }
	}
	m := refining(t, refiner)
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 200})
	return press(t, next.(Model), "enter", "d")
}

// run applies a key and then the message its command returns, if any.
func run(t *testing.T, m Model, key tea.KeyMsg) Model {
	t.Helper()
	next, cmd := m.Update(key)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			next, _ = next.(Model).Update(msg)
		}
	}
	return next.(Model)
}

func TestCursorMovesThroughBlockersSplitAndQuestions(t *testing.T) {
	m := onDraft(t, Refiner{}, 120)
	items := draftItems(m.current().Draft)
	if len(items) != 3 || items[0].text != "Forge needs a deploy key for the private SDK." || items[1].kind != childItem || items[2].text != "How does dating-backend deploy?" {
		t.Fatalf("items = %+v", items)
	}
	if !strings.Contains(m.View(), "▍○ Forge needs a deploy key") {
		t.Errorf("first blocker not focused:\n%s", m.View())
	}

	m = press(t, m, "j", "j", "j")
	if m.focus != 2 || !strings.Contains(m.View(), "▍○ How does dating-backend deploy?") {
		t.Errorf("focus = %d view:\n%s", m.focus, m.View())
	}
	if m = press(t, m, "k", "k", "k"); m.focus != 0 {
		t.Errorf("focus = %d after moving up past the top", m.focus)
	}
}

func TestHeaderShowsTurnsAndTokens(t *testing.T) {
	measured := sample
	measured.Turns = 28
	measured.Usage = draft.Usage{Input: 24, CacheWrite: 68985, CacheRead: 712326, Output: 8974}
	m := onDraft(t, Refiner{Load: func(key string) (draft.Saved, bool, error) { return measured, key == "GEEK-1", nil }}, 160)
	if !strings.Contains(m.View(), "· 28 turns · 781K in · 9K out") {
		t.Errorf("header:\n%s", m.View())
	}
}

func TestSplitTicketOpensBesideTheDraft(t *testing.T) {
	m := press(t, onDraft(t, Refiner{}, 120), "j", "enter")
	if m.side != 0 {
		t.Fatalf("side = %d", m.side)
	}
	view := m.View()
	for _, want := range []string{"╭─ Purchase Tracking - Session Header ", "As an analyst, I want purchases", "plugins/axios.js", "╭─ Your approach "} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if m = press(t, m, "esc"); m.side != -1 || m.screen != drafted {
		t.Errorf("esc should close the ticket first: side %d screen %v", m.side, m.screen)
	}

	narrow := press(t, onDraft(t, Refiner{}, 80), "j", "enter")
	if view := narrow.View(); strings.Contains(view, "╭─ Your approach ") || !strings.Contains(view, "As an analyst") {
		t.Errorf("narrow terminal should show the ticket alone:\n%s", view)
	}
}

func TestAnsweringSavesAndMovesToTheNextQuestion(t *testing.T) {
	var stored draft.Saved
	m := onDraft(t, Refiner{SaveAnswers: func(saved draft.Saved) error { stored = saved; return nil }}, 120)

	m = press(t, m, "enter")
	if !m.answering || !strings.Contains(m.View(), "enter save") {
		t.Fatalf("answering = %v", m.answering)
	}
	m = press(t, m, "no")
	m = run(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if stored.Answers["Forge needs a deploy key for the private SDK."] != "no" {
		t.Errorf("stored answers = %v", stored.Answers)
	}
	if m.answering || m.focus != 2 {
		t.Errorf("answering %v focus %d, want next unanswered question", m.answering, m.focus)
	}
	if view := m.View(); !strings.Contains(view, "✓ Forge needs") || !strings.Contains(view, "↳ no") || !strings.Contains(view, "1/2 answered") {
		t.Errorf("view:\n%s", view)
	}
	if sample.Answers != nil {
		t.Error("answering changed the shared sample")
	}
}

func TestRedraftNeedsEveryAnswerAndResumes(t *testing.T) {
	var got draft.Request
	m := onDraft(t, Refiner{Draft: func(_ context.Context, _ jira.Issue, req draft.Request) (draft.Saved, error) {
		got = req
		return sample, nil
	}}, 120)

	m = press(t, m, "r")
	if m.screen != drafted || !strings.Contains(m.View(), "answer 2 more questions before redrafting") {
		t.Fatalf("screen %v view:\n%s", m.screen, m.View())
	}

	m = press(t, m, "enter", "Forge")
	m = run(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = press(t, m, "enter", "no")
	m = run(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(Model)
	if m.screen != drafting || !strings.Contains(m.View(), "revising the draft with your answers") {
		t.Fatalf("screen %v view:\n%s", m.screen, m.View())
	}
	awaitDraft(t, cmd)
	if !got.Resume || got.Previous == nil || got.Previous.Answers["How does dating-backend deploy?"] != "no" || got.Approach != "Use the SDK." {
		t.Errorf("request = %+v", got)
	}
}

func TestNewApproachCarriesThePreviousAnswers(t *testing.T) {
	var got draft.Request
	answered := sample
	answered.Answers = map[string]string{"How does dating-backend deploy?": "Forge"}
	m := onDraft(t, Refiner{
		Load: func(key string) (draft.Saved, bool, error) { return answered, key == "GEEK-1", nil },
		Draft: func(_ context.Context, _ jira.Issue, req draft.Request) (draft.Saved, error) {
			got = req
			return sample, nil
		},
	}, 120)

	m = press(t, m, "a")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	awaitDraft(t, cmd)
	if got.Resume || got.Previous == nil || got.Previous.Answers["How does dating-backend deploy?"] != "Forge" {
		t.Errorf("request = %+v", got)
	}
}

func TestWriteAsksBeforeWritingToJira(t *testing.T) {
	answered := sample
	answered.Answers = map[string]string{}
	for _, question := range sample.Draft.Questions() {
		answered.Answers[question] = "no"
	}
	var wrote []string
	refiner := Refiner{
		Load: func(key string) (draft.Saved, bool, error) { return answered, key == "GEEK-1", nil },
		Write: func(_ context.Context, key string, d draft.Draft) (jira.Issue, error) {
			wrote = append(wrote, key)
			return jira.Issue{Key: key, Summary: "Delete account", AcceptanceCriteria: "* written"}, nil
		},
	}

	m := press(t, onDraft(t, refiner, 160), "w")
	if !m.confirming || !strings.Contains(m.View(), "replace GEEK-1's Acceptance Criteria") {
		t.Fatalf("confirming = %v view:\n%s", m.confirming, m.View())
	}
	if m = run(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}); m.confirming || len(wrote) != 0 {
		t.Fatalf("n should cancel: confirming %v wrote %v", m.confirming, wrote)
	}

	m = run(t, press(t, m, "w"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if len(wrote) != 1 || wrote[0] != "GEEK-1" {
		t.Fatalf("wrote = %v", wrote)
	}
	if m.screen != list || !strings.Contains(m.View(), "written to GEEK-1") {
		t.Errorf("screen %v, want home with a notice:\n%s", m.screen, m.View())
	}
	if m = press(t, m, "j"); strings.Contains(m.View(), "written to") {
		t.Errorf("notice should clear on the next key:\n%s", m.View())
	}
	for _, issue := range m.issues {
		if issue.Key == "GEEK-1" && issue.AcceptanceCriteria != "* written" {
			t.Errorf("issue not refreshed: %+v", issue)
		}
	}
}

func TestWriteWaitsForAnswers(t *testing.T) {
	called := false
	m := press(t, onDraft(t, Refiner{Write: func(context.Context, string, draft.Draft) (jira.Issue, error) {
		called = true
		return jira.Issue{}, nil
	}}, 160), "w")
	if m.confirming || called || !strings.Contains(m.View(), "before writing to Jira") {
		t.Errorf("confirming %v called %v view:\n%s", m.confirming, called, m.View())
	}
}

func TestWriteShowsJiraErrors(t *testing.T) {
	answered := sample
	answered.Answers = map[string]string{}
	for _, question := range sample.Draft.Questions() {
		answered.Answers[question] = "no"
	}
	m := onDraft(t, Refiner{
		Load: func(key string) (draft.Saved, bool, error) { return answered, key == "GEEK-1", nil },
		Write: func(context.Context, string, draft.Draft) (jira.Issue, error) {
			return jira.Issue{}, errors.New("jira: 400 Bad Request: Field cannot be set")
		},
	}, 160)
	m = run(t, press(t, m, "w"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !strings.Contains(m.View(), "could not write to Jira: jira: 400") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestWriteMovesTheTicketOffTheList(t *testing.T) {
	answered := sample
	answered.Answers = map[string]string{}
	for _, question := range sample.Draft.Questions() {
		answered.Answers[question] = "no"
	}
	var moved string
	m := onDraft(t, Refiner{
		Load: func(key string) (draft.Saved, bool, error) { return answered, key == "GEEK-1", nil },
		Write: func(_ context.Context, key string, _ draft.Draft) (jira.Issue, error) {
			return jira.Issue{Key: key}, nil
		},
		Move:        func(_ context.Context, key string) error { moved = key; return nil },
		ReadySprint: "Ready for Sprint - Team John",
	}, 200)

	m = press(t, m, "w")
	if !strings.Contains(m.View(), "and move it to Ready for Sprint - Team John? y/n") {
		t.Fatalf("confirm:\n%s", m.View())
	}
	m = run(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if moved != "GEEK-1" || m.screen != list {
		t.Fatalf("moved %q screen %v", moved, m.screen)
	}
	if indexOf(m.issues, "GEEK-1") >= 0 {
		t.Errorf("GEEK-1 still listed: %+v", m.issues)
	}
	if !strings.Contains(m.View(), "written to GEEK-1 and moved to Ready for Sprint - Team John") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestWriteKeepsTheTicketWhenTheMoveFails(t *testing.T) {
	answered := sample
	answered.Answers = map[string]string{}
	for _, question := range sample.Draft.Questions() {
		answered.Answers[question] = "no"
	}
	m := onDraft(t, Refiner{
		Load: func(key string) (draft.Saved, bool, error) { return answered, key == "GEEK-1", nil },
		Write: func(_ context.Context, key string, _ draft.Draft) (jira.Issue, error) {
			return jira.Issue{Key: key, Summary: "First", AcceptanceCriteria: "* written"}, nil
		},
		Move:        func(context.Context, string) error { return errors.New("no open sprint named \"Ready\"") },
		ReadySprint: "Ready",
	}, 200)

	m = run(t, press(t, m, "w"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.screen != list || indexOf(m.issues, "GEEK-1") < 0 {
		t.Fatalf("screen %v issues %+v", m.screen, m.issues)
	}
	if !strings.Contains(m.View(), "written to GEEK-1, but could not move it to Ready") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestWriteCreatesTheSplitTicketsBeforeMoving(t *testing.T) {
	answered := sample
	answered.Answers = map[string]string{}
	for _, question := range sample.Draft.Questions() {
		answered.Answers[question] = "no"
	}
	var steps []string
	m := onDraft(t, Refiner{
		Load: func(key string) (draft.Saved, bool, error) { return answered, key == "GEEK-1", nil },
		Write: func(_ context.Context, key string, _ draft.Draft) (jira.Issue, error) {
			steps = append(steps, "write")
			return jira.Issue{Key: key}, nil
		},
		Split: func(_ context.Context, saved draft.Saved) (map[int]string, error) {
			steps = append(steps, "split")
			return map[int]string{0: "GEEK-9"}, nil
		},
		Move:        func(context.Context, string) error { steps = append(steps, "move"); return nil },
		ReadySprint: "Ready",
	}, 300)

	m = press(t, m, "w")
	if !strings.Contains(m.View(), "points (5), create 1 split ticket linked to it and move it to Ready? y/n") {
		t.Fatalf("confirm:\n%s", m.View())
	}
	m = run(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if strings.Join(steps, ",") != "write,split,move" {
		t.Errorf("steps = %v", steps)
	}
	if !strings.Contains(m.View(), "written to GEEK-1, split into GEEK-9 and moved to Ready") {
		t.Errorf("view:\n%s", m.View())
	}
	if m.saved["GEEK-1"].Children[0] != "GEEK-9" {
		t.Errorf("children = %v", m.saved["GEEK-1"].Children)
	}
}

func TestWriteKeepsTheTicketWhenASplitTicketFails(t *testing.T) {
	answered := sample
	answered.Answers = map[string]string{}
	for _, question := range sample.Draft.Questions() {
		answered.Answers[question] = "no"
	}
	moved := false
	m := onDraft(t, Refiner{
		Load: func(key string) (draft.Saved, bool, error) { return answered, key == "GEEK-1", nil },
		Write: func(_ context.Context, key string, _ draft.Draft) (jira.Issue, error) {
			return jira.Issue{Key: key}, nil
		},
		Split: func(context.Context, draft.Saved) (map[int]string, error) {
			return nil, errors.New("Purchase Tracking - Session Header: jira: 400 Bad Request")
		},
		Move:        func(context.Context, string) error { moved = true; return nil },
		ReadySprint: "Ready",
	}, 300)

	m = run(t, press(t, m, "w"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if moved || indexOf(m.issues, "GEEK-1") < 0 {
		t.Fatalf("moved %v issues %+v", moved, m.issues)
	}
	if !strings.Contains(m.View(), "could not create every split ticket") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestConfirmCountsOnlySplitTicketsNotCreatedYet(t *testing.T) {
	answered := sample
	answered.Answers = map[string]string{}
	for _, question := range sample.Draft.Questions() {
		answered.Answers[question] = "no"
	}
	answered.Children = map[int]string{0: "GEEK-9"}
	m := onDraft(t, Refiner{
		Load:  func(key string) (draft.Saved, bool, error) { return answered, key == "GEEK-1", nil },
		Write: func(context.Context, string, draft.Draft) (jira.Issue, error) { return jira.Issue{}, nil },
		Split: func(context.Context, draft.Saved) (map[int]string, error) { return nil, nil },
	}, 300)

	view := press(t, m, "w").View()
	if strings.Contains(view, "create 1 split ticket") || !strings.Contains(view, "Purchase Tracking - Session Header · 1 pt · GEEK-9") {
		t.Errorf("view:\n%s", view)
	}
}

func TestDraftShowsRelatedTickets(t *testing.T) {
	related := sample
	related.Draft.Relations = []draft.Relation{{Key: "GEEK-12000", Kind: "needed_by", Why: "GEEK-12000's live badge needs this store."}}
	m := onDraft(t, Refiner{
		Load: func(key string) (draft.Saved, bool, error) { return related, key == "GEEK-1", nil },
	}, 120)

	view := m.View()
	for _, want := range []string{"Related tickets · 1", "needed by GEEK-12000", "GEEK-12000's live badge needs this store."} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in:\n%s", want, view)
		}
	}
}
