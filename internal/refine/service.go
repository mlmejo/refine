package refine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mlmejo/refine/internal/claude"
	"github.com/mlmejo/refine/internal/gitsync"
	"github.com/mlmejo/refine/internal/jira"
)

// Jira is what refine reads from and writes to Jira.
type Jira interface {
	Myself(ctx context.Context) (string, error)
	Linked(ctx context.Context, jql string) ([]jira.Ticket, error)
	Tickets(ctx context.Context, jql string) ([]jira.Ticket, error)
	GetTicket(ctx context.Context, key string) (jira.Ticket, error)
	Update(ctx context.Context, key string, r jira.Refinement) error
	Comment(ctx context.Context, key, body string) error
	Split(ctx context.Context, parent, sprint string) (jira.SplitTarget, error)
	CreateChild(ctx context.Context, target jira.SplitTarget, child jira.NewChild) (string, error)
	Relate(ctx context.Context, from, to string) error
	MoveToSprint(ctx context.Context, key, sprint string) error
}

// Claude runs one step.
type Claude interface {
	Do(ctx context.Context, call claude.Call, into any, progress func(claude.Activity)) (claude.Run, error)
}

// Store keeps refinements and the repos' deployment profiles.
type Store interface {
	Load(key string) (Refinement, bool, error)
	All() ([]Refinement, error)
	Save(r Refinement) error
	Delete(key string) error
	Profiles() (map[string]Profile, error)
	SaveProfile(p Profile) error
}

// Git fetches a repo and checks a commit out for Claude to read.
type Git interface {
	Fetch(ctx context.Context, name, dir string) gitsync.Status
	Checkout(ctx context.Context, repo, commit, dest string) error
}

// Service runs refinements.
type Service struct {
	Jira   Jira
	Claude Claude
	Store  Store
	Git    Git
	// Sprint is the sprint tickets are refined from, ReadySprint the one a
	// stamped ticket moves into.
	Sprint      string
	ReadySprint string
	// Repos are the configured repos by name, read when called so a change
	// applies to the next step.
	Repos func() map[string]string
	// Worktrees is where each repo's commit is checked out for Claude.
	Worktrees string
	// Hidden reports tickets the developer hid from the list.
	Hidden func(key string) bool
	// WriteupPath is the markdown file of rules for Acceptance Criteria and
	// Technical Implementation. Empty, or a missing file, means the defaults.
	WriteupPath string

	// mu keeps a refinement's read-change-save from racing another.
	mu sync.Mutex
}

// ErrStep is returned for an action the refinement's step does not allow.
var ErrStep = errors.New("not at this step")

// Row is one ticket in the Tickets list.
type Row struct {
	Key       string   `json:"key"`
	Summary   string   `json:"summary"`
	Status    string   `json:"status"`
	Started   bool     `json:"started"`
	Step      string   `json:"step"`
	BlockedBy []string `json:"blocked_by"`
	Blocks    []string `json:"blocks"`
	Updated   string   `json:"updated,omitempty"`
}

// Page is the Tickets list.
type Page struct {
	User string `json:"user"`
	Rows []Row  `json:"rows"`
}

// List reads the unrefined tickets in the sprint: started ones first, then
// the rest, each in Jira's order, with how they block one another.
func (s *Service) List(ctx context.Context) (Page, error) {
	user, err := s.Jira.Myself(ctx)
	if err != nil {
		return Page{}, err
	}
	tickets, err := s.Jira.Linked(ctx, jira.ListJQL(s.Sprint))
	if err != nil {
		return Page{}, err
	}
	all, err := s.Store.All()
	if err != nil {
		return Page{}, err
	}
	records := map[string]Refinement{}
	for _, r := range all {
		records[r.Key] = r
	}

	blockedBy := map[string][]string{}
	add := func(key, blocker string) {
		if !slices.Contains(blockedBy[key], blocker) {
			blockedBy[key] = append(blockedBy[key], blocker)
		}
	}
	for _, t := range tickets {
		for _, l := range t.Links {
			switch {
			case l.Type == "Blocks" && l.Relation == "is blocked by":
				add(t.Key, l.Key)
			case l.Type == "Blocks" && l.Relation == "blocks":
				add(l.Key, t.Key)
			}
		}
	}
	for _, r := range all {
		for _, b := range r.Related.Accepted() {
			add(r.Key, b.Key)
		}
	}
	blocks := map[string][]string{}
	for key, blockers := range blockedBy {
		for _, b := range blockers {
			blocks[b] = append(blocks[b], key)
		}
	}

	var started, rest []Row
	for _, t := range tickets {
		if t.Refined() || (s.Hidden != nil && s.Hidden(t.Key)) {
			continue
		}
		row := Row{Key: t.Key, Summary: t.Summary, Status: t.Status, Step: "Not started",
			BlockedBy: sorted(blockedBy[t.Key]), Blocks: sorted(blocks[t.Key])}
		if r, ok := records[t.Key]; ok {
			row.Started, row.Step = true, r.Label()
			row.Updated = r.Updated.UTC().Format(time.RFC3339)
			started = append(started, row)
			continue
		}
		rest = append(rest, row)
	}
	return Page{User: user, Rows: append(started, rest...)}, nil
}

