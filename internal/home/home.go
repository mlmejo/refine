// Package home is the ticket list and the read-only ticket view.
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
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/jira"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	headingStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	keyStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	quietStyle    = lipgloss.NewStyle().Faint(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	doneStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

// Page is what the home screen shows: who is signed in and their tickets.
type Page struct {
	User   string
	Issues []jira.Issue
}

// Source is where the home screen gets its tickets.
type Source struct {
	List    func(context.Context) (Page, error)
	Get     func(ctx context.Context, key string) (jira.Issue, error)
	Project string

	// Hidden are keys removed from the list earlier; SaveHidden keeps the new set.
	Hidden     []string
	SaveHidden func([]string) error
}

// removal is a ticket taken off the list, kept so u can put it back.
type removal struct {
	issue  jira.Issue
	index  int
	pinned bool
}

type screen int

const (
	loading screen = iota
	list
	detail
	approach
	drafting
	drafted
	briefing
	briefed
)

type loadedMsg struct {
	page Page
	err  error
}

type movedMsg struct {
	key string
	err error
}

type openedMsg struct {
	key   string
	issue jira.Issue
	err   error
}

// Model is the Bubble Tea model for the home screen.
type Model struct {
	source Source
	title  string
	user   string
	screen screen
	issues []jira.Issue
	err    error
	cursor int
	top    int
	scroll int
	width  int
	height int

	refiner   Refiner
	saved     map[string]draft.Saved
	briefs    map[string]draft.SavedBrief
	briefErr  error
	input     textarea.Model
	draftErr  error
	spin      spinner.Model
	started   time.Time
	runID     int
	cancel    context.CancelFunc
	pane      viewport.Model
	resuming  bool
	fromBrief bool

	// The draft screen: cursor stop, open split ticket (-1 for none), answer input.
	focus     int
	side      int
	spans     []span
	sidePane  viewport.Model
	answering bool
	answerIn  textinput.Model
	answerErr error

	// Writing the draft to Jira: asking y/n, waiting, and how it went.
	confirming bool
	writing    bool
	writeErr   error

	// Moving a refined ticket from the home screen: the key asked about, and
	// whether the move is under way.
	moveAsk string
	moving  bool

	// done is what the last write or move did, shown on the home screen until
	// the next key; failed is set when it went wrong.
	done   string
	failed error

	asking  bool
	keyIn   textinput.Model
	opening string
	openErr error
	pinned  []jira.Issue

	exported  string
	exportErr error

	hidden  map[string]bool
	removed []removal
	notice  error
}

// New returns a home screen titled with the listed sprint.
func New(title string, source Source, refiner Refiner) Model {
	keyIn := textinput.New()
	keyIn.Prompt = "Ticket: "
	keyIn.Placeholder = source.Project + "-123, 123 or a link"
	keyIn.CharLimit = 200

	hidden := map[string]bool{}
	for _, key := range source.Hidden {
		hidden[key] = true
	}

	return Model{
		source:   source,
		hidden:   hidden,
		keyIn:    keyIn,
		title:    title,
		width:    80,
		height:   24,
		refiner:  refiner,
		saved:    map[string]draft.Saved{},
		briefs:   map[string]draft.SavedBrief{},
		input:    newInput(),
		side:     -1,
		answerIn: newAnswerInput(),
		spin:     spinner.New(spinner.WithSpinner(spinner.Dot)),
	}
}

// Init starts the first load.
func (m Model) Init() tea.Cmd {
	return m.fetch()
}

func (m Model) fetch() tea.Cmd {
	list := m.source.List
	return func() tea.Msg {
		page, err := list(context.Background())
		return loadedMsg{page: page, err: err}
	}
}

