// Package app wires refine's config, Jira, Claude runs and store into the
// Source and Refiner the TUI and the desktop server share.
package app

import (
	"context"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/mlmejo/refine/internal/config"
	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/gitsync"
	"github.com/mlmejo/refine/internal/hidden"
	"github.com/mlmejo/refine/internal/home"
	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/outline"
	"github.com/mlmejo/refine/internal/reveal"
	"github.com/mlmejo/refine/internal/store"
	"github.com/mlmejo/refine/internal/track"
	"github.com/mlmejo/refine/internal/worktree"
)

// App is refine wired up. Close it when done.
type App struct {
	// Title names the sprint, and the ref the repos are read at if any.
	Title string
	// Site is the Jira site, e.g. geekee.atlassian.net.
	Site    string
	Source  home.Source
	Refiner home.Refiner
	// Drafts is the store the Source and Refiner read and write.
	Drafts *store.Store
	// Tracker records usage when the tracking flag is on; nil otherwise.
	Tracker *track.Tracker
}

// Close closes the store.
func (a *App) Close() error {
	return a.Drafts.Close()
}

// Open reads the config and wires refine up for the ui ("tui" or "gui").
// With ref set, Claude reads the repos at that branch, e.g. develop,
// instead of their current checkout.
func Open(ref, ui string) (*App, error) {
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}

	client := jira.New(cfg.Jira.Site, cfg.Jira.Email, cfg.Jira.Token)
	jql := jira.AssignedJQL(cfg.Jira.Sprint)
	// others reads the open tickets a refinement must not repeat.
	others := func(ctx context.Context, ticket jira.Ticket) ([]jira.Ticket, error) {
		found, err := client.Tickets(ctx, jira.OthersJQL(ticket.Parent, cfg.Jira.Sprint, cfg.Jira.ReadySprint))
		if err != nil {
			return nil, fmt.Errorf("read the other open tickets: %w", err)
		}
		return found, nil
	}

	state, err := config.StateDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return nil, err
	}
	hiddenStore := hidden.Store{Path: filepath.Join(state, "hidden.json")}
	hiddenKeys, err := hiddenStore.Load()
	if err != nil {
		return nil, err
	}

	source := home.Source{
		Project:    cfg.Jira.Project,
		Hidden:     hiddenKeys,
		SaveHidden: hiddenStore.Save,
		// The runner and the origin check read cfg.Repos when called, so a
		// change here applies to the next brief or draft. Each change swaps in
		// a new map rather than writing to one a run may be reading.
		Repos: func() map[string]string { return cfg.Repos },
		AddRepo: func(name, dir string) error {
			if err := config.AddRepo(path, name, dir); err != nil {
				return err
			}
			repos := maps.Clone(cfg.Repos)
			if repos == nil {
				repos = map[string]string{}
			}
			repos[name] = dir
			cfg.Repos = repos
			return nil
		},
		RemoveRepo: func(name string) error {
			if err := config.RemoveRepo(path, name); err != nil {
				return err
			}
			repos := maps.Clone(cfg.Repos)
			delete(repos, name)
			cfg.Repos = repos
			return nil
		},
		List: func(ctx context.Context) (home.Page, error) {
			user, err := client.Myself(ctx)
			if err != nil {
				return home.Page{}, err
			}
			issues, err := client.Search(ctx, jql)
			return home.Page{User: user, Issues: issues}, err
		},
		Ticket: client.GetTicket,
		Family: func(ctx context.Context, parent string) ([]jira.Issue, error) {
			return client.Search(ctx, jira.FamilyJQL(parent))
		},
		Get: func(ctx context.Context, key string) (jira.Issue, error) {
			ticket, err := client.GetTicket(ctx, key)
			return ticket.Issue, err
		},
	}

	drafts, err := store.Open(filepath.Join(state, "refine.db"), filepath.Join(filepath.Dir(path), "exports"))
	if err != nil {
		return nil, err
	}
	// runner checks out the repos for Claude to read.
	runner := func(ctx context.Context) (draft.Runner, error) {
		if len(cfg.Repos) == 0 {
			return draft.Runner{}, fmt.Errorf("add a [repos] section to %s so Claude can read the code", path)
		}
		repos, err := checkout(ctx, cfg.Repos, ref, filepath.Join(state, "worktrees"))
		return draft.Runner{Dir: state, Repos: repos}, err
	}
	// prepare also describes the ticket and the repos' outlines.
	prepare := func(ctx context.Context, key string) (draft.Runner, string, error) {
		run, err := runner(ctx)
		if err != nil {
			return draft.Runner{}, "", err
		}
		repos := run.Repos
		ticket, err := client.GetTicket(ctx, key)
		if err != nil {
			return draft.Runner{}, "", err
		}
		outlines := make(map[string]outline.Outline, len(repos))
		for name, dir := range repos {
			if layout, err := outline.Get(ctx, drafts, name, dir); err == nil {
				outlines[name] = layout
			}
		}
		return run, draft.Context(ticket, repos, outlines, ref), nil
	}

	// triage judges the ticket from Jira alone and keeps the result.
	triage := func(ctx context.Context, key string) (draft.SavedTriage, error) {
		ticket, err := client.GetTicket(ctx, key)
		if err != nil {
			return draft.SavedTriage{}, err
		}
		open, err := others(ctx, ticket)
		if err != nil {
			return draft.SavedTriage{}, err
		}
		result, err := draft.Runner{Dir: state}.Triage(ctx, draft.TriagePrompt(ticket, open))
		if err != nil {
			return draft.SavedTriage{}, err
		}
		return drafts.AddTriage(draft.SavedTriage{
			Key:       key,
			SessionID: result.Run.SessionID,
			CostUSD:   result.Run.CostUSD,
			Turns:     result.Run.Turns,
			Usage:     result.Run.Usage,
			Triage:    result.Triage,
		})
	}

	refiner := home.Refiner{
		Load:       drafts.Latest,
		LoadBrief:  drafts.LatestBrief,
		LoadTriage: drafts.LatestTriage,
		Triage: func(ctx context.Context, issue jira.Issue) (draft.SavedTriage, error) {
			return triage(ctx, issue.Key)
		},
		Brief: func(ctx context.Context, issue jira.Issue) (draft.SavedBrief, error) {
			run, described, err := prepare(ctx, issue.Key)
			if err != nil {
				return draft.SavedBrief{}, err
			}
			// The brief checks the triage's items, so a ticket without a
			// triage is triaged first.
			triaged, ok, err := drafts.LatestTriage(issue.Key)
			if err != nil {
				return draft.SavedBrief{}, err
			}
			if !ok {
				if triaged, err = triage(ctx, issue.Key); err != nil {
					return draft.SavedBrief{}, fmt.Errorf("triage: %w", err)
				}
			}
			result, err := run.Brief(ctx, draft.BriefPrompt(described, triaged.Triage.Items))
			if err != nil {
				return draft.SavedBrief{}, err
			}
			return drafts.AddBrief(draft.SavedBrief{
				Key:       issue.Key,
				Ref:       ref,
				SessionID: result.Run.SessionID,
				CostUSD:   result.Run.CostUSD,
				Turns:     result.Run.Turns,
				Usage:     result.Run.Usage,
				Brief:     result.Brief,
			})
		},
		Draft: func(ctx context.Context, issue jira.Issue, req draft.Request) (draft.Saved, error) {
			// A resumed round keeps the scope answers it started with.
			if req.Resume && req.Previous != nil {
				req.Scope = req.Previous.Scope
			}
			arm := experimentArm(cfg.Experiments, drafts, issue.Key, req.Previous)
			var result draft.Result
			var carried func(draft.Draft) draft.Draft
			spentBefore := 0.0
			switch {
			case req.Resume && req.Previous != nil && req.Previous.SessionID != "":
				run, err := runner(ctx)
				if err != nil {
					return draft.Saved{}, err
				}
				run.Plain = arm == "B"
				spentBefore = req.Previous.SessionCostUSD
				if spentBefore == 0 {
					spentBefore = req.Previous.CostUSD
				}
				result, err = run.Resume(ctx, req.Previous.SessionID, draft.RevisePrompt(*req.Previous))
				if err != nil {
					return draft.Saved{}, err
				}
				previous := *req.Previous
				carried = func(d draft.Draft) draft.Draft {
					d = draft.Carry(previous.Draft, d)
					d.Decided = draft.Decide(previous)
					return d
				}
			default:
				run, described, err := prepare(ctx, issue.Key)
				if err != nil {
					return draft.Saved{}, err
				}
				run.Plain = arm == "B"
				// The questions build on the triage; a ticket without one is
				// triaged first.
				triaged := req.Triage
				if triaged == nil {
					fresh, err := triage(ctx, issue.Key)
					if err != nil {
						return draft.Saved{}, fmt.Errorf("triage: %w", err)
					}
					triaged = &fresh
				}
				given := triaged.Triage.Prompt(req.Scope)
				carried = triaged.Triage.Apply
				if req.Previous != nil {
					given += draft.AnswersPrompt(*req.Previous)
					decided := draft.Decide(*req.Previous)
					apply := carried
					carried = func(d draft.Draft) draft.Draft {
						d = apply(d)
						d.Decided = decided
						return d
					}
				}
				if req.Brief != nil && req.Brief.SessionID != "" && req.Brief.Ref == ref {
					spentBefore = req.Brief.CostUSD
					result, err = run.FromBrief(ctx, req.Brief.SessionID, draft.FromBriefPrompt(req.Approach)+given)
				} else {
					result, err = run.Run(ctx, draft.DraftPrompt(described, req.Approach)+given)
				}
				if err != nil {
					return draft.Saved{}, err
				}
			}
			arms := carried
			carried = func(d draft.Draft) draft.Draft {
				d = arms(d)
				d.Arm = arm
				return d
			}
			saved := draft.Saved{
				Key:       issue.Key,
				Approach:  req.Approach,
				Scope:     req.Scope,
				Ref:       ref,
				Step:      draft.StepQuestions,
				SessionID: result.SessionID,
				CostUSD:   result.CostUSD - spentBefore,
				Created:   time.Now(),

				SessionCostUSD: result.CostUSD,
				Turns:          result.Turns,
				Usage:          result.Usage,
				Draft:          carried(result.Draft),
			}
			return drafts.Add(saved, issue.Summary)
		},
		Stamp: func(ctx context.Context, issue jira.Issue, questions draft.Saved) (draft.Saved, error) {
			run, err := runner(ctx)
			if err != nil {
				return draft.Saved{}, err
			}
			ticket, err := client.GetTicket(ctx, issue.Key)
			if err != nil {
				return draft.Saved{}, err
			}
			open, err := others(ctx, ticket)
			if err != nil {
				return draft.Saved{}, err
			}
			stamp, result, err := run.Stamp(ctx, questions.SessionID, draft.StampPrompt(questions, open))
			if err != nil {
				return draft.Saved{}, err
			}
			// The fork reports what its whole session cost, the questions included.
			spentBefore := questions.SessionCostUSD
			if spentBefore == 0 {
				spentBefore = questions.CostUSD
			}
			// The stamped draft keeps the questions session, so r asks more
			// questions there rather than in the stamp's fork.
			stamped := questions
			stamped.ID, stamped.Markdown, stamped.Children = 0, "", nil
			stamped.Step = draft.StepStamp
			stamped.CostUSD = result.CostUSD - spentBefore
			stamped.Turns, stamped.Usage = result.Turns, result.Usage
			stamped.Created = time.Now()
			stamped.Draft = questions.Draft.WithStamp(stamp)
			// A check that fails leaves the stamp unchecked, which the write
			// confirmation warns about, rather than losing the stamp.
			if check, checked, err := (draft.Runner{Dir: state}).Check(ctx, draft.CheckPrompt(stamped, open)); err == nil {
				stamped.Draft = stamped.Draft.WithCheck(check)
				stamped.CostUSD += checked.CostUSD
			} else if ctx.Err() != nil {
				return draft.Saved{}, ctx.Err()
			}
			return drafts.Add(stamped, issue.Summary)
		},
		SaveAnswers: drafts.SaveAnswers,
		Reveal: func(saved draft.Saved) error {
			return reveal.File(saved.Markdown)
		},
		Write: func(ctx context.Context, key string, d draft.Draft) (jira.Issue, error) {
			if err := client.Update(ctx, key, d.Refinement()); err != nil {
				return jira.Issue{}, err
			}
			ticket, err := client.GetTicket(ctx, key)
			return ticket.Issue, err
		},
		Split: func(ctx context.Context, saved draft.Saved) (map[int]string, error) {
			target, err := client.Split(ctx, saved.Key, cfg.Jira.ReadySprint)
			if err != nil {
				return saved.Children, err
			}
			create := func(child draft.Child) (string, error) {
				return client.CreateChild(ctx, target, jira.NewChild{
					Summary:     child.Title,
					Description: draft.SplitDescription(target.Parent, target.Summary),
					Refinement:  child.Refinement(),
				})
			}
			return draft.CreateSplit(saved, create, drafts.SaveChildren)
		},
		Move: func(ctx context.Context, key string) error {
			return client.MoveToSprint(ctx, key, cfg.Jira.ReadySprint)
		},
		ReadySprint: cfg.Jira.ReadySprint,
	}

	// Read as checked out, the repos may lag origin; at --ref they are read
	// from origin already.
	if ref == "" {
		refiner.Check = func(ctx context.Context) []gitsync.Status {
			return checkRepos(ctx, cfg.Repos)
		}
		refiner.Pull = gitsync.Pull
	}

	// Read as checked out, the repos may lag origin; at --ref they are read
	// from origin already.
	if ref == "" {
		refiner.Check = func(ctx context.Context) []gitsync.Status {
			return checkRepos(ctx, cfg.Repos)
		}
		refiner.Pull = gitsync.Pull
	}

	title := cfg.Jira.Sprint
	if ref != "" {
		title += " · repos at " + ref
	}
	tracker := track.New(drafts, ui, cfg.Flags.Tracking)
	refiner = tracked(refiner, tracker, drafts.Latest)
	source.Track = tracker.Event
	return &App{Title: title, Site: cfg.Jira.Site, Source: source, Refiner: refiner, Drafts: drafts, Tracker: tracker}, nil
}

