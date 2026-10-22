// Package track records how refine is used, when the tracking flag is on.
// Events stay in refine.db on this machine; they hold names, ticket keys,
// counts and indexes, never the text of tickets, approaches or answers.
package track

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/mlmejo/refine/internal/draft"
	"github.com/mlmejo/refine/internal/store"
	"github.com/mlmejo/refine/internal/version"
)

// Tracker records events for one run of refine. A nil or disabled Tracker
// records nothing, so callers never check.
type Tracker struct {
	store   *store.Store
	ui      string
	session string
	build   string
	prompts string
	// now is time.Now, swapped in tests.
	now func() time.Time
}

// New returns a tracker for the ui ("tui" or "gui"), or nil when off. Every
// event it records carries the build and the prompts version.
func New(s *store.Store, ui string, on bool) *Tracker {
	if !on || s == nil {
		return nil
	}
	id := make([]byte, 6)
	_, _ = rand.Read(id)
	return &Tracker{store: s, ui: ui, session: hex.EncodeToString(id), build: version.Build(), prompts: draft.PromptsVersion(), now: time.Now}
}

// On reports whether events are recorded.
func (t *Tracker) On() bool {
	return t != nil
}

// Event records name for the ticket key, which may be empty. A failure to
// record is dropped: tracking never gets in the way of refining.
func (t *Tracker) Event(name, key string, props map[string]any) {
	if t == nil {
		return
	}
	_ = t.store.AddEvent(store.Event{At: t.now(), UI: t.ui, Session: t.session, Name: name, Key: key, Props: props, Build: t.build, Prompts: t.prompts})
}
