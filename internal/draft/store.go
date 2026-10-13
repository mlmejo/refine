package draft

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	return append(slices.Clone(d.Deployment.Blockers), d.OpenQuestions...)
}

// Unanswered counts the questions without an answer.
func (s Saved) Unanswered() int {
	count := 0
	for _, question := range s.Draft.Questions() {
		if strings.TrimSpace(s.Answers[question]) == "" {
			count++
		}
	}
	return count
}

// Store keeps one draft per ticket as Dir/<KEY>.json.
type Store struct {
	Dir string
}

// Save writes the draft, replacing any earlier one for the ticket.
func (s Store) Save(saved Saved) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(saved.Key) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(saved.Key))
}

// Load returns the ticket's saved draft, if there is one.
func (s Store) Load(key string) (Saved, bool, error) {
	data, err := os.ReadFile(s.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return Saved{}, false, nil
	}
	if err != nil {
		return Saved{}, false, err
	}
	var saved Saved
	if err := json.Unmarshal(data, &saved); err != nil {
		return Saved{}, false, err
	}
	return saved, true, nil
}

func (s Store) path(key string) string {
	return filepath.Join(s.Dir, filepath.Base(key)+".json")
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