// withPinned drops hidden tickets and puts tickets opened by key first,
// unless the list already has them.
func (m Model) withPinned(issues []jira.Issue) []jira.Issue {
	var merged []jira.Issue
	for _, pinned := range m.pinned {
		if indexOf(issues, pinned.Key) < 0 {
			merged = append(merged, pinned)
		}
	}
	for _, issue := range issues {
		if !m.hidden[issue.Key] {
			merged = append(merged, issue)
		}
	}
	return merged
}

// remove takes the selected ticket off the list: unpins it, or hides it for good.
func (m Model) remove() Model {
	if len(m.issues) == 0 {
		return m
	}
	issue := m.issues[m.cursor]
	gone := removal{issue: issue, index: m.cursor, pinned: m.isPinned(issue.Key)}
	if gone.pinned {
		m.pinned = slices.DeleteFunc(slices.Clone(m.pinned), func(p jira.Issue) bool { return p.Key == issue.Key })
	} else {
		m.setHidden(issue.Key, true)
	}
	m.issues = slices.Delete(slices.Clone(m.issues), m.cursor, m.cursor+1)
	m.removed = append(m.removed, gone)
	m.clampList()
	return m
}

// undo puts the last removed ticket back where it was.
func (m Model) undo() Model {
	if len(m.removed) == 0 {
		return m
	}
	gone := m.removed[len(m.removed)-1]
	m.removed = m.removed[:len(m.removed)-1]
	if gone.pinned {
		m.pinned = append(slices.Clone(m.pinned), gone.issue)
	} else {
		m.setHidden(gone.issue.Key, false)
	}
	if indexOf(m.issues, gone.issue.Key) < 0 {
		at := min(gone.index, len(m.issues))
		m.issues = slices.Insert(slices.Clone(m.issues), at, gone.issue)
		m.cursor = at
	}
	m.clampList()
	return m
}

// setHidden updates the hidden set and saves it.
func (m *Model) setHidden(key string, hide bool) {
	if m.hidden[key] == hide {
		return
	}
	hidden := maps.Clone(m.hidden)
	if hide {
		hidden[key] = true
	} else {
		delete(hidden, key)
	}
	m.hidden, m.notice = hidden, nil
	if m.source.SaveHidden != nil {
		if err := m.source.SaveHidden(slices.Sorted(maps.Keys(hidden))); err != nil {
			m.notice = fmt.Errorf("could not save hidden tickets: %w", err)
		}
	}
}

func indexOf(issues []jira.Issue, key string) int {
	for i, issue := range issues {
		if issue.Key == key {
			return i
		}
	}
	return -1
}

func (m Model) isPinned(key string) bool {
	return indexOf(m.pinned, key) >= 0
}

// Update handles keys, resizes and load results.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampList()
		m.resizeRefine()
		return m, nil

	case loadedMsg:
		m.issues, m.err = m.withPinned(msg.page.Issues), msg.err
		if msg.page.User != "" {
			m.user = msg.page.User
		}
		m.screen = list
		m.clampList()
		return m, nil

	case openedMsg:
		return m.opened(msg)

	case movedMsg:
		return m.moved(msg), nil

	case draftedMsg, briefedMsg, elapsedMsg, exportedMsg, answerSavedMsg, writtenMsg, spinner.TickMsg:
		return m.updateRefine(msg)

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		switch m.screen {
		case detail:
			return m.updateDetail(msg)
		case approach, drafting, drafted, briefing, briefed:
			return m.updateRefine(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.asking {
		return m.updateAsk(msg)
	}
	if m.moveAsk != "" {
		return m.updateMoveAsk(msg)
	}
	m.done, m.failed = "", nil
	switch msg.String() {
	case "m":
		return m.askMove(), nil
	case "x", "delete":
		return m.remove(), nil
	case "u":
		return m.undo(), nil
	case "o":
		if m.source.Get != nil {
			m.asking, m.openErr = true, nil
			m.keyIn.Reset()
			return m, m.keyIn.Focus()
		}
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.cursor--
	case "down", "j":
		m.cursor++
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(m.issues) - 1
	case "r":
		if m.screen != loading {
			m.screen, m.err = loading, nil
			return m, m.fetch()
		}
	case "enter":
		if m.screen == list && len(m.issues) > 0 {
			m.screen, m.scroll = detail, 0
			m.loadSaved(m.issues[m.cursor].Key)
		}
	}
	m.clampList()
	return m, nil
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxScroll := max(0, len(m.detailLines())-m.bodyHeight())
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace", "h", "left":
		m.screen = list
	case "a":
		return m.openApproach()
	case "b":
		if _, ok := m.briefs[m.issues[m.cursor].Key]; ok {
			return m.openBrief(), nil
		}
		return m.startBrief()
	case "d":
		if _, ok := m.saved[m.issues[m.cursor].Key]; ok {
			return m.openDraft(), nil
		}
	case "up", "k":
		m.scroll--
	case "down", "j":
		m.scroll++
	case "pgup":
		m.scroll -= m.bodyHeight()
	case "pgdown", " ", "f":
		m.scroll += m.bodyHeight()
	case "g", "home":
		m.scroll = 0
	case "G", "end":
		m.scroll = maxScroll
	}
	m.scroll = min(max(m.scroll, 0), maxScroll)
	return m, nil
}

