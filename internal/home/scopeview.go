package home

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mlmejo/refine/internal/draft"
)

// The approach screen answers the triage's scope questions above the
// approach, so the questions step builds on the answers instead of asking
// them again. tab moves between the approach and the questions.

// scopeQuestions are the selected ticket's triage scope questions.
func (m Model) scopeQuestions() []draft.ScopeQuestion {
	if len(m.issues) == 0 {
		return nil
	}
	return m.triages[m.issues[m.cursor].Key].Triage.ScopeQuestions
}

// openScope starts the scope answers from the last draft's.
func (m *Model) openScope(saved draft.Saved, ok bool) {
	m.scope = map[string]string{}
	if ok {
		m.scope = maps.Clone(saved.Scope)
		if m.scope == nil {
			m.scope = map[string]string{}
		}
	}
	m.scopeFocus, m.scopeCursor, m.scopeEditing = false, 0, false
}

// scopeAnswers are the answers to the ticket's current scope questions.
func (m Model) scopeAnswers() map[string]string {
	answers := map[string]string{}
	for _, q := range m.scopeQuestions() {
		if answer := strings.TrimSpace(m.scope[q.Question]); answer != "" {
			answers[q.Question] = answer
		}
	}
	if len(answers) == 0 {
		return nil
	}
	return answers
}

// setScope swaps in a new answers map rather than writing to a shared one.
func (m *Model) setScope(question, answer string) {
	scope := maps.Clone(m.scope)
	if scope == nil {
		scope = map[string]string{}
	}
	if answer == "" {
		delete(scope, question)
	} else {
		scope[question] = answer
	}
	m.scope = scope
}

func (m Model) updateScope(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	questions := m.scopeQuestions()
	if m.scopeEditing {
		switch msg.String() {
		case "esc":
			m.scopeEditing = false
			m.answerIn.Blur()
			return m, nil
		case "enter":
			m.setScope(questions[m.scopeCursor].Question, strings.TrimSpace(m.answerIn.Value()))
			m.scopeEditing = false
			m.answerIn.Blur()
			m.resizeRefine()
			return m, nil
		}
		var cmd tea.Cmd
		m.answerIn, cmd = m.answerIn.Update(msg)
		return m, cmd
	}

	q := questions[m.scopeCursor]
	switch msg.String() {
	case "tab":
		m.scopeFocus = false
		return m, m.input.Focus()
	case "esc":
		m.screen, m.scopeFocus = detail, false
		return m, nil
	case "up", "k":
		m.scopeCursor = max(m.scopeCursor-1, 0)
	case "down", "j":
		m.scopeCursor = min(m.scopeCursor+1, len(questions)-1)
	case "left", "h", "right", "l":
		if len(q.Options) == 0 {
			break
		}
		// Cycle through the options and back to unanswered.
		at := slices.Index(q.Options, m.scope[q.Question])
		step := 1
		if msg.String() == "left" || msg.String() == "h" {
			step = -1
		}
		at = (at+1+step+len(q.Options)+1)%(len(q.Options)+1) - 1
		answer := ""
		if at >= 0 {
			answer = q.Options[at]
		}
		m.setScope(q.Question, answer)
	case "r":
		m.setScope(q.Question, q.Recommended)
	case "R":
		for _, each := range questions {
			if strings.TrimSpace(m.scope[each.Question]) == "" {
				m.setScope(each.Question, each.Recommended)
			}
		}
	case "x", "delete", "backspace":
		m.setScope(q.Question, "")
	case "enter", "e":
		m.scopeEditing = true
		value := m.scope[q.Question]
		if slices.Contains(q.Options, value) {
			value = ""
		}
		m.answerIn.SetValue(value)
		m.answerIn.CursorEnd()
		m.resizeRefine()
		return m, m.answerIn.Focus()
	}
	m.resizeRefine()
	return m, nil
}

// renderScope lays out the scope questions with their options, the picked
// one filled, the recommended one starred.
func (m Model) renderScope(width int) string {
	questions := m.scopeQuestions()
	if len(questions) == 0 {
		return ""
	}
	answered := 0
	for _, q := range questions {
		if strings.TrimSpace(m.scope[q.Question]) != "" {
			answered++
		}
	}
	var lines []string
	head := fmt.Sprintf("Scope · %d/%d answered", answered, len(questions))
	lines = append(lines, headingStyle.Render(head)+quietStyle.Render(" · they settle what to build, so Claude won't ask them again"))
	for i, q := range questions {
		focused := m.scopeFocus && i == m.scopeCursor
		marker := "  "
		if focused {
			marker = focusStyle.Render("▍ ")
		}
		text := hang("? ", q.Question, width-2)
		if focused {
			text = focusStyle.Render(text)
		}
		for _, line := range strings.Split(text, "\n") {
			lines = append(lines, marker+line)
		}
		answer := strings.TrimSpace(m.scope[q.Question])
		for _, option := range q.Options {
			dot := "( ) "
			if answer == option {
				dot = doneStyle.Render("(•) ")
			}
			star := ""
			if option == q.Recommended {
				star = keyStyle.Render(" ★")
			}
			for _, line := range strings.Split(hang("    "+dot, option+star, width-2), "\n") {
				lines = append(lines, marker+line)
			}
		}
		switch {
		case focused && m.scopeEditing:
			lines = append(lines, marker+"    "+m.answerIn.View())
		case answer != "" && !slices.Contains(q.Options, answer):
			lines = append(lines, marker+answerStyle.Render(hang("    ↳ ", answer, width-2)))
		case len(q.Options) == 0:
			lines = append(lines, marker+pendingStyle.Render(hang("    ↳ ", "suggested: "+q.Recommended, width-2)))
		}
		if q.Why != "" && focused {
			lines = append(lines, marker+quietStyle.Render(hang("    ★ ", q.Why, width-2)))
		}
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}
