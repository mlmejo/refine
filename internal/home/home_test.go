package home

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mlmejo/refine/internal/jira"
)

var issues = []jira.Issue{
	{Key: "GEEK-1", Summary: "First", AcceptanceCriteria: "* QA sees the card."},
	{Key: "GEEK-22", Summary: "Second", TechnicalImplementation: "dating\n* a.vue", StoryPointCriteria: "Total: Low"},
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, key := range keys {
		var msg tea.KeyMsg
		switch key {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "ctrl+s":
			msg = tea.KeyMsg{Type: tea.KeyCtrlS}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func loaded(t *testing.T, list []jira.Issue, err error) Model {
	t.Helper()
	m := New("Refinement Ready", Source{List: func(context.Context) (Page, error) {
		return Page{User: "Mark Mejo", Issues: list}, err
	}}, Refiner{})
	msg := m.Init()()
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestListShowsTicketsAndMovesCursor(t *testing.T) {
	m := loaded(t, issues, nil)
	view := m.View()
	for _, want := range []string{"refine · Mark Mejo · Refinement Ready", "GEEK-1", "First", "GEEK-22", "Second"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}

	if m = press(t, m, "j", "j", "j"); m.cursor != 1 {
		t.Errorf("cursor after j×3 = %d", m.cursor)
	}
	if m = press(t, m, "k", "k"); m.cursor != 0 {
		t.Errorf("cursor after k×2 = %d", m.cursor)
	}
}

func TestListMarksFilledACAndTI(t *testing.T) {
	if got := filled(issues[0], false); got != "AC ✓ TI ·  " {
		t.Errorf("GEEK-1 = %q", got)
	}
	if got := filled(issues[1], false); got != "AC · TI ✓  " {
		t.Errorf("GEEK-22 = %q", got)
	}
	if got := lipgloss.Width(filled(issues[0], true)); got != filledWidth {
		t.Errorf("width = %d, want %d", got, filledWidth)
	}

	view := loaded(t, issues, nil).View()
	for _, want := range []string{"GEEK-1   AC ✓ TI ·  First", "GEEK-22  AC · TI ✓  Second"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestEnterOpensReadOnlyFieldsAndEscReturns(t *testing.T) {
	m := press(t, loaded(t, issues, nil), "j", "enter")
	if m.screen != detail {
		t.Fatalf("screen = %v", m.screen)
	}
	view := m.View()
	for _, want := range []string{"GEEK-22", "Acceptance Criteria", "(empty)", "Technical Implementation", "* a.vue", "Story Point Criteria", "Total: Low"} {
		if !strings.Contains(view, want) {
			t.Errorf("detail missing %q:\n%s", want, view)
		}
	}

	if m = press(t, m, "esc"); m.screen != list || m.cursor != 1 {
		t.Errorf("after esc screen = %v cursor = %d", m.screen, m.cursor)
	}
}

func TestEmptyAndErrorStates(t *testing.T) {
	if view := loaded(t, nil, nil).View(); !strings.Contains(view, "No tickets waiting") {
		t.Errorf("empty view:\n%s", view)
	}
	m := loaded(t, nil, errors.New("jira: 401 Unauthorized"))
	if !strings.Contains(m.View(), "401 Unauthorized") {
		t.Errorf("error view:\n%s", m.View())
	}
	if m = press(t, m, "enter"); m.screen != list {
		t.Errorf("enter with no tickets opened %v", m.screen)
	}
}

func TestRefreshReloads(t *testing.T) {
	m := loaded(t, issues, nil)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(Model)
	if m.screen != loading || cmd == nil {
		t.Fatalf("screen = %v cmd = %v", m.screen, cmd)
	}
	if !strings.Contains(m.View(), "Loading") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestListScrollsWithCursor(t *testing.T) {
	m := loaded(t, issues, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 5})
	m = press(t, next.(Model), "j")
	if m.top != 1 || !strings.Contains(m.View(), "GEEK-22") || strings.Contains(m.View(), "GEEK-1 ") {
		t.Errorf("top = %d view:\n%s", m.top, m.View())
	}
}

// refinedIssues has GEEK-1 refined and GEEK-22 missing its AC.
var refinedIssues = []jira.Issue{
	{Key: "GEEK-1", Summary: "First", AcceptanceCriteria: "* QA sees the card.", TechnicalImplementation: "dating\n\n* a.vue"},
	{Key: "GEEK-22", Summary: "Second", TechnicalImplementation: "dating\n* a.vue"},
}

func movable(t *testing.T, move func(context.Context, string) error) Model {
	t.Helper()
	m := New("Refinement Ready", Source{List: func(context.Context) (Page, error) {
		return Page{Issues: slices.Clone(refinedIssues)}, nil
	}}, Refiner{Move: move, ReadySprint: "Ready for Sprint - Team John"})
	next, _ := m.Update(m.Init()())
	next, _ = next.(Model).Update(tea.WindowSizeMsg{Width: 160, Height: 20})
	return next.(Model)
}

func TestMKeyMovesARefinedTicketToTheReadySprint(t *testing.T) {
	var moved []string
	m := movable(t, func(_ context.Context, key string) error { moved = append(moved, key); return nil })
	if !strings.Contains(m.View(), "m ready for sprint") {
		t.Errorf("help:\n%s", m.View())
	}

	m = press(t, m, "m")
	if !strings.Contains(m.View(), "move GEEK-1 to Ready for Sprint - Team John? y/n") {
		t.Fatalf("confirm:\n%s", m.View())
	}
	if m = run(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}); len(moved) != 0 || indexOf(m.issues, "GEEK-1") < 0 {
		t.Fatalf("n should cancel: moved %v", moved)
	}

	m = run(t, press(t, m, "m"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if len(moved) != 1 || moved[0] != "GEEK-1" {
		t.Fatalf("moved = %v", moved)
	}
	if indexOf(m.issues, "GEEK-1") >= 0 || m.cursor != 0 {
		t.Errorf("issues %+v cursor %d", m.issues, m.cursor)
	}
	if !strings.Contains(m.View(), "moved GEEK-1 to Ready for Sprint - Team John") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestMKeyRefusesAnUnrefinedTicket(t *testing.T) {
	called := false
	m := press(t, movable(t, func(context.Context, string) error { called = true; return nil }), "j", "m")
	if m.moveAsk != "" || called {
		t.Fatalf("moveAsk %q called %v", m.moveAsk, called)
	}
	if !strings.Contains(m.View(), "GEEK-22 is not refined yet") {
		t.Errorf("view:\n%s", m.View())
	}
}

func TestMKeyShowsMoveErrors(t *testing.T) {
	m := movable(t, func(context.Context, string) error { return errors.New("no open sprint named \"Ready\"") })
	m = run(t, press(t, m, "m"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if indexOf(m.issues, "GEEK-1") < 0 || !strings.Contains(m.View(), "could not move GEEK-1 to Ready for Sprint - Team John") {
		t.Errorf("issues %+v view:\n%s", m.issues, m.View())
	}
}