func sorted(keys []string) []string {
	out := slices.Clone(keys)
	slices.Sort(out)
	if out == nil {
		return []string{}
	}
	return out
}

// View is a ticket with its refinement.
type View struct {
	Refinement Refinement      `json:"refinement"`
	Ticket     TicketInfo      `json:"ticket"`
	Started    bool            `json:"started"`
	Suggested  []SuggestedRepo `json:"suggested_repos"`
	Profiles   []Profile       `json:"profiles"`
	Plan       []Action        `json:"stamp_plan"`
}

// TicketInfo is the ticket as Jira has it, for the app to show.
type TicketInfo struct {
	Key           string      `json:"key"`
	Summary       string      `json:"summary"`
	Status        string      `json:"status"`
	DescriptionMD string      `json:"description_md"`
	Links         []jira.Link `json:"links"`
}

// Open reads a ticket and its refinement. A ticket waiting on blockers has
// them checked in Jira again, since they may have been refined since.
func (s *Service) Open(ctx context.Context, key string) (View, error) {
	t, err := s.Jira.GetTicket(ctx, key)
	if err != nil {
		return View{}, err
	}
	s.mu.Lock()
	r, started, err := s.Store.Load(key)
	s.mu.Unlock()
	if err != nil {
		return View{}, err
	}
	if !started {
		r = New(key, t.Summary)
	}
	if started && r.Step == StepRelated && len(r.Related.Waiting) > 0 {
		if r, err = s.check(ctx, key); err != nil {
			return View{}, err
		}
	}
	return s.view(t, r, started)
}

func (s *Service) view(t jira.Ticket, r Refinement, started bool) (View, error) {
	profiles, err := s.Store.Profiles()
	if err != nil {
		return View{}, err
	}
	v := View{
		Refinement: r,
		Ticket: TicketInfo{Key: t.Key, Summary: t.Summary, Status: t.Status,
			DescriptionMD: t.DescriptionMarkdown, Links: t.Links},
		Started:   started,
		Suggested: s.suggested(r),
		Plan:      Plan(r),
	}
	for _, name := range r.Repos {
		if p, ok := profiles[name]; ok {
			v.Profiles = append(v.Profiles, p)
		}
	}
	return v, nil
}

// suggested are the repos picked for the tickets this one relates to, then
// those Claude matched to the ticket's title and labels.
func (s *Service) suggested(r Refinement) []SuggestedRepo {
	var keys []string
	for _, b := range r.Related.Blockers {
		if b.Decision != Rejected {
			keys = append(keys, b.Key)
		}
	}
	for _, o := range r.Related.Overlaps {
		keys = append(keys, o.Key)
	}
	var related []string
	for _, key := range keys {
		other, ok, err := s.Store.Load(key)
		if err != nil || !ok {
			continue
		}
		for _, repo := range other.Repos {
			if !slices.Contains(related, repo) {
				related = append(related, repo)
			}
		}
	}
	slices.Sort(related)
	var out []SuggestedRepo
	for _, repo := range related {
		out = append(out, SuggestedRepo{Repo: repo, Reason: "used by a related ticket"})
	}
	if r.Suggestion != nil {
		for _, sr := range r.Suggestion.Repos {
			if !slices.Contains(related, sr.Repo) {
				out = append(out, sr)
			}
		}
	}
	return out
}

// update loads a refinement, lets change edit it, and saves it.
func (s *Service) update(key string, change func(*Refinement) error) (Refinement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok, err := s.Store.Load(key)
	if err != nil {
		return Refinement{}, err
	}
	if !ok {
		return Refinement{}, fmt.Errorf("%s has no refinement", key)
	}
	if err := change(&r); err != nil {
		return Refinement{}, err
	}
	r.Updated = time.Now()
	return r, s.Store.Save(r)
}