// clampList keeps the cursor on a ticket and in view.
func (m *Model) clampList() {
	m.cursor = min(max(m.cursor, 0), max(len(m.issues)-1, 0))
	rows := m.bodyHeight()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+rows {
		m.top = m.cursor - rows + 1
	}
	m.top = min(max(m.top, 0), max(len(m.issues)-rows, 0))
}

// bodyHeight is the rows between the header and the footer.
func (m Model) bodyHeight() int {
	return max(m.height-4, 1)
}

// View renders the current screen.
func (m Model) View() string {
	switch m.screen {
	case detail:
		return m.viewDetail()
	case approach:
		return m.viewApproach()
	case briefing:
		return m.viewBriefing()
	case briefed:
		return m.viewBriefed()
	case drafting:
		return m.viewDrafting()
	case drafted:
		return m.viewDrafted()
	}
	return m.viewList()
}

func (m Model) viewList() string {
	var out strings.Builder
	header := "refine · "
	if m.user != "" {
		header += m.user + " · "
	}
	out.WriteString(titleStyle.Render(truncate(header+m.title, m.width)) + "\n\n")

	switch {
	case m.screen == loading:
		out.WriteString(quietStyle.Render("Loading tickets…") + "\n")
	case m.err != nil:
		out.WriteString(lipgloss.NewStyle().Width(m.width).Render(errorStyle.Render(m.err.Error())) + "\n")
	case len(m.issues) == 0:
		out.WriteString(quietStyle.Render("No tickets waiting for refinement.") + "\n")
	default:
		end := min(m.top+m.bodyHeight(), len(m.issues))
		keyWidth := 0
		for _, issue := range m.issues {
			keyWidth = max(keyWidth, len(issue.Key))
		}
		for i := m.top; i < end; i++ {
			out.WriteString(m.row(m.issues[i], keyWidth, i == m.cursor) + "\n")
		}
	}

	return m.withFooter(out.String(), m.listFooter())
}

func (m Model) listFooter() string {
	if !m.asking {
		if m.notice != nil {
			return errorStyle.Render(truncate(m.notice.Error(), m.width))
		}
		switch {
		case m.moveAsk != "":
			return truncate(fmt.Sprintf("move %s to %s? y/n", m.moveAsk, m.refiner.ReadySprint), m.width)
		case m.moving:
			return truncate("moving to "+m.refiner.ReadySprint+"…", m.width)
		case m.failed != nil:
			return errorStyle.Render(truncate(m.failed.Error(), m.width))
		case m.done != "":
			return truncate(m.done+" · enter open · r refresh · q quit", m.width)
		}
		ready := ""
		if m.refiner.Move != nil {
			ready = " · m ready for sprint"
		}
		help := "↑/↓ move · enter open · o open by key" + ready + " · x remove · r refresh · q quit"
		if len(m.removed) > 0 {
			help = "↑/↓ move · enter open · o open by key" + ready + " · x remove · u undo · r refresh · q quit"
		}
		return truncate(help, m.width)
	}
	status := "enter open · esc cancel"
	switch {
	case m.opening != "":
		status = "opening " + m.opening + "…"
	case m.openErr != nil:
		status = errorStyle.Render(m.openErr.Error())
	}
	m.keyIn.Width = max(min(m.width/3, 30), 10)
	return m.keyIn.View() + "  " + truncate(status, max(m.width-lipgloss.Width(m.keyIn.View())-2, 1))
}

