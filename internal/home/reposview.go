package home

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// folder is a directory the browser lists, and whether it is a git checkout.
type folder struct {
	name string
	git  bool
}

func newNameInput() textinput.Model {
	in := textinput.New()
	in.Prompt = "Name: "
	in.CharLimit = 100
	return in
}

// repoNames are the configured repos' names in order.
func (m Model) repoNames() []string {
	return slices.Sorted(maps.Keys(m.source.Repos()))
}

func (m Model) openRepos() Model {
	if m.source.Repos == nil || m.screen != list {
		return m
	}
	m.screen, m.repoAsk, m.repoErr, m.repoDone = repos, "", nil, ""
	m.repoCursor = min(m.repoCursor, max(len(m.source.Repos())-1, 0))
	return m
}

func (m Model) updateRepos(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	names := m.repoNames()
	if m.repoAsk != "" {
		name := m.repoAsk
		m.repoAsk = ""
		if msg.String() == "y" {
			if err := m.source.RemoveRepo(name); err != nil {
				m.repoErr = err
			} else {
				m.repoDone = "removed " + name
			}
			m.repoCursor = min(m.repoCursor, max(len(names)-2, 0))
		}
		return m, nil
	}
	m.repoErr, m.repoDone = nil, ""
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc", "backspace", "h", "left":
		m.screen = list
	case "up", "k":
		m.repoCursor = max(m.repoCursor-1, 0)
	case "down", "j":
		m.repoCursor = min(m.repoCursor+1, max(len(names)-1, 0))
	case "a":
		return m.openBrowse(m.startDir()), nil
	case "x", "delete":
		if len(names) > 0 {
			m.repoAsk = names[m.repoCursor]
		}
	}
	return m, nil
}

// startDir is where the browser opens: the folder most repos sit in, else home.
func (m Model) startDir() string {
	count := map[string]int{}
	best := ""
	for _, name := range m.repoNames() {
		parent := filepath.Dir(m.source.Repos()[name])
		count[parent]++
		if count[parent] > count[best] {
			best = parent
		}
	}
	if best != "" {
		return best
	}
	home, _ := os.UserHomeDir()
	return home
}

// openBrowse shows dir's folders, keeping the screen as it was when dir
// cannot be read.
func (m Model) openBrowse(dir string) Model {
	entries, err := os.ReadDir(dir)
	if err != nil {
		m.repoErr = err
		return m
	}
	from := filepath.Base(m.dir)
	m.screen, m.dir, m.folders, m.repoErr = browse, dir, nil, nil
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			continue
		}
		_, err := os.Stat(filepath.Join(path, ".git"))
		m.folders = append(m.folders, folder{name: entry.Name(), git: err == nil})
	}
	// Going up lands on the folder just left.
	m.dirCursor, m.dirTop = 0, 0
	for i, f := range m.folders {
		if f.name == from {
			m.dirCursor = i
		}
	}
	m.clampBrowse()
	return m
}

func (m Model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.naming {
		return m.updateName(msg)
	}
	m.repoErr = nil
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		m.screen = repos
	case "up", "k":
		m.dirCursor--
	case "down", "j":
		m.dirCursor++
	case "g", "home":
		m.dirCursor = 0
	case "G", "end":
		m.dirCursor = len(m.folders) - 1
	case "enter", "right", "l":
		if len(m.folders) > 0 {
			return m.openBrowse(filepath.Join(m.dir, m.folders[m.dirCursor].name)), nil
		}
	case "left", "h", "backspace":
		if parent := filepath.Dir(m.dir); parent != m.dir {
			return m.openBrowse(parent), nil
		}
	case "~":
		home, _ := os.UserHomeDir()
		m.dir = ""
		return m.openBrowse(home), nil
	case "a":
		return m.pick()
	}
	m.clampBrowse()
	return m, nil
}

// pick asks for a name for the selected folder, if it is a git checkout not
// listed yet.
func (m Model) pick() (tea.Model, tea.Cmd) {
	if len(m.folders) == 0 {
		return m, nil
	}
	f := m.folders[m.dirCursor]
	path := filepath.Join(m.dir, f.name)
	switch {
	case !f.git:
		m.repoErr = fmt.Errorf("%s is not a git repo", f.name)
		return m, nil
	case m.listedAs(path) != "":
		m.repoErr = fmt.Errorf("%s is already listed as %s", f.name, m.listedAs(path))
		return m, nil
	}
	m.naming = true
	m.nameIn.SetValue(f.name)
	m.nameIn.CursorEnd()
	return m, m.nameIn.Focus()
}

