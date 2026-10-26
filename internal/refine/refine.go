// Package refine runs a ticket's refinement, one step after another:
// related tickets, repos, questions, deployment, generate and stamp. Go
// decides everything with one right answer (which tickets to show, which
// tickets Claude may consider, how many questions, the points, the Jira
// writes); Claude only judges.
package refine

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Step is where a refinement is.
type Step string

const (
	StepRelated   Step = "related"
	StepRepos     Step = "repos"
	StepQuestions Step = "questions"
	StepDeploy    Step = "deploy"
	StepResults   Step = "results"
	StepStamped   Step = "stamped"
)

// Limits Go holds Claude to, whatever it returns.
const (
	MaxQuestions       = 5
	MaxRounds          = 3
	MaxDeployQuestions = 5
	// MaxRepoQuestions caps the one-time questions about how one repo deploys.
	MaxRepoQuestions = 3
	MaxOptions       = 4
	// MaxSuggestedRepos caps the repos Claude suggests from the ticket.
	MaxSuggestedRepos = 5
)

// Refinement is one ticket's refinement as far as it has got.
type Refinement struct {
	Key     string  `json:"key"`
	Summary string  `json:"summary"`
	Step    Step    `json:"step"`
	Related Related `json:"related"`
	// Repos are the repos the developer picked for this ticket, and Synced
	// each one as fetched: the branch and commit Claude reads.
	Repos  []string `json:"repos"`
	Synced []Synced `json:"synced"`
	// Suggestion is the repos Claude matched to the ticket's title and
	// labels, nil until asked.
	Suggestion *Suggestion `json:"suggestion,omitempty"`
	// Rounds are the question rounds, all in one Claude session.
	Rounds          []Round   `json:"rounds"`
	QuestionSession string    `json:"question_session,omitempty"`
	Deploy          *Deploy   `json:"deploy,omitempty"`
	Result          *Result   `json:"result,omitempty"`
	Stamp           Stamping  `json:"stamp"`
	CostUSD         float64   `json:"cost_usd"`
	Updated         time.Time `json:"updated"`
}

// New starts a refinement of a ticket.
func New(key, summary string) Refinement {
	return Refinement{Key: key, Summary: summary, Step: StepRelated}
}

