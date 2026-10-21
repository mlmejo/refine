package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/home"
	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/reposcan"
)

// fake is a ticket store in memory behind a Source and Refiner.
type fake struct {
	issues  []jira.Issue
	hidden  []string
	drafts  map[string]draft.Saved
	briefs  map[string]draft.SavedBrief
	triages map[string]draft.SavedTriage
	req     draft.Request
	written []string
	moved   []string
	split   error
	block   chan struct{}
	repos   map[string]string
}

func newFake() *fake {
	return &fake{
		issues:  []jira.Issue{{Key: "GEEK-1", Summary: "one"}, {Key: "GEEK-2", Summary: "two"}},
		drafts:  map[string]draft.Saved{},
		briefs:  map[string]draft.SavedBrief{},
		triages: map[string]draft.SavedTriage{},
		repos:   map[string]string{},
	}
}

func (f *fake) server() *Server {
	source := home.Source{
		Project:    "GEEK",
		Hidden:     []string{"GEEK-2"},
		SaveHidden: func(keys []string) error { f.hidden = keys; return nil },
		List: func(context.Context) (home.Page, error) {
			return home.Page{User: "me", Issues: append([]jira.Issue(nil), f.issues...)}, nil
		},
		Get: func(_ context.Context, key string) (jira.Issue, error) {
			for _, i := range f.issues {
				if i.Key == key {
					return i, nil
				}
			}
			return jira.Issue{Key: key, Summary: "opened"}, nil
		},
		Repos: func() map[string]string { return f.repos },
		AddRepo: func(name, dir string) error {
			f.repos[name] = dir
			return nil
		},
	}
	load := func(key string) (draft.Saved, bool, error) { d, ok := f.drafts[key]; return d, ok, nil }
	refiner := home.Refiner{
		Load:       load,
		LoadBrief:  func(key string) (draft.SavedBrief, bool, error) { b, ok := f.briefs[key]; return b, ok, nil },
		LoadTriage: func(key string) (draft.SavedTriage, bool, error) { t, ok := f.triages[key]; return t, ok, nil },
		Triage: func(_ context.Context, i jira.Issue) (draft.SavedTriage, error) {
			return draft.SavedTriage{ID: 1, Key: i.Key, Triage: draft.Triage{Problem: "p"}}, nil
		},
		Draft: func(ctx context.Context, i jira.Issue, req draft.Request) (draft.Saved, error) {
			f.req = req
			if f.block != nil {
				select {
				case <-f.block:
				case <-ctx.Done():
					return draft.Saved{}, ctx.Err()
				}
			}
			saved := draft.Saved{ID: 9, Key: i.Key, Step: draft.StepQuestions}
			f.drafts[i.Key] = saved
			return saved, nil
		},
		SaveAnswers: func(saved draft.Saved) error { f.drafts[saved.Key] = saved; return nil },
		Write: func(_ context.Context, key string, _ draft.Draft) (jira.Issue, error) {
			f.written = append(f.written, key)
			return jira.Issue{Key: key, AcceptanceCriteria: "done"}, nil
		},
		Split: func(_ context.Context, saved draft.Saved) (map[int]string, error) {
			return map[int]string{0: "GEEK-3"}, f.split
		},
		Move: func(_ context.Context, key string) error {
			f.moved = append(f.moved, key)
			return nil
		},
		ReadySprint: "Ready",
	}
	return New("Sprint", source, refiner)
}

func call(t *testing.T, srv *Server, method, path, body string, into any) int {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+srv.Token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if into != nil {
		data, _ := io.ReadAll(rec.Body)
		if err := json.Unmarshal(data, into); err != nil {
			t.Fatalf("%s %s: %v: %s", method, path, err, data)
		}
	}
	return rec.Code
}

