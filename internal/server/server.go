// Package server serves refine to the desktop app as JSON over HTTP on
// localhost. It runs the same Source and Refiner the TUI does.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/home"
	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/reposcan"
	"github.com/mlmejo/refine/internal/version"
)

// Server answers the desktop app. Every request needs the token.
type Server struct {
	Title string
	// Site is the Jira site the app links tickets to.
	Site    string
	Source  home.Source
	Refiner home.Refiner
	Token   string
	// Tracking tells the app to send its usage events.
	Tracking bool

	mu     sync.Mutex
	hidden map[string]bool
	jobs   map[string]*job
	nextID int
}

// New returns a server with a fresh random token.
func New(title string, source home.Source, refiner home.Refiner) *Server {
	token := make([]byte, 24)
	_, _ = rand.Read(token)
	hidden := map[string]bool{}
	for _, key := range source.Hidden {
		hidden[key] = true
	}
	return &Server{
		Title:   title,
		Source:  source,
		Refiner: refiner,
		Token:   hex.EncodeToString(token),
		hidden:  hidden,
		jobs:    map[string]*job{},
	}
}

// Handler routes the API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("POST /api/events", s.event)
	mux.HandleFunc("GET /api/tickets", s.tickets)
	mux.HandleFunc("GET /api/lookup", s.lookup)
	mux.HandleFunc("PUT /api/hidden/{key}", s.hide(true))
	mux.HandleFunc("DELETE /api/hidden/{key}", s.hide(false))
	mux.HandleFunc("GET /api/tickets/{key}/saved", s.saved)
	mux.HandleFunc("GET /api/tickets/{key}/context", s.context)
	mux.HandleFunc("PUT /api/tickets/{key}/answers", s.answers)
	mux.HandleFunc("POST /api/tickets/{key}/reveal", s.reveal)
	mux.HandleFunc("POST /api/tickets/{key}/write", s.write)
	mux.HandleFunc("POST /api/tickets/{key}/move", s.move)
	mux.HandleFunc("POST /api/sync/check", s.check)
	mux.HandleFunc("POST /api/sync/pull", s.pull)
	mux.HandleFunc("GET /api/repos", s.repos)
	mux.HandleFunc("POST /api/repos", s.addRepo)
	mux.HandleFunc("DELETE /api/repos/{name}", s.removeRepo)
	mux.HandleFunc("GET /api/folders", s.folders)
	mux.HandleFunc("GET /api/repos/scan", s.scan)
	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("POST /api/jobs", s.startJob)
	mux.HandleFunc("GET /api/jobs/{id}", s.getJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.cancelJob)
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	want := []byte("Bearer " + s.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			fail(w, http.StatusUnauthorized, errors.New("bad token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("read the request: %w", err))
		return false
	}
	return true
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	reply(w, map[string]any{
		"title":        s.Title,
		"site":         s.Site,
		"project":      s.Source.Project,
		"ready_sprint": s.Refiner.ReadySprint,
		"sync":         s.Refiner.Check != nil,
		"tracking":     s.Source.Track != nil && s.Tracking,
		"build":        version.Build(),
		"prompts":      draft.PromptsVersion(),
	})
}

// event records a usage event from the app. The app sends names, ticket
// keys, counts and indexes only.
func (s *Server) event(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string         `json:"name"`
		Key   string         `json:"key"`
		Props map[string]any `json:"props"`
	}
	if !decode(w, r, &body) {
		return
	}
	if s.Tracking && s.Source.Track != nil && body.Name != "" {
		s.Source.Track(body.Name, body.Key, body.Props)
	}
	w.WriteHeader(http.StatusNoContent)
}

