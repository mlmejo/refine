package home

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mlmejo/refine/internal/draft"
)

var (
	borderStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	focusStyle   = lipgloss.NewStyle().Bold(true)
	answerStyle  = lipgloss.NewStyle().Italic(true)
	pendingStyle = lipgloss.NewStyle().Faint(true).Italic(true)
)

// sideMinWidth is the narrowest terminal that shows a split ticket beside the
// draft; narrower ones show it in place of the draft.
const sideMinWidth = 100

type itemKind int

const (
	childItem itemKind = iota
	questionItem
)

// item is something the cursor can stop on in a draft.
type item struct {
	kind  itemKind
	index int    // into Draft.Split for a child
	text  string // the question, for a question
}

// span is the lines an item takes in the rendered draft.
type span struct {
	start, end int
}

type answerSavedMsg struct {
	err error
}

// draftItems lists the cursor stops in render order: blockers, split
// tickets, then open questions.
func draftItems(d draft.Draft) []item {
	var items []item
	for _, blocker := range d.Deployment.Blockers {
		items = append(items, item{kind: questionItem, text: blocker})
	}
	for i := range d.Split {
		items = append(items, item{kind: childItem, index: i})
	}
	for _, question := range d.OpenQuestions {
		items = append(items, item{kind: questionItem, text: question})
	}
	return items
}

func newAnswerInput() textinput.Model {
	input := textinput.New()
	input.Prompt = "↳ "
	input.Placeholder = "Your answer, even just \"no\""
	input.CharLimit = 0
	return input
}

func (m Model) current() draft.Saved {
	return m.saved[m.issues[m.cursor].Key]
}

func (m Model) openDraft() Model {
	m.screen = drafted
	m.exported, m.exportErr, m.answerErr = "", nil, nil
	m.confirming, m.writeErr = false, nil
	m.focus, m.side, m.answering = 0, -1, false
	m.resizeRefine()
	m.pane.GotoTop()
	return m
}

// layoutDraft sizes the panes and renders the draft and any open split ticket.
func (m *Model) layoutDraft() {
	saved := m.current()
	mainWidth, sideWidth := m.paneWidths()
	height := max(m.height-5, 1)
	m.pane.Width, m.pane.Height = mainWidth, height
	m.sidePane.Width, m.sidePane.Height = sideWidth, height

	m.answerIn.Width = max(mainWidth-10, 10)
	content, spans := renderDraft(saved, mainWidth, m.focusFor(), m.answerView())
	m.spans = spans
	m.pane.SetContent(content)
	m.keepFocusVisible()

	if m.side >= 0 && m.side < len(saved.Draft.Split) {
		m.sidePane.SetContent(renderChild(saved.Draft.Split[m.side], saved.Children[m.side], sideWidth))
	}
}

// paneWidths splits the screen between the draft and an open split ticket.
func (m Model) paneWidths() (int, int) {
	width := max(m.width, 20)
	switch {
	case m.side < 0:
		return width, 0
	case width < sideMinWidth:
		return 0, width
	}
	left := width * 11 / 20
	return left, width - left
}

// focusFor is the focused item, or -1 when the draft has none.
func (m Model) focusFor() int {
	if m.focus >= len(draftItems(m.current().Draft)) {
		return -1
	}
	return m.focus
}

func (m Model) answerView() string {
	if !m.answering {
		return ""
	}
	return m.answerIn.View()
}

func (m *Model) keepFocusVisible() {
	if m.focus >= len(m.spans) {
		return
	}
	at := m.spans[m.focus]
	switch {
	case at.start <= 1:
		m.pane.GotoTop()
	case at.start < m.pane.YOffset:
		m.pane.SetYOffset(at.start - 1)
	case at.end >= m.pane.YOffset+m.pane.Height:
		m.pane.SetYOffset(min(at.end+1-m.pane.Height+1, at.start-1))
	}
}