// experimentArm is the ticket's arm in the plain-names experiment: the arm
// its drafts already have, a new one for a ticket refined for the first
// time, and none for a ticket that was in progress before the experiment.
func experimentArm(on config.Experiments, drafts *store.Store, key string, previous *draft.Saved) string {
	if previous != nil {
		return previous.Draft.Arm
	}
	if !on.PlainNames {
		return ""
	}
	arm, err := drafts.Assign(key, "plain_names", func() bool { return rand.N(2) == 0 })
	if err != nil {
		return ""
	}
	return arm
}

// syncBranch is the branch whose checkout is compared with origin before
// Claude reads it.
const syncBranch = "develop"

// checkRepos compares each repo with origin at once, in name order.
func checkRepos(ctx context.Context, repos map[string]string) []gitsync.Status {
	names := slices.Sorted(maps.Keys(repos))
	statuses := make([]gitsync.Status, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			statuses[i] = gitsync.Check(ctx, name, repos[name], syncBranch)
		})
	}
	wg.Wait()
	return statuses
}

// checkout returns the repos to read: as they are, or each at ref in a
// refine-owned worktree under dir.
func checkout(ctx context.Context, repos map[string]string, ref, dir string) (map[string]string, error) {
	if ref == "" {
		return repos, nil
	}
	out := make(map[string]string, len(repos))
	for name, path := range repos {
		dest := filepath.Join(dir, name)
		if _, err := worktree.Checkout(ctx, path, ref, dest, true); err != nil {
			return nil, err
		}
		out[name] = dest
	}
	return out, nil
}
