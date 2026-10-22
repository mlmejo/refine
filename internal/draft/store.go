package draft

import (
	"fmt"
	"maps"
	"slices"
	"time"
)

// Saved is a draft kept on disk with the approach and session behind it.
type Saved struct {
	ID       int64  `json:"id,omitempty"`
	Key      string `json:"key"`
	Approach string `json:"approach"`
	Ref      string `json:"ref,omitempty"`
	// Step is StepQuestions until the draft is stamped. Drafts made before
	// refinement had steps have none and count as stamped.
	Step      string  `json:"step,omitempty"`
	SessionID string  `json:"session_id"`
	CostUSD   float64 `json:"cost_usd"`
	// SessionCostUSD is the session's running total, which a resumed run reports.
	SessionCostUSD float64   `json:"session_cost_usd,omitempty"`
	Turns          int       `json:"turns,omitempty"`
	Usage          Usage     `json:"usage"`
	Created        time.Time `json:"created"`
	Draft          Draft     `json:"draft"`
	// Markdown is the file written for this draft.
	Markdown string `json:"markdown,omitempty"`
	// Answers are the team's answers, keyed by question.
	Answers map[string]string `json:"answers,omitempty"`
	// Scope are the developer's answers to the triage's scope questions,
	// given with the approach, keyed by question.
	Scope map[string]string `json:"scope,omitempty"`
	// Children are the Jira keys of the split tickets already created from
	// this draft, by index into Draft.Split.
	Children map[int]string `json:"children,omitempty"`
}

// The steps a draft goes through.
const (
	StepQuestions = "questions"
	StepStamp     = "stamp"
)

// Stamped reports whether the draft has the fields written to Jira.
func (s Saved) Stamped() bool {
	return s.Step != StepQuestions
}

// Questions are the deploy blockers and open questions the team must answer.
func (d Draft) Questions() []string {
	questions := slices.Clone(d.Deployment.Blockers)
	for _, q := range d.OpenQuestions {
		questions = append(questions, q.Question)
	}
	return questions
}

// Unanswered counts the questions without an answer.
func (s Saved) Unanswered() int {
	count := 0
	for _, question := range s.Draft.Questions() {
		if !decided(s.Answers[question]) {
			count++
		}
	}
	return count
}

// CreateSplit creates each split ticket that saved.Children lacks, in order,
// and keeps the draft after each one so a retry never creates a ticket twice.
// It returns the keys of every split ticket created so far, even on error.
func CreateSplit(saved Saved, create func(Child) (string, error), keep func(Saved) error) (map[int]string, error) {
	children := maps.Clone(saved.Children)
	if children == nil {
		children = map[int]string{}
	}
	for i, child := range saved.Draft.Split {
		if children[i] != "" {
			continue
		}
		key, err := create(child)
		if key != "" {
			children[i] = key
			saved.Children = maps.Clone(children)
			if keepErr := keep(saved); keepErr != nil && err == nil {
				err = fmt.Errorf("created %s, but could not save its key: %w", key, keepErr)
			}
		}
		if err != nil {
			return children, fmt.Errorf("%s: %w", child.Title, err)
		}
	}
	return children, nil
}
