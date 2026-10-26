// Package app wires refine's config, Jira, Claude runs and store into the
// server the desktop app talks to.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"

	"github.com/mlmejo/refine/internal/claude"
	"github.com/mlmejo/refine/internal/config"
	"github.com/mlmejo/refine/internal/gitsync"
	"github.com/mlmejo/refine/internal/hidden"
	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/refine"
	"github.com/mlmejo/refine/internal/server"
	"github.com/mlmejo/refine/internal/store"
	"github.com/mlmejo/refine/internal/worktree"
)

// App is refine wired up. Close it when done.
type App struct {
	Server *server.Server
	Store  *store.Store
}

// Close stops the running Claude run and closes the store.
func (a *App) Close() error {
	a.Server.Close()
	return a.Store.Close()
}

// git fetches with gitsync and checks commits out with worktree.
type git struct{}

func (git) Fetch(ctx context.Context, name, dir string) (gitsync.Status, error) {
	return gitsync.Fetch(ctx, name, dir)
}

func (git) Checkout(ctx context.Context, repo, commit, dest string) error {
	_, err := worktree.Checkout(ctx, repo, commit, dest, false)
	return err
}

// Open reads the config and wires refine up.
func Open() (*App, error) {
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
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
	db, err := store.Open(filepath.Join(state, "refine.db"))
	if err != nil {
		return nil, err
	}
	if db.Backup != "" {
		fmt.Fprintf(os.Stderr, "refine: upgraded refine.db; the old one is at %s\n", db.Backup)
	}

	// The service reads cfg.Repos when called, so a change applies to the
	// next step. Each change swaps in a new map rather than writing to one a
	// run may be reading.
	repos := server.Repos{
		List: func() map[string]string { return cfg.Repos },
		Add: func(name, dir string) error {
			if err := config.AddRepo(path, name, dir); err != nil {
				return err
			}
			next := maps.Clone(cfg.Repos)
			if next == nil {
				next = map[string]string{}
			}
			next[name] = dir
			cfg.Repos = next
			return nil
		},
		Remove: func(name string) error {
			if err := config.RemoveRepo(path, name); err != nil {
				return err
			}
			next := maps.Clone(cfg.Repos)
			delete(next, name)
			cfg.Repos = next
			return nil
		},
	}
	service := &refine.Service{
		Jira:        jira.New(cfg.Jira.Site, cfg.Jira.Email, cfg.Jira.Token),
		Claude:      claude.Runner{Dir: state},
		Store:       db,
		Git:         git{},
		Sprint:      cfg.Jira.Sprint,
		ReadySprint: cfg.Jira.ReadySprint,
		Repos:       repos.List,
		Worktrees:   filepath.Join(state, "worktrees"),
		FormatDir:   filepath.Dir(path),
	}
	for _, f := range []refine.Format{refine.FormatAC, refine.FormatTI} {
		if _, err := os.Stat(service.FormatPath(f)); errors.Is(err, fs.ErrNotExist) {
			if err := service.SetFormat(f, ""); err != nil {
				return nil, err
			}
		}
	}
	info := server.Info{Site: cfg.Jira.Site, Project: cfg.Jira.Project, Sprint: cfg.Jira.Sprint, ReadySprint: cfg.Jira.ReadySprint}
	srv := server.New(info, service, repos, hiddenKeys, hiddenStore.Save)
	return &App{Server: srv, Store: db}, nil
}
