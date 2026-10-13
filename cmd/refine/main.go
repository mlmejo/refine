// Command refine helps fill a ticket's refinement fields with Claude Code.
package main

import (
	"context"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mlmejo/refine/internal/config"
	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/gitsync"
	"github.com/mlmejo/refine/internal/hidden"
	"github.com/mlmejo/refine/internal/home"
	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/outline"
	"github.com/mlmejo/refine/internal/reveal"
	"github.com/mlmejo/refine/internal/store"
	"github.com/mlmejo/refine/internal/worktree"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "refine:", err)
		os.Exit(1)
	}
}

func run() error {
	ref := flag.String("ref", "", "read the repos at this branch, e.g. develop, instead of their current checkout")
	flag.Parse()

	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	client := jira.New(cfg.Jira.Site, cfg.Jira.Email, cfg.Jira.Token)
	jql := jira.AssignedJQL(cfg.Jira.Sprint)
	mineJQL := jira.MineJQL(cfg.Jira.Sprint, cfg.Jira.ReadySprint)

	state, err := config.StateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	hiddenStore := hidden.Store{Path: filepath.Join(state, "hidden.json")}
	hiddenKeys, err := hiddenStore.Load()
	if err != nil {
		return err
	}

	source := home.Source{
		Project:    cfg.Jira.Project,
		Hidden:     hiddenKeys,
		SaveHidden: hiddenStore.Save,
		List: func(ctx context.Context) (home.Page, error) {
			user, err := client.Myself(ctx)
			if err != nil {
				return home.Page{}, err
			}
			issues, err := client.Search(ctx, jql)
			return home.Page{User: user, Issues: issues}, err
		},
		Get: func(ctx context.Context, key string) (jira.Issue, error) {
			ticket, err := client.GetTicket(ctx, key)
			return ticket.Issue, err
		},
	}

	drafts, err := store.Open(filepath.Join(state, "refine.db"), filepath.Join(filepath.Dir(path), "exports"), filepath.Join(state, "drafts"))
	if err != nil {
		return err
	}
	defer drafts.Close()
	// runner checks out the repos for Claude to read.
	runner := func(ctx context.Context) (draft.Runner, error) {
		if len(cfg.Repos) == 0 {
			return draft.Runner{}, fmt.Errorf("add a [repos] section to %s so Claude can read the code", path)
		}
		repos, err := checkout(ctx, cfg.Repos, *ref, filepath.Join(state, "worktrees"))
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
		return run, draft.Context(ticket, repos, outlines, *ref), nil
	}

	// triage judges the ticket from Jira alone and keeps the result.
	triage := func(ctx context.Context, key string) (draft.SavedTriage, error) {
		ticket, err := client.GetTicket(ctx, key)
		if err != nil {
			return draft.SavedTriage{}, err
		}
		mine, err := client.Tickets(ctx, mineJQL)
		if err != nil {
			return draft.SavedTriage{}, fmt.Errorf("read your other tickets: %w", err)
		}
		result, err := draft.Runner{Dir: state}.Triage(ctx, draft.TriagePrompt(ticket, mine))
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
			result, err := run.Brief(ctx, draft.BriefPrompt(described))
			if err != nil {
				return draft.SavedBrief{}, err
			}
			return drafts.AddBrief(draft.SavedBrief{
				Key:       issue.Key,
				Ref:       *ref,
				SessionID: result.Run.SessionID,
				CostUSD:   result.Run.CostUSD,
				Turns:     result.Run.Turns,
				Usage:     result.Run.Usage,
				Brief:     result.Brief,
			})
		},
		Draft: func(ctx context.Context, issue jira.Issue, req draft.Request) (draft.Saved, error) {
			var result draft.Result
			var carried func(draft.Draft) draft.Draft
			spentBefore := 0.0
			switch {
			case req.Resume && req.Previous != nil && req.Previous.SessionID != "":
				run, err := runner(ctx)
				if err != nil {
					return draft.Saved{}, err
				}
				spentBefore = req.Previous.SessionCostUSD
				if spentBefore == 0 {
					spentBefore = req.Previous.CostUSD
				}
				result, err = run.Resume(ctx, req.Previous.SessionID, draft.RevisePrompt(*req.Previous))
				if err != nil {
					return draft.Saved{}, err
				}
				previous := req.Previous.Draft
				carried = func(d draft.Draft) draft.Draft { return draft.Carry(previous, d) }
			default:
				run, described, err := prepare(ctx, issue.Key)
				if err != nil {
					return draft.Saved{}, err
				}
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
				given := triaged.Triage.Prompt()
				carried = triaged.Triage.Apply
				if req.Previous != nil {
					given += draft.AnswersPrompt(*req.Previous)
				}
				if req.Brief != nil && req.Brief.SessionID != "" && req.Brief.Ref == *ref {
					spentBefore = req.Brief.CostUSD
					result, err = run.FromBrief(ctx, req.Brief.SessionID, draft.FromBriefPrompt(req.Approach)+given)
				} else {
					result, err = run.Run(ctx, draft.DraftPrompt(described, req.Approach)+given)
				}
				if err != nil {
					return draft.Saved{}, err
				}
			}
			saved := draft.Saved{
				Key:       issue.Key,
				Approach:  req.Approach,
				Ref:       *ref,
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
			stamp, result, err := run.Stamp(ctx, questions.SessionID, draft.StampPrompt(questions))
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
	if *ref == "" {
		refiner.Check = func(ctx context.Context) []gitsync.Status {
			return checkRepos(ctx, cfg.Repos)
		}
		refiner.Pull = gitsync.Pull
	}

	title := cfg.Jira.Sprint
	if *ref != "" {
		title += " · repos at " + *ref
	}
	_, err = tea.NewProgram(home.New(title, source, refiner), tea.WithAltScreen()).Run()
	return err
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