func (m Model) updateDrafted(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.answering {
		return m.updateAnswer(msg)
	}
	if m.confirming {
		return m.updateConfirm(msg)
	}
	saved := m.current()
	items := draftItems(saved.Draft)

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace", "h", "left":
		if m.side >= 0 {
			m.side = -1
			m.layoutDraft()
			return m, nil
		}
		m.screen = detail
		return m, nil
	case "up", "k", "down", "j":
		if len(items) == 0 {
			break
		}
		if msg.String() == "up" || msg.String() == "k" {
			m.focus = max(m.focus-1, 0)
		} else {
			m.focus = min(m.focus+1, len(items)-1)
		}
		if m.side >= 0 && items[m.focus].kind == childItem {
			m.side = items[m.focus].index
		}
		m.layoutDraft()
		return m, nil
	case "enter", "l", "right":
		if m.focus >= len(items) {
			return m, nil
		}
		focused := items[m.focus]
		if focused.kind == childItem {
			if m.side == focused.index {
				m.side = -1
			} else {
				m.side = focused.index
			}
			m.layoutDraft()
			return m, nil
		}
		m.answering, m.answerErr = true, nil
		m.answerIn.SetValue(saved.Answers[focused.text])
		m.answerIn.CursorEnd()
		m.layoutDraft()
		return m, m.answerIn.Focus()
	case "J", "K":
		if m.side >= 0 {
			if msg.String() == "J" {
				m.sidePane.ScrollDown(1)
			} else {
				m.sidePane.ScrollUp(1)
			}
		}
		return m, nil
	case "a":
		return m.openApproach()
	case "r":
		if left := saved.Unanswered(); left > 0 {
			m.answerErr = fmt.Errorf("answer %d more question%s before redrafting", left, plural(left))
			return m, nil
		}
		m.input.SetValue(saved.Approach)
		return m.startDraft(draft.Request{Approach: saved.Approach, Previous: &saved, Resume: true})
	case "w":
		if m.refiner.Write == nil || m.writing {
			return m, nil
		}
		if left := saved.Unanswered(); left > 0 {
			m.answerErr = fmt.Errorf("answer %d more question%s and redraft before writing to Jira", left, plural(left))
			return m, nil
		}
		m.confirming, m.writeErr, m.answerErr = true, nil, nil
		return m, nil
	case "s":
		reveal := m.refiner.Reveal
		if reveal == nil || saved.Markdown == "" {
			return m, nil
		}
		return m, func() tea.Msg {
			return exportedMsg{path: saved.Markdown, err: reveal(saved)}
		}
	}

	var cmd tea.Cmd
	m.pane, cmd = m.pane.Update(msg)
	return m, cmd
}

// updateConfirm writes the draft to Jira on y and cancels on any other key.
func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.confirming = false
	if msg.String() != "y" {
		return m, nil
	}
	m.writing = true
	saved, write, split, move := m.current(), m.refiner.Write, m.refiner.Split, m.refiner.Move
	return m, func() tea.Msg {
		ctx := context.Background()
		issue, err := write(ctx, saved.Key, saved.Draft)
		if err != nil {
			return writtenMsg{issue: issue, err: err}
		}
		msg := writtenMsg{issue: issue, children: saved.Children}
		if split != nil && len(saved.Draft.Split) > 0 {
			if msg.children, msg.splitErr = split(ctx, saved); msg.splitErr != nil {
				return msg
			}
		}
		if move != nil {
			msg.moveErr = move(ctx, saved.Key)
		}
		return msg
	}
}

// splitLeft counts the current draft's split tickets not created yet.
func (m Model) splitLeft() int {
	if m.refiner.Split == nil {
		return 0
	}
	saved := m.current()
	left := 0
	for i := range saved.Draft.Split {
		if saved.Children[i] == "" {
			left++
		}
	}
	return left
}