// tickets lists the sprint's tickets, hidden ones left out.
func (s *Server) tickets(w http.ResponseWriter, r *http.Request) {
	page, err := s.Source.List(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	s.mu.Lock()
	issues := slices.DeleteFunc(page.Issues, func(i jira.Issue) bool { return s.hidden[i.Key] })
	s.mu.Unlock()
	reply(w, map[string]any{"user": page.User, "issues": toIssues(issues)})
}

// lookup reads one ticket by key, or by number in the project. A ticket
// opened this way is no longer hidden.
func (s *Server) lookup(w http.ResponseWriter, r *http.Request) {
	key, ok := jira.ParseKey(r.URL.Query().Get("q"), s.Source.Project)
	if !ok {
		fail(w, http.StatusBadRequest, errors.New("not a ticket key"))
		return
	}
	found, err := s.Source.Get(r.Context(), key)
	if err != nil {
		fail(w, http.StatusBadGateway, fmt.Errorf("%s: %w", key, err))
		return
	}
	if err := s.setHidden(found.Key, false); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, toIssue(found))
}

func (s *Server) hide(hide bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.setHidden(r.PathValue("key"), hide); err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// setHidden updates the hidden set and saves it.
func (s *Server) setHidden(key string, hide bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hidden[key] == hide {
		return nil
	}
	hidden := maps.Clone(s.hidden)
	if hide {
		hidden[key] = true
	} else {
		delete(hidden, key)
	}
	if s.Source.SaveHidden != nil {
		if err := s.Source.SaveHidden(slices.Sorted(maps.Keys(hidden))); err != nil {
			return fmt.Errorf("could not save hidden tickets: %w", err)
		}
	}
	s.hidden = hidden
	return nil
}

// context is the ticket as Jira has it, unsummarised: description,
// comments, links, and its epic or parent with the parent's other children.
func (s *Server) context(w http.ResponseWriter, r *http.Request) {
	if s.Source.Ticket == nil {
		fail(w, http.StatusNotImplemented, errors.New("no ticket reader"))
		return
	}
	t, err := s.Source.Ticket(r.Context(), r.PathValue("key"))
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	out := ticketContext{
		issue:         toIssue(t.Issue),
		Description:   t.Description,
		DescriptionMD: t.DescriptionMarkdown,
		Comments:      []comment{},
		Links:         []link{},
		Siblings:      []issue{},
	}
	for _, c := range t.Comments {
		out.Comments = append(out.Comments, comment{Author: c.Author, Body: c.Body})
	}
	for _, l := range t.Links {
		out.Links = append(out.Links, link{Relation: l.Relation, Key: l.Key, Summary: l.Summary, Status: l.Status})
	}
	if t.Parent != "" && s.Source.Family != nil {
		// A failed family read leaves the ticket itself to show.
		if family, err := s.Source.Family(r.Context(), t.Parent); err == nil {
			for _, f := range family {
				switch f.Key {
				case t.Parent:
					p := toIssue(f)
					out.Parent = &p
				case t.Key:
				default:
					out.Siblings = append(out.Siblings, toIssue(f))
				}
			}
		}
		if out.Parent == nil {
			out.Parent = &issue{Key: t.Parent}
		}
	}
	reply(w, out)
}

// saved is the ticket's latest draft, brief and triage, each null when it
// has none.
func (s *Server) saved(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	out := struct {
		Draft  *draft.Saved `json:"draft"`
		Brief  *brief       `json:"brief"`
		Triage *triage      `json:"triage"`
	}{}
	if d, ok, err := s.Refiner.Load(key); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	} else if ok {
		out.Draft = &d
	}
	if b, ok, err := s.Refiner.LoadBrief(key); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	} else if ok {
		out.Brief = toBrief(b)
	}
	if t, ok, err := s.Refiner.LoadTriage(key); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	} else if ok {
		out.Triage = toTriage(t)
	}
	reply(w, out)
}

// latest reads the ticket's latest draft, which must be the one the app has.
func (s *Server) latest(w http.ResponseWriter, key string, id int64) (draft.Saved, bool) {
	saved, ok, err := s.Refiner.Load(key)
	switch {
	case err != nil:
		fail(w, http.StatusInternalServerError, err)
		return draft.Saved{}, false
	case !ok:
		fail(w, http.StatusNotFound, fmt.Errorf("%s has no draft", key))
		return draft.Saved{}, false
	case id != 0 && saved.ID != id:
		fail(w, http.StatusConflict, fmt.Errorf("%s has a newer draft; reload it", key))
		return draft.Saved{}, false
	}
	return saved, true
}

