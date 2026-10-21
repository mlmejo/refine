package draft

import (
	_ "embed"
	"time"
)

//go:embed brief.md
var briefText string

var briefRules = withShared(briefText)

//go:embed brief.schema.json
var briefSchema string

// Brief explains the area a ticket touches to a developer new to it, before
// they write their approach.
type Brief struct {
	Summary string   `json:"summary"`
	Flow    []string `json:"flow"`
	Parts   []Part   `json:"parts"`
	Terms   []Term   `json:"terms"`
	Gotchas []string `json:"gotchas"`
	AskTeam []string `json:"ask_team"`
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

// BriefPrompt asks for a brief on the ticket's area.
func BriefPrompt(context string) string {
	return context + "\nBrief the developer on the area this ticket touches. They have not written their approach yet.\n"
}

// FromBriefPrompt asks the brief's session for the draft, now that the
// developer has read the brief and written their approach.
func FromBriefPrompt(approach string) string {
	return "The developer has read your brief and written their approach. Now refine the ticket as your instructions say, reusing the code you already read and reading more only where the approach needs it.\n" + approachSection(approach)
}