// refined reports whether the ticket has both AC and TI, the ones the home
// screen ticks.
func refined(issue jira.Issue) bool {
	return issue.AcceptanceCriteria != "" && issue.TechnicalImplementation != ""
}

// askMove asks to move the selected ticket to the ready sprint, once it is refined.
func (m Model) askMove() Model {
	if m.refiner.Move == nil || m.moving || m.screen != list || len(m.issues) == 0 {
		return m
	}
	issue := m.issues[m.cursor]
	if !refined(issue) {
		m.failed = fmt.Errorf("%s is not refined yet: fill its AC and TI first", issue.Key)
		return m
	}
	m.moveAsk = issue.Key
	return m
}

// updateMoveAsk moves the ticket on y and cancels on any other key.
func (m Model) updateMoveAsk(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := m.moveAsk
	m.moveAsk = ""
	if msg.String() != "y" {
		return m, nil
	}
	m.moving = true
	move := m.refiner.Move
	return m, func() tea.Msg {
		return movedMsg{key: key, err: move(context.Background(), key)}
	}
}

// moved takes a ticket that left the refinement sprint off the list.
func (m Model) moved(msg movedMsg) Model {
	m.moving = false
	if msg.err != nil {
		m.failed = fmt.Errorf("could not move %s to %s: %w", msg.key, m.refiner.ReadySprint, msg.err)
		return m
	}
	m.dropIssue(msg.key)
	m.done = "moved " + msg.key + " to " + m.refiner.ReadySprint
	return m
}

// dropIssue takes a ticket off the list without hiding it.
func (m *Model) dropIssue(key string) {
	m.issues = slices.DeleteFunc(slices.Clone(m.issues), func(i jira.Issue) bool { return i.Key == key })
	m.pinned = slices.DeleteFunc(slices.Clone(m.pinned), func(i jira.Issue) bool { return i.Key == key })
	m.clampList()
}

func (m Model) updateAsk(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.asking, m.opening, m.openErr = false, "", nil
		m.keyIn.Blur()
		return m, nil
	case "enter":
		if m.opening != "" {
			return m, nil
		}
		key, ok := jira.ParseKey(m.keyIn.Value(), m.source.Project)
		if !ok {
			m.openErr = fmt.Errorf("not a ticket key")
			return m, nil
		}
		if i := indexOf(m.issues, key); i >= 0 {
			return m.showTicket(i), nil
		}
		m.opening, m.openErr = key, nil
		get := m.source.Get
		return m, func() tea.Msg {
			issue, err := get(context.Background(), key)
			return openedMsg{key: key, issue: issue, err: err}
		}
	}
	var cmd tea.Cmd
	m.keyIn, cmd = m.keyIn.Update(msg)
	m.openErr = nil
	return m, cmd
}

func (m Model) opened(msg openedMsg) (tea.Model, tea.Cmd) {
	if msg.key != m.opening {
		return m, nil
	}
	m.opening = ""
	if msg.err != nil {
		m.openErr = fmt.Errorf("%s: %w", msg.key, msg.err)
		return m, nil
	}
	if i := indexOf(m.issues, msg.issue.Key); i >= 0 {
		return m.showTicket(i), nil
	}
	m.setHidden(msg.issue.Key, false)
	m.pinned = append(m.pinned, msg.issue)
	m.issues = append([]jira.Issue{msg.issue}, m.issues...)
	m.err = nil
	return m.showTicket(0), nil
}

