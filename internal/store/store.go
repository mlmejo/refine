// Package store keeps every draft of every ticket in SQLite, each mapped to
// the markdown file written for it.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mlmejo/refine/internal/draft"
)

const schema = `
CREATE TABLE drafts (
	id         INTEGER PRIMARY KEY,
	key        TEXT NOT NULL,
	summary    TEXT NOT NULL,
	approach   TEXT NOT NULL,
	ref        TEXT NOT NULL,
	session_id TEXT NOT NULL,
	cost_usd   REAL NOT NULL,
	created    TEXT NOT NULL,
	draft      TEXT NOT NULL,
	markdown   TEXT NOT NULL
);
CREATE INDEX drafts_key ON drafts (key, id);
`

// Store is the draft history, with markdown written under Exports.
type Store struct {
	db      *sql.DB
	Exports string
}

// Open opens or creates the database at path.
func Open(path, exports string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, Exports: exports}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version < 1 {
		if _, err := s.db.Exec(schema); err != nil {
			return err
		}
	}
	if version < 2 {
		for _, column := range []string{
			`answers TEXT NOT NULL DEFAULT '{}'`,
			`session_cost_usd REAL NOT NULL DEFAULT 0`,
		} {
			if _, err := s.db.Exec(`ALTER TABLE drafts ADD COLUMN ` + column); err != nil {
				return err
			}
		}
	}
	if version < 3 {
		if _, err := s.db.Exec(`
			ALTER TABLE drafts ADD COLUMN turns INTEGER NOT NULL DEFAULT 0;
			ALTER TABLE drafts ADD COLUMN usage TEXT NOT NULL DEFAULT '{}';
			CREATE TABLE outlines (
				repo       TEXT NOT NULL,
				commit_sha TEXT NOT NULL,
				outline    TEXT NOT NULL,
				created    TEXT NOT NULL,
				PRIMARY KEY (repo, commit_sha)
			);`); err != nil {
			return err
		}
	}
	if version < 4 {
		if _, err := s.db.Exec(`
			CREATE TABLE briefs (
				id         INTEGER PRIMARY KEY,
				key        TEXT NOT NULL,
				ref        TEXT NOT NULL,
				session_id TEXT NOT NULL,
				cost_usd   REAL NOT NULL,
				turns      INTEGER NOT NULL,
				usage      TEXT NOT NULL,
				created    TEXT NOT NULL,
				brief      TEXT NOT NULL
			);
			CREATE INDEX briefs_key ON briefs (key, id);`); err != nil {
			return err
		}
	}
	if version < 5 {
		if _, err := s.db.Exec(`ALTER TABLE drafts ADD COLUMN children TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return err
		}
	}
	if version < 6 {
		if _, err := s.db.Exec(`
			ALTER TABLE drafts ADD COLUMN step TEXT NOT NULL DEFAULT '';
			CREATE TABLE triages (
				id         INTEGER PRIMARY KEY,
				key        TEXT NOT NULL,
				session_id TEXT NOT NULL,
				cost_usd   REAL NOT NULL,
				turns      INTEGER NOT NULL,
				usage      TEXT NOT NULL,
				created    TEXT NOT NULL,
				triage     TEXT NOT NULL
			);
			CREATE INDEX triages_key ON triages (key, id);`); err != nil {
			return err
		}
	}
	if version < 7 {
		if _, err := s.db.Exec(`ALTER TABLE drafts ADD COLUMN scope TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return err
		}
	}
	if version < 8 {
		if _, err := s.db.Exec(`
			CREATE TABLE events (
				id      INTEGER PRIMARY KEY,
				at      TEXT NOT NULL,
				ui      TEXT NOT NULL,
				session TEXT NOT NULL,
				name    TEXT NOT NULL,
				key     TEXT NOT NULL,
				props   TEXT NOT NULL
			);
			CREATE INDEX events_at ON events (at);`); err != nil {
			return err
		}
	}
	_, err := s.db.Exec("PRAGMA user_version = 8")
	return err
}

