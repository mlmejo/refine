package home

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/gitsync"
	"github.com/mlmejo/refine/internal/jira"
)

// Refiner drafts refinements and reads saved ones.
type Refiner struct {
	Load  func(key string) (draft.Saved, bool, error)
	Draft func(ctx context.Context, issue jira.Issue, req draft.Request) (draft.Saved, error)
	// LoadBrief reads the ticket's latest brief; Brief writes a new one.
	LoadBrief func(key string) (draft.SavedBrief, bool, error)
	Brief     func(ctx context.Context, issue jira.Issue) (draft.SavedBrief, error)
	// LoadTriage reads the ticket's latest triage; Triage writes a new one.
	LoadTriage func(key string) (draft.SavedTriage, bool, error)
	Triage     func(ctx context.Context, issue jira.Issue) (draft.SavedTriage, error)
	// Stamp writes up a draft's questions into the fields Jira takes,
	// returning the stamped draft.
	Stamp func(ctx context.Context, issue jira.Issue, saved draft.Saved) (draft.Saved, error)
	// SaveAnswers keeps the answers on a draft.
	SaveAnswers func(saved draft.Saved) error
	// Reveal shows the draft's markdown file.
	Reveal func(saved draft.Saved) error
	// Write puts the draft in the ticket's refinement fields and returns the
	// ticket as Jira now has it.
	Write func(ctx context.Context, key string, d draft.Draft) (jira.Issue, error)
	// Split creates the draft's split tickets that saved.Children does not
	// have yet, keeping each key as it goes. It returns the keys of every
	// split ticket created so far, by index into Draft.Split, even when it
	// fails partway.
	Split func(ctx context.Context, saved draft.Saved) (map[int]string, error)
	// Move puts a written ticket in ReadySprint.
	Move        func(ctx context.Context, key string) error
	ReadySprint string
	// Check compares the repos Claude reads with origin before a fresh brief
	// or draft; Pull fast-forwards one that is behind. Check is nil when the
	// repos are read at --ref, which already reads origin.
	Check func(ctx context.Context) []gitsync.Status
	Pull  func(ctx context.Context, s gitsync.Status) error
}

// writtenMsg is how writing went. children are the split tickets created so
// far; splitErr is set when the ticket was written but not every split ticket
// was created, and moveErr when it could not be moved.
type writtenMsg struct {
	issue    jira.Issue
	err      error
	children map[int]string
	splitErr error
	moveErr  error
}

type exportedMsg struct {
	path string
	err  error
}

type draftedMsg struct {
	runID int
	saved draft.Saved
	err   error
}

type elapsedMsg struct {
	runID int
}

// childKeys lists split ticket keys in split order.
func childKeys(children map[int]string) string {
	indexes := slices.Sorted(maps.Keys(children))
	keys := make([]string, 0, len(indexes))
	for _, i := range indexes {
		keys = append(keys, children[i])
	}
	return strings.Join(keys, ", ")
}

func newInput() textarea.Model {
	input := textarea.New()
	input.Placeholder = "How would you build it? Mark anything you're unsure of with ?"
	input.ShowLineNumbers = false
	input.Prompt = "│ "
	input.CharLimit = 0
	return input
}

// loadSaved reads the ticket's saved draft, brief and triage once.
func (m *Model) loadSaved(key string) {
	m.loadTriage(key)
	if _, ok := m.briefs[key]; !ok && m.refiner.LoadBrief != nil {
		if brief, ok, err := m.refiner.LoadBrief(key); err == nil && ok {
			m.briefs[key] = brief
		}
	}
	if _, ok := m.saved[key]; ok || m.refiner.Load == nil {
		return
	}
	if saved, ok, err := m.refiner.Load(key); err == nil && ok {
		m.saved[key] = saved
	}
}

func (m *Model) loadTriage(key string) {
	if _, ok := m.triages[key]; ok || m.refiner.LoadTriage == nil {
		return
	}
	if triage, ok, err := m.refiner.LoadTriage(key); err == nil && ok {
		m.triages[key] = triage
	}
}

func (m Model) openApproach() (tea.Model, tea.Cmd) {
	key := m.issues[m.cursor].Key
	m.input.Reset()
	saved, ok := m.saved[key]
	if ok {
		m.input.SetValue(saved.Approach)
	}
	m.openScope(saved, ok)
	m.screen, m.draftErr = approach, nil
	m.resizeRefine()
	return m, m.input.Focus()
}

func (m *Model) resizeRefine() {
	inputWidth, briefWidth := m.approachWidths()
	m.input.SetWidth(max(inputWidth-2, 20))
	if briefWidth > 0 {
		m.sidePane.Width, m.sidePane.Height = briefWidth, max(m.height-4, 1)
		m.sidePane.SetContent(m.renderSide(briefWidth))
	}
	if m.screen == briefed && len(m.issues) > 0 {
		m.layoutBrief()
	}
	if m.screen == triaged && len(m.issues) > 0 {
		m.layoutTriage()
	}
	scope := 0
	if m.screen == approach && len(m.issues) > 0 {
		if block := m.renderScope(max(inputWidth-2, 20)); block != "" {
			scope = lipgloss.Height(block) + 1
		}
	}
	m.input.SetHeight(max(m.height-8-scope, 3))
	if m.screen == drafted && len(m.issues) > 0 {
		m.layoutDraft()
	}
}

