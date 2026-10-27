package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mlmejo/refine/internal/claude"
	"github.com/mlmejo/refine/internal/gitsync"
	"github.com/mlmejo/refine/internal/hidden"
	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/refine"
	"github.com/mlmejo/refine/internal/store"
)

// fakeRepos is a fixed list of repos; it can't change.
type fakeRepos map[string]string

func (r fakeRepos) List() map[string]string  { return maps.Clone(r) }
func (fakeRepos) Add(name, dir string) error { return errors.ErrUnsupported }
func (fakeRepos) Remove(name string) error   { return errors.ErrUnsupported }

// fakeJira holds tickets. When hold is set, Update waits on it, telling
// held it has started.
type fakeJira struct {
	tickets map[string]jira.Ticket
	hold    chan struct{}
	held    chan struct{}
}

func (f fakeJira) Myself(context.Context) (string, error) { return "Dev", nil }
func (f fakeJira) Linked(context.Context, string) ([]jira.Ticket, error) {
	return []jira.Ticket{f.tickets["GEEK-101"], f.tickets["GEEK-100"]}, nil
}
func (f fakeJira) Tickets(context.Context, string) ([]jira.Ticket, error) {
	return []jira.Ticket{f.tickets["GEEK-100"]}, nil
}
func (f fakeJira) Ticket(_ context.Context, key string) (jira.Ticket, error) {
	t, ok := f.tickets[key]
	if !ok {
		return jira.Ticket{}, &jira.APIError{StatusCode: http.StatusNotFound, Messages: []string{"no " + key}}
	}
	return t, nil
}
func (f fakeJira) Update(ctx context.Context, _ string, _ jira.Refinement) error {
	if f.hold != nil {
		f.held <- struct{}{}
		<-f.hold
	}
	return ctx.Err()
}
func (fakeJira) Comment(context.Context, string, string) error { return nil }
func (fakeJira) Split(context.Context, string, string) (jira.SplitTarget, error) {
	return jira.SplitTarget{}, nil
}
func (fakeJira) CreateChild(ctx context.Context, _ jira.SplitTarget, _ jira.NewChild) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "GEEK-900", nil
}
func (fakeJira) Relate(context.Context, string, string) error       { return nil }
func (fakeJira) MoveToSprint(context.Context, string, string) error { return nil }

// slowClaude finds GEEK-100 as a blocker once released.
type slowClaude struct{ release chan struct{} }

func (c slowClaude) Do(ctx context.Context, call claude.Call, into any, progress func(claude.Activity)) (claude.Run, error) {
	progress(claude.Activity{Verb: "Read", What: "the ticket"})
	select {
	case <-c.release:
	case <-ctx.Done():
		return claude.Run{}, ctx.Err()
	}
	data := `{"blockers":[{"key":"GEEK-100","reason":"needs Stripe","quote":"Stripe"}],"overlaps":[]}`
	return claude.Run{SessionID: "s1"}, json.Unmarshal([]byte(data), into)
}

type noGit struct{}

func (noGit) Fetch(_ context.Context, name, dir string) (gitsync.Status, error) {
	return gitsync.Status{Name: name, Dir: dir, Branch: "develop", Commit: "abc"}, nil
}
func (noGit) Checkout(context.Context, string, string, string) error { return nil }

func setup(t *testing.T) (*Server, chan struct{}) {
	t.Helper()
	return setupJira(t, fakeJira{})
}

func setupJira(t *testing.T, j fakeJira) (*Server, chan struct{}) {
	t.Helper()
	release := make(chan struct{})
	return setupWith(t, j, slowClaude{release: release}), release
}

