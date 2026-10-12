package home

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mlmejo/refine/internal/jira"
)

func opening(t *testing.T, get func(context.Context, string) (jira.Issue, error)) Model {
	t.Helper()
	m := New("Refinement Ready", Source{
		Project: "GEEK",
		List:    func(context.Context) (Page, error) { return Page{Issues: issues}, nil },
		Get:     get,
	}, Refiner{})
	next, _ := m.Update(m.Init()())
	return next.(Model)
}

// submit types input into the key prompt, presses enter and delivers the fetch result.
func submit(t *testing.T, m Model, input string) Model {
	t.Helper()
	m = press(t, m, "o", input)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		next, _ = m.Update(cmd())
		m = next.(Model)
	}
	return m
}

func TestOpenByNumberPinsTicketAndShowsIt(t *testing.T) {
	var asked string
	m := opening(t, func(_ context.Context, key string) (jira.Issue, error) {
		asked = key
		return jira.Issue{Key: key, Summary: "Test clone", AcceptanceCriteria: "* QA sees it."}, nil
	})

	m = submit(t, m, "11963")
	if asked != "GEEK-11963" || m.screen != detail || !strings.Contains(m.View(), "Test clone") {
		t.Fatalf("asked %q screen %v view:\n%s", asked, m.screen, m.View())
	}

	m = press(t, m, "esc")
	if m.issues[0].Key != "GEEK-11963" || !strings.Contains(m.View(), "GEEK-11963") || !strings.Contains(m.View(), "· opened") {
		t.Errorf("list:\n%s", m.View())
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	next, _ = next.(Model).Update(cmd())
	if got := next.(Model).issues; len(got) != 3 || got[0].Key != "GEEK-11963" {
		t.Errorf("after refresh issues = %v", got)
	}
}

func TestOpenKeyAlreadyListedMovesCursor(t *testing.T) {
	m := opening(t, func(context.Context, string) (jira.Issue, error) {
		t.Error("fetched a ticket already in the list")
		return jira.Issue{}, nil
	})
	m = submit(t, m, "https://geekee.atlassian.net/browse/geek-22")
	if m.screen != detail || m.cursor != 1 || len(m.pinned) != 0 {
		t.Errorf("screen %v cursor %d pinned %v", m.screen, m.cursor, m.pinned)
	}
}

func TestOpenErrorsStayInPrompt(t *testing.T) {
	m := opening(t, func(context.Context, string) (jira.Issue, error) {
		return jira.Issue{}, errors.New("jira: 404 Not Found: Issue does not exist")
	})
	m.width = 200

	m = submit(t, m, "GEEK-9")
	if m.screen != list || !m.asking || !strings.Contains(m.View(), "GEEK-9: jira: 404") {
		t.Errorf("screen %v view:\n%s", m.screen, m.View())
	}

	m = press(t, m, "esc")
	m = submit(t, m, "hello")
	if !strings.Contains(m.View(), "not a ticket key") {
		t.Errorf("view:\n%s", m.View())
	}
	if m = press(t, m, "esc"); m.asking || m.screen != list {
		t.Errorf("esc left prompt open")
	}
}

func TestKeysTypedInPromptDoNotQuit(t *testing.T) {
	m := opening(t, nil)
	if m = press(t, m, "o"); m.asking {
		t.Error("prompt opened without a Get")
	}
	m = opening(t, func(context.Context, string) (jira.Issue, error) { return jira.Issue{}, nil })
	next, cmd := press(t, m, "o").Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Error("q in prompt quit")
		}
	}
	if next.(Model).keyIn.Value() != "q" {
		t.Errorf("value = %q", next.(Model).keyIn.Value())
	}
}
