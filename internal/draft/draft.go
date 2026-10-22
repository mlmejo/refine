// Package draft asks headless Claude Code to refine a ticket and keeps the result.
package draft

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/outline"
)

//go:embed questions.md
var questionsText string

//go:embed efficiency.md
var efficiency string

var questionRules = withShared(questionsText)

//go:embed questions.schema.json
var questionsSchema string

//go:embed plain.md
var plainText string

// plainRules and plainSchema are the questions step in arm B of the
// plain-names experiment: plain words in the prose, the code in terms.
var (
	plainRules  = questionRules + plainText
	plainSchema = withTerms(questionsSchema)
)

// withTerms adds the required terms list to the questions schema.
func withTerms(schema string) string {
	var s map[string]any
	if err := json.Unmarshal([]byte(schema), &s); err != nil {
		panic(err)
	}
	s["properties"].(map[string]any)["terms"] = map[string]any{
		"type":        "array",
		"description": "Each plain name used in the prose, once, with the code it stands for.",
		"items": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"name", "code", "where"},
			"properties": map[string]any{
				"name":  map[string]any{"type": "string", "description": "The plain name exactly as written in the prose."},
				"code":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "The identifiers it stands for."},
				"where": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Where they live, as path:line."},
			},
		},
	}
	s["required"] = append(s["required"].([]any), "terms")
	out, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(out)
}

//go:embed stamp.md
var stampRules string

//go:embed stamp.schema.json
var stampSchema string

// withShared puts the sections that steps reading code share in place of
// their markers.
func withShared(rules string) string {
	return strings.Replace(rules, "{{working efficiently}}\n", efficiency, 1)
}

// Draft is Claude's refinement. The questions step fills the findings,
// deployment, approach review and open questions (questions.schema.json); the
// stamp step adds the fields written to Jira (stamp.schema.json). Problem,
// Solution, TicketBlockers and Relations come from the ticket's triage.
type Draft struct {
	Problem        string   `json:"problem,omitempty"`
	Solution       string   `json:"solution,omitempty"`
	TicketBlockers []string `json:"ticket_blockers,omitempty"`
	Findings       []string `json:"findings,omitempty"`

	AcceptanceCriteria      []string           `json:"acceptance_criteria"`
	TechnicalImplementation []RepoFiles        `json:"technical_implementation"`
	StoryPointCriteria      StoryPointCriteria `json:"story_point_criteria"`
	Points                  int                `json:"points"`
	Deployment              Deployment         `json:"deployment"`
	Split                   []Child            `json:"split"`
	// Elsewhere is what the stamp left out because another ticket already
	// delivers it.
	Elsewhere      []Elsewhere    `json:"elsewhere,omitempty"`
	Relations      []Relation     `json:"relations"`
	ApproachReview ApproachReview `json:"approach_review"`
	OpenQuestions  []Choice       `json:"open_questions"`
	// Decided is the record of earlier rounds' decisions; refine fills it,
	// Claude does not.
	Decided []Decided `json:"decided,omitempty"`
	// Arm is the ticket's arm in the plain-names experiment, "" when it is
	// not in it; refine fills it. Terms are the plain names Claude used in
	// arm B, with the code they stand for.
	Arm   string     `json:"arm,omitempty"`
	Terms []Glossary `json:"terms,omitempty"`
	// Checked is set once the stamped criteria were checked against the
	// other open tickets; Duplicates is what that check found.
	Checked    bool        `json:"checked,omitempty"`
	Duplicates []Duplicate `json:"duplicates,omitempty"`
}

