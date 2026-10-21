package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mlmejo/refine/internal/draft"
)

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(filepath.Join(dir, "refine.db"), filepath.Join(dir, "exports"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAddKeepsEveryVersionWithItsMarkdown(t *testing.T) {
	s := open(t, t.TempDir())
	first := time.Date(2026, 10, 8, 9, 0, 0, 0, time.Local)

	older, err := s.Add(draft.Saved{Key: "GEEK-1", Approach: "payments", Created: first, Draft: draft.Draft{Points: 5}}, "SDK")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.Add(draft.Saved{Key: "GEEK-1", Approach: "operator replies", Created: first.Add(time.Hour), Draft: draft.Draft{Points: 2}, Turns: 12, Usage: draft.Usage{CacheRead: 300}}, "SDK")
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(s.Exports, "GEEK-1", "20261008-100000.md"); newer.Markdown != want {
		t.Errorf("markdown = %q, want %q", newer.Markdown, want)
	}
	if data, _ := os.ReadFile(older.Markdown); !strings.HasPrefix(string(data), "# GEEK-1: SDK") {
		t.Errorf("older markdown:\n%s", data)
	}

	latest, ok, err := s.Latest("GEEK-1")
	if err != nil || !ok || latest.ID != newer.ID || latest.Approach != "operator replies" || latest.Draft.Points != 2 || latest.Turns != 12 || latest.Usage.CacheRead != 300 || !latest.Created.Equal(newer.Created) {
		t.Errorf("latest = %+v ok %v err %v", latest, ok, err)
	}

	history, err := s.History("GEEK-1")
	if err != nil || len(history) != 2 || history[1].Markdown != older.Markdown {
		t.Errorf("history = %+v err %v", history, err)
	}

	if _, ok, err := s.Latest("GEEK-2"); ok || err != nil {
		t.Errorf("unknown ticket ok %v err %v", ok, err)
	}
}

func TestSaveAnswersPersistsAndRewritesMarkdown(t *testing.T) {
	s := open(t, t.TempDir())
	saved, err := s.Add(draft.Saved{Key: "GEEK-3", Draft: draft.Draft{OpenQuestions: []string{"Which queue?"}}}, "SDK")
	if err != nil {
		t.Fatal(err)
	}

	saved.Answers = map[string]string{"Which queue?": "starvault"}
	if err := s.SaveAnswers(saved); err != nil {
		t.Fatal(err)
	}

	latest, _, err := s.Latest("GEEK-3")
	if err != nil || latest.Answers["Which queue?"] != "starvault" {
		t.Errorf("answers = %v err %v", latest.Answers, err)
	}
	if data, _ := os.ReadFile(saved.Markdown); !strings.Contains(string(data), "**Answer:** starvault") || !strings.HasPrefix(string(data), "# GEEK-3: SDK") {
		t.Errorf("markdown:\n%s", data)
	}
}

func TestSaveChildrenPersistsAndRewritesMarkdown(t *testing.T) {
	s := open(t, t.TempDir())
	saved, err := s.Add(draft.Saved{Key: "GEEK-5", Draft: draft.Draft{Split: []draft.Child{{Title: "Books - Adding"}, {Title: "Books - Deleting"}}}}, "Books")
	if err != nil {
		t.Fatal(err)
	}

	saved.Children = map[int]string{1: "GEEK-7"}
	if err := s.SaveChildren(saved); err != nil {
		t.Fatal(err)
	}

	latest, _, err := s.Latest("GEEK-5")
	if err != nil || len(latest.Children) != 1 || latest.Children[1] != "GEEK-7" {
		t.Errorf("children = %v err %v", latest.Children, err)
	}
	if data, _ := os.ReadFile(saved.Markdown); !strings.Contains(string(data), "Created as GEEK-7.") {
		t.Errorf("markdown:\n%s", data)
	}
}

func TestOpenMigratesVersionOneDatabase(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	if _, err := s.db.Exec(`ALTER TABLE drafts DROP COLUMN answers; ALTER TABLE drafts DROP COLUMN session_cost_usd; ALTER TABLE drafts DROP COLUMN turns; ALTER TABLE drafts DROP COLUMN usage; ALTER TABLE drafts DROP COLUMN children; ALTER TABLE drafts DROP COLUMN step; DROP TABLE outlines; DROP TABLE briefs; DROP TABLE triages; PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s = open(t, dir)
	if _, err := s.Add(draft.Saved{Key: "GEEK-4", Answers: map[string]string{"q": "no"}}, ""); err != nil {
		t.Fatal(err)
	}
	if latest, _, _ := s.Latest("GEEK-4"); latest.Answers["q"] != "no" || !latest.Stamped() {
		t.Errorf("answers = %v, stamped = %v", latest.Answers, latest.Stamped())
	}
}

func TestStepIsKeptPerDraft(t *testing.T) {
	s := open(t, t.TempDir())
	if _, err := s.Add(draft.Saved{Key: "GEEK-9", Step: draft.StepQuestions, Draft: draft.Draft{Findings: []string{"f"}, Problem: "p"}}, ""); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Latest("GEEK-9")
	if err != nil || got.Stamped() || got.Draft.Findings[0] != "f" || got.Draft.Problem != "p" {
		t.Errorf("latest = %+v err %v", got, err)
	}
	if _, err := s.Add(draft.Saved{Key: "GEEK-9", Step: draft.StepStamp}, ""); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Latest("GEEK-9"); got.Step != draft.StepStamp {
		t.Errorf("step = %q", got.Step)
	}
}

func TestTriagesKeepTheLatestPerTicket(t *testing.T) {
	s := open(t, t.TempDir())
	if _, ok, err := s.LatestTriage("GEEK-5"); ok || err != nil {
		t.Fatalf("empty store: ok %v err %v", ok, err)
	}
	if _, err := s.AddTriage(draft.SavedTriage{Key: "GEEK-5", Triage: draft.Triage{Problem: "old"}}); err != nil {
		t.Fatal(err)
	}
	added, err := s.AddTriage(draft.SavedTriage{Key: "GEEK-5", SessionID: "t-2", CostUSD: 0.05, Turns: 1, Usage: draft.Usage{Output: 300},
		Triage: draft.Triage{Problem: "new", Blockers: []string{"GEEK-1"}, Relations: []draft.Relation{{Key: "GEEK-2", Kind: "needs"}}}})
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LatestTriage("GEEK-5")
	if !ok || err != nil || got.ID != added.ID || got.SessionID != "t-2" || got.Turns != 1 || got.Usage.Output != 300 ||
		got.Triage.Problem != "new" || got.Triage.Blockers[0] != "GEEK-1" || got.Triage.Relations[0].Key != "GEEK-2" {
		t.Errorf("latest = %+v ok %v err %v", got, ok, err)
	}
}

func TestOutlinesAreCachedPerRepoAndCommit(t *testing.T) {
	s := open(t, t.TempDir())
	if _, ok, err := s.Outline("dating", "abc"); ok || err != nil {
		t.Fatalf("empty cache ok %v err %v", ok, err)
	}
	if err := s.SaveOutline("dating", "abc", "plugins/ (18)"); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := s.Outline("dating", "abc"); !ok || err != nil || got != "plugins/ (18)" {
		t.Errorf("outline = %q ok %v err %v", got, ok, err)
	}
	if _, ok, _ := s.Outline("dating", "def"); ok {
		t.Error("another commit hit the cache")
	}
}

func TestBriefsKeepTheLatestPerTicket(t *testing.T) {
	s := open(t, t.TempDir())
	if _, ok, err := s.LatestBrief("GEEK-5"); ok || err != nil {
		t.Fatalf("empty ok %v err %v", ok, err)
	}
	if _, err := s.AddBrief(draft.SavedBrief{Key: "GEEK-5", SessionID: "s-1", Brief: draft.Brief{Summary: "old"}}); err != nil {
		t.Fatal(err)
	}
	added, err := s.AddBrief(draft.SavedBrief{Key: "GEEK-5", Ref: "develop", SessionID: "s-2", CostUSD: 0.9, Turns: 12,
		Usage: draft.Usage{Output: 4000}, Brief: draft.Brief{Summary: "new", Parts: []draft.Part{{Repo: "dating", Path: "a.vue", Role: "card"}}}})
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.LatestBrief("GEEK-5")
	if !ok || err != nil || got.ID != added.ID || got.SessionID != "s-2" || got.Ref != "develop" || got.Turns != 12 ||
		got.Usage.Output != 4000 || got.Brief.Summary != "new" || got.Brief.Parts[0].Role != "card" {
		t.Errorf("latest = %+v ok %v err %v", got, ok, err)
	}
}
