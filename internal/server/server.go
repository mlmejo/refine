// Package server serves refine to the desktop app as JSON over HTTP on
// localhost.
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

	"github.com/mlmejo/refine/internal/claude"
	"github.com/mlmejo/refine/internal/jira"
	"github.com/mlmejo/refine/internal/refine"
	"github.com/mlmejo/refine/internal/reposcan"
	"github.com/mlmejo/refine/internal/version"
)

// Repos reads and changes the configured repos. List returns a map the
// caller may keep.
type Repos interface {
	List() map[string]string
	Add(name, dir string) error
	Remove(name string) error
}

// Info is what the app shows about this refine.
type Info struct {
	Site        string `json:"site"`
	Project     string `json:"project"`
	Sprint      string `json:"sprint"`
	ReadySprint string `json:"ready_sprint"`
}

// Server answers the desktop app. Every request needs the token.
type Server struct {
	Info    Info
	Service *refine.Service
	Repos   Repos
	// SaveHidden keeps the hidden ticket keys.
	SaveHidden func(keys []string) error
	Token      string

	mu     sync.Mutex
	hidden map[string]bool
	job    *job
	nextID int
	// stamping holds the tickets being written to Jira.
	stamping map[string]bool
}

// New returns a server with a fresh random token. hidden are the keys
// already hidden from the list.
func New(info Info, service *refine.Service, repos Repos, hidden []string, saveHidden func([]string) error) *Server {
	token := make([]byte, 24)
	_, _ = rand.Read(token)
	s := &Server{Info: info, Service: service, Repos: repos, SaveHidden: saveHidden, Token: hex.EncodeToString(token), hidden: map[string]bool{}, stamping: map[string]bool{}}
	for _, key := range hidden {
		s.hidden[key] = true
	}
	service.Hidden = func(key string) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.hidden[key]
	}
	return s
}

// Handler routes the API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("GET /api/tickets", s.tickets)
	mux.HandleFunc("GET /api/lookup", s.lookup)
	mux.HandleFunc("PUT /api/hidden/{key}", s.hide(true))
	mux.HandleFunc("DELETE /api/hidden/{key}", s.hide(false))
	mux.HandleFunc("GET /api/tickets/{key}", s.open)
	mux.HandleFunc("DELETE /api/tickets/{key}", s.reset)
	mux.HandleFunc("PUT /api/tickets/{key}/decisions", s.decide)
	mux.HandleFunc("POST /api/tickets/{key}/continue", s.advance)
	mux.HandleFunc("PUT /api/tickets/{key}/repos", s.setRepos)
	mux.HandleFunc("POST /api/tickets/{key}/suggestion", s.suggest)
	mux.HandleFunc("DELETE /api/tickets/{key}/sync", s.chooseRepos)
	mux.HandleFunc("POST /api/tickets/{key}/sync", s.sync)
	mux.HandleFunc("PUT /api/tickets/{key}/answers", s.answers)
	mux.HandleFunc("DELETE /api/tickets/{key}/rounds", s.restartQuestions)
	mux.HandleFunc("PUT /api/tickets/{key}/result", s.edit)
	mux.HandleFunc("POST /api/tickets/{key}/stamp", s.stamp)
	mux.HandleFunc("GET /api/repos", s.repos)
	mux.HandleFunc("POST /api/repos", s.addRepo)
	mux.HandleFunc("DELETE /api/repos/{name}", s.removeRepo)
	mux.HandleFunc("GET /api/repos/scan", s.scan)
	mux.HandleFunc("GET /api/folders", s.folders)
	mux.HandleFunc("PUT /api/profiles/{repo}", s.setProfile)
	mux.HandleFunc("GET /api/formats/{format}", s.format)
	mux.HandleFunc("PUT /api/formats/{format}", s.setFormat)
	mux.HandleFunc("DELETE /api/formats/{format}", s.setFormat)
	mux.HandleFunc("GET /api/job", s.currentJob)
	mux.HandleFunc("POST /api/jobs", s.startJob)
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

