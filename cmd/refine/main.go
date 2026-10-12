// Command refine helps fill a ticket's refinement fields with Claude Code.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mlmejo/refine/internal/config"
	"github.com/mlmejo/refine/internal/draft"
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

	refiner := home.Refiner{
		Load:      drafts.Latest,
		LoadBrief: drafts.LatestBrief,
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
			default:
				run, described, err := prepare(ctx, issue.Key)
				if err != nil {
					return draft.Saved{}, err
				}
				// Only a fresh session needs your other tickets; a resumed one has them.
				mine, err := client.Tickets(ctx, mineJQL)
				if err != nil {
					return draft.Saved{}, fmt.Errorf("read your other tickets: %w", err)
				}
				others := draft.OthersPrompt(issue.Key, mine)
				var answers string
				if req.Previous != nil {
					answers = draft.AnswersPrompt(*req.Previous)
				}
				if req.Brief != nil && req.Brief.SessionID != "" && req.Brief.Ref == *ref {
					spentBefore = req.Brief.CostUSD
					result, err = run.FromBrief(ctx, req.Brief.SessionID, draft.FromBriefPrompt(req.Approach)+answers+others)
				} else {
					result, err = run.Run(ctx, draft.DraftPrompt(described, req.Approach)+answers+others)
				}
				if err != nil {
					return draft.Saved{}, err
				}
			}
			saved := draft.Saved{
				Key:       issue.Key,
				Approach:  req.Approach,
				Ref:       *ref,
				SessionID: result.SessionID,
				CostUSD:   result.CostUSD - spentBefore,
				Created:   time.Now(),

				SessionCostUSD: result.CostUSD,
				Turns:          result.Turns,
				Usage:          result.Usage,
				Draft:          result.Draft,
			}
			return drafts.Add(saved, issue.Summary)
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

	title := cfg.Jira.Sprint
	if *ref != "" {
		title += " · repos at " + *ref
	}
	_, err = tea.NewProgram(home.New(title, source, refiner), tea.WithAltScreen()).Run()
	return err
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
