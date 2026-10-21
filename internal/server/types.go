package server

import (
	"time"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/gitsync"
	"github.com/mlmejo/refine/internal/jira"
)

// The types here are what the desktop app reads. draft.Saved and draft.Draft
// carry their own JSON names; the rest get theirs here so the domain types
// stay free of them.

type issue struct {
	Key                     string `json:"key"`
	Summary                 string `json:"summary"`
	Status                  string `json:"status"`
	AcceptanceCriteria      string `json:"acceptance_criteria"`
	TechnicalImplementation string `json:"technical_implementation"`
	StoryPointCriteria      string `json:"story_point_criteria"`
}

func toIssue(i jira.Issue) issue {
	return issue{
		Key:                     i.Key,
		Summary:                 i.Summary,
		Status:                  i.Status,
		AcceptanceCriteria:      i.AcceptanceCriteria,
		TechnicalImplementation: i.TechnicalImplementation,
		StoryPointCriteria:      i.StoryPointCriteria,
	}
}

func toIssues(in []jira.Issue) []issue {
	out := make([]issue, 0, len(in))
	for _, i := range in {
		out = append(out, toIssue(i))
	}
	return out
}

type run struct {
	SessionID string      `json:"session_id"`
	CostUSD   float64     `json:"cost_usd"`
	Turns     int         `json:"turns"`
	Usage     draft.Usage `json:"usage"`
	Created   time.Time   `json:"created"`
}

type triage struct {
	ID int64 `json:"id"`
	run
	Triage draft.Triage `json:"triage"`
}

func toTriage(t draft.SavedTriage) *triage {
	return &triage{
		ID:     t.ID,
		run:    run{SessionID: t.SessionID, CostUSD: t.CostUSD, Turns: t.Turns, Usage: t.Usage, Created: t.Created},
		Triage: t.Triage,
	}
}

type brief struct {
	ID  int64  `json:"id"`
	Ref string `json:"ref,omitempty"`
	run
	Brief draft.Brief `json:"brief"`
}

func toBrief(b draft.SavedBrief) *brief {
	return &brief{
		ID:    b.ID,
		Ref:   b.Ref,
		run:   run{SessionID: b.SessionID, CostUSD: b.CostUSD, Turns: b.Turns, Usage: b.Usage, Created: b.Created},
		Brief: b.Brief,
	}
}

// status is a repo compared with origin. The app sends the stale ones back
// to be pulled.
type status struct {
	Name   string `json:"name"`
	Dir    string `json:"dir"`
	Branch string `json:"branch"`
	Behind int    `json:"behind"`
	Origin string `json:"origin"`
	Skip   string `json:"skip,omitempty"`
}

func toStatus(s gitsync.Status) status {
	return status{Name: s.Name, Dir: s.Dir, Branch: s.Branch, Behind: s.Behind, Origin: s.Origin, Skip: s.Skip}
}

func (s status) gitsync() gitsync.Status {
	return gitsync.Status{Name: s.Name, Dir: s.Dir, Branch: s.Branch, Behind: s.Behind, Origin: s.Origin, Skip: s.Skip}
}

type repo struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}
