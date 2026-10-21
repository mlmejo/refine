package home

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mlmejo/refine/internal/draft"
)

type triagedMsg struct {
	runID  int
	triage draft.SavedTriage
	err    error
}

// startTriage judges the ticket from Jira alone. It reads no code, so it does
// not wait on the repos being up to date.
func (m Model) startTriage() (tea.Model, tea.Cmd) {
	if m.refiner.Triage == nil {
		return m, nil
	}
	issue := m.issues[m.cursor]
	ctx, cancel := context.WithCancel(context.Background())
	m.runID++
	m.cancel, m.started = cancel, time.Now()
	m.screen, m.triageErr = triaging, nil

	runID, run := m.runID, m.refiner.Triage
	work := func() tea.Msg {
		triage, err := run(ctx, issue)
		return triagedMsg{runID: runID, triage: triage, err: err}
	}
	return m, tea.Batch(work, m.spin.Tick, tickElapsed(runID))
}

func (m Model) triageDone(msg triagedMsg) (tea.Model, tea.Cmd) {
	if msg.runID != m.runID || m.screen != triaging {
		return m, nil
	}
	m.cancel = nil
	if msg.err != nil {
		m.triageErr = msg.err
		return m.openTriage(), nil
	}
	m.triages[msg.triage.Key] = msg.triage
	return m.openTriage(), nil
}

func (m Model) openTriage() Model {
	m.screen = triaged
	m.resizeRefine()
	m.pane.GotoTop()
	return m
}

func (m *Model) layoutTriage() {
	m.pane.Width, m.pane.Height = max(m.width, 20), max(m.height-5, 1)
	if triage, ok := m.triages[m.issues[m.cursor].Key]; ok {
		m.pane.SetContent(renderTriage(triage.Triage, m.pane.Width, true))
	} else {
		m.pane.SetContent("")
	}
}

func (m Model) updateTriage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.screen == triaging {
		if msg.String() == "esc" {
			m.cancel()
			m.cancel, m.runID = nil, m.runID+1
			m.triageErr = fmt.Errorf("cancelled")
			return m.openTriage(), nil
		}
		return m, nil
	}

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace", "h", "left":
		m.screen = detail
		return m, nil
	case "a":
		return m.openApproach()
	case "b":
		if _, ok := m.briefs[m.issues[m.cursor].Key]; ok {
			return m.openBrief(), nil
		}
		return m.gate(nil)
	case "R", "t":
		if _, ok := m.triages[m.issues[m.cursor].Key]; !ok || msg.String() == "R" {
			return m.startTriage()
		}
	}
	var cmd tea.Cmd
	m.pane, cmd = m.pane.Update(msg)
	return m, cmd
}

func (m Model) viewTriaging() string {
	elapsed := time.Since(m.started).Truncate(time.Second)
	body := m.ticketHeader() + "\n\n" +
		m.spin.View() + " Claude is triaging the ticket… " + elapsed.String() + "\n" +
		quietStyle.Render("Reads Jira only: the ticket, its links and your other tickets. Usually under a minute.") + "\n"
	return m.withFooter(body, "esc cancel")
}

func (m Model) viewTriaged() string {
	triage, ok := m.triages[m.issues[m.cursor].Key]
	if !ok {
		body := m.ticketHeader() + "\n\n"
		if m.triageErr != nil {
			body += lipgloss.NewStyle().Width(m.width).Render(errorStyle.Render(m.triageErr.Error())) + "\n"
		}
		return m.withFooter(body, "t try again · a approach without a triage · esc back")
	}

	meta := fmt.Sprintf("triage · $%.2f · %s", triage.CostUSD, triage.Created.Local().Format("Jan 2 15:04"))
	help := "↑/↓ scroll · a write your approach · b brief · R triage again · esc back"
	if m.triageErr != nil {
		help = errorStyle.Render("triage again failed: " + m.triageErr.Error())
	}
	body := m.ticketHeader() + "\n" + quietStyle.Render(meta) + "\n\n" + m.pane.View() + "\n"
	return m.withFooter(body, truncate(help, m.width))
}

// triageMaxWidth caps the triage boxes so they stay readable on wide terminals.
const triageMaxWidth = 59

// renderTriage lays out a triage as bordered sections at the given width,
// its scope questions too when withScope is set,
// capped at triageMaxWidth.
func renderTriage(t draft.Triage, width int, withScope bool) string {
	width = min(width, triageMaxWidth)
	inner := max(width-4, 10)
	var all []string
	section := func(title string, lines []string) {
		if len(all) > 0 {
			all = append(all, "")
		}
		all = append(all, box(title, lines, width)...)
	}
	bullets := func(marker string, items []string) []string {
		if len(items) == 0 {
			return []string{pendingStyle.Render("none")}
		}
		var lines []string
		for _, text := range items {
			lines = append(lines, strings.Split(hang(marker, text, inner), "\n")...)
		}
		return lines
	}

	section("Problem", strings.Split(hang("", t.Problem, inner), "\n"))
	section("Solution", strings.Split(hang("", t.Solution, inner), "\n"))
	section("Blockers", bullets("⛔ ", t.Blockers))
	if withScope {
		section("Scope questions", scopeLines(t.ScopeQuestions, inner))
	}
	if len(t.Relations) > 0 {
		section("Related tickets", relationLines(t.Relations, inner))
	}
	return strings.Join(all, "\n")
}

// scopeLines lists the scope questions with their options, the recommended
// one starred with its reason.
func scopeLines(questions []draft.ScopeQuestion, width int) []string {
	if len(questions) == 0 {
		return []string{pendingStyle.Render("none")}
	}
	var lines []string
	for i, q := range questions {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, strings.Split(titleStyle.Render(hang("? ", q.Question, width)), "\n")...)
		for _, option := range q.Options {
			star := ""
			if option == q.Recommended {
				star = keyStyle.Render(" ★")
			}
			lines = append(lines, strings.Split(hang("  • ", option+star, width), "\n")...)
		}
		advice := q.Why
		if len(q.Options) == 0 {
			advice = strings.TrimSuffix(q.Recommended+", "+q.Why, ", ")
		}
		if advice != "" {
			lines = append(lines, quietStyle.Render(hang("  ★ ", advice, width)))
		}
	}
	return lines
}

func relationLines(relations []draft.Relation, width int) []string {
	var lines []string
	for _, relation := range relations {
		lines = append(lines, keyStyle.Render(relation.Label()+" "+relation.Key))
		lines = append(lines, strings.Split(hang("  ", relation.Why, width), "\n")...)
	}
	return lines
}