func (m Model) updateAnswer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.answering = false
		m.answerIn.Blur()
		m.layoutDraft()
		return m, nil
	case "enter":
		saved := m.current()
		question := draftItems(saved.Draft)[m.focus].text
		answers := maps.Clone(saved.Answers)
		if answers == nil {
			answers = map[string]string{}
		}
		if answer := strings.TrimSpace(m.answerIn.Value()); answer == "" {
			delete(answers, question)
		} else {
			answers[question] = answer
		}
		saved.Answers = answers
		m.saved[saved.Key] = saved

		m.answering = false
		m.answerIn.Blur()
		m.focus = nextUnanswered(saved, m.focus)
		m.layoutDraft()
		save := m.refiner.SaveAnswers
		if save == nil {
			return m, nil
		}
		return m, func() tea.Msg { return answerSavedMsg{err: save(saved)} }
	}
	var cmd tea.Cmd
	m.answerIn, cmd = m.answerIn.Update(msg)
	m.layoutDraft()
	return m, cmd
}

// nextUnanswered is the next question without an answer after from, or from
// when every question is answered.
func nextUnanswered(saved draft.Saved, from int) int {
	items := draftItems(saved.Draft)
	for step := 1; step <= len(items); step++ {
		i := (from + step) % len(items)
		if items[i].kind == questionItem && strings.TrimSpace(saved.Answers[items[i].text]) == "" {
			return i
		}
	}
	return from
}

func (m Model) viewDrafted() string {
	saved := m.current()
	meta := fmt.Sprintf("draft · %d pts · $%.2f · %s", saved.Draft.Points, saved.CostUSD, saved.Created.Local().Format("Jan 2 15:04"))
	if saved.Ref != "" {
		meta += " · repos at " + saved.Ref
	}
	if saved.Turns > 0 {
		usage := saved.Usage
		meta += fmt.Sprintf(" · %d turns · %s in · %s out", saved.Turns, tokens(usage.Input+usage.CacheWrite+usage.CacheRead), tokens(usage.Output))
	}
	if total := len(saved.Draft.Questions()); total > 0 {
		meta += fmt.Sprintf(" · %d/%d answered", total-saved.Unanswered(), total)
	}

	panes := m.pane.View()
	switch mainWidth, _ := m.paneWidths(); {
	case m.side >= 0 && mainWidth == 0:
		panes = m.sidePane.View()
	case m.side >= 0:
		panes = lipgloss.JoinHorizontal(lipgloss.Top, panes, m.sidePane.View())
	}
	body := m.ticketHeader() + "\n" + quietStyle.Render(meta) + "\n\n" + panes + "\n"

	help := "↑/↓ move · enter open/answer · r redraft with answers · a redo approach · s markdown · w write to Jira · esc back"
	switch {
	case m.answering:
		help = "enter save · esc cancel"
	case m.confirming:
		help = fmt.Sprintf("replace %s's Acceptance Criteria, Technical Implementation, Story Point Criteria and points (%d)", saved.Key, saved.Draft.Points)
		if left := m.splitLeft(); left > 0 {
			help += fmt.Sprintf(", create %d split ticket%s linked to it", left, plural(left))
		}
		if m.refiner.Move != nil {
			help += " and move it to " + m.refiner.ReadySprint
		}
		help += "? y/n"
	case m.writing:
		help = "writing to " + saved.Key + "…"
	case m.writeErr != nil:
		help = errorStyle.Render(truncate("could not write to Jira: "+m.writeErr.Error(), m.width))
	case m.answerErr != nil:
		help = errorStyle.Render(truncate(m.answerErr.Error(), m.width))
	case m.exportErr != nil && m.exported != "":
		help = errorStyle.Render(truncate("could not open "+m.exported+": "+m.exportErr.Error(), m.width))
	case m.exported != "":
		help = m.exported + " · esc back · q quit"
	case m.side >= 0:
		help = "↑/↓ move · J/K scroll ticket · enter/esc close · r redraft · a redo approach"
	}
	return m.withFooter(body, truncate(help, m.width))
}

