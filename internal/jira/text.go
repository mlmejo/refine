package jira

import (
	"encoding/json"
	"strconv"
	"strings"
)

// adfNode is one node of an Atlassian Document Format tree.
type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Content []adfNode      `json:"content"`
	Attrs   map[string]any `json:"attrs"`
	Marks   []adfMark      `json:"marks"`
}

// adfMark is inline formatting on a text node: strong, em, code, link, ...
type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

// fieldText reads a text field that Jira returns as null, a string, or ADF.
func fieldText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}

	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}

	var doc adfNode
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	var out strings.Builder
	writeBlocks(&out, doc.Content, "")
	return strings.TrimSpace(out.String())
}

func writeBlocks(out *strings.Builder, nodes []adfNode, indent string) {
	for _, node := range nodes {
		switch node.Type {
		case "bulletList", "orderedList":
			for i, item := range node.Content {
				marker := "* "
				if node.Type == "orderedList" {
					marker = strconv.Itoa(i+1) + ". "
				}
				out.WriteString(indent + marker)
				writeItem(out, item.Content, indent+"  ")
			}
		default:
			out.WriteString(indent)
			writeInline(out, node.Content)
			out.WriteString("\n")
		}
	}
}

// writeItem writes a list item: its first paragraph on the marker line, the rest indented.
func writeItem(out *strings.Builder, nodes []adfNode, indent string) {
	if len(nodes) == 0 {
		out.WriteString("\n")
		return
	}
	if nodes[0].Type == "paragraph" {
		writeInline(out, nodes[0].Content)
		out.WriteString("\n")
		nodes = nodes[1:]
	} else {
		out.WriteString("\n")
	}
	writeBlocks(out, nodes, indent)
}

func writeInline(out *strings.Builder, nodes []adfNode) {
	for _, node := range nodes {
		switch node.Type {
		case "text":
			out.WriteString(node.Text)
		case "hardBreak":
			out.WriteString("\n")
		default:
			writeInline(out, node.Content)
		}
	}
}

// fieldMarkdown reads a text field like fieldText, keeping the formatting as
// markdown: headings, rules, lists, quotes, code, bold, italics and links.
// It is for showing a field as Jira renders it; prompts keep fieldText.
func fieldMarkdown(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var doc adfNode
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	var out strings.Builder
	mdBlocks(&out, doc.Content, "")
	return strings.TrimSpace(out.String())
}

// mdBlocks writes block nodes, each followed by a blank line; prefix starts
// every line, for list nesting and quotes.
func mdBlocks(out *strings.Builder, nodes []adfNode, prefix string) {
	for _, node := range nodes {
		switch node.Type {
		case "heading":
			level := 2
			if l, ok := node.Attrs["level"].(float64); ok && l >= 1 && l <= 6 {
				level = int(l)
			}
			out.WriteString(prefix + strings.Repeat("#", level) + " " + mdInline(node.Content) + "\n\n")
		case "rule":
			out.WriteString(prefix + "---\n\n")
		case "bulletList", "orderedList":
			for i, item := range node.Content {
				marker := "- "
				if node.Type == "orderedList" {
					marker = strconv.Itoa(i+1) + ". "
				}
				var inner strings.Builder
				mdBlocks(&inner, item.Content, "")
				lines := strings.Split(strings.TrimSpace(inner.String()), "\n")
				for j, line := range lines {
					if j == 0 {
						out.WriteString(prefix + marker + line + "\n")
					} else if line != "" {
						out.WriteString(prefix + strings.Repeat(" ", len(marker)) + line + "\n")
					}
				}
			}
			out.WriteString("\n")
		case "blockquote", "panel":
			var inner strings.Builder
			mdBlocks(&inner, node.Content, "")
			for _, line := range strings.Split(strings.TrimSpace(inner.String()), "\n") {
				out.WriteString(strings.TrimRight(prefix+"> "+line, " ") + "\n")
			}
			out.WriteString("\n")
		case "codeBlock":
			var code strings.Builder
			for _, c := range node.Content {
				code.WriteString(c.Text)
			}
			out.WriteString(prefix + "```\n" + code.String() + "\n" + prefix + "```\n\n")
		case "paragraph":
			if text := mdInline(node.Content); strings.TrimSpace(text) != "" {
				out.WriteString(prefix + text + "\n\n")
			}
		default:
			if len(node.Content) > 0 {
				mdBlocks(out, node.Content, prefix)
			}
		}
	}
}

// mdInline writes inline nodes with their marks as markdown.
func mdInline(nodes []adfNode) string {
	var out strings.Builder
	for _, node := range nodes {
		switch node.Type {
		case "text":
			out.WriteString(mdMarked(node.Text, node.Marks))
		case "hardBreak":
			out.WriteString("  \n")
		case "mention", "emoji", "date", "status":
			if text, ok := node.Attrs["text"].(string); ok {
				out.WriteString(text)
			}
		case "inlineCard":
			if url, ok := node.Attrs["url"].(string); ok {
				out.WriteString(url)
			}
		default:
			out.WriteString(mdInline(node.Content))
		}
	}
	return out.String()
}

// mdMarked wraps text in its marks. Surrounding spaces stay outside the
// markers, so "**Link:** " does not become "**Link: **".
func mdMarked(text string, marks []adfMark) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	lead := text[:len(text)-len(strings.TrimLeft(text, " "))]
	trail := text[len(strings.TrimRight(text, " ")):]
	core := strings.TrimSpace(text)
	var href string
	for _, m := range marks {
		switch m.Type {
		case "code":
			core = "`" + core + "`"
		case "strong":
			core = "**" + core + "**"
		case "em":
			core = "*" + core + "*"
		case "strike":
			core = "~~" + core + "~~"
		case "link":
			href, _ = m.Attrs["href"].(string)
		}
	}
	if href != "" && href != strings.Trim(core, "*`~") {
		core = "[" + core + "](" + href + ")"
	}
	return lead + core + trail
}
