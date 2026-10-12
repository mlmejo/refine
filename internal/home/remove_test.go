package home

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mlmejo/refine/internal/jira"
)

func removing(t *testing.T, hidden []string, saved *[]string) Model {
	t.Helper()
	m := New("Refinement Ready", Source{
		Project: "GEEK",
		List:    func(context.Context) (Page, error) { return Page{Issues: issues}, nil },
		Get: func(_ context.Context, key string) (jira.Issue, error) {
			return jira.Issue{Key: key, Summary: "Opened"}, nil
		},
		Hidden: hidden,
		SaveHidden: func(keys []string) error {
			*saved = keys
			return nil
		},
	}, Refiner{})
	next, _ := m.Update(m.Init()())
	return next.(Model)
}

func keys(m Model) []string {
	var out []string
	for _, issue := range m.issues {
		out = append(out, issue.Key)
	}
	return out
}

func TestRemoveHidesAndSurvivesRefresh(t *testing.T) {
	var saved []string
	m := removing(t, nil, &saved)

	m = press(t, m, "x")
	if !slices.Equal(keys(m), []string{"GEEK-22"}) || !slices.Equal(saved, []string{"GEEK-1"}) {
		t.Fatalf("issues %v saved %v", keys(m), saved)
	}
	if !strings.Contains(m.View(), "u undo") {
		t.Errorf("footer:\n%s", m.View())
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	next, _ = next.(Model).Update(cmd())
	if got := keys(next.(Model)); !slices.Equal(got, []string{"GEEK-22"}) {
		t.Errorf("after refresh %v", got)
	}
}

func TestStartupHidesSavedKeys(t *testing.T) {
	var saved []string
	if got := keys(removing(t, []string{"GEEK-22"}, &saved)); !slices.Equal(got, []string{"GEEK-1"}) {
		t.Errorf("issues %v", got)
	}
}

func TestUndoRestoresInPlace(t *testing.T) {
	var saved []string
	m := removing(t, nil, &saved)
	m = press(t, m, "j", "x", "x")
	if len(m.issues) != 0 || !strings.Contains(m.View(), "No tickets") {
		t.Fatalf("issues %v", keys(m))
	}

	m = press(t, m, "u")
	if !slices.Equal(keys(m), []string{"GEEK-1"}) || !slices.Equal(saved, []string{"GEEK-22"}) {
		t.Errorf("after one undo issues %v saved %v", keys(m), saved)
	}
	m = press(t, m, "u")
	if !slices.Equal(keys(m), []string{"GEEK-1", "GEEK-22"}) || m.cursor != 1 || len(saved) != 0 {
		t.Errorf("after two undos issues %v cursor %d saved %v", keys(m), m.cursor, saved)
	}
	if m = press(t, m, "u"); len(m.issues) != 2 {
		t.Errorf("undo with nothing removed changed list")
	}
}

func TestRemovePinnedOnlyUnpins(t *testing.T) {
	var saved []string
	m := removing(t, nil, &saved)
	m = press(t, submit(t, m, "500"), "esc")
	if keys(m)[0] != "GEEK-500" {
		t.Fatalf("issues %v", keys(m))
	}

	m = press(t, m, "g", "x")
	if slices.Contains(keys(m), "GEEK-500") || len(m.pinned) != 0 || saved != nil {
		t.Errorf("issues %v pinned %v saved %v", keys(m), m.pinned, saved)
	}
	if m = press(t, m, "u"); keys(m)[0] != "GEEK-500" || !m.isPinned("GEEK-500") {
		t.Errorf("undo: issues %v", keys(m))
	}
}

func TestOpeningHiddenTicketUnhidesIt(t *testing.T) {
	var saved []string
	m := removing(t, []string{"GEEK-22"}, &saved)
	saved = []string{"GEEK-22"}
	m = submit(t, m, "22")
	if m.screen != detail || m.hidden["GEEK-22"] || len(saved) != 0 {
		t.Errorf("screen %v hidden %v saved %v", m.screen, m.hidden, saved)
	}
}

func TestSaveErrorShowsInFooter(t *testing.T) {
	m := New("Refinement Ready", Source{
		List:       func(context.Context) (Page, error) { return Page{Issues: issues}, nil },
		SaveHidden: func([]string) error { return errors.New("disk full") },
	}, Refiner{})
	next, _ := m.Update(m.Init()())
	m = press(t, next.(Model), "x")
	if !strings.Contains(m.View(), "could not save hidden tickets: disk full") {
		t.Errorf("view:\n%s", m.View())
	}
}
