package draft

import (
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/mlmejo/refine/internal/jira"
)

//go:embed triage.md
var triageRules string

//go:embed triage.schema.json
var triageSchema string

// Triage is the first step: what the ticket is for and what stands in its
// way, judged from Jira alone before anyone reads code.
type Triage struct {
	Problem  string `json:"problem"`
	Solution string `json:"solution"`
	// Items are the things the ticket asks for, one per deliverable, which
	// the brief then checks against the code.
	Items          []Item          `json:"items"`
	Blockers       []string        `json:"blockers"`
	ScopeQuestions []ScopeQuestion `json:"scope_questions"`
	Relations      []Relation      `json:"relations"`
}

// Item is one thing the ticket asks for, with the ticket's own words for it
// and the section they come from.
type Item struct {
	Item    string `json:"item"`
	Quote   string `json:"quote"`
	Section string `json:"section"`
}

// TriageResult is a finished triage with the run behind it.
type TriageResult struct {
	Triage Triage
	Run    Run
}

// SavedTriage is a triage kept for the ticket.
type SavedTriage struct {
	ID        int64
	Key       string
	SessionID string
	CostUSD   float64
	Turns     int
	Usage     Usage
	Created   time.Time
	Triage    Triage
}

// TriagePrompt describes the ticket, its links and the other open
// tickets, for a triage that reads no code.
func TriagePrompt(ticket jira.Ticket, others []jira.Ticket) string {
	return TicketPrompt(ticket) + OthersPrompt(ticket.Key, others) +
		"\nTriage this ticket as your instructions say.\n"
}

// Prompt hands the triage to the questions step.
func (t Triage) Prompt(scope map[string]string) string {
	var out strings.Builder
	out.WriteString("\nThe ticket's triage, from Jira alone:\n")
	fmt.Fprintf(&out, "Problem: %s\nSolution: %s\n", t.Problem, t.Solution)
	list := func(name string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&out, "%s:\n", name)
		for _, item := range items {
			fmt.Fprintf(&out, "- %s\n", item)
		}
	}
	list("Blockers already known", t.Blockers)
	var open []string
	for _, q := range t.ScopeQuestions {
		if strings.TrimSpace(scope[q.Question]) == "" {
			open = append(open, q.String())
		}
	}
	list("Scope questions", open)
	var related []string
	for _, r := range t.Relations {
		related = append(related, fmt.Sprintf("%s %s: %s", r.Label(), r.Key, r.Why))
	}
	list("Related tickets", related)
	out.WriteString(ScopePrompt(scope))
	return out.String()
}

// Apply copies what the draft shows from the triage.
func (t Triage) Apply(d Draft) Draft {
	d.Problem, d.Solution = t.Problem, t.Solution
	d.TicketBlockers, d.Relations = t.Blockers, t.Relations
	return d
}

// Carry copies what a draft took from its triage into the next one.
func Carry(from, to Draft) Draft {
	to.Problem, to.Solution = from.Problem, from.Solution
	to.TicketBlockers, to.Relations = from.TicketBlockers, from.Relations
	return to
}