// failFor picks the status for an error from the service.
func failFor(w http.ResponseWriter, err error) {
	if errors.Is(err, refine.ErrStep) {
		fail(w, http.StatusConflict, err)
		return
	}
	fail(w, http.StatusBadRequest, err)
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		fail(w, http.StatusBadRequest, fmt.Errorf("read the request: %w", err))
		return false
	}
	return true
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	reply(w, map[string]any{
		"site":          s.Info.Site,
		"project":       s.Info.Project,
		"sprint":        s.Info.Sprint,
		"ready_sprint":  s.Info.ReadySprint,
		"max_rounds":    refine.MaxRounds,
		"max_questions": refine.MaxQuestions,
		"build":         version.Build(),
		"prompts":       refine.PromptsVersion(),
	})
}

func (s *Server) tickets(w http.ResponseWriter, r *http.Request) {
	page, err := s.Service.List(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	reply(w, page)
}

// lookup reads a ticket key, or a number in the project, and unhides it.
func (s *Server) lookup(w http.ResponseWriter, r *http.Request) {
	key, ok := jira.ParseKey(r.URL.Query().Get("q"), s.Info.Project)
	if !ok {
		fail(w, http.StatusBadRequest, errors.New("not a ticket key"))
		return
	}
	if err := s.setHidden(key, false); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, map[string]string{"key": key})
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

func (s *Server) setHidden(key string, hide bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hidden[key] == hide {
		return nil
	}
	if hide {
		s.hidden[key] = true
	} else {
		delete(s.hidden, key)
	}
	if s.SaveHidden == nil {
		return nil
	}
	return s.SaveHidden(slices.Sorted(maps.Keys(s.hidden)))
}

// open reads a ticket and its refinement, or the start of one.
func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	view, err := s.Service.Open(r.Context(), r.PathValue("key"))
	if err != nil {
		var apiErr *jira.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			fail(w, http.StatusNotFound, err)
			return
		}
		fail(w, http.StatusBadGateway, err)
		return
	}
	reply(w, view)
}

// describe replies with the refinement after a change.
func (s *Server) describe(ctx context.Context, w http.ResponseWriter, key string, err error) {
	if err != nil {
		failFor(w, err)
		return
	}
	view, err := s.Service.Describe(ctx, key)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, view)
}

func (s *Server) reset(w http.ResponseWriter, r *http.Request) {
	if s.busy(r.PathValue("key")) {
		fail(w, http.StatusConflict, errors.New("cancel the running step first"))
		return
	}
	if err := s.Service.Reset(r.Context(), r.PathValue("key")); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Blocker  string `json:"blocker"`
		Decision string `json:"decision"`
	}
	if !decode(w, r, &body) {
		return
	}
	key := r.PathValue("key")
	_, err := s.Service.Decide(r.Context(), key, body.Blocker, body.Decision)
	s.describe(r.Context(), w, key, err)
}

func (s *Server) advance(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	_, err := s.Service.Continue(r.Context(), key)
	s.describe(r.Context(), w, key, err)
}

func (s *Server) restartQuestions(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	_, err := s.Service.RestartQuestions(r.Context(), key)
	s.describe(r.Context(), w, key, err)
}

func (s *Server) chooseRepos(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	_, err := s.Service.ChooseRepos(r.Context(), key)
	s.describe(r.Context(), w, key, err)
}

func (s *Server) setRepos(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repos []string `json:"repos"`
	}
	if !decode(w, r, &body) {
		return
	}
	key := r.PathValue("key")
	_, err := s.Service.SetRepos(r.Context(), key, body.Repos)
	s.describe(r.Context(), w, key, err)
}

func (s *Server) suggest(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	_, err := s.Service.Suggest(r.Context(), key)
	s.describe(r.Context(), w, key, err)
}

func (s *Server) sync(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	_, err := s.Service.Sync(r.Context(), key)
	s.describe(r.Context(), w, key, err)
}

func (s *Server) answers(w http.ResponseWriter, r *http.Request) {
	var body refine.Answers
	if !decode(w, r, &body) {
		return
	}
	key := r.PathValue("key")
	_, err := s.Service.Answer(r.Context(), key, body)
	s.describe(r.Context(), w, key, err)
}