func (m Model) updateRefine(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case draftedMsg:
		if msg.runID != m.runID || m.screen != drafting {
			return m, nil
		}
		m.cancel = nil
		if m.stamping {
			m.stamping = false
			if msg.err != nil {
				m = m.openDraft()
				m.answerErr = fmt.Errorf("could not stamp: %w", msg.err)
				return m, nil
			}
		}
		if msg.err != nil {
			m.screen, m.draftErr = approach, msg.err
			return m, m.input.Focus()
		}
		m.saved[msg.saved.Key] = msg.saved
		// A fresh draft triages the ticket first when it had no triage.
		m.loadTriage(msg.saved.Key)
		return m.openDraft(), nil

	case exportedMsg:
		m.exported, m.exportErr = msg.path, msg.err
		return m, nil

	case writtenMsg:
		m.writing = false
		if msg.err != nil {
			m.writeErr = msg.err
			return m, nil
		}
		key := msg.issue.Key
		written := "written to " + key
		if saved, ok := m.saved[key]; ok && len(msg.children) > 0 {
			saved.Children = msg.children
			m.saved[key] = saved
			written += ", split into " + childKeys(msg.children)
		}
		m.screen, m.done, m.failed = list, written, nil
		switch {
		case msg.splitErr != nil:
			m.done, m.failed = "", fmt.Errorf("%s, but could not create every split ticket, so it stays here; w again creates the rest: %w", written, msg.splitErr)
		case msg.moveErr != nil:
			m.done, m.failed = "", fmt.Errorf("%s, but could not move it to %s: %w", written, m.refiner.ReadySprint, msg.moveErr)
		case m.refiner.Move != nil:
			// It has left the refinement sprint, so it leaves the list.
			m.dropIssue(key)
			m.done += " and moved to " + m.refiner.ReadySprint
			return m, nil
		}
		for i, issue := range m.issues {
			if issue.Key == key {
				m.issues[i] = msg.issue
			}
		}
		return m, nil

	case answerSavedMsg:
		if msg.err != nil {
			m.answerErr = fmt.Errorf("could not save the answer: %w", msg.err)
		}
		return m, nil

	case briefedMsg:
		return m.briefDone(msg)

	case triagedMsg:
		return m.triageDone(msg)

	case elapsedMsg:
		if msg.runID != m.runID || (m.screen != drafting && m.screen != briefing && m.screen != triaging) {
			return m, nil
		}
		return m, tickElapsed(m.runID)

	case spinner.TickMsg:
		if m.screen != drafting && m.screen != briefing && m.screen != triaging && m.screen != checking {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		switch m.screen {
		case approach:
			if msg.String() == "ctrl+s" {
				req := draft.Request{Approach: m.input.Value(), Scope: m.scopeAnswers()}
				if saved, ok := m.saved[m.issues[m.cursor].Key]; ok {
					req.Previous = &saved
				}
				m.scopeFocus, m.scopeEditing = false, false
				return m.gate(&req)
			}
			if m.scopeFocus {
				return m.updateScope(msg)
			}
			switch msg.String() {
			case "tab":
				if len(m.scopeQuestions()) > 0 {
					m.scopeFocus = true
					m.input.Blur()
					m.resizeRefine()
					return m, nil
				}
			case "esc":
				m.screen = detail
				m.input.Blur()
				return m, nil
			case "pgup", "pgdown":
				if msg.String() == "pgup" {
					m.sidePane.HalfPageUp()
				} else {
					m.sidePane.HalfPageDown()
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd

		case drafting:
			if msg.String() == "esc" {
				m.cancel()
				m.cancel, m.runID = nil, m.runID+1
				if m.stamping {
					m.stamping = false
					m = m.openDraft()
					m.answerErr = fmt.Errorf("stamp cancelled")
					return m, nil
				}
				m.screen, m.draftErr = approach, fmt.Errorf("cancelled")
				return m, m.input.Focus()
			}

		case drafted:
			return m.updateDrafted(msg)

		case briefing, briefed:
			return m.updateBrief(msg)

		case triaging, triaged:
			return m.updateTriage(msg)
		}
	}
	return m, nil
}

func (m Model) startDraft(req draft.Request) (tea.Model, tea.Cmd) {
	issue := m.issues[m.cursor]
	if brief, ok := m.briefs[issue.Key]; ok && !req.Resume {
		req.Brief = &brief
	}
	if triage, ok := m.triages[issue.Key]; ok && !req.Resume {
		req.Triage = &triage
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.runID++
	m.cancel, m.started, m.resuming, m.fromBrief, m.stamping = cancel, time.Now(), req.Resume, req.Brief != nil, false
	if req.Resume {
		m.syncNotes = nil
	}
	m.screen, m.draftErr = drafting, nil
	m.input.Blur()

	runID, run := m.runID, m.refiner.Draft
	work := func() tea.Msg {
		saved, err := run(ctx, issue, req)
		return draftedMsg{runID: runID, saved: saved, err: err}
	}
	return m, tea.Batch(work, m.spin.Tick, tickElapsed(runID))
}

func tickElapsed(runID int) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return elapsedMsg{runID: runID} })
}