// Search asks Claude which of the developer's open tickets this one builds
// on or overlaps. An explanation from the developer goes to Claude with the
// earlier ones. Decisions on blockers found again are kept.
func (s *Service) Search(ctx context.Context, key, explanation string, progress func(claude.Activity)) (Refinement, error) {
	t, err := s.Jira.GetTicket(ctx, key)
	if err != nil {
		return Refinement{}, err
	}
	s.mu.Lock()
	r, ok, err := s.Store.Load(key)
	s.mu.Unlock()
	if err != nil {
		return Refinement{}, err
	}
	if !ok {
		r = New(key, t.Summary)
	}
	if r.Step != StepRelated {
		return Refinement{}, fmt.Errorf("related tickets: %w", ErrStep)
	}
	candidates, err := s.Jira.Tickets(ctx, jira.CandidatesJQL(key, s.Sprint, s.ReadySprint))
	if err != nil {
		return Refinement{}, fmt.Errorf("read your other tickets: %w", err)
	}
	explanations := slices.Clone(r.Related.Explanations)
	if e := strings.TrimSpace(explanation); e != "" {
		explanations = append(explanations, e)
	}

	var out struct {
		Blockers []struct{ Key, Reason, Quote string } `json:"blockers"`
		Overlaps []struct{ Key, Reason string }        `json:"overlaps"`
	}
	run, err := s.Claude.Do(ctx, claude.Call{
		Prompt: relatedPrompt(t, candidates, explanations),
		Rules:  relatedRules,
		Schema: relatedSchema,
	}, &out, progress)
	if err != nil {
		return Refinement{}, err
	}

	byKey := map[string]jira.Ticket{}
	for _, c := range candidates {
		byKey[c.Key] = c
	}
	decided := map[string]string{}
	for _, b := range r.Related.Blockers {
		decided[b.Key] = b.Decision
	}
	related := Related{Searched: true, Explanations: explanations, Blockers: []Blocker{}, Overlaps: []Overlap{}}
	seen := map[string]bool{key: true}
	for _, b := range out.Blockers {
		c, ok := byKey[b.Key]
		if !ok || seen[b.Key] {
			continue
		}
		seen[b.Key] = true
		related.Blockers = append(related.Blockers, Blocker{Key: c.Key, Summary: c.Summary, Reason: b.Reason,
			Quote: b.Quote, Refined: c.Refined(), Decision: decided[c.Key]})
	}
	for _, o := range out.Overlaps {
		c, ok := byKey[o.Key]
		if !ok || seen[o.Key] {
			continue
		}
		seen[o.Key] = true
		related.Overlaps = append(related.Overlaps, Overlap{Key: c.Key, Summary: c.Summary, Reason: o.Reason})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	r.Summary, r.Related = t.Summary, related
	r.CostUSD += run.CostUSD
	r.Updated = time.Now()
	return r, s.Store.Save(r)
}

// Decide accepts or rejects a blocker. A blocker Claude found while reading
// the code is decided on the questions step; accepting it takes the ticket
// back through the related step, since the questions so far did not build on
// that ticket's work.
func (s *Service) Decide(key, blocker, decision string) (Refinement, error) {
	if decision != Accepted && decision != Rejected {
		return Refinement{}, fmt.Errorf("decision %q is not %s or %s", decision, Accepted, Rejected)
	}
	return s.update(key, func(r *Refinement) error {
		i := slices.IndexFunc(r.Related.Blockers, func(b Blocker) bool { return b.Key == blocker })
		if i < 0 {
			return fmt.Errorf("%s is not a blocker of %s", blocker, key)
		}
		b := &r.Related.Blockers[i]
		switch {
		case r.Step == StepRelated:
			b.Decision = decision
			r.Related.Waiting = nil
		case r.Step == StepQuestions && b.FromCode && b.Decision == "":
			b.Decision = decision
			if decision == Accepted {
				r.Step = StepRelated
				r.Related.Waiting = nil
				r.Rounds, r.QuestionSession = nil, ""
			}
		default:
			return fmt.Errorf("blockers: %w", ErrStep)
		}
		return nil
	})
}

// Continue moves on to choosing repos once every blocker is decided and every
// accepted one is refined in Jira. Otherwise the ticket waits, and Waiting
// names the blockers still unrefined.
func (s *Service) Continue(ctx context.Context, key string) (Refinement, error) {
	r, ok, err := s.Store.Load(key)
	if err != nil {
		return Refinement{}, err
	}
	if !ok || r.Step != StepRelated || !r.Related.Searched {
		return Refinement{}, fmt.Errorf("continue: %w", ErrStep)
	}
	if n := r.Related.Undecided(); n > 0 {
		return Refinement{}, fmt.Errorf("accept or reject %d more blocker%s first", n, plural(n))
	}
	return s.check(ctx, key)
}

// check reads each accepted blocker from Jira and moves on when all are
// refined.
func (s *Service) check(ctx context.Context, key string) (Refinement, error) {
	r, _, err := s.Store.Load(key)
	if err != nil {
		return Refinement{}, err
	}
	var waiting []string
	for _, b := range r.Related.Accepted() {
		t, err := s.Jira.GetTicket(ctx, b.Key)
		if err != nil {
			return Refinement{}, fmt.Errorf("read blocker %s: %w", b.Key, err)
		}
		if !t.Refined() {
			waiting = append(waiting, b.Key)
		}
	}
	return s.update(key, func(r *Refinement) error {
		r.Related.Waiting = waiting
		for i, b := range r.Related.Blockers {
			r.Related.Blockers[i].Refined = !slices.Contains(waiting, b.Key)
		}
		if len(waiting) == 0 {
			r.Step = StepRepos
		}
		return nil
	})
}

// SetRepos records the repos picked for the ticket.
func (s *Service) SetRepos(key string, repos []string) (Refinement, error) {
	configured := s.Repos()
	for _, name := range repos {
		if _, ok := configured[name]; !ok {
			return Refinement{}, fmt.Errorf("no repo named %s in the config", name)
		}
	}
	return s.update(key, func(r *Refinement) error {
		if r.Step != StepRepos {
			return fmt.Errorf("repos: %w", ErrStep)
		}
		r.Repos = slices.Sorted(slices.Values(slices.Compact(slices.Sorted(slices.Values(repos)))))
		r.Synced = nil
		return nil
	})
}

// Suggest asks Claude which configured repos the ticket's title and labels
// point at, once per refinement. Claude reads only the repos' names.
func (s *Service) Suggest(ctx context.Context, key string) (Refinement, error) {
	s.mu.Lock()
	r, ok, err := s.Store.Load(key)
	s.mu.Unlock()
	if err != nil {
		return Refinement{}, err
	}
	if !ok || r.Step != StepRepos {
		return Refinement{}, fmt.Errorf("suggest repos: %w", ErrStep)
	}
	if r.Suggestion != nil {
		return r, nil
	}
	repos := slices.Sorted(maps.Keys(s.Repos()))
	suggestion := Suggestion{Repos: []SuggestedRepo{}}
	var cost float64
	if len(repos) > 0 {
		t, err := s.Jira.GetTicket(ctx, key)
		if err != nil {
			return Refinement{}, err
		}
		var out struct {
			Repos []SuggestedRepo `json:"repos"`
		}
		run, err := s.Claude.Do(ctx, claude.Call{
			Prompt: suggestPrompt(t, repos),
			Rules:  suggestRules,
			Schema: suggestSchema,
		}, &out, nil)
		if err != nil {
			return Refinement{}, err
		}
		cost = run.CostUSD
		for _, sr := range out.Repos {
			if !slices.Contains(repos, sr.Repo) || slices.ContainsFunc(suggestion.Repos, func(o SuggestedRepo) bool { return o.Repo == sr.Repo }) {
				continue
			}
			suggestion.Repos = append(suggestion.Repos, SuggestedRepo{Repo: sr.Repo, Reason: strings.TrimSpace(sr.Reason)})
			if len(suggestion.Repos) == MaxSuggestedRepos {
				break
			}
		}
	}
	return s.update(key, func(r *Refinement) error {
		r.Suggestion = &suggestion
		r.CostUSD += cost
		return nil
	})
}

// ChooseRepos goes back to the repos step from a later one, so the developer
// can pick again. What Claude read from the old repos is dropped: the rounds
// and their session, the deployment and the result. Decided blockers stay
// decided. Once stamping has begun the repos are fixed.
func (s *Service) ChooseRepos(key string) (Refinement, error) {
	return s.update(key, func(r *Refinement) error {
		switch r.Step {
		case StepQuestions, StepDeploy, StepResults:
		default:
			return fmt.Errorf("repos: %w", ErrStep)
		}
		if len(r.Stamp.Done) > 0 {
			return fmt.Errorf("repos: %w", ErrStep)
		}
		r.Step = StepRepos
		r.Synced, r.Rounds, r.QuestionSession, r.Deploy, r.Result = nil, nil, "", nil, nil
		return nil
	})
}

// Sync fetches each picked repo from origin. When every fetch works, the
// refinement moves on to questions; a repo that cannot be fetched blocks it.
func (s *Service) Sync(ctx context.Context, key string) (Refinement, error) {
	r, ok, err := s.Store.Load(key)
	if err != nil {
		return Refinement{}, err
	}
	if !ok || r.Step != StepRepos {
		return Refinement{}, fmt.Errorf("sync: %w", ErrStep)
	}
	if len(r.Repos) == 0 {
		return Refinement{}, errors.New("pick at least one repo")
	}
	configured := s.Repos()
	statuses := make([]gitsync.Status, len(r.Repos))
	var wg sync.WaitGroup
	for i, name := range r.Repos {
		wg.Go(func() {
			dir, ok := configured[name]
			if !ok {
				statuses[i] = gitsync.Status{Name: name, Error: "no longer in the config"}
				return
			}
			statuses[i] = s.Git.Fetch(ctx, name, dir)
		})
	}
	wg.Wait()
	return s.update(key, func(r *Refinement) error {
		r.Synced = statuses
		if !slices.ContainsFunc(statuses, func(st gitsync.Status) bool { return st.Error != "" }) {
			r.Step = StepQuestions
		}
		return nil
	})
}

// checkout puts each repo's synced commit in its worktree and returns the
// directories Claude reads.
func (s *Service) checkout(ctx context.Context, r Refinement) (map[string]string, error) {
	configured := s.Repos()
	dirs := map[string]string{}
	for _, name := range r.Repos {
		st, ok := r.Commit(name)
		if !ok {
			return nil, fmt.Errorf("%s has not been fetched", name)
		}
		dir, ok := configured[name]
		if !ok {
			return nil, fmt.Errorf("%s is no longer in the config", name)
		}
		dest := filepath.Join(s.Worktrees, name)
		if err := s.Git.Checkout(ctx, dir, st.Commit, dest); err != nil {
			return nil, err
		}
		dirs[name] = dest
	}
	return dirs, nil
}

// blockers reads the accepted blockers from Jira, with their fields as they
// are now.
func (s *Service) blockers(ctx context.Context, r Refinement) ([]jira.Ticket, error) {
	var out []jira.Ticket
	for _, b := range r.Related.Accepted() {
		t, err := s.Jira.GetTicket(ctx, b.Key)
		if err != nil {
			return nil, fmt.Errorf("read blocker %s: %w", b.Key, err)
		}
		out = append(out, t)
	}
	return out, nil
}

// Ask runs the next round of questions. The first round reads the code
// fresh; later ones continue its session with the answers. A round with no
// questions, or the end of the last round, moves on to deployment.
func (s *Service) Ask(ctx context.Context, key string, progress func(claude.Activity)) (Refinement, error) {
	r, ok, err := s.Store.Load(key)
	if err != nil {
		return Refinement{}, err
	}
	if !ok || r.Step != StepQuestions {
		return Refinement{}, fmt.Errorf("questions: %w", ErrStep)
	}
	if n := len(r.Rounds); n > 0 && !Answered(r.Rounds[n-1].Questions) {
		return Refinement{}, errors.New("answer every question first")
	}
	if pending := r.Related.Pending(); len(pending) > 0 {
		return Refinement{}, fmt.Errorf("accept or reject %s first", pending[0].Key)
	}
	if len(r.Rounds) >= MaxRounds {
		return s.update(key, func(r *Refinement) error {
			r.Step = StepDeploy
			return nil
		})
	}
	dirs, err := s.checkout(ctx, r)
	if err != nil {
		return Refinement{}, err
	}
	call := claude.Call{Rules: questionsRules, Schema: questionsSchema, Repos: dirs}
	if len(r.Rounds) == 0 || r.QuestionSession == "" {
		t, err := s.Jira.GetTicket(ctx, key)
		if err != nil {
			return Refinement{}, err
		}
		blockers, err := s.blockers(ctx, r)
		if err != nil {
			return Refinement{}, err
		}
		candidates, err := s.Jira.Tickets(ctx, jira.CandidatesJQL(key, s.Sprint, s.ReadySprint))
		if err != nil {
			return Refinement{}, fmt.Errorf("read your other tickets: %w", err)
		}
		call.Prompt = firstRoundPrompt(t, blockers, candidates, r, dirs)
	} else {
		call.Prompt, call.Session = nextRoundPrompt(r), r.QuestionSession
	}
	var out struct {
		Questions []struct {
			Question    string   `json:"question"`
			Options     []string `json:"options"`
			Recommended int      `json:"recommended"`
			Why         string   `json:"why"`
		} `json:"questions"`
		Blockers []struct{ Key, Reason string } `json:"blockers"`
	}
	run, err := s.Claude.Do(ctx, call, &out, progress)
	if err != nil {
		return Refinement{}, err
	}
	found := s.found(ctx, key, r, out.Blockers)
	var qs []Question
	for _, q := range out.Questions {
		qs = append(qs, Question{Text: q.Question, Options: q.Options, Recommended: q.Recommended, Why: q.Why})
	}
	qs = normalize(qs, MaxQuestions)
	return s.update(key, func(r *Refinement) error {
		r.CostUSD += run.CostUSD
		if run.SessionID != "" {
			r.QuestionSession = run.SessionID
		}
		r.Related.Blockers = append(r.Related.Blockers, found...)
		if len(qs) == 0 && len(found) == 0 {
			r.Step = StepDeploy
			return nil
		}
		if len(qs) == 0 {
			return nil
		}
		r.Rounds = append(r.Rounds, Round{Questions: qs})
		return nil
	})
}

// RestartQuestions drops every round and the session behind them, so the
// next Ask reads the code fresh. Decided blockers stay decided.
func (s *Service) RestartQuestions(key string) (Refinement, error) {
	return s.update(key, func(r *Refinement) error {
		if r.Step != StepQuestions {
			return fmt.Errorf("questions: %w", ErrStep)
		}
		r.Rounds, r.QuestionSession = nil, ""
		return nil
	})
}

// found turns the blockers Claude reported while reading the code into
// blockers for the developer to decide: only other open tickets, and none
// already decided or proposed.
func (s *Service) found(ctx context.Context, key string, r Refinement, reported []struct{ Key, Reason string }) []Blocker {
	var out []Blocker
	for _, b := range reported {
		if b.Key == key || slices.ContainsFunc(r.Related.Blockers, func(x Blocker) bool { return x.Key == b.Key }) ||
			slices.ContainsFunc(out, func(x Blocker) bool { return x.Key == b.Key }) {
			continue
		}
		t, err := s.Jira.GetTicket(ctx, b.Key)
		if err != nil {
			continue
		}
		out = append(out, Blocker{Key: t.Key, Summary: t.Summary, Reason: b.Reason, Refined: t.Refined(), FromCode: true})
	}
	return out
}

// Answers are the developer's answers to the questions on screen, by index.
type Answers struct {
	Round  []string `json:"round,omitempty"`
	Repos  []string `json:"repos,omitempty"`
	Ticket []string `json:"ticket,omitempty"`
}

// Answer saves answers as the developer gives them: to the current round's
// questions, or to the deployment questions.
func (s *Service) Answer(key string, a Answers) (Refinement, error) {
	fill := func(qs []Question, answers []string) {
		for i := range qs {
			if i < len(answers) {
				qs[i].Answer = strings.TrimSpace(answers[i])
			}
		}
	}
	return s.update(key, func(r *Refinement) error {
		switch r.Step {
		case StepQuestions:
			if len(r.Rounds) == 0 {
				return fmt.Errorf("answers: %w", ErrStep)
			}
			fill(r.Rounds[len(r.Rounds)-1].Questions, a.Round)
		case StepDeploy:
			if r.Deploy == nil {
				return fmt.Errorf("answers: %w", ErrStep)
			}
			for i := range r.Deploy.Repos {
				if i < len(a.Repos) {
					r.Deploy.Repos[i].Answer = strings.TrimSpace(a.Repos[i])
				}
			}
			fill(r.Deploy.Ticket, a.Ticket)
		default:
			return fmt.Errorf("answers: %w", ErrStep)
		}
		return nil
	})
}

// AskDeploy asks the deployment questions in a fresh session: how each repo
// without a saved profile deploys, then what this ticket needs at deploy.
func (s *Service) AskDeploy(ctx context.Context, key string, progress func(claude.Activity)) (Refinement, error) {
	r, ok, err := s.Store.Load(key)
	if err != nil {
		return Refinement{}, err
	}
	if !ok || r.Step != StepDeploy {
		return Refinement{}, fmt.Errorf("deployment: %w", ErrStep)
	}
	t, err := s.Jira.GetTicket(ctx, key)
	if err != nil {
		return Refinement{}, err
	}
	blockers, err := s.blockers(ctx, r)
	if err != nil {
		return Refinement{}, err
	}
	profiles, err := s.Store.Profiles()
	if err != nil {
		return Refinement{}, err
	}
	dirs, err := s.checkout(ctx, r)
	if err != nil {
		return Refinement{}, err
	}
	type question struct {
		Repo        string   `json:"repo"`
		Question    string   `json:"question"`
		Options     []string `json:"options"`
		Recommended int      `json:"recommended"`
		Why         string   `json:"why"`
	}
	var out struct {
		Repo   []question `json:"repo_questions"`
		Ticket []question `json:"ticket_questions"`
	}
	run, err := s.Claude.Do(ctx, claude.Call{
		Prompt: deployPrompt(t, blockers, r, dirs, profiles),
		Rules:  deployRules,
		Schema: deploySchema,
		Repos:  dirs,
	}, &out, progress)
	if err != nil {
		return Refinement{}, err
	}
	deploy := Deploy{Repos: []RepoQuestion{}, Ticket: []Question{}}
	for _, name := range r.Repos {
		if p, ok := profiles[name]; ok && len(p.Answers) > 0 {
			continue
		}
		var qs []Question
		for _, q := range out.Repo {
			if q.Repo == name {
				qs = append(qs, Question{Text: q.Question, Options: q.Options, Recommended: q.Recommended, Why: q.Why})
			}
		}
		for _, q := range normalize(qs, MaxRepoQuestions) {
			deploy.Repos = append(deploy.Repos, RepoQuestion{Repo: name, Question: q})
		}
	}
	var ticket []Question
	for _, q := range out.Ticket {
		ticket = append(ticket, Question{Text: q.Question, Options: q.Options, Recommended: q.Recommended, Why: q.Why})
	}
	if qs := normalize(ticket, MaxDeployQuestions); qs != nil {
		deploy.Ticket = qs
	}
	return s.update(key, func(r *Refinement) error {
		r.CostUSD += run.CostUSD
		r.Deploy = &deploy
		return nil
	})
}

// Generate saves the repos' deployment profiles from the answers, then
// writes up the ticket in a fresh session.
func (s *Service) Generate(ctx context.Context, key string, progress func(claude.Activity)) (Refinement, error) {
	r, ok, err := s.Store.Load(key)
	if err != nil {
		return Refinement{}, err
	}
	if !ok || r.Step != StepDeploy || r.Deploy == nil {
		return Refinement{}, fmt.Errorf("generate: %w", ErrStep)
	}
	if !r.Deploy.Answered() {
		return Refinement{}, errors.New("answer every deployment question first")
	}
	byRepo := map[string][]QA{}
	for _, q := range r.Deploy.Repos {
		byRepo[q.Repo] = append(byRepo[q.Repo], QA{Question: q.Text, Answer: q.Answer})
	}
	for _, name := range slices.Sorted(maps.Keys(byRepo)) {
		if err := s.Store.SaveProfile(Profile{Repo: name, Answers: byRepo[name], Updated: time.Now()}); err != nil {
			return Refinement{}, err
		}
	}
	t, err := s.Jira.GetTicket(ctx, key)
	if err != nil {
		return Refinement{}, err
	}
	blockers, err := s.blockers(ctx, r)
	if err != nil {
		return Refinement{}, err
	}
	profiles, err := s.Store.Profiles()
	if err != nil {
		return Refinement{}, err
	}
	writeup, err := s.Writeup()
	if err != nil {
		return Refinement{}, err
	}
	dirs, err := s.checkout(ctx, r)
	if err != nil {
		return Refinement{}, err
	}
	var result Result
	run, err := s.Claude.Do(ctx, claude.Call{
		Prompt: generatePrompt(t, blockers, r, dirs, profiles),
		Rules:  withWriteup(generateText, writeup),
		Schema: generateSchema,
		Repos:  dirs,
	}, &result, progress)
	if err != nil {
		return Refinement{}, err
	}
	result = tidy(result).Scored()
	return s.update(key, func(r *Refinement) error {
		r.CostUSD += run.CostUSD
		r.Result = &result
		r.Step = StepResults
		return nil
	})
}

// Writeup reads the rules for Acceptance Criteria and Technical
// Implementation: the developer's file, or the defaults when there is none.
func (s *Service) Writeup() (string, error) {
	if s.WriteupPath == "" {
		return DefaultWriteup, nil
	}
	data, err := os.ReadFile(s.WriteupPath)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && strings.TrimSpace(string(data)) == "") {
		return DefaultWriteup, nil
	}
	return string(data), err
}