func (s *Server) edit(w http.ResponseWriter, r *http.Request) {
	var body refine.Result
	if !decode(w, r, &body) {
		return
	}
	key := r.PathValue("key")
	_, err := s.Service.Edit(r.Context(), key, body)
	s.describe(r.Context(), w, key, err)
}

// stampTimeout bounds one stamp's Jira writes.
const stampTimeout = 2 * time.Minute

// stamp writes to Jira. A failed action comes back in the refinement's
// stamp progress, not as an error, so the app can offer a retry.
func (s *Server) stamp(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !s.claim(key) {
		fail(w, http.StatusConflict, errors.New("a step is running on this ticket"))
		return
	}
	defer s.release(key)
	// The Jira writes must not stop halfway because the app dropped the
	// request: a create cut off mid-flight may still have made the ticket.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), stampTimeout)
	defer cancel()
	_, err := s.Service.Stamp(ctx, key)
	s.describe(ctx, w, key, err)
}

type repo struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

func (s *Server) repos(w http.ResponseWriter, r *http.Request) {
	all := s.Repos.List()
	profiles, err := s.Service.Profiles(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	byRepo := map[string]refine.Profile{}
	for _, p := range profiles {
		byRepo[p.Repo] = p
	}
	type listed struct {
		repo
		Profile *refine.Profile `json:"profile,omitempty"`
	}
	out := make([]listed, 0, len(all))
	for _, name := range slices.Sorted(maps.Keys(all)) {
		l := listed{repo: repo{Name: name, Dir: all[name]}}
		if p, ok := byRepo[name]; ok {
			l.Profile = &p
		}
		out = append(out, l)
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
	if err := s.Repos.Add(body.Name, filepath.Clean(body.Dir)); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.repos(w, r)
}

func (s *Server) removeRepo(w http.ResponseWriter, r *http.Request) {
	if err := s.Repos.Remove(r.PathValue("name")); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.repos(w, r)
}

// scan lists the git checkouts at or under a folder, with the names they
// would be added as.
func (s *Server) scan(w http.ResponseWriter, r *http.Request) {
	found, err := reposcan.Scan(r.URL.Query().Get("dir"), s.Repos.List())
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
	all := s.Repos.List()
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

func (s *Server) setProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Answers []refine.QA `json:"answers"`
	}
	if !decode(w, r, &body) {
		return
	}
	name := r.PathValue("repo")
	if _, ok := s.Repos.List()[name]; !ok {
		fail(w, http.StatusNotFound, fmt.Errorf("no repo named %s", name))
		return
	}
	if err := s.Service.SetProfile(r.Context(), refine.Profile{Repo: name, Answers: body.Answers}); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	s.repos(w, r)
}

// format replies with where a field's rules live and whether they are the
// developer's own.
func (s *Server) format(w http.ResponseWriter, r *http.Request) {
	f := refine.Format(r.PathValue("format"))
	def, ok := refine.DefaultFormats[f]
	if !ok {
		fail(w, http.StatusNotFound, fmt.Errorf("no format %q", f))
		return
	}
	text, err := s.Service.Format(f)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, map[string]any{"path": s.Service.FormatPath(f), "custom": strings.TrimSpace(text) != strings.TrimSpace(def)})
}