// Add writes the draft's markdown to Exports/<KEY>/<created>.md and records
// both, returning the draft with its ID and markdown path set.
func (s *Store) Add(saved draft.Saved, summary string) (draft.Saved, error) {
	if saved.Created.IsZero() {
		saved.Created = time.Now()
	}
	saved.Markdown = filepath.Join(s.Exports, filepath.Base(saved.Key), saved.Created.Local().Format("20060102-150405")+".md")
	if err := draft.WriteMarkdown(saved.Markdown, saved, summary); err != nil {
		return draft.Saved{}, err
	}

	data, err := json.Marshal(saved.Draft)
	if err != nil {
		return draft.Saved{}, err
	}
	answers, err := json.Marshal(saved.Answers)
	if err != nil {
		return draft.Saved{}, err
	}
	usage, err := json.Marshal(saved.Usage)
	if err != nil {
		return draft.Saved{}, err
	}
	scope, err := json.Marshal(saved.Scope)
	if err != nil {
		return draft.Saved{}, err
	}
	result, err := s.db.Exec(
		`INSERT INTO drafts (key, summary, approach, ref, step, session_id, cost_usd, session_cost_usd, turns, usage, created, draft, markdown, answers, scope)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		saved.Key, summary, saved.Approach, saved.Ref, saved.Step, saved.SessionID, saved.CostUSD, saved.SessionCostUSD, saved.Turns, string(usage),
		saved.Created.UTC().Format(time.RFC3339Nano), string(data), saved.Markdown, string(answers), string(scope),
	)
	if err != nil {
		return draft.Saved{}, err
	}
	saved.ID, err = result.LastInsertId()
	return saved, err
}

// SaveAnswers records the draft's answers and rewrites its markdown with them.
func (s *Store) SaveAnswers(saved draft.Saved) error {
	answers, err := json.Marshal(saved.Answers)
	if err != nil {
		return err
	}
	var summary string
	if err := s.db.QueryRow(`SELECT summary FROM drafts WHERE id = ?`, saved.ID).Scan(&summary); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE drafts SET answers = ? WHERE id = ?`, string(answers), saved.ID); err != nil {
		return err
	}
	return draft.WriteMarkdown(saved.Markdown, saved, summary)
}

// SaveChildren records the keys of the split tickets created from the draft
// and rewrites its markdown with them.
func (s *Store) SaveChildren(saved draft.Saved) error {
	children, err := json.Marshal(saved.Children)
	if err != nil {
		return err
	}
	var summary string
	if err := s.db.QueryRow(`SELECT summary FROM drafts WHERE id = ?`, saved.ID).Scan(&summary); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE drafts SET children = ? WHERE id = ?`, string(children), saved.ID); err != nil {
		return err
	}
	return draft.WriteMarkdown(saved.Markdown, saved, summary)
}

// Outline returns the repo's cached outline at a commit, if there is one.
func (s *Store) Outline(repo, commit string) (string, bool, error) {
	var outline string
	err := s.db.QueryRow(`SELECT outline FROM outlines WHERE repo = ? AND commit_sha = ?`, repo, commit).Scan(&outline)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return outline, err == nil, err
}

// SaveOutline caches the repo's outline at a commit. A commit's files never
// change, so an entry never goes stale.
func (s *Store) SaveOutline(repo, commit, outline string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO outlines (repo, commit_sha, outline, created) VALUES (?, ?, ?, ?)`,
		repo, commit, outline, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

// Latest returns the ticket's newest draft, if there is one.
func (s *Store) Latest(key string) (draft.Saved, bool, error) {
	drafts, err := s.query(`WHERE key = ? ORDER BY id DESC LIMIT 1`, key)
	if err != nil || len(drafts) == 0 {
		return draft.Saved{}, false, err
	}
	return drafts[0], true, nil
}

// History returns every draft of the ticket, newest first.
func (s *Store) History(key string) ([]draft.Saved, error) {
	return s.query(`WHERE key = ? ORDER BY id DESC`, key)
}

func (s *Store) query(where string, args ...any) ([]draft.Saved, error) {
	rows, err := s.db.Query(`SELECT id, key, approach, ref, step, session_id, cost_usd, session_cost_usd, turns, usage, created, draft, markdown, answers, children, scope FROM drafts `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var drafts []draft.Saved
	for rows.Next() {
		var saved draft.Saved
		var created, data, answers, usage, children, scope string
		if err := rows.Scan(&saved.ID, &saved.Key, &saved.Approach, &saved.Ref, &saved.Step, &saved.SessionID, &saved.CostUSD, &saved.SessionCostUSD, &saved.Turns, &usage, &created, &data, &saved.Markdown, &answers, &children, &scope); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(usage), &saved.Usage); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(answers), &saved.Answers); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(children), &saved.Children); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(scope), &saved.Scope); err != nil {
			return nil, err
		}
		if saved.Created, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &saved.Draft); err != nil {
			return nil, err
		}
		drafts = append(drafts, saved)
	}
	return drafts, rows.Err()
}