func TestNeedsToken(t *testing.T) {
	srv := newFake().server()
	req := httptest.NewRequest("GET", "/api/info", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
}

func TestTicketsLeaveOutHidden(t *testing.T) {
	f := newFake()
	srv := f.server()
	var page struct {
		User   string  `json:"user"`
		Issues []issue `json:"issues"`
	}
	call(t, srv, "GET", "/api/tickets", "", &page)
	if page.User != "me" || len(page.Issues) != 1 || page.Issues[0].Key != "GEEK-1" {
		t.Fatalf("page = %+v", page)
	}

	call(t, srv, "PUT", "/api/hidden/GEEK-1", "", nil)
	call(t, srv, "GET", "/api/tickets", "", &page)
	if len(page.Issues) != 0 || len(f.hidden) != 2 {
		t.Fatalf("after hide: issues %+v, saved %v", page.Issues, f.hidden)
	}

	// Opening a hidden ticket by number unhides it.
	var opened issue
	if code := call(t, srv, "GET", "/api/lookup?q=2", "", &opened); code != http.StatusOK || opened.Key != "GEEK-2" {
		t.Fatalf("lookup = %d %+v", code, opened)
	}
	if len(f.hidden) != 1 || f.hidden[0] != "GEEK-1" {
		t.Fatalf("hidden = %v", f.hidden)
	}
}

// wait polls a job until it finishes.
func wait(t *testing.T, srv *Server, id string) job {
	t.Helper()
	for range 200 {
		var j job
		call(t, srv, "GET", "/api/jobs/"+id, "", &j)
		if j.Done {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return job{}
}

func TestFreshDraftBuildsOnBriefAndTriage(t *testing.T) {
	f := newFake()
	f.briefs["GEEK-1"] = draft.SavedBrief{ID: 2, SessionID: "b"}
	f.triages["GEEK-1"] = draft.SavedTriage{ID: 3}
	srv := f.server()

	var started job
	call(t, srv, "POST", "/api/jobs", `{"kind":"draft","key":"GEEK-1","approach":"cache it","scope":{"Pop-ups?":"no"}}`, &started)
	if !started.FromBrief {
		t.Errorf("from_brief = false, want true")
	}
	done := wait(t, srv, started.ID)
	if done.Error != "" || done.Draft == nil || done.Draft.ID != 9 {
		t.Fatalf("job = %+v", done)
	}
	if f.req.Approach != "cache it" || f.req.Scope["Pop-ups?"] != "no" || f.req.Brief == nil || f.req.Triage == nil || f.req.Resume {
		t.Fatalf("request = %+v", f.req)
	}
}

func TestResumeNeedsEveryAnswer(t *testing.T) {
	f := newFake()
	f.drafts["GEEK-1"] = draft.Saved{ID: 4, Key: "GEEK-1", Approach: "a", Step: draft.StepQuestions,
		Draft: draft.Draft{OpenQuestions: []draft.Choice{{Question: "why?"}}}}
	srv := f.server()

	var failed map[string]string
	if code := call(t, srv, "POST", "/api/jobs", `{"kind":"draft","key":"GEEK-1","resume":true}`, &failed); code != http.StatusConflict {
		t.Fatalf("code = %d, want 409: %v", code, failed)
	}

	var saved draft.Saved
	call(t, srv, "PUT", "/api/tickets/GEEK-1/answers", `{"draft_id":4,"answers":{"why?":"because"}}`, &saved)
	if saved.Answers["why?"] != "because" {
		t.Fatalf("answers = %v", saved.Answers)
	}
	var started job
	call(t, srv, "POST", "/api/jobs", `{"kind":"draft","key":"GEEK-1","resume":true}`, &started)
	wait(t, srv, started.ID)
	if !f.req.Resume || f.req.Previous == nil || f.req.Approach != "a" {
		t.Fatalf("request = %+v", f.req)
	}
}

func TestAnswersToAnOldDraftConflict(t *testing.T) {
	f := newFake()
	f.drafts["GEEK-1"] = draft.Saved{ID: 5, Key: "GEEK-1"}
	srv := f.server()
	if code := call(t, srv, "PUT", "/api/tickets/GEEK-1/answers", `{"draft_id":4,"answers":{}}`, nil); code != http.StatusConflict {
		t.Fatalf("code = %d, want 409", code)
	}
}

func TestOneJobPerTicketAndCancel(t *testing.T) {
	f := newFake()
	f.block = make(chan struct{})
	srv := f.server()

	var started job
	call(t, srv, "POST", "/api/jobs", `{"kind":"draft","key":"GEEK-1"}`, &started)
	if code := call(t, srv, "POST", "/api/jobs", `{"kind":"triage","key":"GEEK-1"}`, nil); code != http.StatusConflict {
		t.Fatalf("second job code = %d, want 409", code)
	}
	call(t, srv, "DELETE", "/api/jobs/"+started.ID, "", nil)
	if code := call(t, srv, "GET", "/api/jobs/"+started.ID, "", nil); code != http.StatusNotFound {
		t.Fatalf("cancelled job code = %d, want 404", code)
	}
	var triaged job
	call(t, srv, "POST", "/api/jobs", `{"kind":"triage","key":"GEEK-1"}`, &triaged)
	if done := wait(t, srv, triaged.ID); done.Triage == nil || done.Triage.Triage.Problem != "p" {
		t.Fatalf("triage job = %+v", done)
	}
}

func TestWriteNeedsStamp(t *testing.T) {
	f := newFake()
	f.drafts["GEEK-1"] = draft.Saved{ID: 6, Key: "GEEK-1", Step: draft.StepQuestions}
	srv := f.server()
	if code := call(t, srv, "POST", "/api/tickets/GEEK-1/write", `{"draft_id":6}`, nil); code != http.StatusConflict {
		t.Fatalf("code = %d, want 409", code)
	}
	if len(f.written) != 0 {
		t.Fatalf("written = %v", f.written)
	}
}

type written struct {
	Issue      issue          `json:"issue"`
	Children   map[int]string `json:"children"`
	SplitError string         `json:"split_error"`
	Moved      bool           `json:"moved"`
}

func TestWriteSplitsAndMoves(t *testing.T) {
	f := newFake()
	f.drafts["GEEK-1"] = draft.Saved{ID: 7, Key: "GEEK-1", Step: draft.StepStamp,
		Draft: draft.Draft{Split: []draft.Child{{Title: "child"}}}}
	srv := f.server()

	var out written
	call(t, srv, "POST", "/api/tickets/GEEK-1/write", `{"draft_id":7}`, &out)
	if out.Issue.AcceptanceCriteria != "done" || out.Children[0] != "GEEK-3" || !out.Moved {
		t.Fatalf("out = %+v", out)
	}
	if len(f.moved) != 1 {
		t.Fatalf("moved = %v", f.moved)
	}
}

func TestWriteStopsBeforeMoveWhenSplitFails(t *testing.T) {
	f := newFake()
	f.split = errors.New("jira down")
	f.drafts["GEEK-1"] = draft.Saved{ID: 7, Key: "GEEK-1", Step: draft.StepStamp,
		Draft: draft.Draft{Split: []draft.Child{{Title: "a"}, {Title: "b"}}}}
	srv := f.server()

	var out written
	call(t, srv, "POST", "/api/tickets/GEEK-1/write", `{"draft_id":7}`, &out)
	if out.SplitError != "jira down" || out.Moved || len(f.moved) != 0 {
		t.Fatalf("out = %+v, moved %v", out, f.moved)
	}
}

func TestScanThenAddRepos(t *testing.T) {
	f := newFake()
	root := t.TempDir()
	for _, dir := range []string{"api/.git", "web/.git", "notes"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.repos["api"] = filepath.Join(root, "api")
	srv := f.server()

	var found []reposcan.Found
	call(t, srv, "GET", "/api/repos/scan?dir="+url.QueryEscape(root), "", &found)
	if len(found) != 2 || found[0].Listed != "api" || found[1].Name != "web" || found[1].Listed != "" {
		t.Fatalf("found %+v", found)
	}
	body := fmt.Sprintf(`{"name":"web","dir":%q}`, found[1].Dir)
	if code := call(t, srv, "POST", "/api/repos", body, nil); code != http.StatusOK || f.repos["web"] == "" {
		t.Fatalf("add code %d, repos %v", code, f.repos)
	}
	body = fmt.Sprintf(`{"name":"notes","dir":%q}`, filepath.Join(root, "notes"))
	if code := call(t, srv, "POST", "/api/repos", body, nil); code != http.StatusBadRequest {
		t.Fatalf("adding a plain folder: code %d", code)
	}
}
