// Package draft asks headless Claude Code to refine a ticket and keeps the result.
package draft

import (
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/outline"
)

//go:embed rules.md
var rules string

//go:embed schema.json
var schema string

// Draft is Claude's refinement, shaped by schema.json.
type Draft struct {
	AcceptanceCriteria      []string           `json:"acceptance_criteria"`
	TechnicalImplementation []RepoFiles        `json:"technical_implementation"`
	StoryPointCriteria      StoryPointCriteria `json:"story_point_criteria"`
	Points                  int                `json:"points"`
	Deployment              Deployment         `json:"deployment"`
	Split                   []Child            `json:"split"`
	Relations               []Relation         `json:"relations"`
	ApproachReview          ApproachReview     `json:"approach_review"`
	OpenQuestions           []string           `json:"open_questions"`
}

// RepoFiles is one repo's files in the Technical Implementation.
type RepoFiles struct {
	Repo  string `json:"repo"`
	Files []File `json:"files"`
}

// File is a path, marked when the ticket adds it.
type File struct {
	Path string `json:"path"`
	New  bool   `json:"new"`
}

// StoryPointCriteria rates each dimension Low, Medium or High.
type StoryPointCriteria struct {
	Novelty    string `json:"novelty"`
	Risk       string `json:"risk"`
	Dependency string `json:"dependency"`
	Testing    string `json:"testing"`
	Deployment string `json:"deployment"`
	Total      string `json:"total"`
}

// Deployment is how the ticket reaches production, and what must be settled first.
type Deployment struct {
	Platform string   `json:"platform"`
	Evidence []string `json:"evidence"`
	Steps    []string `json:"steps"`
	Blockers []string `json:"blockers"`
}

// Child is a proposed split ticket: one behaviour on top of the parent's foundation.
type Child struct {
	Title                   string      `json:"title"`
	Points                  int         `json:"points"`
	AcceptanceCriteria      []string    `json:"acceptance_criteria"`
	TechnicalImplementation []RepoFiles `json:"technical_implementation"`
	// StoryPointCriteria is empty on drafts made before children were rated.
	StoryPointCriteria StoryPointCriteria `json:"story_point_criteria"`
}

// Relation is how this ticket stands to another of the developer's tickets.
type Relation struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	Why  string `json:"why"`
}

// Label reads the relation's kind as a phrase before the other ticket's key.
func (r Relation) Label() string {
	switch r.Kind {
	case "needs":
		return "needs"
	case "needed_by":
		return "needed by"
	case "duplicates":
		return "duplicates"
	case "conflicts":
		return "conflicts with"
	case "overlaps":
		return "overlaps"
	}
	return r.Kind
}

// OthersPrompt lists the developer's other tickets, so the draft can say how
// it relates to them. A ticket without Acceptance Criteria shows its description.
func OthersPrompt(self string, others []jira.Ticket) string {
	var out strings.Builder
	for _, ticket := range others {
		if ticket.Key == self {
			continue
		}
		if out.Len() == 0 {
			out.WriteString("\nThe developer's other tickets in refinement and ready for sprint. Say in relations how this ticket stands to them:\n")
		}
		fmt.Fprintf(&out, "\n%s (%s): %s\n", ticket.Key, ticket.Status, ticket.Summary)
		if ticket.AcceptanceCriteria == "" {
			section(&out, "Description", clip(ticket.Description, maxOtherDescription))
		} else {
			section(&out, "Acceptance Criteria", ticket.AcceptanceCriteria)
		}
		if ticket.TechnicalImplementation != "" {
			section(&out, "Technical Implementation", ticket.TechnicalImplementation)
		}
	}
	return out.String()
}

// maxOtherDescription caps each unrefined ticket's description, so the list
// stays a small part of the prompt.
const maxOtherDescription = 1500

func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return strings.TrimSpace(text[:limit]) + "…"
}

// ApproachReview is Claude's check of the developer's approach.
type ApproachReview struct {
	Holds []string `json:"holds"`
	// Unsure answers what the developer marked as unsure, from the code.
	Unsure       []string `json:"unsure"`
	Misses       []string `json:"misses"`
	Alternatives []string `json:"alternatives"`
}

