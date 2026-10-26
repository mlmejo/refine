package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mlmejo/refine/internal/refine"
)

// now is the clock the tests give the store.
var now = time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC)

func TestSaveLoadAndDelete(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "refine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Now = func() time.Time { return now }
	if s.Backup != "" {
		t.Errorf("a new database was backed up to %s", s.Backup)
	}
	r := refine.New("GEEK-1", "Refunds")
	r.Repos = []string{"admin"}
	if err := s.Save(r); err != nil {
		t.Fatal(err)
	}
	r.Step = refine.StepRepos
	if err := s.Save(r); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Load("GEEK-1")
	if err != nil || !ok || got.Step != refine.StepRepos || got.Repos[0] != "admin" || !got.Updated.Equal(now) {
		t.Fatalf("load = %+v %v %v", got, ok, err)
	}
	all, _ := s.All()
	if len(all) != 1 {
		t.Fatalf("all = %+v", all)
	}
	s.Delete("GEEK-1")
	if _, ok, _ := s.Load("GEEK-1"); ok {
		t.Fatal("deleted refinement still there")
	}
}

func TestProfiles(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "refine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Now = func() time.Time { return now }
	s.SaveProfile(refine.Profile{Repo: "admin", Answers: []refine.QA{{Question: "How?", Answer: "Forge"}}})
	got, _ := s.Profiles()
	if got["admin"].Answers[0].Answer != "Forge" || !got["admin"].Updated.Equal(now) {
		t.Fatalf("profiles = %+v", got)
	}
	s.SaveProfile(refine.Profile{Repo: "admin"})
	if got, _ := s.Profiles(); len(got) != 0 {
		t.Fatalf("emptied profile kept: %+v", got)
	}
}

func TestUpgradeBacksUpThenDropsTheOldTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refine.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE drafts (id INTEGER PRIMARY KEY, key TEXT);
		INSERT INTO drafts (key) VALUES ('GEEK-1');
		CREATE TABLE triages (id INTEGER PRIMARY KEY);
		PRAGMA user_version = 10;`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Backup == "" {
		t.Fatal("no backup")
	}
	if _, err := os.Stat(s.Backup); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('drafts', 'triages')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("old tables left: %d %v", n, err)
	}
	backup, err := sql.Open("sqlite3", s.Backup)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var key string
	if err := backup.QueryRow(`SELECT key FROM drafts`).Scan(&key); err != nil || key != "GEEK-1" {
		t.Fatalf("backup = %q %v", key, err)
	}

	// Opening again changes nothing.
	s.Close()
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.Backup != "" {
		t.Errorf("backed up twice: %s", again.Backup)
	}
}
