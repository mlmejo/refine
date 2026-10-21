package home

import (
	"regexp"

	"github.com/charmbracelet/lipgloss"
)

// codeStyle marks `code` in text Claude wrote.
var codeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

// inlinePattern matches the inline markdown Claude writes: `code` and
// **bold**. An unmatched backtick or star stays as it is.
var inlinePattern = regexp.MustCompile("`([^`\n]+)`|\\*\\*([^*\n]+)\\*\\*")

// inline renders text's inline markdown as styles, dropping the markers.
func inline(text string) string {
	return inlinePattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := inlinePattern.FindStringSubmatch(match)
		if parts[1] != "" {
			return codeStyle.Render(parts[1])
		}
		return titleStyle.Render(parts[2])
	})
}
