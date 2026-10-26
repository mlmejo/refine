package refine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/mlmejo/refine/internal/claude"
	"github.com/mlmejo/refine/internal/gitsync"
	"github.com/mlmejo/refine/internal/jira"
)

type fakeJira struct {
	tickets  map[string]jira.Ticket
	list     []string
	ready    []string
	jqls     []string
	writes   []string
	failOn   string
	children int
}

func (f *fakeJira) Myself(context.Context) (string, error) { return "Dev", nil }
func (f *fakeJira) Linked(_ context.Context, jql string) ([]jira.Ticket, error) {
	f.jqls = append(f.jqls, jql)
	keys := f.list
	if strings.Contains(jql, "Ready for Sprint") {
		keys = f.ready
	}
	var out []jira.Ticket
	for _, key := range keys {
		out = append(out, f.tickets[key])
	}
	return out, nil
}
func (f *fakeJira) Tickets(_ context.Context, jql string) ([]jira.Ticket, error) {
	f.jqls = append(f.jqls, jql)
	var out []jira.Ticket
	for _, key := range slices.Sorted(maps.Keys(f.tickets)) {
		if !strings.Contains(jql, `key != "`+key+`"`) {
			out = append(out, f.tickets[key])
		}
	}
	return out, nil
}
func (f *fakeJira) GetTicket(_ context.Context, key string) (jira.Ticket, error) {
	t, ok := f.tickets[key]
	if !ok {
		return jira.Ticket{}, fmt.Errorf("no %s", key)
	}
	return t, nil
}
func (f *fakeJira) record(what string) error {
	if f.failOn != "" && strings.HasPrefix(what, f.failOn) {
		return errors.New("jira refused " + what)
	}
	f.writes = append(f.writes, what)
	return nil
}
func (f *fakeJira) Update(_ context.Context, key string, r jira.Refinement) error {
	return f.record(fmt.Sprintf("update %s %d", key, r.Points))
}
func (f *fakeJira) Comment(_ context.Context, key, body string) error {
	return f.record("comment " + key)
}
func (f *fakeJira) Split(_ context.Context, parent, sprint string) (jira.SplitTarget, error) {
	return jira.SplitTarget{Parent: parent}, nil
}
func (f *fakeJira) CreateChild(_ context.Context, t jira.SplitTarget, c jira.NewChild) (string, error) {
	if err := f.record("create " + c.Summary); err != nil {
		return "", err
	}
	f.children++
	return fmt.Sprintf("GEEK-%d", 900+f.children), nil
}
func (f *fakeJira) Relate(_ context.Context, from, to string) error {
	return f.record("relate " + from + " " + to)
}
func (f *fakeJira) MoveToSprint(_ context.Context, key, sprint string) error {
	return f.record("move " + key + " " + sprint)
}

// fakeClaude answers each call with the next canned answer.
type fakeClaude struct {
	answers []any
	calls   []claude.Call
}

func (f *fakeClaude) Do(_ context.Context, call claude.Call, into any, progress func(claude.Activity)) (claude.Run, error) {
	f.calls = append(f.calls, call)
	if len(f.answers) == 0 {
		return claude.Run{}, errors.New("no answer left")
	}
	data, _ := json.Marshal(f.answers[0])
	f.answers = f.answers[1:]
	if progress != nil {
		progress(claude.Activity{Verb: "Read", What: "x"})
	}
	return claude.Run{SessionID: fmt.Sprintf("s%d", len(f.calls)), CostUSD: 0.1}, json.Unmarshal(data, into)
}

type memStore struct {
	refinements map[string]Refinement
	profiles    map[string]Profile
}

func (m *memStore) Load(key string) (Refinement, bool, error) {
	r, ok := m.refinements[key]
	data, _ := json.Marshal(r)
	var copy Refinement
	_ = json.Unmarshal(data, &copy)
	return copy, ok, nil
}
func (m *memStore) All() ([]Refinement, error) {
	var out []Refinement
	for _, key := range slices.Sorted(maps.Keys(m.refinements)) {
		out = append(out, m.refinements[key])
	}
	return out, nil
}
func (m *memStore) Save(r Refinement) error               { m.refinements[r.Key] = r; return nil }
func (m *memStore) Delete(key string) error               { delete(m.refinements, key); return nil }
func (m *memStore) Profiles() (map[string]Profile, error) { return maps.Clone(m.profiles), nil }
func (m *memStore) SaveProfile(p Profile) error           { m.profiles[p.Repo] = p; return nil }

type fakeGit struct {
	fail      map[string]bool
	checkouts []string
}

