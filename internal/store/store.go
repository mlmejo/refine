// Package store keeps refinements and the repos' deployment profiles in
// SQLite.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mlmejo/refine/internal/refine"
)

// version is the schema this package writes. Version 11 replaced the old
// triage, brief and draft tables with refinements.
const version = 11

const schema = `
CREATE TABLE refinements (
	key     TEXT PRIMARY KEY,
	state   TEXT NOT NULL,
	updated TEXT NOT NULL
);
CREATE TABLE deploy_profiles (
	repo    TEXT PRIMARY KEY,
	profile TEXT NOT NULL,
	updated TEXT NOT NULL
);
`

// old are the tables the process before version 11 kept.
var old = []string{"drafts", "briefs", "triages", "outlines", "events", "experiments"}

// Store is refine's database.
type Store struct {
	db *sql.DB
	// Backup is the copy of the database made before an upgrade dropped the
	// old tables, or "" when there was none.
	Backup string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Open opens or creates the database at path. A database from before
// version 11 is copied next to it first, then its old tables are dropped.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(ctx, path); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// now reads the store's clock.
func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context, path string) error {
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current >= version {
		return nil
	}
	if current > 0 {
		backup := fmt.Sprintf("%s.bak-%s", path, time.Now().Format("20060102-150405"))
		if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, backup); err != nil {
			return fmt.Errorf("back up %s before upgrading it: %w", path, err)
		}
		s.Backup = backup
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range old {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	return tx.Commit()
}

// Load returns a ticket's refinement, if it has one.
func (s *Store) Load(ctx context.Context, key string) (refine.Refinement, bool, error) {
	var state []byte
	err := s.db.QueryRowContext(ctx, `SELECT state FROM refinements WHERE key = ?`, key).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return refine.Refinement{}, false, nil
	}
	if err != nil {
		return refine.Refinement{}, false, err
	}
	var r refine.Refinement
	return r, true, json.Unmarshal(state, &r)
}

// All returns every refinement, by key.
func (s *Store) All(ctx context.Context) ([]refine.Refinement, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT state FROM refinements ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []refine.Refinement
	for rows.Next() {
		var state []byte
		if err := rows.Scan(&state); err != nil {
			return nil, err
		}
		var r refine.Refinement
		if err := json.Unmarshal(state, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Save writes a refinement, replacing the ticket's earlier one.
func (s *Store) Save(ctx context.Context, r refine.Refinement) error {
	if r.Updated.IsZero() {
		r.Updated = s.now()
	}
	state, err := json.Marshal(r)
	if err != nil {
		return err
	}
	// Bytes bind as a BLOB; the CAST keeps state stored as TEXT.
	_, err = s.db.ExecContext(ctx, `INSERT INTO refinements (key, state, updated) VALUES (?, CAST(? AS TEXT), ?)
		ON CONFLICT (key) DO UPDATE SET state = excluded.state, updated = excluded.updated`,
		r.Key, state, r.Updated.UTC().Format(time.RFC3339Nano))
	return err
}

// Delete forgets a ticket's refinement.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM refinements WHERE key = ?`, key)
	return err
}

// Profiles returns every repo's deployment profile, by repo.
func (s *Store) Profiles(ctx context.Context) (map[string]refine.Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT profile FROM deploy_profiles`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]refine.Profile{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var p refine.Profile
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			return nil, err
		}
		out[p.Repo] = p
	}
	return out, rows.Err()
}

// SaveProfile writes a repo's deployment profile; one with no answers is
// removed, so the repo is asked about again.
func (s *Store) SaveProfile(ctx context.Context, p refine.Profile) error {
	if len(p.Answers) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM deploy_profiles WHERE repo = ?`, p.Repo)
		return err
	}
	if p.Updated.IsZero() {
		p.Updated = s.now()
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO deploy_profiles (repo, profile, updated) VALUES (?, ?, ?)
		ON CONFLICT (repo) DO UPDATE SET profile = excluded.profile, updated = excluded.updated`,
		p.Repo, string(data), p.Updated.UTC().Format(time.RFC3339Nano))
	return err
}