// answers keeps the answers on the ticket's latest draft.
func (s *Server) answers(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DraftID int64             `json:"draft_id"`
		Answers map[string]string `json:"answers"`
	}
	if !decode(w, r, &body) {
		return
	}
	saved, ok := s.latest(w, r.PathValue("key"), body.DraftID)
	if !ok {
		return
	}
	saved.Answers = body.Answers
	if err := s.Refiner.SaveAnswers(saved); err != nil {
		fail(w, http.StatusInternalServerError, fmt.Errorf("could not save the answer: %w", err))
		return
	}
	reply(w, saved)
}

// reveal shows the latest draft's markdown file.
func (s *Server) reveal(w http.ResponseWriter, r *http.Request) {
	saved, ok := s.latest(w, r.PathValue("key"), 0)
	if !ok {
		return
	}
	if saved.Markdown == "" || s.Refiner.Reveal == nil {
		fail(w, http.StatusNotFound, errors.New("the draft has no markdown file"))
		return
	}
	if err := s.Refiner.Reveal(saved); err != nil {
		fail(w, http.StatusInternalServerError, fmt.Errorf("could not open %s: %w", saved.Markdown, err))
		return
	}
	reply(w, map[string]string{"path": saved.Markdown})
}

// write puts the stamped draft in the ticket, creates its split tickets, and
// moves it to the ready sprint, as the TUI's w does. A split or move that
// fails after the write comes back as split_error or move_error.
func (s *Server) write(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DraftID int64 `json:"draft_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	saved, ok := s.latest(w, r.PathValue("key"), body.DraftID)
	if !ok {
		return
	}
	if !saved.Stamped() {
		fail(w, http.StatusConflict, errors.New("stamp the ticket before writing to Jira"))
		return
	}
	// A draft from before steps had no stamp, so its questions had to be
	// answered and redrafted instead.
	if left := saved.Unanswered(); left > 0 && saved.Step == "" {
		fail(w, http.StatusConflict, fmt.Errorf("answer %d more question%s and redraft before writing to Jira", left, plural(left)))
		return
	}
	// Writing goes on if the app goes away mid-way, so a split is not left
	// half made.
	ctx := context.WithoutCancel(r.Context())
	written, err := s.Refiner.Write(ctx, saved.Key, saved.Draft)
	if err != nil {
		fail(w, http.StatusBadGateway, fmt.Errorf("could not write to Jira: %w", err))
		return
	}
	out := struct {
		Issue      issue          `json:"issue"`
		Children   map[int]string `json:"children,omitempty"`
		SplitError string         `json:"split_error,omitempty"`
		MoveError  string         `json:"move_error,omitempty"`
		Moved      bool           `json:"moved"`
	}{Issue: toIssue(written), Children: saved.Children}
	if s.Refiner.Split != nil && len(saved.Draft.Split) > 0 {
		var splitErr error
		out.Children, splitErr = s.Refiner.Split(ctx, saved)
		if splitErr != nil {
			out.SplitError = splitErr.Error()
			reply(w, out)
			return
		}
	}
	if s.Refiner.Move != nil {
		out.MoveError = errText(s.Refiner.Move(ctx, saved.Key))
		out.Moved = out.MoveError == ""
	}
	reply(w, out)
}

func (s *Server) move(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if err := s.Refiner.Move(r.Context(), key); err != nil {
		fail(w, http.StatusBadGateway, fmt.Errorf("could not move %s to %s: %w", key, s.Refiner.ReadySprint, err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// check compares the repos with origin. With the repos read at a ref, there
// is nothing to compare and it lists none.
func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	out := []status{}
	if s.Refiner.Check != nil {
		for _, st := range s.Refiner.Check(r.Context()) {
			out = append(out, toStatus(st))
		}
	}
	reply(w, out)
}

// pull fast-forwards the given repos in order, stopping at the first that
// will not.
func (s *Server) pull(w http.ResponseWriter, r *http.Request) {
	var body []status
	if !decode(w, r, &body) {
		return
	}
	out := struct {
		Pulled []status `json:"pulled"`
		Error  string   `json:"error,omitempty"`
	}{Pulled: []status{}}
	for _, st := range body {
		if err := s.Refiner.Pull(r.Context(), st.gitsync()); err != nil {
			out.Error = err.Error()
			break
		}
		out.Pulled = append(out.Pulled, st)
	}
	reply(w, out)
}

func (s *Server) repos(w http.ResponseWriter, r *http.Request) {
	all := s.Source.Repos()
	out := make([]repo, 0, len(all))
	for _, name := range slices.Sorted(maps.Keys(all)) {
		out = append(out, repo{Name: name, Dir: all[name]})
	}
	reply(w, out)
}

func (s *Server) addRepo(w http.ResponseWriter, r *http.Request) {
	var body repo
	if !decode(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		fail(w, http.StatusBadRequest, errors.New("give the repo a name"))
		return
	}
	if !reposcan.IsRepo(body.Dir) {
		fail(w, http.StatusBadRequest, fmt.Errorf("%s is not a git repo", body.Dir))
		return
	}
	if err := s.Source.AddRepo(body.Name, filepath.Clean(body.Dir)); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.repos(w, r)
}

func (s *Server) removeRepo(w http.ResponseWriter, r *http.Request) {
	if err := s.Source.RemoveRepo(r.PathValue("name")); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.repos(w, r)
}

// scan lists the git checkouts at or under a folder, with the names they
// would be added as.
func (s *Server) scan(w http.ResponseWriter, r *http.Request) {
	found, err := reposcan.Scan(r.URL.Query().Get("dir"), s.Source.Repos())
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, found)
}

// folders suggests where to start looking for a repo to add: the folder most
// repos sit in, else home.
func (s *Server) folders(w http.ResponseWriter, r *http.Request) {
	count := map[string]int{}
	best := ""
	all := s.Source.Repos()
	for _, name := range slices.Sorted(maps.Keys(all)) {
		parent := filepath.Dir(all[name])
		count[parent]++
		if count[parent] > count[best] {
			best = parent
		}
	}
	if best == "" {
		best, _ = os.UserHomeDir()
	}
	reply(w, map[string]string{"start": best})
}

// A job is a Claude run: triage, brief, questions or stamp. Runs take
// minutes, so the app starts one and polls it.
type job struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Key       string    `json:"key"`
	Resume    bool      `json:"resume,omitempty"`
	FromBrief bool      `json:"from_brief,omitempty"`
	Started   time.Time `json:"started"`
	Done      bool      `json:"done"`
	Error     string    `json:"error,omitempty"`

	Triage *triage      `json:"triage,omitempty"`
	Brief  *brief       `json:"brief,omitempty"`
	Draft  *draft.Saved `json:"draft,omitempty"`

	cancel context.CancelFunc
}

const (
	kindTriage = "triage"
	kindBrief  = "brief"
	kindDraft  = "draft"
	kindStamp  = "stamp"
)

// Close cancels the running jobs, so their Claude runs stop with the server.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, j := range s.jobs {
		j.cancel()
		delete(s.jobs, id)
	}
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	out := make([]job, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, *j)
	}
	s.mu.Unlock()
	slices.SortFunc(out, func(a, b job) int { return a.Started.Compare(b.Started) })
	reply(w, out)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	j, ok := s.jobs[r.PathValue("id")]
	var out job
	if ok {
		out = *j
		// A finished job is read once; the result is in the store from then on.
		if j.Done {
			delete(s.jobs, j.ID)
		}
	}
	s.mu.Unlock()
	if !ok {
		fail(w, http.StatusNotFound, errors.New("no such job"))
		return
	}
	reply(w, out)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	j, ok := s.jobs[r.PathValue("id")]
	if ok {
		j.cancel()
		delete(s.jobs, j.ID)
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// startJob starts a Claude run on a ticket. One ticket runs one job at a
// time.
func (s *Server) startJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind     string            `json:"kind"`
		Key      string            `json:"key"`
		Approach string            `json:"approach"`
		Scope    map[string]string `json:"scope"`
		Resume   bool              `json:"resume"`
	}
	if !decode(w, r, &body) {
		return
	}
	var work func(context.Context, jira.Issue, *job) error
	fromBrief := false
	switch body.Kind {
	case kindTriage:
		work = func(ctx context.Context, i jira.Issue, j *job) error {
			t, err := s.Refiner.Triage(ctx, i)
			j.Triage = toTriage(t)
			return err
		}
	case kindBrief:
		work = func(ctx context.Context, i jira.Issue, j *job) error {
			b, err := s.Refiner.Brief(ctx, i)
			j.Brief = toBrief(b)
			return err
		}
	case kindDraft:
		req, ok := s.request(w, body.Key, body.Approach, body.Scope, body.Resume)
		if !ok {
			return
		}
		fromBrief = req.Brief != nil
		work = func(ctx context.Context, i jira.Issue, j *job) error {
			saved, err := s.Refiner.Draft(ctx, i, req)
			j.Draft = &saved
			return err
		}
	case kindStamp:
		saved, ok := s.latest(w, body.Key, 0)
		if !ok {
			return
		}
		if saved.Step == "" {
			fail(w, http.StatusConflict, errors.New("this draft is from before steps; redraft it to stamp"))
			return
		}
		// Stamp only once a round comes back with no questions: by then
		// Claude has seen every answer and the developer every decision.
		if !saved.Draft.Clear() {
			fail(w, http.StatusConflict, errors.New("Claude still has questions: decide them and run the next round; stamp once a round comes back with none"))
			return
		}
		work = func(ctx context.Context, i jira.Issue, j *job) error {
			stamped, err := s.Refiner.Stamp(ctx, i, saved)
			j.Draft = &stamped
			return err
		}
	default:
		fail(w, http.StatusBadRequest, fmt.Errorf("unknown job kind %q", body.Kind))
		return
	}

	s.mu.Lock()
	for _, other := range s.jobs {
		if other.Key == body.Key && !other.Done {
			s.mu.Unlock()
			fail(w, http.StatusConflict, fmt.Errorf("%s already has a %s running", body.Key, other.Kind))
			return
		}
	}
	s.nextID++
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{
		ID:        strconv.Itoa(s.nextID),
		Kind:      body.Kind,
		Key:       body.Key,
		Resume:    body.Resume,
		FromBrief: fromBrief,
		Started:   time.Now(),
		cancel:    cancel,
	}
	s.jobs[j.ID] = j
	out := *j
	s.mu.Unlock()

	go func() {
		defer cancel()
		result := &job{}
		err := func() error {
			i, err := s.Source.Get(ctx, j.Key)
			if err != nil {
				return err
			}
			return work(ctx, i, result)
		}()
		s.mu.Lock()
		defer s.mu.Unlock()
		j.Done, j.Error = true, errText(err)
		if err == nil {
			j.Triage, j.Brief, j.Draft = result.Triage, result.Brief, result.Draft
		}
	}()
	reply(w, out)
}

// request builds a draft run as the TUI does: a fresh one builds on the
// ticket's brief and triage and carries the last draft's answers; a resumed
// one continues the last draft's session with its answers.
func (s *Server) request(w http.ResponseWriter, key, approach string, scope map[string]string, resume bool) (draft.Request, bool) {
	req := draft.Request{Approach: approach, Scope: scope, Resume: resume}
	previous, ok, err := s.Refiner.Load(key)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return req, false
	}
	if ok {
		req.Previous = &previous
	}
	if resume {
		if !ok {
			fail(w, http.StatusConflict, fmt.Errorf("%s has no draft to continue", key))
			return req, false
		}
		if left := previous.Unanswered(); left > 0 {
			fail(w, http.StatusConflict, fmt.Errorf("answer %d more question%s before redrafting", left, plural(left)))
			return req, false
		}
		req.Approach, req.Scope = previous.Approach, previous.Scope
		return req, true
	}
	if b, ok, err := s.Refiner.LoadBrief(key); err == nil && ok {
		req.Brief = &b
	}
	if t, ok, err := s.Refiner.LoadTriage(key); err == nil && ok {
		req.Triage = &t
	}
	return req, true
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