func (m Model) updateName(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.naming, m.repoErr = false, nil
		m.nameIn.Blur()
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.nameIn.Value())
		if name == "" {
			m.repoErr = fmt.Errorf("give the repo a name")
			return m, nil
		}
		path := filepath.Join(m.dir, m.folders[m.dirCursor].name)
		if err := m.source.AddRepo(name, path); err != nil {
			m.repoErr = err
			return m, nil
		}
		m.naming = false
		m.nameIn.Blur()
		m.screen, m.repoDone = repos, "added "+name
		m.repoCursor = max(slices.Index(m.repoNames(), name), 0)
		return m, nil
	}
	var cmd tea.Cmd
	m.nameIn, cmd = m.nameIn.Update(msg)
	m.repoErr = nil
	return m, cmd
}

// listedAs is the name a repo at path is listed under, or "".
func (m Model) listedAs(path string) string {
	for name, dir := range m.source.Repos() {
		if filepath.Clean(dir) == path {
			return name
		}
	}
	return ""
}

// clampBrowse keeps the browser's cursor on a folder and in view.
func (m *Model) clampBrowse() {
	m.dirCursor = min(max(m.dirCursor, 0), max(len(m.folders)-1, 0))
	rows := m.bodyHeight() - 1
	if m.dirCursor < m.dirTop {
		m.dirTop = m.dirCursor
	}
	if m.dirCursor >= m.dirTop+rows {
		m.dirTop = m.dirCursor - rows + 1
	}
	m.dirTop = min(max(m.dirTop, 0), max(len(m.folders)-rows, 0))
}

func (m Model) viewRepos() string {
	var out strings.Builder
	out.WriteString(titleStyle.Render("Repos Claude reads") + "\n\n")
	names := m.repoNames()
	if len(names) == 0 {
		out.WriteString(quietStyle.Render("No repos yet. Press a to add one.") + "\n")
	}
	width := 0
	for _, name := range names {
		width = max(width, lipgloss.Width(name))
	}
	for i, name := range names {
		dir := m.source.Repos()[name]
		tag := ""
		if _, err := os.Stat(dir); err != nil {
			tag = " · missing"
		}
		label := fmt.Sprintf("%-*s", width, name)
		path := truncate(tildePath(dir), m.width-width-4-len(tag))
		if i == m.repoCursor {
			out.WriteString(selectedStyle.Render("> "+label+"  "+path+tag) + "\n")
		} else {
			out.WriteString("  " + keyStyle.Render(label) + "  " + path + errorStyle.Render(tag) + "\n")
		}
	}

	help := "↑/↓ move · a add · x remove · esc back"
	switch {
	case m.repoAsk != "":
		help = fmt.Sprintf("remove %s from the config? y/n", m.repoAsk)
	case m.repoErr != nil:
		help = errorStyle.Render(m.repoErr.Error())
	case m.repoDone != "":
		help = m.repoDone + " · " + help
	}
	return m.withFooter(out.String(), truncate(help, m.width))
}

func (m Model) viewBrowse() string {
	var out strings.Builder
	out.WriteString(titleStyle.Render("Add a repo") + "  " + quietStyle.Render(truncate(tildePath(m.dir), m.width-12)) + "\n\n")
	if len(m.folders) == 0 {
		out.WriteString(quietStyle.Render("No folders here.") + "\n")
	}
	end := min(m.dirTop+m.bodyHeight()-1, len(m.folders))
	for i := m.dirTop; i < end; i++ {
		f := m.folders[i]
		tag := ""
		switch {
		case m.listedAs(filepath.Join(m.dir, f.name)) != "":
			tag = " · added"
		case f.git:
			tag = " · git"
		}
		name := truncate(f.name+"/", m.width-4-len(tag))
		if i == m.dirCursor {
			out.WriteString(selectedStyle.Render("> "+name+tag) + "\n")
		} else {
			out.WriteString("  " + name + doneStyle.Render(tag) + "\n")
		}
	}

	help := "↑/↓ move · → open · ← up · ~ home · a add repo · esc back"
	switch {
	case m.naming:
		status := "enter add · esc cancel"
		if m.repoErr != nil {
			status = errorStyle.Render(m.repoErr.Error())
		}
		m.nameIn.Width = max(min(m.width/3, 30), 10)
		return m.withFooter(out.String(), m.nameIn.View()+"  "+truncate(status, max(m.width-lipgloss.Width(m.nameIn.View())-2, 1)))
	case m.repoErr != nil:
		help = errorStyle.Render(m.repoErr.Error())
	}
	return m.withFooter(out.String(), truncate(help, m.width))
}

// tildePath shortens a path under the home directory to ~/….
func tildePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		if rel == "." {
			return "~"
		}
		return "~/" + rel
	}
	return path
}
