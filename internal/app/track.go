package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/home"
	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/track"
)

// tracked wraps the refiner so every Claude run, answer, scope pick and
// write is recorded the same way whichever UI drives it. With tracking off
// it returns the refiner as it is.
func tracked(r home.Refiner, t *track.Tracker, latest func(string) (draft.Saved, bool, error)) home.Refiner {
	if !t.On() {
		return r
	}
	run := func(kind, key string, started time.Time, props map[string]any, cost float64, turns int, err error) {
		if props == nil {
			props = map[string]any{}
		}
		props["kind"], props["ok"] = kind, err == nil
		props["secs"] = roundSecs(time.Since(started))
		if err == nil {
			props["cost_usd"], props["turns"] = cost, turns
		} else if err == context.Canceled {
			props["cancelled"] = true
		}
		t.Event("run", key, props)
	}

	triage := r.Triage
	r.Triage = func(ctx context.Context, issue jira.Issue) (draft.SavedTriage, error) {
		started := time.Now()
		saved, err := triage(ctx, issue)
		run("triage", issue.Key, started, map[string]any{"scope_questions": len(saved.Triage.ScopeQuestions)}, saved.CostUSD, saved.Turns, err)
		return saved, err
	}
	brief := r.Brief
	r.Brief = func(ctx context.Context, issue jira.Issue) (draft.SavedBrief, error) {
		started := time.Now()
		saved, err := brief(ctx, issue)
		run("brief", issue.Key, started, nil, saved.CostUSD, saved.Turns, err)
		return saved, err
	}
	questions := r.Draft
	r.Draft = func(ctx context.Context, issue jira.Issue, req draft.Request) (draft.Saved, error) {
		started := time.Now()
		saved, err := questions(ctx, issue, req)
		// The arm is known once the run has assigned it.
		if !req.Resume && req.Triage != nil {
			props := scopeProps(req.Triage.Triage.ScopeQuestions, req.Scope)
			props["arm"] = saved.Draft.Arm
			t.Event("scope", issue.Key, props)
		}
		props := map[string]any{
			"resume":         req.Resume,
			"from_brief":     req.Brief != nil && !req.Resume,
			"approach_chars": len(strings.TrimSpace(req.Approach)),
			"unsure_marks":   strings.Count(req.Approach, "?"),
		}
		if err == nil {
			d := saved.Draft
			props["findings"], props["questions"], props["blockers"] = len(d.Findings), len(d.OpenQuestions), len(d.Deployment.Blockers)
			props["with_options"] = countWhere(d.OpenQuestions, func(c draft.Choice) bool { return len(c.Options) > 0 })
			props["finding_chars"] = totalChars(d.Findings)
			props["arm"], props["terms"] = d.Arm, len(d.Terms)
			// The primary metric of the plain-names experiment: earlier
			// decisions this round reopens, out of those made so far.
			props["decided_before"] = len(d.Decided)
			props["reopened"] = countWhere(d.OpenQuestions, func(c draft.Choice) bool { _, ok := c.Reopened(); return ok })
		}
		run("questions", issue.Key, started, props, saved.CostUSD, saved.Turns, err)
		return saved, err
	}
	stamp := r.Stamp
	r.Stamp = func(ctx context.Context, issue jira.Issue, prev draft.Saved) (draft.Saved, error) {
		started := time.Now()
		saved, err := stamp(ctx, issue, prev)
		props := map[string]any{"open_questions": prev.Unanswered(), "arm": prev.Draft.Arm, "decisions": len(draft.Decide(prev))}
		if err == nil {
			d := saved.Draft
			props["criteria"], props["points"], props["split"] = len(d.AcceptanceCriteria), d.Points, len(d.Split)
			props["checked"], props["duplicates"] = d.Checked, len(d.Duplicates)
		}
		run("stamp", issue.Key, started, props, saved.CostUSD, saved.Turns, err)
		return saved, err
	}
	save := r.SaveAnswers
	r.SaveAnswers = func(saved draft.Saved) error {
		var before map[string]string
		if prev, ok, err := latest(saved.Key); err == nil && ok && prev.ID == saved.ID {
			before = prev.Answers
		}
		err := save(saved)
		if err == nil {
			for _, props := range answerProps(saved, before) {
				t.Event("answer", saved.Key, props)
			}
		}
		return err
	}
	write := r.Write
	r.Write = func(ctx context.Context, key string, d draft.Draft) (jira.Issue, error) {
		started := time.Now()
		issue, err := write(ctx, key, d)
		t.Event("write", key, map[string]any{"ok": err == nil, "secs": roundSecs(time.Since(started)), "checked": d.Checked, "duplicates": len(d.Duplicates), "split": len(d.Split)})
		return issue, err
	}
	move := r.Move
	if move != nil {
		r.Move = func(ctx context.Context, key string) error {
			err := move(ctx, key)
			t.Event("move", key, map[string]any{"ok": err == nil})
			return err
		}
	}
	return r
}

// scopeProps counts how the scope questions were answered against the
// triage's recommendations.
func scopeProps(questions []draft.ScopeQuestion, answers map[string]string) map[string]any {
	agreed, other, own, open := 0, 0, 0, 0
	for _, q := range questions {
		answer := strings.TrimSpace(answers[q.Question])
		switch {
		case answer == "":
			open++
		case answer == q.Recommended:
			agreed++
		case slices.Contains(q.Options, answer):
			other++
		default:
			own++
		}
	}
	return map[string]any{"questions": len(questions), "agreed": agreed, "other_option": other, "own_words": own, "unanswered": open}
}

// answerProps describes each answer that changed since before: which
// question, whether it took Claude's recommendation, another option, or the
// developer's own words.
func answerProps(saved draft.Saved, before map[string]string) []map[string]any {
	var out []map[string]any
	describe := func(kind string, index int, c draft.Choice) {
		answer := strings.TrimSpace(saved.Answers[c.Question])
		if answer == strings.TrimSpace(before[c.Question]) {
			return
		}
		props := map[string]any{"kind": kind, "index": index, "options": len(c.Options), "step": saved.Step, "arm": saved.Draft.Arm}
		note, unknown := draft.IsUnknown(answer)
		switch {
		case answer == "":
			props["cleared"] = true
		case unknown:
			props["unknown"], props["chars"] = true, len(note)
		case len(c.Options) > 0 && slices.Contains(c.Options, answer):
			props["picked"] = slices.Index(c.Options, answer)
			props["agreed"] = answer == c.Recommended
		default:
			props["own_words"], props["chars"] = true, len(answer)
			props["agreed"] = c.Recommended != "" && strings.EqualFold(answer, c.Recommended)
		}
		props["changed"] = strings.TrimSpace(before[c.Question]) != ""
		out = append(out, props)
	}
	for i, b := range saved.Draft.Deployment.Blockers {
		describe("blocker", i, draft.Choice{Question: b})
	}
	for i, c := range saved.Draft.OpenQuestions {
		describe("question", i, c)
	}
	return out
}

func countWhere[T any](items []T, keep func(T) bool) int {
	n := 0
	for _, item := range items {
		if keep(item) {
			n++
		}
	}
	return n
}

func totalChars(items []string) int {
	n := 0
	for _, item := range items {
		n += len(item)
	}
	return n
}

func roundSecs(d time.Duration) float64 {
	return float64(d.Round(100*time.Millisecond)) / float64(time.Second)
}