func (g *fakeGit) Fetch(_ context.Context, name, dir string) gitsync.Status {
	if g.fail[name] {
		return gitsync.Status{Name: name, Dir: dir, Error: "could not reach origin"}
	}
	return gitsync.Status{Name: name, Dir: dir, Branch: "develop", Commit: "abc123" + name, New: 2}
}
func (g *fakeGit) Checkout(_ context.Context, repo, commit, dest string) error {
	g.checkouts = append(g.checkouts, commit+" "+dest)
	return nil
}

func ticket(key, summary string, refined bool) jira.Ticket {
	t := jira.Ticket{Issue: jira.Issue{Key: key, Summary: summary, Status: "To Do"}, Description: summary + " description"}
	if refined {
		t.AcceptanceCriteria, t.TechnicalImplementation = "* It works.", "dating-backend\n\n* app/Stripe.php"
	}
	return t
}

func setup() (*Service, *fakeJira, *fakeClaude, *memStore, *fakeGit) {
	j := &fakeJira{tickets: map[string]jira.Ticket{
		"GEEK-100": ticket("GEEK-100", "Integrate Stripe payments API", true),
		"GEEK-101": ticket("GEEK-101", "Let support refund a payment", false),
		"GEEK-112": ticket("GEEK-112", "Add the admin payment page", false),
		"GEEK-119": ticket("GEEK-119", "Server-side purchase events", false),
	}}
	c := &fakeClaude{}
	st := &memStore{refinements: map[string]Refinement{}, profiles: map[string]Profile{}}
	g := &fakeGit{fail: map[string]bool{}}
	s := &Service{Jira: j, Claude: c, Store: st, Git: g, Sprint: "Refinement Ready - Team John", ReadySprint: "Ready for Sprint - Team John",
		Repos: func() map[string]string {
			return map[string]string{"admin": "/r/admin", "dating-backend": "/r/dating-backend", "costs": "/r/costs"}
		},
		Worktrees: "/w"}
	return s, j, c, st, g
}

var related = map[string]any{
	"blockers": []map[string]string{
		{"key": "GEEK-100", "reason": "Refunds go through the Stripe integration.", "quote": "Using the new Stripe integration"},
		{"key": "GEEK-404", "reason": "made up", "quote": ""},
		{"key": "GEEK-112", "reason": "The button goes on the admin payment page.", "quote": "admin payment page"},
	},
	"overlaps": []map[string]string{
		{"key": "GEEK-119", "reason": "Both send purchase events"},
		{"key": "GEEK-100", "reason": "already a blocker"},
	},
}