func setupWith(t *testing.T, j fakeJira, c refine.Claude) *Server {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "refine.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	j.tickets = map[string]jira.Ticket{
		"GEEK-100": {Issue: jira.Issue{Key: "GEEK-100", Summary: "Stripe", AcceptanceCriteria: "* a", TechnicalImplementation: "x"}},
		"GEEK-101": {Issue: jira.Issue{Key: "GEEK-101", Summary: "Refunds"}, DescriptionMarkdown: "Use **Stripe**."},
	}
	hiddenSet, err := hidden.Open(filepath.Join(t.TempDir(), "hidden.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := &refine.Service{Jira: j, Claude: c, Store: st, Git: noGit{},
		Sprint: "S", ReadySprint: "R", Repos: fakeRepos{"admin": "/r/admin"}, Hidden: hiddenSet}
	srv := New(Info{Project: "GEEK"}, svc, fakeRepos{"admin": "/r/admin"}, hiddenSet)
	t.Cleanup(srv.Close)
	return srv
}

func call(t *testing.T, srv *Server, method, path, body string, out any) int {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: %v: %s", method, path, err, rec.Body.String())
		}
	}
	return rec.Code
}

func TestNeedsTheToken(t *testing.T) {
	srv, _ := setup(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/info", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestNewLeavesTheServiceAlone(t *testing.T) {
	svc := &refine.Service{}
	srv := New(Info{}, svc, fakeRepos{}, hiddenKeys{})
	t.Cleanup(srv.Close)
	if svc.Hidden != nil {
		t.Fatal("New set the service's Hidden")
	}
	if len(srv.Token()) != 48 {
		t.Fatalf("token = %q", srv.Token())
	}
}

func TestHideWritesTheSetItWasGiven(t *testing.T) {
	keys := hiddenKeys{}
	srv := New(Info{Project: "GEEK"}, &refine.Service{}, fakeRepos{}, keys)
	t.Cleanup(srv.Close)
	call(t, srv, "PUT", "/api/hidden/GEEK-7", "", nil)
	if !keys["GEEK-7"] {
		t.Fatalf("keys = %v", keys)
	}
	call(t, srv, "GET", "/api/lookup?q=7", "", nil)
	if keys["GEEK-7"] {
		t.Fatalf("lookup left GEEK-7 hidden: %v", keys)
	}
}

// hiddenKeys hides the keys set to true.
type hiddenKeys map[string]bool

func (h hiddenKeys) Set(key string, hide bool) error {
	if hide {
		h[key] = true
	} else {
		delete(h, key)
	}
	return nil
}

func TestListLeavesOutRefinedAndHiddenTickets(t *testing.T) {
	srv, _ := setup(t)
	var page refine.Page
	call(t, srv, "GET", "/api/tickets", "", &page)
	if len(page.Rows) != 1 || page.Rows[0].Key != "GEEK-101" || page.Rows[0].Step != "Not started" {
		t.Fatalf("page = %+v", page)
	}
	call(t, srv, "PUT", "/api/hidden/GEEK-101", "", nil)
	call(t, srv, "GET", "/api/tickets", "", &page)
	if len(page.Rows) != 0 {
		t.Fatalf("hidden ticket listed: %+v", page)
	}
	var found map[string]string
	call(t, srv, "GET", "/api/lookup?q=101", "", &found)
	call(t, srv, "GET", "/api/tickets", "", &page)
	if found["key"] != "GEEK-101" || len(page.Rows) != 1 {
		t.Fatalf("lookup = %v, page = %+v", found, page)
	}
}

func TestSearchJobThenDecideAndContinue(t *testing.T) {
	srv, release := setup(t)
	var view refine.View
	call(t, srv, "GET", "/api/tickets/GEEK-101", "", &view)
	if view.Started || view.Ticket.DescriptionMD != "Use **Stripe**." || view.Refinement.Step != refine.StepRelated {
		t.Fatalf("view = %+v", view)
	}

	var started job
	if code := call(t, srv, "POST", "/api/jobs", `{"kind":"search","key":"GEEK-101"}`, &started); code != 200 {
		t.Fatalf("start = %d", code)
	}
	if code := call(t, srv, "POST", "/api/jobs", `{"kind":"search","key":"GEEK-102"}`, nil); code != http.StatusConflict {
		t.Fatalf("second job = %d", code)
	}
	var current job
	for deadline := time.Now().Add(time.Second); len(current.Log) == 0 && time.Now().Before(deadline); {
		call(t, srv, "GET", "/api/job", "", &current)
	}
	if current.ID != started.ID || current.Done || current.Log[0].What != "the ticket" {
		t.Fatalf("running job = %+v", current)
	}
	close(release)
	for deadline := time.Now().Add(time.Second); !current.Done && time.Now().Before(deadline); {
		call(t, srv, "GET", "/api/job", "", &current)
	}
	if !current.Done || current.Error != "" {
		t.Fatalf("finished job = %+v", current)
	}

	call(t, srv, "PUT", "/api/tickets/GEEK-101/decisions", `{"blocker":"GEEK-100","decision":"accepted"}`, &view)
	if view.Refinement.Related.Blockers[0].Decision != refine.Accepted || len(view.Plan) != 0 {
		t.Fatalf("after deciding = %+v", view.Refinement.Related)
	}
	call(t, srv, "POST", "/api/tickets/GEEK-101/continue", "", &view)
	if view.Refinement.Step != refine.StepRepos {
		t.Fatalf("after continue = %+v", view.Refinement)
	}
	if code := call(t, srv, "PUT", "/api/tickets/GEEK-101/answers", `{"round":["a"]}`, nil); code != http.StatusConflict {
		t.Fatalf("answers at the wrong step = %d", code)
	}
	call(t, srv, "PUT", "/api/tickets/GEEK-101/repos", `{"repos":["admin"]}`, &view)
	call(t, srv, "POST", "/api/tickets/GEEK-101/sync", "", &view)
	if view.Refinement.Step != refine.StepQuestions || view.Refinement.Synced[0].Commit != "abc" {
		t.Fatalf("after sync = %+v", view.Refinement)
	}

	var page refine.Page
	call(t, srv, "GET", "/api/tickets", "", &page)
	if !page.Rows[0].Started || !strings.HasPrefix(page.Rows[0].Step, "Questions") {
		t.Fatalf("page = %+v", page)
	}
}

func TestOpenAMissingTicketIsNotFound(t *testing.T) {
	srv, _ := setup(t)
	if code := call(t, srv, "GET", "/api/tickets/GEEK-999", "", nil); code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
}

// stubbornClaude keeps running after cancel until unwound, like a Claude
// process given time to exit.
type stubbornClaude struct{ started, unwind chan struct{} }

func (c stubbornClaude) Do(ctx context.Context, call claude.Call, into any, progress func(claude.Activity)) (claude.Run, error) {
	c.started <- struct{}{}
	<-ctx.Done()
	<-c.unwind
	return claude.Run{}, ctx.Err()
}

// returnsBefore reports whether f returns before unwind is closed.
func returnsBefore(t *testing.T, f func(), unwind chan struct{}) {
	t.Helper()
	returned := make(chan struct{})
	go func() {
		f()
		close(returned)
	}()
	select {
	case <-returned:
		t.Fatal("returned while the job was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(unwind)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("did not return after the job stopped")
	}
}

func TestCancelJobWaitsForTheRunToStop(t *testing.T) {
	c := stubbornClaude{started: make(chan struct{}, 2), unwind: make(chan struct{})}
	srv := setupWith(t, fakeJira{}, c)
	var started job
	if code := call(t, srv, "POST", "/api/jobs", `{"kind":"search","key":"GEEK-101"}`, &started); code != 200 {
		t.Fatalf("start = %d", code)
	}
	<-c.started
	returnsBefore(t, func() {
		go func() {
			time.Sleep(20 * time.Millisecond)
			if code := call(t, srv, "POST", "/api/jobs", `{"kind":"search","key":"GEEK-101"}`, nil); code != http.StatusConflict {
				t.Errorf("job started while the cancelled one still ran: %d", code)
			}
		}()
		call(t, srv, "DELETE", "/api/jobs/"+started.ID, "", nil)
	}, c.unwind)
	var current *job
	call(t, srv, "GET", "/api/job", "", &current)
	if current != nil {
		t.Fatalf("job after cancel = %+v", current)
	}
	if code := call(t, srv, "POST", "/api/jobs", `{"kind":"search","key":"GEEK-101"}`, nil); code != 200 {
		t.Fatalf("restart = %d", code)
	}
}

func TestCloseWaitsForTheRunToStop(t *testing.T) {
	c := stubbornClaude{started: make(chan struct{}, 1), unwind: make(chan struct{})}
	srv := setupWith(t, fakeJira{}, c)
	if code := call(t, srv, "POST", "/api/jobs", `{"kind":"search","key":"GEEK-101"}`, nil); code != 200 {
		t.Fatalf("start = %d", code)
	}
	<-c.started
	returnsBefore(t, srv.Close, c.unwind)
}

func TestSetProfileNeedsAKnownRepo(t *testing.T) {
	srv, _ := setup(t)
	if code := call(t, srv, "PUT", "/api/profiles/nope", `{"answers":[]}`, nil); code != http.StatusNotFound {
		t.Fatalf("code = %d", code)
	}
	var repos []map[string]any
	call(t, srv, "PUT", "/api/profiles/admin", `{"answers":[{"question":"How?","answer":"Forge"}]}`, &repos)
	if len(repos) != 1 || repos[0]["profile"] == nil {
		t.Fatalf("repos = %+v", repos)
	}
}

// readyToStamp puts GEEK-101 at its results, with one split ticket.
func readyToStamp(t *testing.T, srv *Server) {
	t.Helper()
	err := srv.service.Store.Save(context.Background(), refine.Refinement{Key: "GEEK-101", Step: refine.StepResults,
		Result: &refine.Result{Points: 2, Split: []refine.Child{{Title: "Refunds - Partial", Points: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStampFinishesWhenTheAppDropsTheRequest(t *testing.T) {
	srv, _ := setup(t)
	readyToStamp(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/api/tickets/GEEK-101/stamp", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+srv.Token())
	srv.Handler().ServeHTTP(httptest.NewRecorder(), req)

	r, _, err := srv.service.Store.Load(context.Background(), "GEEK-101")
	if err != nil || r.Step != refine.StepStamped || r.Stamp.Children[0] != "GEEK-900" {
		t.Fatalf("after stamp = %+v, err %v", r, err)
	}
}

func TestOverlappingStampsOnATicketConflict(t *testing.T) {
	hold, held := make(chan struct{}), make(chan struct{}, 2)
	srv, _ := setupJira(t, fakeJira{hold: hold, held: held})
	readyToStamp(t, srv)
	first := make(chan int)
	go func() { first <- call(t, srv, "POST", "/api/tickets/GEEK-101/stamp", "", nil) }()
	<-held
	if code := call(t, srv, "POST", "/api/tickets/GEEK-101/stamp", "", nil); code != http.StatusConflict {
		t.Errorf("second stamp = %d", code)
	}
	if code := call(t, srv, "DELETE", "/api/tickets/GEEK-101", "", nil); code != http.StatusConflict {
		t.Errorf("reset while stamping = %d", code)
	}
	close(hold)
	if code := <-first; code != http.StatusOK {
		t.Fatalf("first stamp = %d", code)
	}
	if code := call(t, srv, "DELETE", "/api/tickets/GEEK-101", "", nil); code != http.StatusNoContent {
		t.Errorf("reset after the stamp finished = %d", code)
	}
}

func TestHandlersStopWhenTheRequestIsCancelled(t *testing.T) {
	srv, _ := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, path := range []string{"/api/tickets/GEEK-101", "/api/repos"} {
		method := "DELETE"
		if path == "/api/repos" {
			method = "GET"
		}
		req := httptest.NewRequestWithContext(ctx, method, path, nil)
		req.Header.Set("Authorization", "Bearer "+srv.Token())
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "context canceled") {
			t.Errorf("%s %s = %d %s", method, path, rec.Code, rec.Body.String())
		}
	}
}
