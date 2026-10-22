package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mlmejo/refine/internal/config"
	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/home"
	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/store"
	"github.com/mlmejo/refine/internal/track"
)

func TestAnswerPropsSaysHowEachChangedAnswerWasGiven(t *testing.T) {
	saved := draft.Saved{Step: draft.StepQuestions, Draft: draft.Draft{
		Deployment: draft.Deployment{Blockers: []string{"Who deploys?"}},
		OpenQuestions: []draft.Choice{
			{Question: "Rename?", Options: []string{"yes", "no"}, Recommended: "yes"},
			{Question: "Test?", Options: []string{"jest", "manual"}, Recommended: "manual"},
			{Question: "Owner?", Recommended: "data team"},
		},
	}, Answers: map[string]string{"Who deploys?": "DevOps", "Rename?": "no", "Test?": "manual", "Owner?": "data team"}}
	before := map[string]string{"Test?": "manual"}

	got := answerProps(saved, before)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 (Test? did not change): %v", len(got), got)
	}
	if got[0]["kind"] != "blocker" || got[0]["own_words"] != true || got[0]["agreed"] != false {
		t.Errorf("blocker = %v", got[0])
	}
	if got[1]["picked"] != 1 || got[1]["agreed"] != false || got[1]["options"] != 2 {
		t.Errorf("Rename? = %v", got[1])
	}
	if got[2]["own_words"] != true || got[2]["agreed"] != true {
		t.Errorf("Owner? typed as suggested = %v", got[2])
	}
}

func TestScopePropsCountsAgreement(t *testing.T) {
	qs := []draft.ScopeQuestion{
		{Question: "a", Options: []string{"x", "y"}, Recommended: "x"},
		{Question: "b", Options: []string{"x", "y"}, Recommended: "x"},
		{Question: "c", Options: []string{"x", "y"}, Recommended: "x"},
		{Question: "d", Recommended: "z"},
	}
	got := scopeProps(qs, map[string]string{"a": "x", "b": "y", "c": "something else"})
	want := map[string]any{"questions": 4, "agreed": 1, "other_option": 1, "own_words": 1, "unanswered": 1}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestTrackedRecordsRunsOnlyWhenOn(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "r.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := home.Refiner{Triage: func(context.Context, jira.Issue) (draft.SavedTriage, error) {
		return draft.SavedTriage{CostUSD: 0.04, Turns: 1}, nil
	}}

	off := tracked(base, track.New(s, "gui", false), s.Latest)
	_, _ = off.Triage(context.Background(), jira.Issue{Key: "GEEK-1"})
	on := tracked(base, track.New(s, "gui", true), s.Latest)
	_, _ = on.Triage(context.Background(), jira.Issue{Key: "GEEK-1"})

	events, err := s.Events(time.Time{})
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %+v, %v", events, err)
	}
	e := events[0]
	if e.Name != "run" || e.UI != "gui" || e.Key != "GEEK-1" || e.Props["kind"] != "triage" || e.Props["ok"] != true || e.Props["cost_usd"] != 0.04 {
		t.Errorf("event = %+v", e)
	}
}

func TestExperimentArmIsPerTicketAndSticky(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "r.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	on, off := config.Experiments{PlainNames: true}, config.Experiments{}

	if arm := experimentArm(off, s, "GEEK-1", nil); arm != "" {
		t.Errorf("off: arm %q", arm)
	}
	first := experimentArm(on, s, "GEEK-1", nil)
	if first != "A" && first != "B" {
		t.Fatalf("on: arm %q", first)
	}
	if again := experimentArm(on, s, "GEEK-1", nil); again != first {
		t.Errorf("reassigned %q to %q", first, again)
	}
	// A later round keeps its draft's arm; a ticket in progress before the
	// experiment stays out.
	if arm := experimentArm(on, s, "GEEK-2", &draft.Saved{Draft: draft.Draft{Arm: "B"}}); arm != "B" {
		t.Errorf("later round: arm %q", arm)
	}
	if arm := experimentArm(on, s, "GEEK-3", &draft.Saved{}); arm != "" {
		t.Errorf("in progress before the experiment: arm %q", arm)
	}
}