// Prompt describes the ticket, the repos (checked out at ref, when set) with
// their outlines, and the developer's approach.
func Prompt(ticket jira.Ticket, repos map[string]string, outlines map[string]outline.Outline, ref, approach string) string {
	return DraftPrompt(Context(ticket, repos, outlines, ref), approach)
}

// DraftPrompt adds the developer's approach to the ticket's context.
func DraftPrompt(context, approach string) string {
	return context + approachSection(approach)
}

// Context describes the ticket and the repos (checked out at ref, when set)
// with their outlines.
func Context(ticket jira.Ticket, repos map[string]string, outlines map[string]outline.Outline, ref string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Ticket %s: %s\n", ticket.Key, ticket.Summary)
	points := "none"
	if ticket.Points != nil {
		points = strconv.FormatFloat(*ticket.Points, 'f', -1, 64)
	}
	fmt.Fprintf(&out, "Status: %s. Story points: %s.\n", ticket.Status, points)

	section(&out, "Description", ticket.Description)
	section(&out, "Acceptance Criteria", ticket.AcceptanceCriteria)
	section(&out, "Technical Implementation", ticket.TechnicalImplementation)
	section(&out, "Story Point Criteria", ticket.StoryPointCriteria)

	if len(ticket.Comments) > 0 {
		out.WriteString("\nComments:\n")
		for _, comment := range ticket.Comments {
			fmt.Fprintf(&out, "- %s: %q\n", comment.Author, comment.Body)
		}
	}

	if len(repos) > 0 {
		if ref == "" {
			out.WriteString("\nRepos you can read:\n")
		} else {
			fmt.Fprintf(&out, "\nRepos you can read, checked out at their %s branch:\n", ref)
		}
		for _, name := range repoNames(repos) {
			fmt.Fprintf(&out, "- %s: %s\n", name, repos[name])
		}
	}
	if len(outlines) > 0 {
		out.WriteString("\nRepo outlines, one line per directory with its file count. Use them instead of listing directories:\n")
		for _, name := range repoNames(repos) {
			if layout, ok := outlines[name]; ok {
				fmt.Fprintf(&out, "\n%s at %s:\n%s", name, short(layout.Commit), layout.Text)
			}
		}
	}

	return out.String()
}

func approachSection(approach string) string {
	approach = strings.TrimSpace(approach)
	if approach == "" {
		return "\nThe developer does not know yet how to approach it. Say what they need to find out.\n"
	}
	return fmt.Sprintf("\nDeveloper's approach, written before seeing yours. It sets the scope:\n%q\n", approach)
}

// Request is what a draft run starts from.
type Request struct {
	Approach string
	// Previous is the draft being redone; its answers go to Claude.
	Previous *Saved
	// Resume continues Previous's session instead of reading the repos from scratch.
	Resume bool
	// Brief, when set, is the ticket's brief; a fresh draft continues its
	// session so the code it read is not read again.
	Brief *SavedBrief
}

// AnswersPrompt gives Claude the team's answers to the previous draft's questions.
func AnswersPrompt(previous Saved) string {
	var out strings.Builder
	for _, question := range previous.Draft.Questions() {
		answer := strings.TrimSpace(previous.Answers[question])
		if answer == "" {
			continue
		}
		if out.Len() == 0 {
			out.WriteString("\nThe team answered these questions from the previous draft. Use the answers, and do not ask a settled question again:\n")
		}
		fmt.Fprintf(&out, "- Q: %s\n  A: %q\n", question, answer)
	}
	return out.String()
}

// RevisePrompt asks a resumed session to redo its draft with the answers.
func RevisePrompt(previous Saved) string {
	return "Revise your draft with the team's answers. Reread code where an answer changes what you need to know, and recheck every earlier claim an answer touches. An answer can raise new decisions; ask them." + AnswersPrompt(previous)
}

func short(commit string) string {
	return commit[:min(len(commit), 12)]
}

func section(out *strings.Builder, name, text string) {
	if text == "" {
		fmt.Fprintf(out, "\n%s: empty.\n", name)
		return
	}
	fmt.Fprintf(out, "\n%s:\n%s\n", name, text)
}

func repoNames(repos map[string]string) []string {
	names := make([]string, 0, len(repos))
	for name := range repos {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