// renderDraft lays out a draft as bordered sections at the given width, with
// the focused item marked and answer, when set, in place of its answer. It
// returns the lines each cursor stop takes.
func renderDraft(saved draft.Saved, width, focus int, answer string) (string, []span) {
	d := saved.Draft
	inner := max(width-4, 10)
	var all []string
	var spans []span
	stop := 0

	section := func(title string, lines []string) {
		if len(all) > 0 {
			all = append(all, "")
		}
		all = append(all, box(title, lines, width)...)
	}
	// stops are the cursor stops in the section being built, by line within it.
	type pending struct{ start, end int }
	var stops []pending
	mark := func(lines []string, body []string) []string {
		start := len(lines)
		if stop == focus {
			for i, line := range body {
				body[i] = focusStyle.Render("▍") + line
			}
		} else {
			for i, line := range body {
				body[i] = " " + line
			}
		}
		stops = append(stops, pending{start, start + len(body) - 1})
		stop++
		return append(lines, body...)
	}
	place := func(title string, lines []string) {
		base := len(all)
		if base > 0 {
			base++
		}
		for _, p := range stops {
			spans = append(spans, span{base + 1 + p.start, base + 1 + p.end})
		}
		stops = nil
		section(title, lines)
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
	question := func(lines []string, text string) []string {
		focused := stop == focus
		state := "○ "
		reply := strings.TrimSpace(saved.Answers[text])
		if reply != "" {
			state = "✓ "
		}
		body := strings.Split(hang(state, text, inner-1), "\n")
		switch {
		case focused && answer != "":
			body = append(body, "  "+answer)
		case reply != "":
			body = append(body, answerStyle.Render(hang("  ↳ ", reply, inner-1)))
		default:
			body = append(body, pendingStyle.Render("  ↳ needs an answer"))
		}
		if focused {
			body[0] = focusStyle.Render(body[0])
		}
		return mark(lines, strings.Split(strings.Join(body, "\n"), "\n"))
	}

	approach := []string{pendingStyle.Render("you didn't know yet")}
	if strings.TrimSpace(saved.Approach) != "" {
		approach = strings.Split(hang("", saved.Approach, inner), "\n")
	}
	place("Your approach", approach)

	var review []string
	for _, part := range []struct {
		name  string
		items []string
	}{{"Holds", d.ApproachReview.Holds}, {"Unsure, answered", d.ApproachReview.Unsure}, {"Not covered yet", d.ApproachReview.Misses}, {"Alternatives", d.ApproachReview.Alternatives}} {
		review = append(review, titleStyle.Render(part.name))
		review = append(review, bullets("• ", part.items)...)
	}
	place("Approach review", review)

	place(fmt.Sprintf("Acceptance Criteria · %d", len(d.AcceptanceCriteria)), bullets("* ", d.AcceptanceCriteria))
	place("Technical Implementation", repoLines(d.TechnicalImplementation, inner))

	var points []string
	for _, row := range d.StoryPointCriteria.Rows() {
		points = append(points, fmt.Sprintf("%-40s %s", row[0], row[1]))
	}
	points = append(points, titleStyle.Render(fmt.Sprintf("%-40s %d", "Points", d.Points)))
	place("Story Point Criteria", points)

	deploy := strings.Split(hang("Platform: ", d.Deployment.Platform, inner), "\n")
	for _, part := range []struct {
		name  string
		items []string
	}{{"Evidence", d.Deployment.Evidence}, {"Steps", d.Deployment.Steps}} {
		deploy = append(deploy, titleStyle.Render(part.name))
		deploy = append(deploy, bullets("• ", part.items)...)
	}
	deploy = append(deploy, titleStyle.Render("Blockers"))
	if len(d.Deployment.Blockers) == 0 {
		deploy = append(deploy, pendingStyle.Render("none"))
	}
	for _, blocker := range d.Deployment.Blockers {
		deploy = question(deploy, blocker)
	}
	place("Deployment", deploy)

	if len(d.Split) > 0 {
		var split []string
		for i, child := range d.Split {
			text := fmt.Sprintf("%s · %d %s", child.Title, child.Points, pts(child.Points))
			if key := saved.Children[i]; key != "" {
				text += " · " + key
			}
			line := strings.Split(hang("↗ ", text, inner-1), "\n")
			if stop == focus {
				line[0] = focusStyle.Render(line[0])
			}
			split = mark(split, line)
		}
		place(fmt.Sprintf("Split · %d tickets", len(d.Split)), split)
	}

	if len(d.Relations) > 0 {
		var related []string
		for _, relation := range d.Relations {
			related = append(related, keyStyle.Render(relation.Label()+" "+relation.Key))
			related = append(related, strings.Split(hang("  ", relation.Why, inner), "\n")...)
		}
		place(fmt.Sprintf("Related tickets · %d", len(d.Relations)), related)
	}

	var questions []string
	if len(d.OpenQuestions) == 0 {
		questions = append(questions, pendingStyle.Render("none"))
	}
	for _, text := range d.OpenQuestions {
		questions = question(questions, text)
	}
	total := len(d.Questions())
	place(fmt.Sprintf("Open questions · %d/%d answered", total-saved.Unanswered(), total), questions)

	return strings.Join(all, "\n"), spans
}

// renderChild lays out one split ticket, and its key once created, for the side pane.
func renderChild(child draft.Child, key string, width int) string {
	inner := max(width-4, 10)
	var lines []string
	head := fmt.Sprintf("%d %s", child.Points, pts(child.Points))
	if key != "" {
		head += " · created as " + key
	}
	lines = append(lines, titleStyle.Render(head), "")
	lines = append(lines, titleStyle.Render("Acceptance Criteria"))
	if len(child.AcceptanceCriteria) == 0 {
		lines = append(lines, pendingStyle.Render("none"))
	}
	for _, story := range child.AcceptanceCriteria {
		lines = append(lines, strings.Split(hang("* ", story, inner), "\n")...)
	}
	lines = append(lines, "", titleStyle.Render("Technical Implementation"))
	lines = append(lines, repoLines(child.TechnicalImplementation, inner)...)
	if child.StoryPointCriteria.Rated() {
		lines = append(lines, "", titleStyle.Render("Story Point Criteria"))
		for _, row := range child.StoryPointCriteria.Rows() {
			lines = append(lines, truncate(fmt.Sprintf("%-40s %s", row[0], row[1]), inner))
		}
	}
	return strings.Join(box(child.Title, lines, width), "\n")
}

func repoLines(repos []draft.RepoFiles, width int) []string {
	if len(repos) == 0 {
		return []string{pendingStyle.Render("none")}
	}
	var lines []string
	for _, repo := range repos {
		lines = append(lines, keyStyle.Render(repo.Repo))
		for _, file := range repo.Files {
			path := file.Path
			if file.New {
				path += " (new)"
			}
			lines = append(lines, strings.Split(hang("  ", path, width), "\n")...)
		}
	}
	return lines
}

// box draws lines in a rounded border with the title set into its top edge.
func box(title string, lines []string, width int) []string {
	width = max(width, 12)
	title = truncate(title, width-6)
	top := borderStyle.Render("╭─ ") + titleStyle.Render(title) + " " + borderStyle.Render(strings.Repeat("─", max(width-5-lipgloss.Width(title), 0))+"╮")
	body := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderTop(false).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Width(width - 2).
		Render(strings.Join(lines, "\n"))
	return append([]string{top}, strings.Split(body, "\n")...)
}

// tokens shortens a token count, e.g. 781K.
func tokens(count int) string {
	if count < 1000 {
		return fmt.Sprint(count)
	}
	return fmt.Sprintf("%dK", (count+500)/1000)
}

func pts(points int) string {
	if points == 1 {
		return "pt"
	}
	return "pts"
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
