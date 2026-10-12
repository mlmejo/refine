package jira

import (
	"encoding/json"
	"strconv"
	"strings"
)

// adfNode is one node of an Atlassian Document Format tree.
type adfNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Content []adfNode `json:"content"`
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
