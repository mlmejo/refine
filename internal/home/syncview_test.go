package home

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/gitsync"
	"github.com/mlmejo/refine/internal/jira"
)

func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

var behind = gitsync.Status{Name: "dating", Dir: "/repos/dating", Branch: "develop", Behind: 3, Origin: "abc"}

// syncing returns a model on the approach screen whose repos check as statuses.
func syncing(t *testing.T, statuses []gitsync.Status, pull func(context.Context, gitsync.Status) error) (Model, *int) {
	t.Helper()
	drafts := 0
	m := refining(t, Refiner{
		Draft: func(context.Context, jira.Issue, draft.Request) (draft.Saved, error) {
			drafts++
			return sample, nil
		},
		Check: func(context.Context) []gitsync.Status { return statuses },
		Pull:  pull,
	})
	return press(t, m, "enter", "a", "my plan"), &drafts
}

func TestStaleRepoContinueRemembersChoice(t *testing.T) {
	m, drafts := syncing(t, []gitsync.Status{behind}, nil)

	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.screen != checking {
		t.Fatalf("screen = %v", m.screen)
	}
	m, _ = update(t, m, await[checkedMsg](t, cmd))
	view := m.View()
	if m.screen != stale || !strings.Contains(view, "develop is 3 commits behind origin/develop") || !strings.Contains(view, "p pull now · c continue on stale code") {
		t.Fatalf("screen = %v view:\n%s", m.screen, view)
	}

	m, cmd = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if m.screen != drafting || !strings.Contains(m.View(), "dating: reading develop 3 commits behind origin") {
		t.Fatalf("screen = %v view:\n%s", m.screen, m.View())
	}
	m, _ = update(t, m, await[draftedMsg](t, cmd))
	if *drafts != 1 || m.screen != drafted {
		t.Fatalf("drafts = %d screen = %v", *drafts, m.screen)
	}

	// Origin has not moved, so the next draft does not ask again.
	m = press(t, m, "esc", "a")
	m, cmd = update(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m, _ = update(t, m, await[checkedMsg](t, cmd))
	if m.screen != drafting {
		t.Fatalf("asked again: screen = %v", m.screen)
	}
}

func TestStaleRepoPull(t *testing.T) {
	var pulled []string
	m, drafts := syncing(t, []gitsync.Status{behind}, func(_ context.Context, s gitsync.Status) error {
		pulled = append(pulled, s.Name)
		return nil
	})

	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m, _ = update(t, m, await[checkedMsg](t, cmd))
	m, cmd = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if m.screen != checking || !strings.Contains(m.View(), "Pulling") {
		t.Fatalf("screen = %v view:\n%s", m.screen, m.View())
	}
	m, cmd = update(t, m, await[pulledMsg](t, cmd))
	if len(pulled) != 1 || m.screen != drafting || !strings.Contains(m.View(), "dating: pulled develop, 3 commits") {
		t.Fatalf("pulled %v screen = %v view:\n%s", pulled, m.screen, m.View())
	}
	update(t, m, await[draftedMsg](t, cmd))
	if *drafts != 1 {
		t.Errorf("drafts = %d", *drafts)
	}
}

func TestStaleRepoPullFails(t *testing.T) {
	m, drafts := syncing(t, []gitsync.Status{behind}, func(context.Context, gitsync.Status) error {
		return errors.New("Not possible to fast-forward")
	})
	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m, _ = update(t, m, await[checkedMsg](t, cmd))
	m, cmd = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m, _ = update(t, m, await[pulledMsg](t, cmd))
	if m.screen != stale || !strings.Contains(m.View(), "could not pull: Not possible to fast-forward") || *drafts != 0 {
		t.Fatalf("screen = %v drafts = %d view:\n%s", m.screen, *drafts, m.View())
	}

	m = press(t, m, "esc")
	if m.screen != approach || m.input.Value() != "my plan" || !m.input.Focused() {
		t.Errorf("screen = %v input %q", m.screen, m.input.Value())
	}
}

func TestSkippedRepoSaysWhy(t *testing.T) {
	offline := gitsync.Status{Name: "dating", Branch: "develop", Skip: "could not fetch origin (offline?), so develop is read as it is"}
	m, _ := syncing(t, []gitsync.Status{offline, {Name: "api"}}, nil)
	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m, _ = update(t, m, await[checkedMsg](t, cmd))
	if m.screen != drafting || !strings.Contains(m.View(), "dating: could not fetch origin (offline?)") {
		t.Fatalf("screen = %v view:\n%s", m.screen, m.View())
	}
}

func TestBriefChecksRepos(t *testing.T) {
	m := refining(t, Refiner{
		Brief: func(context.Context, jira.Issue) (draft.SavedBrief, error) { return draft.SavedBrief{}, nil },
		Check: func(context.Context) []gitsync.Status { return []gitsync.Status{behind} },
	})
	m, cmd := update(t, press(t, m, "enter"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m, _ = update(t, m, await[checkedMsg](t, cmd))
	if m.screen != stale {
		t.Fatalf("screen = %v", m.screen)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if m.screen != briefing {
		t.Fatalf("screen = %v", m.screen)
	}
	if m = press(t, m, "esc"); m.screen != briefed {
		t.Fatalf("screen = %v", m.screen)
	}
}