// showTicket closes the key prompt and opens the ticket view at index i.
func (m Model) showTicket(i int) Model {
	m.asking, m.openErr = false, nil
	m.keyIn.Blur()
	m.cursor, m.screen, m.scroll = i, detail, 0
	m.clampList()
	m.loadSaved(m.issues[i].Key)
	return m
}

func (m Model) row(issue jira.Issue, keyWidth int, selected bool) string {
	key := fmt.Sprintf("%-*s", keyWidth, issue.Key)
	tag := ""
	if m.isPinned(issue.Key) {
		tag = " · opened"
	}
	summary := truncate(issue.Summary, m.width-keyWidth-4-filledWidth-len(tag))
	if selected {
		return selectedStyle.Render("> " + key + "  " + filled(issue, false) + summary + tag)
	}
	return "  " + keyStyle.Render(key) + "  " + filled(issue, true) + summary + quietStyle.Render(tag)
}

// filledWidth is the width filled takes, its trailing gap included:
// "AC ✓ TI ✓  ".
const filledWidth = 11

// filled marks which of AC and TI the ticket already has: ✓ when filled, ·
// when empty. styled is false on the selected row, whose reverse style an
// inner style would break.
func filled(issue jira.Issue, styled bool) string {
	var parts []string
	for _, field := range []struct{ name, text string }{
		{"AC", issue.AcceptanceCriteria},
		{"TI", issue.TechnicalImplementation},
	} {
		switch {
		case field.text != "" && styled:
			parts = append(parts, field.name+" "+doneStyle.Render("✓"))
		case field.text != "":
			parts = append(parts, field.name+" ✓")
		case styled:
			parts = append(parts, quietStyle.Render(field.name+" ·"))
		default:
			parts = append(parts, field.name+" ·")
		}
	}
	return strings.Join(parts, " ") + "  "
}

func (m Model) viewDetail() string {
	issue := m.issues[m.cursor]
	header := titleStyle.Render(issue.Key) + "  " + truncate(issue.Summary, m.width-len(issue.Key)-2)

	lines := m.detailLines()
	end := min(m.scroll+m.bodyHeight(), len(lines))
	body := strings.Join(lines[m.scroll:end], "\n")

	brief := "b brief me"
	if _, ok := m.briefs[issue.Key]; ok {
		brief = "b brief"
	}
	help := "↑/↓ scroll · " + brief + " · a approach & draft · esc back · q quit"
	if _, ok := m.saved[issue.Key]; ok {
		help = "↑/↓ scroll · " + brief + " · a redo approach · d saved draft · esc back · q quit"
	}
	return m.withFooter(header+"\n\n"+body+"\n", help)
}

// detailLines is the selected ticket's three fields, wrapped to the width.
func (m Model) detailLines() []string {
	issue := m.issues[m.cursor]
	wrap := lipgloss.NewStyle().Width(max(m.width-2, 20))

	var lines []string
	for i, field := range []struct{ name, text string }{
		{"Acceptance Criteria", issue.AcceptanceCriteria},
		{"Technical Implementation", issue.TechnicalImplementation},
		{"Story Point Criteria", issue.StoryPointCriteria},
	} {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, headingStyle.Render(field.name))
		text := field.text
		if text == "" {
			text = quietStyle.Render("(empty)")
		}
		lines = append(lines, strings.Split(wrap.Render(text), "\n")...)
	}
	return lines
}

func (m Model) withFooter(body, help string) string {
	gap := max(m.height-lipgloss.Height(body)-1, 0)
	return body + strings.Repeat("\n", gap) + quietStyle.Render(help)
}

func truncate(s string, width int) string {
	if width <= 1 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "…"
}
