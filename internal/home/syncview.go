package home

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/gitsync"
)

type checkedMsg struct {
	runID    int
	statuses []gitsync.Status
}

// pulledMsg is how pulling went: pulled are the repos fast-forwarded, err the
// first one that would not.
type pulledMsg struct {
	runID  int
	pulled []gitsync.Status
	err    error
}

// gate checks the repos are up to date before a fresh brief (req nil) or
// draft reads the code, asking to pull any that are behind origin.
func (m Model) gate(req *draft.Request) (tea.Model, tea.Cmd) {
	if req == nil && m.refiner.Brief == nil {
		return m, nil
	}
	m.syncNotes, m.syncErr = nil, nil
	if m.refiner.Check == nil {
		return m.begin(req)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.runID++
	m.cancel, m.pending, m.syncBack, m.pulling = cancel, req, m.screen, false
	m.screen = checking
	m.input.Blur()

	runID, check := m.runID, m.refiner.Check
	work := func() tea.Msg {
		return checkedMsg{runID: runID, statuses: check(ctx)}
	}
	return m, tea.Batch(work, m.spin.Tick)
}

func (m Model) begin(req *draft.Request) (tea.Model, tea.Cmd) {
	if req == nil {
		return m.startBrief()
	}
	return m.startDraft(*req)
}

func (m Model) checked(msg checkedMsg) (tea.Model, tea.Cmd) {
	if msg.runID != m.runID || m.screen != checking {
		return m, nil
	}
	m.cancel, m.stale = nil, nil
	for _, s := range msg.statuses {
		switch {
		case s.Skip != "":
			m.syncNotes = append(m.syncNotes, s.Name+": "+s.Skip)
		case s.Behind > 0 && m.declined[s.Name] != s.Origin:
			m.stale = append(m.stale, s)
		case s.Behind > 0:
			m.syncNotes = append(m.syncNotes, staleNote(s))
		}
	}
	if len(m.stale) == 0 {
		return m.begin(m.pending)
	}
	m.screen = stale
	return m, nil
}

func (m Model) pulled(msg pulledMsg) (tea.Model, tea.Cmd) {
	if msg.runID != m.runID || m.screen != checking {
		return m, nil
	}
	m.cancel = nil
	done := map[string]bool{}
	for _, s := range msg.pulled {
		done[s.Name] = true
		m.syncNotes = append(m.syncNotes, fmt.Sprintf("%s: pulled %s, %s", s.Name, s.Branch, commits(s.Behind)))
	}
	if msg.err == nil {
		return m.begin(m.pending)
	}
	var left []gitsync.Status
	for _, s := range m.stale {
		if !done[s.Name] {
			left = append(left, s)
		}
	}
	m.stale, m.syncErr, m.screen = left, msg.err, stale
	return m, nil
}

func (m Model) updateSync(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.screen == checking {
		if msg.String() == "esc" {
			m.cancel()
			m.cancel, m.runID = nil, m.runID+1
			return m.syncCancelled()
		}
		return m, nil
	}

	switch msg.String() {
	case "esc":
		return m.syncCancelled()
	case "c":
		for _, s := range m.stale {
			m.declined[s.Name] = s.Origin
			m.syncNotes = append(m.syncNotes, staleNote(s))
		}
		return m.begin(m.pending)
	case "p":
		ctx, cancel := context.WithCancel(context.Background())
		m.runID++
		m.cancel, m.screen, m.pulling, m.syncErr = cancel, checking, true, nil
		runID, pull, repos := m.runID, m.refiner.Pull, m.stale
		work := func() tea.Msg {
			var done []gitsync.Status
			for _, s := range repos {
				if err := pull(ctx, s); err != nil {
					return pulledMsg{runID: runID, pulled: done, err: err}
				}
				done = append(done, s)
			}
			return pulledMsg{runID: runID, pulled: done}
		}
		return m, tea.Batch(work, m.spin.Tick)
	}
	return m, nil
}

// syncCancelled goes back to where the brief or draft was asked for.
func (m Model) syncCancelled() (tea.Model, tea.Cmd) {
	m.screen = m.syncBack
	if m.screen == approach {
		return m, m.input.Focus()
	}
	return m, nil
}

func staleNote(s gitsync.Status) string {
	return fmt.Sprintf("%s: reading %s %s behind origin", s.Name, s.Branch, commits(s.Behind))
}

func commits(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

func (m Model) viewChecking() string {
	doing := " Checking the repos are up to date with origin…"
	if m.pulling {
		doing = " Pulling…"
	}
	return m.withFooter(m.ticketHeader()+"\n\n"+m.spin.View()+doing+"\n", "esc cancel")
}

func (m Model) viewStale() string {
	wrap := lipgloss.NewStyle().Width(max(m.width-2, 20))
	var out strings.Builder
	out.WriteString(m.ticketHeader() + "\n\n")
	out.WriteString(wrap.Render("Claude would read code that is behind origin:") + "\n\n")
	for _, s := range m.stale {
		out.WriteString(hang("  • ", fmt.Sprintf("%s: %s is %s behind origin/%s", keyStyle.Render(s.Name), s.Branch, commits(s.Behind), s.Branch), m.width) + "\n")
	}
	out.WriteString("\n" + quietStyle.Render(wrap.Render("Pull fast-forwards each one; it never merges or touches local changes.")) + "\n")
	if m.syncErr != nil {
		out.WriteString("\n" + wrap.Render(errorStyle.Render("could not pull: "+m.syncErr.Error())) + "\n")
	}
	return m.withFooter(out.String(), truncate("p pull now · c continue on stale code · esc back", m.width))
}

// syncLines are the notes from checking the repos, shown while Claude works.
func (m Model) syncLines() string {
	if len(m.syncNotes) == 0 {
		return ""
	}
	wrap := lipgloss.NewStyle().Width(max(m.width-2, 20))
	var out strings.Builder
	out.WriteString("\n")
	for _, note := range m.syncNotes {
		out.WriteString(quietStyle.Render(wrap.Render(note)) + "\n")
	}
	return out.String()
}
