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

// loadSaved reads the ticket's saved draft and brief once.
func (m *Model) loadSaved(key string) {
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

func (m Model) openApproach() (tea.Model, tea.Cmd) {
	key := m.issues[m.cursor].Key
	m.input.Reset()
	if saved, ok := m.saved[key]; ok {
		m.input.SetValue(saved.Approach)
	}
	m.screen, m.draftErr = approach, nil
	m.resizeRefine()
	return m, m.input.Focus()
}

func (m *Model) resizeRefine() {
	inputWidth, briefWidth := m.approachWidths()
	m.input.SetWidth(max(inputWidth-2, 20))
	if briefWidth > 0 {
		m.sidePane.Width, m.sidePane.Height = briefWidth, max(m.height-4, 1)
		m.sidePane.SetContent(renderBrief(m.briefs[m.issues[m.cursor].Key], briefWidth))
	}
	if m.screen == briefed && len(m.issues) > 0 {
		m.layoutBrief()
	}
	m.input.SetHeight(max(m.height-8, 3))
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
		if msg.err != nil {
			m.screen, m.draftErr = approach, msg.err
			return m, m.input.Focus()
		}
		m.saved[msg.saved.Key] = msg.saved
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

	case elapsedMsg:
		if msg.runID != m.runID || (m.screen != drafting && m.screen != briefing) {
			return m, nil
		}
		return m, tickElapsed(m.runID)

	case spinner.TickMsg:
		if m.screen != drafting && m.screen != briefing && m.screen != checking {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		switch m.screen {
		case approach:
			switch msg.String() {
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
			case "ctrl+s":
				req := draft.Request{Approach: m.input.Value()}
				if saved, ok := m.saved[m.issues[m.cursor].Key]; ok {
					req.Previous = &saved
				}
				return m.gate(&req)
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd

		case drafting:
			if msg.String() == "esc" {
				m.cancel()
				m.cancel, m.runID = nil, m.runID+1
				m.screen, m.draftErr = approach, fmt.Errorf("cancelled")
				return m, m.input.Focus()
			}

		case drafted:
			return m.updateDrafted(msg)

		case briefing, briefed:
			return m.updateBrief(msg)
		}
	}
	return m, nil
}

func (m Model) startDraft(req draft.Request) (tea.Model, tea.Cmd) {
	issue := m.issues[m.cursor]
	if brief, ok := m.briefs[issue.Key]; ok && !req.Resume {
		req.Brief = &brief
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.runID++
	m.cancel, m.started, m.resuming, m.fromBrief = cancel, time.Now(), req.Resume, req.Brief != nil
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
// ticket's brief, which shows beside it on a wide enough terminal.
func (m Model) approachWidths() (int, int) {
	width := max(m.width, 20)
	if len(m.issues) == 0 || width < sideMinWidth {
		return width, 0
	}
	if _, ok := m.briefs[m.issues[m.cursor].Key]; !ok {
		return width, 0
	}
	left := width * 9 / 20
	return left, width - left
}

func (m Model) viewApproach() string {
	inputWidth, briefWidth := m.approachWidths()
	var out strings.Builder
	out.WriteString(lipgloss.NewStyle().Width(inputWidth).Render(quietStyle.Render(
		"Write what you know, in a few lines. Mark anything you're unsure of with ? and Claude answers it from the code. Empty is fine too.")) + "\n\n")
	out.WriteString(m.input.View() + "\n")
	if m.draftErr != nil {
		out.WriteString(lipgloss.NewStyle().Width(inputWidth).Render(errorStyle.Render(m.draftErr.Error())) + "\n")
	}
	body := out.String()
	help := "ctrl+s draft with Claude · esc back"
	if briefWidth > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(inputWidth).Render(body), m.sidePane.View())
		help = "ctrl+s draft with Claude · pgup/pgdn scroll brief · esc back"
	} else if _, ok := m.briefs[m.issues[m.cursor].Key]; ok {
		help = "ctrl+s draft with Claude · esc back · widen the terminal to see the brief here"
	}
	return m.withFooter(m.ticketHeader()+"\n"+body+"\n", truncate(help, m.width))
}

func (m Model) viewDrafting() string {
	elapsed := time.Since(m.started).Truncate(time.Second)
	body := m.ticketHeader() + "\n\n" +
		m.spin.View() + " Claude is reading the repos and drafting… " + elapsed.String() + "\n" +
		quietStyle.Render("Usually about 2 minutes, on your Claude plan.") + "\n"
	if m.fromBrief {
		body = m.ticketHeader() + "\n\n" +
			m.spin.View() + " Claude is drafting from the brief… " + elapsed.String() + "\n" +
			quietStyle.Render("Continues the brief's session, so it rereads little code.") + "\n"
	}
	if m.resuming {
		body = m.ticketHeader() + "\n\n" +
			m.spin.View() + " Claude is revising the draft with your answers… " + elapsed.String() + "\n" +
			quietStyle.Render("Picks up the earlier session, so it rereads little code.") + "\n"
	}
	return m.withFooter(body+m.syncLines(), "esc cancel")
}

// hang wraps text after a marker, indenting following lines under the text.
func hang(marker, text string, width int) string {
	indent := lipgloss.Width(marker)
	wrapped := lipgloss.NewStyle().Width(max(width-indent-1, 10)).Render(text)
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
