package cli

import (
	"net/http"
	"strings"
	"unicode"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// validDraftKey accepts a box name and what it is for, such as
// "reply:t-k2m9" or "node:example-proj/rerun-nov": a lowercase name, a
// colon, then up to 300 printable characters without spaces.
func validDraftKey(k string) bool {
	box, rest, ok := strings.Cut(k, ":")
	if !ok || box == "" || len(box) > 32 || rest == "" || len(rest) > 300 {
		return false
	}
	for _, r := range box {
		if !(r >= 'a' && r <= 'z' || r == '-') {
			return false
		}
	}
	for _, r := range rest {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// apiDraft returns the draft saved on the desk for one box (GET
// api/draft?key=reply:t-k2m9): its value, JSON null when there is none, and
// when it last changed.
func (s *server) apiDraft(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if !validDraftKey(key) {
		jsonError(w, http.StatusBadRequest, "key names a box, such as reply:t-k2m9")
		return
	}
	st, err := s.a.store()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d, err := st.Draft(key)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, d)
}

// apiDrafts lists one box's live drafts (GET api/drafts?box=task): the
// New task forms not filed yet, on any device, newest first.
func (s *server) apiDrafts(w http.ResponseWriter, r *http.Request) {
	box := r.URL.Query().Get("box")
	if !validDraftKey(box + ":x") {
		jsonError(w, http.StatusBadRequest, "box names a kind of box, such as task")
		return
	}
	st, err := s.a.store()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ds, err := st.DraftsIn(box)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"drafts": ds})
}

// apiDraftPut saves a box's draft on the desk (POST api/draft {key, draft,
// device}); a null draft drops it, so the other devices drop theirs.
func (s *server) apiDraftPut(_ *App, body request) (any, error) {
	if !validDraftKey(body.Key) {
		return nil, usagef("key names a box, such as reply:t-k2m9")
	}
	if len(body.Draft) > store.MaxDraft {
		return nil, usagef("a draft is at most %d KB", store.MaxDraft>>10)
	}
	device := body.Device
	if len(device) > 64 {
		device = device[:64]
	}
	st, err := s.a.store()
	if err != nil {
		return nil, err
	}
	return st.PutDraft(body.Key, body.Draft, device, s.a.Now())
}

// draftedItems is the full ids of the items the user has a draft for.
func (s *server) draftedItems(d *desk.Desk) map[string]bool {
	st, err := s.a.store()
	if err != nil {
		return nil
	}
	ids, err := st.DraftItems()
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, id := range ids {
		if e, err := d.Resolve(id); err == nil {
			out[e.ID] = true
		}
	}
	return out
}

// markDrafted flags the listed items that have a draft.
func markDrafted(v *desk.View, drafted map[string]bool) {
	if len(drafted) == 0 {
		return
	}
	mark := func(items []desk.Item) {
		for i := range items {
			items[i].Draft = drafted[items[i].Event.ID]
		}
	}
	mark(v.Pinned)
	for _, gs := range [][]desk.Group{v.NeedsYou, v.Deliverables, v.Responses, v.New} {
		for _, g := range gs {
			mark(g.Items)
		}
	}
}