func TestSearchKeepsOnlyListedTicketsAndCountsTheCost(t *testing.T) {
	s, j, c, _, _ := setup()
	c.answers = []any{related}
	r, err := s.Search(context.Background(), "GEEK-101", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, b := range r.Related.Blockers {
		keys = append(keys, b.Key)
	}
	if !slices.Equal(keys, []string{"GEEK-100", "GEEK-112"}) || len(r.Related.Overlaps) != 1 || r.Related.Overlaps[0].Key != "GEEK-119" {
		t.Fatalf("related = %+v", r.Related)
	}
	if !r.Related.Blockers[0].Refined || r.Related.Blockers[1].Refined {
		t.Errorf("refined = %+v", r.Related.Blockers)
	}
	if len(c.calls[0].Repos) != 0 || !strings.Contains(c.calls[0].Prompt, "GEEK-112 (To Do): Add the admin payment page") {
		t.Errorf("call = %+v", c.calls[0])
	}
	if !strings.Contains(j.jqls[0], `sprint in ("Refinement Ready - Team John", "Ready for Sprint - Team John") AND key != "GEEK-101"`) {
		t.Errorf("jql = %s", j.jqls[0])
	}
	if r.CostUSD != 0.1 || r.Step != StepRelated {
		t.Errorf("refinement = %+v", r)
	}
}

func TestSearchAgainSendsTheExplanationAndKeepsDecisions(t *testing.T) {
	s, _, c, _, _ := setup()
	c.answers = []any{related, related}
	ctx := context.Background()
	if _, err := s.Search(ctx, "GEEK-101", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide("GEEK-101", "GEEK-100", Accepted); err != nil {
		t.Fatal(err)
	}
	r, err := s.Search(ctx, "GEEK-101", "GEEK-131 also blocks this", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.calls[1].Prompt, `"GEEK-131 also blocks this"`) || r.Related.Blockers[0].Decision != Accepted {
		t.Fatalf("prompt = %s\nrelated = %+v", c.calls[1].Prompt, r.Related)
	}
}

func TestContinueWaitsOnUnrefinedBlockersThenMovesOn(t *testing.T) {
	s, j, c, _, _ := setup()
	c.answers = []any{related}
	ctx := context.Background()
	s.Search(ctx, "GEEK-101", "", nil)
	s.Decide("GEEK-101", "GEEK-100", Accepted)
	if _, err := s.Continue(ctx, "GEEK-101"); err == nil || !strings.Contains(err.Error(), "1 more blocker") {
		t.Fatalf("undecided: %v", err)
	}
	s.Decide("GEEK-101", "GEEK-112", Accepted)
	r, err := s.Continue(ctx, "GEEK-101")
	if err != nil {
		t.Fatal(err)
	}
	if r.Step != StepRelated || !slices.Equal(r.Related.Waiting, []string{"GEEK-112"}) || r.Label() != "Waiting" {
		t.Fatalf("waiting = %+v", r)
	}
	// Someone refines GEEK-112 in Jira; opening the ticket notices.
	j.tickets["GEEK-112"] = ticket("GEEK-112", "Add the admin payment page", true)
	v, err := s.Open(ctx, "GEEK-101")
	if err != nil {
		t.Fatal(err)
	}
	if v.Refinement.Step != StepRepos || len(v.Refinement.Related.Waiting) != 0 {
		t.Fatalf("after refining the blocker: %+v", v.Refinement)
	}
}

// toRepos runs the related step with GEEK-100 accepted and GEEK-112 rejected.
func toRepos(t *testing.T, s *Service, c *fakeClaude) {
	t.Helper()
	ctx := context.Background()
	c.answers = append([]any{related}, c.answers...)
	if _, err := s.Search(ctx, "GEEK-101", "", nil); err != nil {
		t.Fatal(err)
	}
	s.Decide("GEEK-101", "GEEK-100", Accepted)
	s.Decide("GEEK-101", "GEEK-112", Rejected)
	if r, err := s.Continue(ctx, "GEEK-101"); err != nil || r.Step != StepRepos {
		t.Fatalf("continue: %+v %v", r, err)
	}
}

func TestSyncBlocksOnAFailedFetch(t *testing.T) {
	s, _, c, _, g := setup()
	toRepos(t, s, c)
	ctx := context.Background()
	if _, err := s.SetRepos("GEEK-101", []string{"nope"}); err == nil {
		t.Fatal("unknown repo accepted")
	}
	s.SetRepos("GEEK-101", []string{"dating-backend", "admin", "admin"})
	g.fail["admin"] = true
	r, err := s.Sync(ctx, "GEEK-101")
	if err != nil {
		t.Fatal(err)
	}
	if r.Step != StepRepos || len(r.Synced) != 2 || r.Synced[0].Name != "admin" || r.Synced[0].Error == "" {
		t.Fatalf("blocked sync = %+v", r)
	}
	g.fail["admin"] = false
	if r, _ = s.Sync(ctx, "GEEK-101"); r.Step != StepQuestions {
		t.Fatalf("sync = %+v", r)
	}
}

func question(text string, options ...string) map[string]any {
	return map[string]any{"question": text, "options": options, "recommended": 1, "why": "because"}
}

// toQuestions runs the steps up to questions, with dating-backend picked.
func toQuestions(t *testing.T, s *Service, c *fakeClaude) {
	t.Helper()
	toRepos(t, s, c)
	s.SetRepos("GEEK-101", []string{"dating-backend"})
	if r, err := s.Sync(context.Background(), "GEEK-101"); err != nil || r.Step != StepQuestions {
		t.Fatalf("sync: %+v %v", r, err)
	}
}

func TestAskHoldsClaudeToTheLimitsAndEndsAfterTheLastRound(t *testing.T) {
	s, _, c, _, g := setup()
	many := []map[string]any{
		question("One option only?", "yes"),
		question("Q1?", "a", "b", "b", " "),
		question("Q2?", "a", "b"), question("Q3?", "a", "b"), question("Q4?", "a", "b"),
		question("Q5?", "a", "b"), question("Q6?", "a", "b"),
	}
	c.answers = []any{
		map[string]any{"questions": many},
		map[string]any{"questions": []map[string]any{question("Next?", "x", "y")}},
		map[string]any{"questions": []map[string]any{question("Last?", "x", "y")}},
	}
	toQuestions(t, s, c)
	ctx := context.Background()
	r, err := s.Ask(ctx, "GEEK-101", nil)
	if err != nil {
		t.Fatal(err)
	}
	qs := r.Rounds[0].Questions
	if len(qs) != MaxQuestions || qs[0].Text != "Q1?" || !slices.Equal(qs[0].Options, []string{"a", "b"}) || qs[0].Recommended != 1 {
		t.Fatalf("round 1 = %+v", qs)
	}
	first := c.calls[1]
	if first.Session != "" || first.Repos["dating-backend"] != "/w/dating-backend" ||
		!strings.Contains(first.Prompt, "Their work is delivered elsewhere") || !strings.Contains(first.Prompt, "GEEK-100: Integrate Stripe payments API") ||
		!strings.Contains(first.Prompt, "GEEK-112 (To Do): Add the admin payment page\nThe developer said this ticket does not build on it.") ||
		!strings.Contains(first.Prompt, "It overlaps this ticket (Both send purchase events).") ||
		strings.Contains(first.Prompt, "GEEK-100 (To Do)") {
		t.Fatalf("first round call = %+v", first)
	}
	if g.checkouts[0] != "abc123dating-backend /w/dating-backend" {
		t.Errorf("checkouts = %v", g.checkouts)
	}
	if _, err := s.Ask(ctx, "GEEK-101", nil); err == nil {
		t.Fatal("asked again with questions unanswered")
	}
	s.Answer("GEEK-101", Answers{Round: []string{"a", "b", "a", "b", "my own words"}})
	r, err = s.Ask(ctx, "GEEK-101", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.calls[2].Session != "s2" || !strings.Contains(c.calls[2].Prompt, `A: "my own words"`) || !strings.Contains(c.calls[2].Prompt, "round 2 of 3") {
		t.Fatalf("round 2 call = %+v", c.calls[2])
	}
	s.Answer("GEEK-101", Answers{Round: []string{"x"}})
	r, _ = s.Ask(ctx, "GEEK-101", nil)
	if len(r.Rounds) != 3 || !strings.Contains(c.calls[3].Prompt, "last round") {
		t.Fatalf("round 3 = %+v", r.Rounds)
	}
	s.Answer("GEEK-101", Answers{Round: []string{"y"}})
	r, _ = s.Ask(ctx, "GEEK-101", nil)
	if r.Step != StepDeploy || len(c.calls) != 4 {
		t.Fatalf("after round 3: step %s, %d calls", r.Step, len(c.calls))
	}
}

func TestAskMovesOnWhenClaudeHasNoQuestions(t *testing.T) {
	s, _, c, _, _ := setup()
	c.answers = []any{map[string]any{"questions": []any{}}}
	toQuestions(t, s, c)
	r, err := s.Ask(context.Background(), "GEEK-101", nil)
	if err != nil || r.Step != StepDeploy || len(r.Rounds) != 0 {
		t.Fatalf("r = %+v, err = %v", r, err)
	}
}

func TestRestartQuestionsAsksRoundOneFresh(t *testing.T) {
	s, _, c, _, _ := setup()
	c.answers = []any{
		map[string]any{"questions": []map[string]any{question("Bad?", "a", "b")}},
		map[string]any{"questions": []map[string]any{question("Better?", "a", "b")}},
	}
	toQuestions(t, s, c)
	ctx := context.Background()
	s.Ask(ctx, "GEEK-101", nil)
	r, err := s.RestartQuestions("GEEK-101")
	if err != nil || len(r.Rounds) != 0 || r.QuestionSession != "" {
		t.Fatalf("restart: %+v %v", r, err)
	}
	r, err = s.Ask(ctx, "GEEK-101", nil)
	if err != nil || len(r.Rounds) != 1 || r.Rounds[0].Questions[0].Text != "Better?" || c.calls[len(c.calls)-1].Session != "" {
		t.Fatalf("ask after restart: %+v %v", r, err)
	}
}

func TestChooseReposDropsWhatClaudeReadAndPicksAgain(t *testing.T) {
	s, _, c, _, _ := setup()
	c.answers = []any{map[string]any{"questions": []map[string]any{question("Which?", "a", "b")}}}
	toQuestions(t, s, c)
	s.Ask(context.Background(), "GEEK-101", nil)
	r, err := s.ChooseRepos("GEEK-101")
	if err != nil || r.Step != StepRepos || len(r.Rounds) != 0 || r.QuestionSession != "" || len(r.Synced) != 0 {
		t.Fatalf("choose repos: %+v %v", r, err)
	}
	if !slices.Equal(r.Repos, []string{"dating-backend"}) {
		t.Fatalf("picked repos should stay: %v", r.Repos)
	}
	if r, err = s.SetRepos("GEEK-101", []string{"admin"}); err != nil || !slices.Equal(r.Repos, []string{"admin"}) {
		t.Fatalf("pick again: %+v %v", r, err)
	}
	if _, err := s.ChooseRepos("GEEK-101"); !errors.Is(err, ErrStep) {
		t.Fatalf("already at repos: %v", err)
	}
}

func deployAnswer() map[string]any {
	return map[string]any{
		"repo_questions": []map[string]any{
			{"repo": "dating-backend", "question": "How does it reach prod?", "options": []string{"GitHub Actions", "Forge"}, "recommended": 0, "why": "workflow"},
			{"repo": "admin", "question": "Not picked", "options": []string{"a", "b"}, "recommended": 0, "why": ""},
		},
		"ticket_questions": []map[string]any{question("Who turns on charge.refunded?", "This ticket", "Already on")},
	}
}

var generated = map[string]any{
	"acceptance_criteria":      []string{"Support can refund a payment.", " "},
	"technical_implementation": []map[string]any{{"repo": "dating-backend", "files": []map[string]any{{"path": "app/Refund.php", "new": true}}}},
	"ratings":                  map[string]string{"novelty": "Medium", "risk": "Medium", "dependency": "Low", "testing": "Medium", "deployment": "Low"},
	"split": []map[string]any{{
		"title": "Refunds - Status", "acceptance_criteria": []string{"Status shows."}, "technical_implementation": []any{},
		"ratings": map[string]string{"novelty": "Low", "risk": "Low", "dependency": "Low", "testing": "N/A", "deployment": "Low"},
	}},
	"split_reason": " Status is its own behaviour a user can check. ",
	"deploy_needs": []string{"Turn on `charge.refunded` for the webhook."},
}

// toResults runs every step up to the results.
func toResults(t *testing.T, s *Service, c *fakeClaude) Refinement {
	t.Helper()
	c.answers = []any{map[string]any{"questions": []any{}}, deployAnswer(), generated}
	toQuestions(t, s, c)
	ctx := context.Background()
	s.Ask(ctx, "GEEK-101", nil)
	r, err := s.AskDeploy(ctx, "GEEK-101", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Deploy.Repos) != 1 || r.Deploy.Repos[0].Repo != "dating-backend" || len(r.Deploy.Ticket) != 1 {
		t.Fatalf("deploy = %+v", r.Deploy)
	}
	if _, err := s.Generate(ctx, "GEEK-101", nil); err == nil {
		t.Fatal("generated with deploy questions unanswered")
	}
	s.Answer("GEEK-101", Answers{Repos: []string{"GitHub Actions"}, Ticket: []string{"This ticket"}})
	r, err = s.Generate(ctx, "GEEK-101", nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestGenerateSavesTheProfileAndScoresTheResult(t *testing.T) {
	s, _, c, st, _ := setup()
	r := toResults(t, s, c)
	if r.Step != StepResults || r.Result.Points != 2 || r.Result.Split[0].Points != 1 ||
		r.Result.SplitReason != "Status is its own behaviour a user can check." ||
		!slices.Equal(r.Result.AcceptanceCriteria, []string{"Support can refund a payment."}) {
		t.Fatalf("result = %+v", r.Result)
	}
	if p := st.profiles["dating-backend"]; len(p.Answers) != 1 || p.Answers[0].Answer != "GitHub Actions" {
		t.Fatalf("profile = %+v", p)
	}
	gen := c.calls[len(c.calls)-1]
	if gen.Session != "" || !strings.Contains(gen.Prompt, "How dating-backend deploys:") || !strings.Contains(gen.Prompt, `A: "This ticket"`) {
		t.Fatalf("generate call = %s", gen.Prompt)
	}
	deploy := c.calls[len(c.calls)-2]
	if !strings.Contains(deploy.Prompt, "Repos with no saved deployment profile: dating-backend.") {
		t.Fatalf("deploy prompt = %s", deploy.Prompt)
	}
}

func TestEditKeepsPointsInStepWithTheRatings(t *testing.T) {
	s, _, c, _, _ := setup()
	r := toResults(t, s, c)
	edited := *r.Result
	edited.Ratings.Novelty, edited.Ratings.Risk = "High", "High"
	edited.Points = 99
	edited.Split = []Child{{Title: "Refunds - Status and history", Points: 7}}
	r, err := s.Edit("GEEK-101", edited)
	if err != nil {
		t.Fatal(err)
	}
	if r.Result.Points != 2 || r.Result.Split[0].Title != "Refunds - Status and history" || r.Result.Split[0].Points != 1 {
		t.Fatalf("edited = %+v", r.Result)
	}
}

func TestRewriteReplacesOnlyTheCriteriaAndFiles(t *testing.T) {
	s, _, c, _, _ := setup()
	s.FormatDir = t.TempDir()
	r := toResults(t, s, c)
	if rules := c.calls[len(c.calls)-1].Rules; !strings.Contains(rules, "## Acceptance Criteria") || !strings.Contains(rules, "## Technical Implementation") {
		t.Fatal("generate rules lack the default formats")
	}
	if err := s.SetFormat(FormatAC, "## Acceptance Criteria\nGiven/When/Then, one per line.\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c.answers = []any{map[string]any{"acceptance_criteria": []string{"Given a payment, when support refunds it, then it shows refunded."},
		"technical_implementation": []any{}, "split": []any{}}}
	if _, err := s.Rewrite(ctx, "GEEK-101", nil); err == nil || !strings.Contains(err.Error(), "1") {
		t.Fatalf("rewrite with a split ticket missing: %v", err)
	}
	c.answers = []any{map[string]any{
		"acceptance_criteria":      []string{"Given a payment, when support refunds it, then it shows refunded."},
		"technical_implementation": []map[string]any{{"repo": "dating-backend", "files": []map[string]any{{"path": "app/Refunds.php", "new": true}}}},
		"split":                    []map[string]any{{"acceptance_criteria": []string{"Given a refund, then its status shows."}, "technical_implementation": []any{}}},
	}}
	after, err := s.Rewrite(ctx, "GEEK-101", nil)
	if err != nil {
		t.Fatal(err)
	}
	call := c.calls[len(c.calls)-1]
	if !strings.Contains(call.Rules, "Given/When/Then, one per line.") || !strings.Contains(call.Rules, "## Technical Implementation") || !strings.Contains(call.Prompt, "Support can refund a payment.") || !strings.Contains(call.Prompt, `"Refunds - Status"`) {
		t.Fatalf("rewrite call = %+v", call)
	}
	res := after.Result
	if res.AcceptanceCriteria[0] != "Given a payment, when support refunds it, then it shows refunded." || res.TechnicalImplementation[0].Files[0].Path != "app/Refunds.php" ||
		res.Split[0].AcceptanceCriteria[0] != "Given a refund, then its status shows." || res.Split[0].Title != "Refunds - Status" ||
		res.Ratings != r.Result.Ratings || res.Points != r.Result.Points || !slices.Equal(res.DeployNeeds, r.Result.DeployNeeds) {
		t.Fatalf("rewritten = %+v", res)
	}
	if err := s.SetFormat(FormatAC, ""); err != nil {
		t.Fatal(err)
	}
	if text, _ := s.Format(FormatAC); text != DefaultFormats[FormatAC] {
		t.Fatalf("reset format = %q", text)
	}
}

func TestStampWritesInOrderAndRetriesFromTheFailure(t *testing.T) {
	s, j, c, _, _ := setup()
	toResults(t, s, c)
	ctx := context.Background()
	j.failOn = "relate"
	r, err := s.Stamp(ctx, "GEEK-101")
	if err != nil {
		t.Fatal(err)
	}
	if r.Step != StepResults || r.Stamp.Failed != "link:GEEK-100" || !strings.Contains(r.Stamp.Error, "jira refused") || r.Label() != "Stamping" {
		t.Fatalf("failed stamp = %+v", r.Stamp)
	}
	j.failOn = ""
	r, err = s.Stamp(ctx, "GEEK-101")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"update GEEK-101 2",
		"comment GEEK-101",
		"create Refunds - Status",
		"relate GEEK-101 GEEK-100",
		"move GEEK-101 Ready for Sprint - Team John",
	}
	if !slices.Equal(j.writes, want) || r.Step != StepStamped || r.Stamp.Children[0] != "GEEK-901" {
		t.Fatalf("writes = %q, stamp = %+v", j.writes, r.Stamp)
	}
}

func TestListPinsStartedTicketsAndShowsBlocking(t *testing.T) {
	s, j, c, _, _ := setup()
	toRepos(t, s, c)
	j.tickets["GEEK-140"] = jira.Ticket{Issue: jira.Issue{Key: "GEEK-140", Summary: "Email"}, Links: []jira.Link{
		{Type: "Blocks", Relation: "is blocked by", Key: "GEEK-101"},
	}}
	j.list = []string{"GEEK-140", "GEEK-100", "GEEK-112", "GEEK-101"}
	s.Hidden = func(key string) bool { return key == "GEEK-112" }
	page, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, row := range page.Rows {
		got = append(got, fmt.Sprintf("%s %v %s %v %v", row.Key, row.Started, row.Step, row.BlockedBy, row.Blocks))
	}
	want := []string{
		"GEEK-101 true Choose repos [GEEK-100] [GEEK-140]",
		"GEEK-140 false Not started [GEEK-101] []",
	}
	if !slices.Equal(got, want) || page.User != "Dev" {
		t.Fatalf("rows = %q", got)
	}
}

func TestListReadsTheReadySprintWithPoints(t *testing.T) {
	s, j, _, _, _ := setup()
	two := 2.0
	j.tickets["GEEK-150"] = jira.Ticket{Issue: jira.Issue{Key: "GEEK-150", Summary: "Track", Status: "To Do"}, Points: &two}
	j.tickets["GEEK-151"] = jira.Ticket{Issue: jira.Issue{Key: "GEEK-151", Summary: "SDK", Status: "To Do"}}
	j.ready = []string{"GEEK-150", "GEEK-151"}
	page, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Ready) != 2 || page.Ready[0].Key != "GEEK-150" || *page.Ready[0].Points != 2 || page.Ready[1].Points != nil {
		t.Fatalf("ready = %+v", page.Ready)
	}
	if !slices.Contains(j.jqls, jira.ReadyJQL("Ready for Sprint - Team John")) {
		t.Fatalf("jqls = %q", j.jqls)
	}
}

func TestPlanListsEveryJiraWrite(t *testing.T) {
	r := Refinement{Key: "GEEK-1", Result: &Result{Points: 1, Split: []Child{{Title: "A - B"}}}}
	r.Related.Blockers = []Blocker{{Key: "GEEK-2", Decision: Accepted}, {Key: "GEEK-3", Decision: Rejected}}
	var ids []string
	for _, a := range Plan(r) {
		ids = append(ids, a.ID)
	}
	if !slices.Equal(ids, []string{"write", "child:0", "link:GEEK-2", "move"}) {
		t.Fatalf("plan = %v", ids)
	}
}

func TestFieldsFollowJiraLayout(t *testing.T) {
	f := fields([]string{"One.", "Two."}, []RepoFiles{{Repo: "a", Files: []File{{Path: "x.go", New: true}, {Path: "y.go"}}}},
		[]string{"Soft delete, keep 30 days"},
		Ratings{Novelty: "Medium", Risk: "Medium", Dependency: "Low", Testing: "Medium", Deployment: "Low"}, 2)
	if f.AcceptanceCriteria != "* One.\n* Two." || f.TechnicalImplementation != "a\n\n* x.go (new)\n* y.go\n\n---\n\n* Soft delete, keep 30 days" ||
		!strings.HasSuffix(f.StoryPointCriteria, "Deployment Complexity: Low\nTotal: Medium (1.6)") {
		t.Fatalf("fields = %+v", f)
	}
	if got := DeployComment([]string{"Set `KEY` on prod"}); !strings.Contains(got, "* Set {{KEY}} on prod") {
		t.Fatalf("comment = %q", got)
	}
}

func TestBlockerFoundInTheCodeWaitsForTheDeveloper(t *testing.T) {
	s, _, c, _, _ := setup()
	withBlockers := func(qs []map[string]any, keys ...string) map[string]any {
		var bs []map[string]string
		for _, k := range keys {
			bs = append(bs, map[string]string{"key": k, "reason": "uses its SDK"})
		}
		return map[string]any{"questions": qs, "blockers": bs}
	}
	c.answers = []any{
		// GEEK-100 is accepted already and GEEK-101 is the ticket itself.
		withBlockers([]map[string]any{question("Q1?", "a", "b")}, "GEEK-119", "GEEK-100", "GEEK-101", "GEEK-404"),
		withBlockers(nil),
	}
	toQuestions(t, s, c)
	ctx := context.Background()
	r, err := s.Ask(ctx, "GEEK-101", nil)
	if err != nil {
		t.Fatal(err)
	}
	pending := r.Related.Pending()
	if len(pending) != 1 || pending[0].Key != "GEEK-119" || !pending[0].FromCode || len(r.Rounds) != 1 {
		t.Fatalf("pending = %+v, rounds = %d", pending, len(r.Rounds))
	}
	s.Answer("GEEK-101", Answers{Round: []string{"a"}})
	if _, err := s.Ask(ctx, "GEEK-101", nil); err == nil || !strings.Contains(err.Error(), "GEEK-119") {
		t.Fatalf("asked on with a pending blocker: %v", err)
	}
	if _, err := s.Decide("GEEK-101", "GEEK-100", Rejected); err == nil {
		t.Fatal("changed a blocker decided in the related step")
	}

	// Accepting it goes back through the related step and starts the questions over.
	r, err = s.Decide("GEEK-101", "GEEK-119", Accepted)
	if err != nil {
		t.Fatal(err)
	}
	if r.Step != StepRelated || len(r.Rounds) != 0 || r.QuestionSession != "" || len(r.Repos) != 1 {
		t.Fatalf("after accepting = %+v", r)
	}
	r, err = s.Continue(ctx, "GEEK-101")
	if err != nil || r.Step != StepRelated || !slices.Equal(r.Related.Waiting, []string{"GEEK-119"}) {
		t.Fatalf("GEEK-119 is not refined, so the ticket waits: %+v %v", r.Related, err)
	}
}

func TestRejectingABlockerFoundInTheCodeCarriesOn(t *testing.T) {
	s, _, c, _, _ := setup()
	c.answers = []any{
		map[string]any{"questions": []any{}, "blockers": []map[string]string{{"key": "GEEK-119", "reason": "r"}}},
		map[string]any{"questions": []map[string]any{question("Q1?", "a", "b")}, "blockers": []any{}},
	}
	toQuestions(t, s, c)
	ctx := context.Background()
	r, err := s.Ask(ctx, "GEEK-101", nil)
	if err != nil || r.Step != StepQuestions || len(r.Rounds) != 0 {
		t.Fatalf("a round with only a blocker stays on questions: %+v %v", r, err)
	}
	s.Decide("GEEK-101", "GEEK-119", Rejected)
	r, err = s.Ask(ctx, "GEEK-101", nil)
	if err != nil || len(r.Rounds) != 1 {
		t.Fatalf("after rejecting: %+v %v", r, err)
	}
}

func TestSuggestMatchesTitleAndLabelsToRepoNamesOnce(t *testing.T) {
	s, j, c, st, _ := setup()
	toRepos(t, s, c)
	tk := j.tickets["GEEK-101"]
	tk.Labels = []string{"Dating_Brands"}
	j.tickets["GEEK-101"] = tk
	c.answers = []any{map[string]any{"repos": []map[string]string{
		{"repo": "admin", "reason": " title names the admin page "},
		{"repo": "nope", "reason": "made up"},
		{"repo": "admin", "reason": "again"},
		{"repo": "dating-backend", "reason": "label Dating_Brands"},
	}}}
	ctx := context.Background()
	r, err := s.Suggest(ctx, "GEEK-101")
	if err != nil {
		t.Fatal(err)
	}
	want := []SuggestedRepo{{Repo: "admin", Reason: "title names the admin page"}, {Repo: "dating-backend", Reason: "label Dating_Brands"}}
	if r.Suggestion == nil || !slices.Equal(r.Suggestion.Repos, want) {
		t.Fatalf("suggestion = %+v", r.Suggestion)
	}
	call := c.calls[len(c.calls)-1]
	if len(call.Repos) != 0 || !strings.Contains(call.Prompt, "Labels: Dating_Brands.") || !strings.Contains(call.Prompt, "- admin\n- costs\n- dating-backend\n") {
		t.Errorf("prompt = %s", call.Prompt)
	}
	if strings.Contains(call.Prompt, "description") {
		t.Errorf("prompt has the description: %s", call.Prompt)
	}
	calls := len(c.calls)
	if _, err := s.Suggest(ctx, "GEEK-101"); err != nil || len(c.calls) != calls {
		t.Fatalf("asked again: %v, %d calls", err, len(c.calls))
	}

	// A repo a related ticket picked comes first and is not repeated.
	st.refinements["GEEK-100"] = Refinement{Key: "GEEK-100", Repos: []string{"dating-backend"}}
	v, err := s.Describe("GEEK-101")
	if err != nil {
		t.Fatal(err)
	}
	merged := []SuggestedRepo{{Repo: "dating-backend", Reason: "used by a related ticket"}, {Repo: "admin", Reason: "title names the admin page"}}
	if !slices.Equal(v.Suggested, merged) {
		t.Errorf("suggested = %+v", v.Suggested)
	}
}

func TestSuggestOnlyAtTheReposStep(t *testing.T) {
	s, _, c, _, _ := setup()
	c.answers = []any{related}
	ctx := context.Background()
	s.Search(ctx, "GEEK-101", "", nil)
	if _, err := s.Suggest(ctx, "GEEK-101"); !errors.Is(err, ErrStep) {
		t.Fatalf("err = %v", err)
	}
}