func (m Model) ticketHeader() string {
	issue := m.issues[m.cursor]
	return titleStyle.Render(issue.Key) + "  " + truncate(issue.Summary, m.width-len(issue.Key)-2)
}

// approachWidths splits the approach screen between the input and the
// ticket's triage and brief, which show beside it on a wide enough terminal.
func (m Model) approachWidths() (int, int) {
	width := max(m.width, 20)
	if len(m.issues) == 0 || width < sideMinWidth || !m.hasSide() {
		return width, 0
	}
	left := width * 9 / 20
	return left, width - left
}

// hasSide reports whether the ticket has a triage or brief to show beside
// the approach.
func (m Model) hasSide() bool {
	key := m.issues[m.cursor].Key
	_, triaged := m.triages[key]
	_, briefed := m.briefs[key]
	return triaged || briefed
}

// renderSide is the triage, then the brief, for the approach screen.
func (m Model) renderSide(width int) string {
	key := m.issues[m.cursor].Key
	var parts []string
	if triage, ok := m.triages[key]; ok {
		// The scope questions are answered beside it, so they are left out.
		parts = append(parts, renderTriage(triage.Triage, width, false))
	}
	if brief, ok := m.briefs[key]; ok {
		parts = append(parts, renderBrief(brief, width))
	}
	return strings.Join(parts, "\n\n")
}

func (m Model) viewApproach() string {
	inputWidth, briefWidth := m.approachWidths()
	var out strings.Builder
	out.WriteString(lipgloss.NewStyle().Width(inputWidth).Render(quietStyle.Render(
		"Write what you know, in a few lines. Mark anything you're unsure of with ? and Claude answers it from the code. Empty is fine too.")) + "\n\n")
	if block := m.renderScope(max(inputWidth-2, 20)); block != "" {
		out.WriteString(block + "\n\n")
	}
	out.WriteString(m.input.View() + "\n")
	if m.draftErr != nil {
		out.WriteString(lipgloss.NewStyle().Width(inputWidth).Render(errorStyle.Render(m.draftErr.Error())) + "\n")
	}
	body := out.String()
	help := "ctrl+s ask Claude's questions · esc back"
	if m.scopeFocus {
		help = "↑/↓ question · ←/→ pick · enter own answer · r recommended · R all recommended · tab approach · ctrl+s ask"
		if m.scopeEditing {
			help = "enter save · esc cancel"
		}
		return m.withFooter(m.ticketHeader()+"\n"+body+"\n", truncate(help, m.width))
	}
	if len(m.scopeQuestions()) > 0 {
		help = "ctrl+s ask Claude's questions · tab scope questions · esc back"
	}
	if briefWidth > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(inputWidth).Render(body), m.sidePane.View())
		help = "ctrl+s ask Claude's questions · pgup/pgdn scroll triage and brief · esc back"
	} else if m.hasSide() {
		help = "ctrl+s ask Claude's questions · esc back · widen the terminal to see the triage and brief here"
	}
	return m.withFooter(m.ticketHeader()+"\n"+body+"\n", truncate(help, m.width))
}

func (m Model) viewDrafting() string {
	elapsed := time.Since(m.started).Truncate(time.Second)
	body := m.ticketHeader() + "\n\n" +
		m.spin.View() + " Claude is reading the repos for its questions… " + elapsed.String() + "\n" +
		quietStyle.Render("Usually about 2 minutes, on your Claude plan. Triages the ticket first if it has no triage.") + "\n"
	if m.fromBrief {
		body = m.ticketHeader() + "\n\n" +
			m.spin.View() + " Claude is drafting from the brief… " + elapsed.String() + "\n" +
			quietStyle.Render("Continues the brief's session, so it rereads little code.") + "\n"
	}
	if m.resuming {
		body = m.ticketHeader() + "\n\n" +
			m.spin.View() + " Claude is working through your answers… " + elapsed.String() + "\n" +
			quietStyle.Render("Picks up the earlier session, so it rereads little code.") + "\n"
	}
	if m.stamping {
		body = m.ticketHeader() + "\n\n" +
			m.spin.View() + " Claude is stamping the ticket from your answers… " + elapsed.String() + "\n" +
			quietStyle.Render("Writes Acceptance Criteria, Technical Implementation, points and the split, then checks them against the other open tickets. Forks the questions session.") + "\n"
	}
	return m.withFooter(body+m.syncLines(), "esc cancel")
}

// hang wraps text after a marker, indenting following lines under the text.
// The text's inline markdown shows as styles.
func hang(marker, text string, width int) string {
	indent := lipgloss.Width(marker)
	wrapped := lipgloss.NewStyle().Width(max(width-indent-1, 10)).Render(inline(text))
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
		if i == 0 {
			lines[i] = marker + lines[i]
		} else {
			lines[i] = strings.Repeat(" ", indent) + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
