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

type briefedMsg struct {
	runID int
	brief draft.SavedBrief
	err   error
}

func (m Model) startBrief() (tea.Model, tea.Cmd) {
	if m.refiner.Brief == nil {
		return m, nil
	}
	issue := m.issues[m.cursor]
	ctx, cancel := context.WithCancel(context.Background())
	m.runID++
	m.cancel, m.started = cancel, time.Now()
	m.screen, m.briefErr = briefing, nil

	runID, run := m.runID, m.refiner.Brief
	work := func() tea.Msg {
		brief, err := run(ctx, issue)
		return briefedMsg{runID: runID, brief: brief, err: err}
	}
	return m, tea.Batch(work, m.spin.Tick, tickElapsed(runID))
}

func (m Model) briefDone(msg briefedMsg) (tea.Model, tea.Cmd) {
	if msg.runID != m.runID || m.screen != briefing {
		return m, nil
	}
	m.cancel = nil
	if msg.err != nil {
		m.briefErr = msg.err
		return m.openBrief(), nil
	}
	m.briefs[msg.brief.Key] = msg.brief
	return m.openBrief(), nil
}

func (m Model) openBrief() Model {
	m.screen = briefed
	m.resizeRefine()
	m.pane.GotoTop()
	return m
}

func (m *Model) layoutBrief() {
	m.pane.Width, m.pane.Height = max(m.width, 20), max(m.height-5, 1)
	if brief, ok := m.briefs[m.issues[m.cursor].Key]; ok {
		m.pane.SetContent(renderBrief(brief, m.pane.Width))
	} else {
		m.pane.SetContent("")
	}
}

func (m Model) updateBrief(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.screen == briefing {
		if msg.String() == "esc" {
			m.cancel()
			m.cancel, m.runID = nil, m.runID+1
			m.briefErr = fmt.Errorf("cancelled")
			return m.openBrief(), nil
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
	case "R":
		return m.gate(nil)
	case "b":
		if _, ok := m.briefs[m.issues[m.cursor].Key]; !ok {
			return m.gate(nil)
		}
	}
	var cmd tea.Cmd
	m.pane, cmd = m.pane.Update(msg)
	return m, cmd
}

func (m Model) viewBriefing() string {
	elapsed := time.Since(m.started).Truncate(time.Second)
	body := m.ticketHeader() + "\n\n" +
		m.spin.View() + " Claude is reading the code to brief you… " + elapsed.String() + "\n" +
		quietStyle.Render("Usually about 2 minutes. The draft continues this session, so the code is read once.") + "\n" +
		m.syncLines()
	return m.withFooter(body, "esc cancel")
}

func (m Model) viewBriefed() string {
	brief, ok := m.briefs[m.issues[m.cursor].Key]
	if !ok {
		body := m.ticketHeader() + "\n\n"
		if m.briefErr != nil {
			body += lipgloss.NewStyle().Width(m.width).Render(errorStyle.Render(m.briefErr.Error())) + "\n"
		}
		return m.withFooter(body, "b try again · a approach without a brief · esc back")
	}

	meta := fmt.Sprintf("brief · $%.2f · %s", brief.CostUSD, brief.Created.Local().Format("Jan 2 15:04"))
	if brief.Ref != "" {
		meta += " · repos at " + brief.Ref
	}
	if brief.Turns > 0 {
		meta += fmt.Sprintf(" · %d turns", brief.Turns)
	}
	help := "↑/↓ scroll · a write your approach · R brief again · esc back"
	if m.briefErr != nil {
		help = errorStyle.Render("brief again failed: " + m.briefErr.Error())
	}
	body := m.ticketHeader() + "\n" + quietStyle.Render(meta) + "\n\n" + m.pane.View() + "\n"
	return m.withFooter(body, truncate(help, m.width))
}

// renderBrief lays out a brief as bordered sections at the given width.
func renderBrief(saved draft.SavedBrief, width int) string {
	b := saved.Brief
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

	section("What this area does", strings.Split(hang("", b.Summary, inner), "\n"))

	var flow []string
	for i, step := range b.Flow {
		flow = append(flow, strings.Split(hang(fmt.Sprintf("%d. ", i+1), step, inner), "\n")...)
	}
	if len(flow) == 0 {
		flow = []string{pendingStyle.Render("none")}
	}
	section("How it flows today", flow)

	var parts []string
	repo := ""
	for _, part := range b.Parts {
		if part.Repo != repo {
			repo = part.Repo
			parts = append(parts, keyStyle.Render(repo))
		}
		parts = append(parts, strings.Split(hang("  ", part.Path, inner), "\n")...)
		parts = append(parts, quietStyle.Render(hang("    ", part.Role, inner)))
	}
	if len(parts) == 0 {
		parts = []string{pendingStyle.Render("none")}
	}
	section("Key files", parts)

	var terms []string
	for _, term := range b.Terms {
		terms = append(terms, strings.Split(hang("• ", titleStyle.Render(term.Term)+": "+term.Meaning, inner), "\n")...)
	}
	if len(terms) == 0 {
		terms = []string{pendingStyle.Render("none")}
	}
	section("Terms", terms)

	section("Gotchas", bullets("• ", b.Gotchas))
	section("Ask the team", bullets("? ", b.AskTeam))
	return strings.Join(all, "\n")
}