// Synced is one repo as last fetched: the branch and commit Claude reads,
// how many commits were new, or why the fetch failed. Repo keeps the "name"
// key of the records saved before it had its own type.
type Synced struct {
	Repo   string `json:"name"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	New    int    `json:"new"`
	Error  string `json:"error,omitempty"`
}

// Related is what the search for related tickets found, and the developer's
// decisions on it.
type Related struct {
	Searched bool `json:"searched"`
	// Explanations are what the developer told Claude when asking it to
	// search again, oldest first.
	Explanations []string  `json:"explanations,omitempty"`
	Blockers     []Blocker `json:"blockers"`
	Overlaps     []Overlap `json:"overlaps"`
	// Waiting are the accepted blockers that were not refined when last
	// checked in Jira.
	Waiting []string `json:"waiting,omitempty"`
}

// Suggestion is the repos Claude thinks the ticket touches, most likely
// first.
type Suggestion struct {
	Repos []SuggestedRepo `json:"repos"`
}

// SuggestedRepo is a repo suggested for the ticket, and why.
type SuggestedRepo struct {
	Repo   string `json:"repo"`
	Reason string `json:"reason"`
}

// Decisions on a blocker.
const (
	Accepted = "accepted"
	Rejected = "rejected"
)

// Blocker is a ticket this one builds on.
type Blocker struct {
	Key      string `json:"key"`
	Summary  string `json:"summary"`
	Reason   string `json:"reason"`
	Quote    string `json:"quote"`
	Refined  bool   `json:"refined"`
	Decision string `json:"decision,omitempty"`
	// FromCode marks a blocker Claude found while reading the code for the
	// questions, rather than in the search.
	FromCode bool `json:"from_code,omitempty"`
}

// Overlap is a ticket that does some of the same work. The developer
// resolves it in Jira; refine only shows it.
type Overlap struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Reason  string `json:"reason"`
}

// Accepted returns the blockers the developer accepted.
func (r Related) Accepted() []Blocker {
	var out []Blocker
	for _, b := range r.Blockers {
		if b.Decision == Accepted {
			out = append(out, b)
		}
	}
	return out
}

// Pending are the blockers found while reading the code that the developer
// has not decided yet. The questions wait on them.
func (r Related) Pending() []Blocker {
	var out []Blocker
	for _, b := range r.Blockers {
		if b.FromCode && b.Decision == "" {
			out = append(out, b)
		}
	}
	return out
}

// Undecided counts the blockers without a decision.
func (r Related) Undecided() int {
	n := 0
	for _, b := range r.Blockers {
		if b.Decision == "" {
			n++
		}
	}
	return n
}

// Question is a multiple-choice question with Claude's recommended option.
// Answer is the picked option or the developer's own words.
type Question struct {
	Text        string   `json:"text"`
	Options     []string `json:"options"`
	Recommended int      `json:"recommended"`
	Why         string   `json:"why,omitempty"`
	Answer      string   `json:"answer,omitempty"`
}

// Round is one batch of questions.
type Round struct {
	Questions []Question `json:"questions"`
}

// Answered reports whether every question has an answer.
func Answered(qs []Question) bool {
	for _, q := range qs {
		if strings.TrimSpace(q.Answer) == "" {
			return false
		}
	}
	return true
}

// Deploy is the deployment step: one-time questions about how a repo
// deploys, for repos with nothing saved, then this ticket's deploy needs.
type Deploy struct {
	Repos  []RepoQuestion `json:"repos"`
	Ticket []Question     `json:"ticket"`
}

// RepoQuestion asks how one repo deploys.
type RepoQuestion struct {
	Repo string `json:"repo"`
	Question
}

// Answered reports whether every deploy question has an answer.
func (d Deploy) Answered() bool {
	for _, q := range d.Repos {
		if strings.TrimSpace(q.Answer) == "" {
			return false
		}
	}
	return Answered(d.Ticket)
}

// Profile is how a repo deploys, as the developer answered once.
type Profile struct {
	Repo    string    `json:"repo"`
	Answers []QA      `json:"answers"`
	Updated time.Time `json:"updated"`
}

// QA is a question with its answer.
type QA struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// Result is what gets written to Jira: the ticket's fields, its split and
// its deploy needs.
type Result struct {
	AcceptanceCriteria      []string    `json:"acceptance_criteria"`
	TechnicalImplementation []RepoFiles `json:"technical_implementation"`
	Decisions               []string    `json:"decisions"`
	Ratings                 Ratings     `json:"ratings"`
	Points                  int         `json:"points"`
	Split                   []Child     `json:"split"`
	SplitReason             string      `json:"split_reason"`
	DeployNeeds             []string    `json:"deploy_needs"`
}

// Child is a split ticket created next to the original.
type Child struct {
	Title                   string      `json:"title"`
	AcceptanceCriteria      []string    `json:"acceptance_criteria"`
	TechnicalImplementation []RepoFiles `json:"technical_implementation"`
	Decisions               []string    `json:"decisions"`
	Ratings                 Ratings     `json:"ratings"`
	Points                  int         `json:"points"`
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

// Ratings rate each Story Point Criteria dimension Low, Medium or High;
// Testing may also be N/A. Claude rates; Go scores.
type Ratings struct {
	Novelty    string `json:"novelty"`
	Risk       string `json:"risk"`
	Dependency string `json:"dependency"`
	Testing    string `json:"testing"`
	Deployment string `json:"deployment"`
}

var ratingValue = map[string]int{"Low": 1, "Medium": 2, "High": 3}

// Score averages the five ratings (Low 1, Medium 2, High 3, N/A 0, which
// only Testing may be) over all five and rounds to the nearest point. ok is
// false when a rating is missing or not allowed.
func (r Ratings) Score() (points int, average float64, ok bool) {
	sum := 0
	for _, v := range []string{r.Novelty, r.Risk, r.Dependency, r.Deployment} {
		n, found := ratingValue[v]
		if !found {
			return 0, 0, false
		}
		sum += n
	}
	if r.Testing != "N/A" {
		n, found := ratingValue[r.Testing]
		if !found {
			return 0, 0, false
		}
		sum += n
	}
	average = float64(sum) / 5
	return int(math.Round(average)), average, true
}

// Total is the Story Point Criteria's total line, e.g. "Medium (1.6)", or
// "" when the ratings cannot be scored.
func (r Ratings) Total() string {
	points, average, ok := r.Score()
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s (%.1f)", [...]string{"", "Low", "Medium", "High"}[points], average)
}

// Rows pairs each rating with its label as Jira's field writes it.
func (r Ratings) Rows() [][2]string {
	return [][2]string{
		{"Novelty & Complexity of Implementation", r.Novelty},
		{"Risk & Feature Impact", r.Risk},
		{"Dependency & Integration Complexity", r.Dependency},
		{"Testing & QA Effort", r.Testing},
		{"Deployment Complexity", r.Deployment},
	}
}

// Scored is the result with every points figure worked out from its ratings.
func (r Result) Scored() Result {
	r.Points, _, _ = r.Ratings.Score()
	split := make([]Child, len(r.Split))
	for i, c := range r.Split {
		c.Points, _, _ = c.Ratings.Score()
		split[i] = c
	}
	r.Split = split
	return r
}

// Stamping is how far writing to Jira got. Done holds the actions finished,
// so a retry skips them.
type Stamping struct {
	Done     []string       `json:"done,omitempty"`
	Children map[int]string `json:"children,omitempty"`
	Failed   string         `json:"failed,omitempty"`
	Error    string         `json:"error,omitempty"`
}

// Label is the step as the Tickets list shows it.
func (r Refinement) Label() string {
	switch r.Step {
	case StepRelated:
		if len(r.Related.Waiting) > 0 {
			return "Waiting"
		}
		return "Related tickets"
	case StepRepos:
		return "Choose repos"
	case StepQuestions:
		round := max(len(r.Rounds), 1)
		return fmt.Sprintf("Questions, round %d of %d", round, MaxRounds)
	case StepDeploy:
		return "Deployment"
	case StepResults:
		if len(r.Stamp.Done) > 0 {
			return "Stamping"
		}
		return "Ready to stamp"
	case StepStamped:
		return "Stamped"
	}
	return string(r.Step)
}

// Commit is the commit Claude reads in a repo, from the last sync.
func (r Refinement) Commit(repo string) (Synced, bool) {
	for _, s := range r.Synced {
		if s.Repo == repo && s.Error == "" && s.Commit != "" {
			return s, true
		}
	}
	return Synced{}, false
}