// setFormat takes a field's uploaded rules, or puts the defaults back.
func (s *Server) setFormat(w http.ResponseWriter, r *http.Request) {
	f := refine.Format(r.PathValue("format"))
	if _, ok := refine.DefaultFormats[f]; !ok {
		fail(w, http.StatusNotFound, fmt.Errorf("no format %q", f))
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if r.Method == http.MethodPut {
		if !decode(w, r, &body) {
			return
		}
		if strings.TrimSpace(body.Text) == "" {
			fail(w, http.StatusBadRequest, errors.New("the file is empty"))
			return
		}
	}
	if err := s.Service.SetFormat(f, body.Text); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	s.format(w, r)
}

// A job is one Claude run. Runs take minutes, so the app starts one and
// polls it, showing what Claude reads as it goes. One runs at a time, since
// the developer refines one ticket at a time.
type job struct {
	ID      string            `json:"id"`
	Kind    string            `json:"kind"`
	Key     string            `json:"key"`
	Started time.Time         `json:"started"`
	Done    bool              `json:"done"`
	Error   string            `json:"error,omitempty"`
	Log     []claude.Activity `json:"log"`

	cancel context.CancelFunc
}

// The kinds of job.
const (
	kindSearch   = "search"
	kindAsk      = "ask"
	kindDeploy   = "deploy"
	kindGenerate = "generate"
	kindRewrite  = "rewrite"
)

// Close cancels the running job, so its Claude run stops with the server.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil {
		s.job.cancel()
	}
}

// busy reports whether a job or a stamp is running on the ticket.
func (s *Server) busy(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busyLocked(key)
}

func (s *Server) busyLocked(key string) bool {
	return s.stamping[key] || s.job != nil && !s.job.Done && s.job.Key == key
}

// claim marks the ticket as stamping, unless something runs on it already.
func (s *Server) claim(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busyLocked(key) {
		return false
	}
	s.stamping[key] = true
	return true
}

func (s *Server) release(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.stamping, key)
}

// currentJob replies with the running job, or the last one until the app
// has read that it finished.
func (s *Server) currentJob(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job == nil {
		reply(w, nil)
		return
	}
	out := *s.job
	out.Log = slices.Clone(s.job.Log)
	reply(w, out)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.job != nil && s.job.ID == r.PathValue("id") {
		s.job.cancel()
		s.job = nil
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// startJob starts a Claude run on a ticket, unless one is running.
func (s *Server) startJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind        string `json:"kind"`
		Key         string `json:"key"`
		Explanation string `json:"explanation"`
	}
	if !decode(w, r, &body) {
		return
	}
	var work func(context.Context, func(claude.Activity)) error
	switch body.Kind {
	case kindSearch:
		work = func(ctx context.Context, p func(claude.Activity)) error {
			_, err := s.Service.Search(ctx, body.Key, body.Explanation, p)
			return err
		}
	case kindAsk:
		work = func(ctx context.Context, p func(claude.Activity)) error {
			_, err := s.Service.Ask(ctx, body.Key, p)
			return err
		}
	case kindDeploy:
		work = func(ctx context.Context, p func(claude.Activity)) error {
			_, err := s.Service.AskDeploy(ctx, body.Key, p)
			return err
		}
	case kindGenerate:
		work = func(ctx context.Context, p func(claude.Activity)) error {
			_, err := s.Service.Generate(ctx, body.Key, p)
			return err
		}
	case kindRewrite:
		work = func(ctx context.Context, p func(claude.Activity)) error {
			_, err := s.Service.Rewrite(ctx, body.Key, p)
			return err
		}
	default:
		fail(w, http.StatusBadRequest, fmt.Errorf("unknown job kind %q", body.Kind))
		return
	}

	s.mu.Lock()
	if s.job != nil && !s.job.Done {
		running := *s.job
		s.mu.Unlock()
		fail(w, http.StatusConflict, fmt.Errorf("Claude is already working on %s", running.Key))
		return
	}
	if s.stamping[body.Key] {
		s.mu.Unlock()
		fail(w, http.StatusConflict, fmt.Errorf("%s is being stamped", body.Key))
		return
	}
	s.nextID++
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{ID: strconv.Itoa(s.nextID), Kind: body.Kind, Key: body.Key, Started: time.Now(), Log: []claude.Activity{}, cancel: cancel}
	s.job = j
	out := *j
	s.mu.Unlock()

	go func() {
		defer cancel()
		err := work(ctx, func(a claude.Activity) {
			s.mu.Lock()
			j.Log = append(j.Log, a)
			s.mu.Unlock()
		})
		s.mu.Lock()
		defer s.mu.Unlock()
		j.Done = true
		if err != nil {
			j.Error = err.Error()
		}
	}()
	reply(w, out)
}