// Elsewhere is a behaviour left to another ticket that already delivers it.
type Elsewhere struct {
	Key       string `json:"key"`
	Behaviour string `json:"behaviour"`
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

// StoryPointCriteria rates each dimension Low, Medium or High; Testing may
// also be N/A when QA cannot verify the ticket. Claude rates the dimensions;
// refine fills Total from them (see Score).
type StoryPointCriteria struct {
	Novelty    string `json:"novelty"`
	Risk       string `json:"risk"`
	Dependency string `json:"dependency"`
	Testing    string `json:"testing"`
	Deployment string `json:"deployment"`
	Total      string `json:"total"`
}

var ratingValue = map[string]int{"Low": 1, "Medium": 2, "High": 3}

// Score averages the five ratings (Low 1, Medium 2, High 3, N/A 0, which only
// Testing may be) over all five and rounds to the nearest point. The sum over
// five never lands on a half, so rounding has no tie. ok is false when a
// rating is missing or not allowed.
func (c StoryPointCriteria) Score() (points int, average float64, ok bool) {
	sum := 0
	for _, r := range []string{c.Novelty, c.Risk, c.Dependency, c.Deployment} {
		v, found := ratingValue[r]
		if !found {
			return 0, 0, false
		}
		sum += v
	}
	if c.Testing != "N/A" {
		v, found := ratingValue[c.Testing]
		if !found {
			return 0, 0, false
		}
		sum += v
	}
	average = float64(sum) / 5
	return int(math.Round(average)), average, true
}

// Scored fills Total and returns the points. Criteria that cannot be scored
// keep an empty Total, so they read as unrated, and score 0 points.
func (c StoryPointCriteria) Scored() (StoryPointCriteria, int) {
	points, average, ok := c.Score()
	if !ok {
		c.Total = ""
		return c, 0
	}
	c.Total = fmt.Sprintf("%s (%.1f)", [...]string{"", "Low", "Medium", "High"}[points], average)
	return c, points
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

// OthersPrompt lists the other open tickets, so the triage can say how this
// one stands to them. A ticket without Acceptance Criteria shows its description.
func OthersPrompt(self string, others []jira.Ticket) string {
	return othersList("\nOther open tickets: the developer's in refinement, ready for sprint and the current sprint, and the rest of the epic. Say in relations how this ticket stands to them:\n", others, self)
}

// othersList lists others under intro, leaving out the skipped keys, or
// returns nothing when no ticket is left.
func othersList(intro string, others []jira.Ticket, skip ...string) string {
	var out strings.Builder
	for _, ticket := range others {
		if slices.Contains(skip, ticket.Key) {
			continue
		}
		if out.Len() == 0 {
			out.WriteString(intro)
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

// Glossary is a plain name used in place of code, with the identifiers it
// stands for and where they live.
type Glossary struct {
	Name  string   `json:"name"`
	Code  []string `json:"code"`
	Where []string `json:"where"`
}

// ApproachReview is Claude's check of the developer's approach.
type ApproachReview struct {
	Holds []string `json:"holds"`
	// Unsure answers what the developer marked as unsure, from the code.
	Unsure       []string `json:"unsure"`
	Misses       []string `json:"misses"`
	Alternatives []string `json:"alternatives"`
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

	if len(ticket.Links) > 0 {
		out.WriteString("\nLinks:\n")
		for _, link := range ticket.Links {
			fmt.Fprintf(&out, "- %s %s (%s): %s\n", link.Relation, link.Key, link.Status, link.Summary)
		}
	}

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

// TicketPrompt describes the ticket alone, without repos.
func TicketPrompt(ticket jira.Ticket) string {
	return Context(ticket, nil, nil, "")
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
	// Scope answers the triage's scope questions, keyed by question.
	Scope map[string]string
	// Previous is the draft being redone; its answers go to Claude.
	Previous *Saved
	// Resume continues Previous's session instead of reading the repos from scratch.
	Resume bool
	// Brief, when set, is the ticket's brief; a fresh draft continues its
	// session so the code it read is not read again.
	Brief *SavedBrief
	// Triage, when set, is the ticket's triage; a fresh draft without one
	// triages the ticket first.
	Triage *SavedTriage
}

// AnswersPrompt gives Claude the team's answers to the previous draft's questions.
func AnswersPrompt(previous Saved) string {
	var out strings.Builder
	for _, question := range previous.Draft.Questions() {
		answer := strings.TrimSpace(previous.Answers[question])
		if !decided(answer) {
			continue
		}
		if out.Len() == 0 {
			out.WriteString("\nThe team answered these questions from the previous draft. Use the answers, and do not ask a settled question again:\n")
		}
		fmt.Fprintf(&out, "- Q: %s\n  A: %q\n", question, answer)
	}
	return out.String()
}

// RevisePrompt asks a resumed session for its next round of questions, with
// the answers to the last.
func RevisePrompt(previous Saved) string {
	return "Revise your findings and questions with the team's answers. Reread code where an answer changes what you need to know, and recheck every earlier claim an answer touches. An answer can raise new decisions; ask them." + DecidedPrompt(Decide(previous)) + UnknownPrompt(previous)
}

// Stamp is the write-up of a ticket whose questions are settled, shaped by
// stamp.schema.json.
type Stamp struct {
	AcceptanceCriteria      []string           `json:"acceptance_criteria"`
	TechnicalImplementation []RepoFiles        `json:"technical_implementation"`
	StoryPointCriteria      StoryPointCriteria `json:"story_point_criteria"`
	Points                  int                `json:"points"`
	Split                   []Child            `json:"split"`
	Elsewhere               []Elsewhere        `json:"elsewhere"`
}

// Scored is the stamp with its points and its split tickets' points worked out
// from their ratings: Claude rates, refine scores.
func (s Stamp) Scored() Stamp {
	s.StoryPointCriteria, s.Points = s.StoryPointCriteria.Scored()
	s.Split = slices.Clone(s.Split)
	for i := range s.Split {
		s.Split[i].StoryPointCriteria, s.Split[i].Points = s.Split[i].StoryPointCriteria.Scored()
	}
	return s
}

// WithStamp is the draft with the stamp's fields filled in. A new stamp has
// not been checked against the other tickets yet.
func (d Draft) WithStamp(s Stamp) Draft {
	d.AcceptanceCriteria = s.AcceptanceCriteria
	d.TechnicalImplementation = s.TechnicalImplementation
	d.StoryPointCriteria = s.StoryPointCriteria
	d.Points = s.Points
	d.Split = s.Split
	d.Elsewhere = s.Elsewhere
	d.Checked, d.Duplicates = false, nil
	return d
}

// StampPrompt asks the questions session for the write-up, with every answer,
// the questions the developer chose to proceed with unanswered, and the other
// open tickets the write-up must not repeat. The split tickets already created
// from this draft are not among them.
func StampPrompt(saved Saved, others []jira.Ticket) string {
	var out strings.Builder
	out.WriteString("The questions are done. Write up the ticket as your instructions say.\n")
	out.WriteString(DecidedPrompt(Decide(saved)))
	var open []string
	for _, question := range saved.Draft.Questions() {
		if !decided(saved.Answers[question]) {
			open = append(open, question)
		}
	}
	if len(open) > 0 {
		out.WriteString("\nThe developer chose to proceed with these still open. Follow your recommendation where a field depends on one, and do not present it as settled:\n")
		for _, question := range open {
			fmt.Fprintf(&out, "- %s\n", question)
		}
	}
	out.WriteString(othersList("\nOther open tickets. Do not give this ticket or a split child a behaviour one of them already delivers:\n", others, ownKeys(saved)...))
	return out.String()
}

// ownKeys are the ticket's key and the split tickets created from it, which
// are not other tickets to compare it with.
func ownKeys(saved Saved) []string {
	keys := []string{saved.Key}
	for _, key := range saved.Children {
		keys = append(keys, key)
	}
	return keys
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