// SetWriteup replaces the rules for Acceptance Criteria and Technical
// Implementation; empty text puts the defaults back.
func (s *Service) SetWriteup(text string) error {
	if s.WriteupPath == "" {
		return errors.New("no file for the write-up rules")
	}
	if strings.TrimSpace(text) == "" {
		text = DefaultWriteup
	}
	if err := os.MkdirAll(filepath.Dir(s.WriteupPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.WriteupPath, []byte(text), 0o600)
}

// Rewrite asks Claude for the Acceptance Criteria and Technical
// Implementation again, under the current write-up rules, for the original
// and every split ticket. The ratings, the split and the deploy needs stay.
func (s *Service) Rewrite(ctx context.Context, key string, progress func(claude.Activity)) (Refinement, error) {
	r, ok, err := s.Store.Load(key)
	if err != nil {
		return Refinement{}, err
	}
	if !ok || r.Step != StepResults || r.Result == nil || len(r.Stamp.Done) > 0 {
		return Refinement{}, fmt.Errorf("rewrite: %w", ErrStep)
	}
	t, err := s.Jira.GetTicket(ctx, key)
	if err != nil {
		return Refinement{}, err
	}
	blockers, err := s.blockers(ctx, r)
	if err != nil {
		return Refinement{}, err
	}
	writeup, err := s.Writeup()
	if err != nil {
		return Refinement{}, err
	}
	dirs, err := s.checkout(ctx, r)
	if err != nil {
		return Refinement{}, err
	}
	var rewritten struct {
		AcceptanceCriteria      []string    `json:"acceptance_criteria"`
		TechnicalImplementation []RepoFiles `json:"technical_implementation"`
		Split                   []struct {
			AcceptanceCriteria      []string    `json:"acceptance_criteria"`
			TechnicalImplementation []RepoFiles `json:"technical_implementation"`
		} `json:"split"`
	}
	run, err := s.Claude.Do(ctx, claude.Call{
		Prompt: rewritePrompt(t, blockers, r, dirs, *r.Result),
		Rules:  withWriteup(rewriteText, writeup),
		Schema: rewriteSchema,
		Repos:  dirs,
	}, &rewritten, progress)
	if err != nil {
		return Refinement{}, err
	}
	if len(rewritten.Split) != len(r.Result.Split) {
		return Refinement{}, fmt.Errorf("Claude rewrote %d split tickets, not %d; try again", len(rewritten.Split), len(r.Result.Split))
	}
	return s.update(key, func(r *Refinement) error {
		if r.Step != StepResults || r.Result == nil || len(r.Stamp.Done) > 0 || len(r.Result.Split) != len(rewritten.Split) {
			return fmt.Errorf("rewrite: %w", ErrStep)
		}
		next := *r.Result
		next.AcceptanceCriteria = rewritten.AcceptanceCriteria
		next.TechnicalImplementation = rewritten.TechnicalImplementation
		next.Split = slices.Clone(next.Split)
		for i, c := range rewritten.Split {
			next.Split[i].AcceptanceCriteria = c.AcceptanceCriteria
			next.Split[i].TechnicalImplementation = c.TechnicalImplementation
		}
		next = tidy(next).Scored()
		r.CostUSD += run.CostUSD
		r.Result = &next
		return nil
	})
}

// tidy drops empty lines and makes the lists non-nil.
func tidy(r Result) Result {
	clean := func(in []string) []string {
		out := []string{}
		for _, s := range in {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	r.AcceptanceCriteria = clean(r.AcceptanceCriteria)
	r.DeployNeeds = clean(r.DeployNeeds)
	if r.TechnicalImplementation == nil {
		r.TechnicalImplementation = []RepoFiles{}
	}
	split := []Child{}
	for _, c := range r.Split {
		c.Title = strings.TrimSpace(c.Title)
		if c.Title == "" {
			continue
		}
		c.AcceptanceCriteria = clean(c.AcceptanceCriteria)
		if c.TechnicalImplementation == nil {
			c.TechnicalImplementation = []RepoFiles{}
		}
		split = append(split, c)
	}
	r.Split = split
	return r
}

// Edit saves the developer's edits to the result: the original's fields and
// the split tickets' titles. The points follow the ratings.
func (s *Service) Edit(key string, edited Result) (Refinement, error) {
	return s.update(key, func(r *Refinement) error {
		if r.Step != StepResults || r.Result == nil || len(r.Stamp.Done) > 0 {
			return fmt.Errorf("edit: %w", ErrStep)
		}
		next := *r.Result
		next.AcceptanceCriteria = edited.AcceptanceCriteria
		next.TechnicalImplementation = edited.TechnicalImplementation
		next.Ratings = edited.Ratings
		next.DeployNeeds = edited.DeployNeeds
		for i := range next.Split {
			if i < len(edited.Split) && strings.TrimSpace(edited.Split[i].Title) != "" {
				next.Split[i].Title = strings.TrimSpace(edited.Split[i].Title)
			}
		}
		next = tidy(next).Scored()
		r.Result = &next
		return nil
	})
}

// Reset forgets a ticket's refinement, so it starts over.
func (s *Service) Reset(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Store.Delete(key)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Describe is a ticket's refinement for the app, without reading the ticket
// from Jira again.
func (s *Service) Describe(key string) (View, error) {
	r, started, err := s.Store.Load(key)
	if err != nil {
		return View{}, err
	}
	if !started {
		return View{}, fmt.Errorf("%s has no refinement", key)
	}
	return s.view(jira.Ticket{}, r, started)
}

// Profiles lists every saved deployment profile, by repo.
func (s *Service) Profiles() ([]Profile, error) {
	all, err := s.Store.Profiles()
	if err != nil {
		return nil, err
	}
	out := []Profile{}
	for _, name := range slices.Sorted(maps.Keys(all)) {
		out = append(out, all[name])
	}
	return out, nil
}

// SetProfile replaces a repo's deployment profile; one with no answers is
// forgotten, so the repo is asked about again.
func (s *Service) SetProfile(p Profile) error {
	var answers []QA
	for _, qa := range p.Answers {
		qa.Question, qa.Answer = strings.TrimSpace(qa.Question), strings.TrimSpace(qa.Answer)
		if qa.Question != "" && qa.Answer != "" {
			answers = append(answers, qa)
		}
	}
	p.Answers, p.Updated = answers, time.Now()
	return s.Store.SaveProfile(p)
}