// AddBrief records a brief, returning it with its ID set.
func (s *Store) AddBrief(saved draft.SavedBrief) (draft.SavedBrief, error) {
	if saved.Created.IsZero() {
		saved.Created = time.Now()
	}
	brief, err := json.Marshal(saved.Brief)
	if err != nil {
		return draft.SavedBrief{}, err
	}
	usage, err := json.Marshal(saved.Usage)
	if err != nil {
		return draft.SavedBrief{}, err
	}
	result, err := s.db.Exec(
		`INSERT INTO briefs (key, ref, session_id, cost_usd, turns, usage, created, brief) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		saved.Key, saved.Ref, saved.SessionID, saved.CostUSD, saved.Turns, string(usage),
		saved.Created.UTC().Format(time.RFC3339Nano), string(brief),
	)
	if err != nil {
		return draft.SavedBrief{}, err
	}
	saved.ID, err = result.LastInsertId()
	return saved, err
}

// LatestBrief returns the ticket's newest brief, if there is one.
func (s *Store) LatestBrief(key string) (draft.SavedBrief, bool, error) {
	var saved draft.SavedBrief
	var created, usage, brief string
	err := s.db.QueryRow(
		`SELECT id, key, ref, session_id, cost_usd, turns, usage, created, brief FROM briefs WHERE key = ? ORDER BY id DESC LIMIT 1`, key,
	).Scan(&saved.ID, &saved.Key, &saved.Ref, &saved.SessionID, &saved.CostUSD, &saved.Turns, &usage, &created, &brief)
	if errors.Is(err, sql.ErrNoRows) {
		return draft.SavedBrief{}, false, nil
	}
	if err != nil {
		return draft.SavedBrief{}, false, err
	}
	if err := json.Unmarshal([]byte(usage), &saved.Usage); err != nil {
		return draft.SavedBrief{}, false, err
	}
	if err := json.Unmarshal([]byte(brief), &saved.Brief); err != nil {
		return draft.SavedBrief{}, false, err
	}
	saved.Created, err = time.Parse(time.RFC3339Nano, created)
	return saved, err == nil, err
}

// AddTriage records a triage, returning it with its ID set.
func (s *Store) AddTriage(saved draft.SavedTriage) (draft.SavedTriage, error) {
	if saved.Created.IsZero() {
		saved.Created = time.Now()
	}
	triage, err := json.Marshal(saved.Triage)
	if err != nil {
		return draft.SavedTriage{}, err
	}
	usage, err := json.Marshal(saved.Usage)
	if err != nil {
		return draft.SavedTriage{}, err
	}
	result, err := s.db.Exec(
		`INSERT INTO triages (key, session_id, cost_usd, turns, usage, created, triage) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		saved.Key, saved.SessionID, saved.CostUSD, saved.Turns, string(usage),
		saved.Created.UTC().Format(time.RFC3339Nano), string(triage),
	)
	if err != nil {
		return draft.SavedTriage{}, err
	}
	saved.ID, err = result.LastInsertId()
	return saved, err
}

// LatestTriage returns the ticket's newest triage, if there is one.
func (s *Store) LatestTriage(key string) (draft.SavedTriage, bool, error) {
	var saved draft.SavedTriage
	var created, usage, triage string
	err := s.db.QueryRow(
		`SELECT id, key, session_id, cost_usd, turns, usage, created, triage FROM triages WHERE key = ? ORDER BY id DESC LIMIT 1`, key,
	).Scan(&saved.ID, &saved.Key, &saved.SessionID, &saved.CostUSD, &saved.Turns, &usage, &created, &triage)
	if errors.Is(err, sql.ErrNoRows) {
		return draft.SavedTriage{}, false, nil
	}
	if err != nil {
		return draft.SavedTriage{}, false, err
	}
	if err := json.Unmarshal([]byte(usage), &saved.Usage); err != nil {
		return draft.SavedTriage{}, false, err
	}
	if err := json.Unmarshal([]byte(triage), &saved.Triage); err != nil {
		return draft.SavedTriage{}, false, err
	}
	saved.Created, err = time.Parse(time.RFC3339Nano, created)
	return saved, err == nil, err
}

// Event is one thing that happened in refine, recorded when tracking is on.
type Event struct {
	At      time.Time
	UI      string
	Session string
	Name    string
	Key     string
	Props   map[string]any
}

// AddEvent records an event.
func (s *Store) AddEvent(e Event) error {
	props, err := json.Marshal(e.Props)
	if err != nil {
		return err
	}
	if e.Props == nil {
		props = []byte("{}")
	}
	_, err = s.db.Exec(`INSERT INTO events (at, ui, session, name, key, props) VALUES (?, ?, ?, ?, ?, ?)`,
		e.At.UTC().Format(time.RFC3339Nano), e.UI, e.Session, e.Name, e.Key, string(props))
	return err
}

// Events returns the events recorded since a time, oldest first.
func (s *Store) Events(since time.Time) ([]Event, error) {
	rows, err := s.db.Query(`SELECT at, ui, session, name, key, props FROM events WHERE at >= ? ORDER BY id`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var e Event
		var at, props string
		if err := rows.Scan(&at, &e.UI, &e.Session, &e.Name, &e.Key, &props); err != nil {
			return nil, err
		}
		if e.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(props), &e.Props); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
