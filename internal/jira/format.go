package jira

import (
	"regexp"
	"strings"
)

// Draft is a ticket's refinement before it is laid out for Jira's fields.
type Draft struct {
	AcceptanceCriteria []string
	Repos              []RepoFiles
	Decisions          []string
	// Ratings pair each Story Point Criteria label with its rating, and
	// Total is the total line, "" when the ratings cannot be scored.
	Ratings [][2]string
	Total   string
	Points  int
}

// RepoFiles is one repo's files in the Technical Implementation.
type RepoFiles struct {
	Repo  string
	Files []File
}

// File is a path, marked when the ticket adds it.
type File struct {
	Path string
	New  bool
}

// Fields is a ticket's refinement as Jira's fields take it. The fields
// render plain text, so nothing here is markdown.
func Fields(d Draft) Refinement {
	var ac strings.Builder
	for _, item := range d.AcceptanceCriteria {
		ac.WriteString("* " + item + "\n")
	}
	var ti strings.Builder
	for i, repo := range d.Repos {
		if i > 0 {
			ti.WriteString("\n")
		}
		ti.WriteString(repo.Repo + "\n\n")
		for _, file := range repo.Files {
			line := "* " + file.Path
			if file.New {
				line += " (new)"
			}
			ti.WriteString(line + "\n")
		}
	}
	if len(d.Decisions) > 0 {
		ti.WriteString("\n---\n\n")
		for _, decision := range d.Decisions {
			ti.WriteString("* " + decision + "\n")
		}
	}
	var spc []string
	if d.Total != "" {
		for _, row := range d.Ratings {
			spc = append(spc, row[0]+": "+row[1])
		}
		spc = append(spc, "Total: "+d.Total)
	}
	return Refinement{
		AcceptanceCriteria:      strings.TrimSpace(ac.String()),
		TechnicalImplementation: strings.TrimSpace(ti.String()),
		StoryPointCriteria:      strings.Join(spc, "\n"),
		Points:                  d.Points,
	}
}

// DeployComment is the deploy needs as one Jira comment in wiki markup.
func DeployComment(needs []string) string {
	var out strings.Builder
	out.WriteString("Deploy needs found while refining this ticket.\n\n")
	for _, need := range needs {
		out.WriteString("* " + wiki(need) + "\n")
	}
	return out.String()
}

var backticked = regexp.MustCompile("`([^`]+)`")

// wiki puts one line of text in Jira wiki markup: `code` becomes {{code}},
// and line breaks become spaces so a list item stays one item.
func wiki(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	return backticked.ReplaceAllString(text, "{{$1}}")
}

// BlockedBy reports whether the other ticket blocks this one.
func (l Link) BlockedBy() bool {
	return l.Type == "Blocks" && l.Relation == "is blocked by"
}

// Blocks reports whether this ticket blocks the other one.
func (l Link) Blocks() bool {
	return l.Type == "Blocks" && l.Relation == "blocks"
}
