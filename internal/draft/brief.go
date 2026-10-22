package draft

import (
	_ "embed"
	"fmt"
	"strings"
	"time"
)

//go:embed brief.md
var briefText string

var briefRules = withShared(briefText)

//go:embed brief.schema.json
var briefSchema string

// Brief checks what the ticket asks for against the code and explains the
// area it touches to a developer new to it, before they write their approach.
type Brief struct {
	Items   []ItemStatus `json:"items"`
	Claims  []Claim      `json:"claims"`
	Summary string       `json:"summary"`
	Flow    []string `json:"flow"`
	Parts   []Part   `json:"parts"`
	Terms   []Term   `json:"terms"`
	Gotchas []string `json:"gotchas"`
	AskTeam []string `json:"ask_team"`
}

// ItemStatus is what the code shows about one of the triage's items: needed,
// done, looks_unused, ask_team or blocked. Whether a feature is dead is the
// developer's call; the code can only show that it looks unused.
type ItemStatus struct {
	Item     string `json:"item"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
	Note     string `json:"note"`
}

// Claim is something the ticket says about today's code that the code
// contradicts. Ticket descriptions are often written by AI and can be wrong.
type Claim struct {
	Claim    string `json:"claim"`
	Finding  string `json:"finding"`
	Evidence string `json:"evidence"`
}

// Part is a file that matters to the area, and what it does there.
type Part struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	Role string `json:"role"`
}

// Term is a domain word from the ticket and what it means in this code.
type Term struct {
	Term    string `json:"term"`
	Meaning string `json:"meaning"`
}

// BriefResult is a finished brief with the run behind it.
type BriefResult struct {
	Brief Brief
	Run   Run
}

// SavedBrief is a brief kept with the session that read the code for it.
type SavedBrief struct {
	ID        int64
	Key       string
	Ref       string
	SessionID string
	CostUSD   float64
	Turns     int
	Usage     Usage
	Created   time.Time
	Brief     Brief
}

// BriefPrompt asks for a brief on the ticket's area, checking each of the
// triage's items against the code.
func BriefPrompt(context string, items []Item) string {
	var out strings.Builder
	out.WriteString(context)
	if len(items) > 0 {
		out.WriteString("\nThe ticket's items, from its triage:\n")
		for _, item := range items {
			fmt.Fprintf(&out, "- %s (%s: %q)\n", item.Item, item.Section, item.Quote)
		}
	}
	out.WriteString("\nCheck the ticket against the code and brief the developer on the area it touches. They have not written their approach yet.\n")
	return out.String()
}

// FromBriefPrompt asks the brief's session for the draft, now that the
// developer has read the brief and written their approach.
func FromBriefPrompt(approach string) string {
	return "The developer has read your brief and written their approach. Now refine the ticket as your instructions say, reusing the code you already read and reading more only where the approach needs it.\n" + approachSection(approach)
}
