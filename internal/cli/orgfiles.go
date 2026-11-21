package cli

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/render"
)

// The org files: the desk rendered as plain org under <desk>/render (the
// inbox, a board per project, a ledger per month), for Emacs and cat. The
// dashboard server renders them once the desk has been quiet for a moment
// (renderPrefs, set in Settings and shared by every device); writes from
// the command line do not. `agent-desk render` and the dashboard's Render
// now render them by hand, and Export downloads them as a zip.

// renderPrefs is the shared "render" dashboard setting.
type renderPrefs struct {
	Enabled bool `json:"enabled"`
	// Debounce is how many seconds the desk must be quiet before a render.
	Debounce int `json:"debounce"`
}

var defaultRenderPrefs = renderPrefs{Enabled: true, Debounce: 2}

// The debounce Settings may choose, in seconds, and the longest a render
// waits while changes keep coming.
const (
	minRenderDebounce = 1
	maxRenderDebounce = 600
	maxRenderWait     = time.Minute
)

// orgState is the server's record of its renders, for Settings.
type orgState struct {
	mu   sync.Mutex // one render at a time from this server
	last time.Time
	took time.Duration
	err  string
}

// renderPrefs reads the shared setting, the defaults when unset or
// unreadable.
func (s *server) renderPrefs() renderPrefs {
	p := defaultRenderPrefs
	st, err := s.a.store()
	if err != nil {
		return p
	}
	prefs, err := st.DashboardPrefs()
	if err != nil || len(prefs["render"]) == 0 {
		return p
	}
	if json.Unmarshal(prefs["render"], &p) != nil || p.Debounce < minRenderDebounce || p.Debounce > maxRenderDebounce {
		return defaultRenderPrefs
	}
	return p
}

// checkRenderPrefs validates a "render" setting from the dashboard.
func checkRenderPrefs(data json.RawMessage) error {
	var p renderPrefs
	if err := json.Unmarshal(data, &p); err != nil {
		return usagef("render is {enabled, debounce}")
	}
	if p.Debounce < minRenderDebounce || p.Debounce > maxRenderDebounce {
		return usagef("render debounce is %d to %d seconds", minRenderDebounce, maxRenderDebounce)
	}
	return nil
}

// renderOrg renders the org files now and records how it went.
func (s *server) renderOrg() error {
	s.org.mu.Lock()
	defer s.org.mu.Unlock()
	start := time.Now()
	err := render.All(s.a.Home, s.a.load)
	s.org.last, s.org.took, s.org.err = time.Now(), time.Since(start), ""
	if err != nil {
		s.org.err = err.Error()
		fmt.Fprintf(s.logger, "agent-desk: rendering the org files failed: %v\n", err)
	}
	return err
}

// renderLoop renders the org files once the desk has been quiet for the
// debounce, and at least every maxRenderWait while changes keep coming,
// until ctx ends. It renders once at start: writes made while no server
// ran left the files behind.
func (s *server) renderLoop(ctx context.Context) {
	tick := time.NewTicker(s.poll)
	defer tick.Stop()
	last, _ := s.stamp()
	var changed, first time.Time // the latest change seen, the first one not rendered
	if s.renderPrefs().Enabled {
		s.renderOrg()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		now := time.Now()
		if cur, err := s.stamp(); err == nil && cur != last {
			last, changed = cur, now
			if first.IsZero() {
				first = now
			}
		}
		if first.IsZero() {
			continue
		}
		p := s.renderPrefs()
		if !p.Enabled {
			first = time.Time{}
			continue
		}
		if now.Sub(changed) >= time.Duration(p.Debounce)*time.Second || now.Sub(first) >= maxRenderWait {
			first = time.Time{}
			s.renderOrg()
		}
	}
}

// apiRender is the org files' state for Settings (GET api/render): the
// setting, where the files are, and the last render by this server.
func (s *server) apiRender(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.renderStatus())
}

func (s *server) renderStatus() map[string]any {
	p := s.renderPrefs()
	s.org.mu.Lock()
	defer s.org.mu.Unlock()
	out := map[string]any{"enabled": p.Enabled, "debounce": p.Debounce, "dir": filepath.Join(s.a.Home, "render")}
	if !s.org.last.IsZero() {
		out["last"] = s.org.last.Format(time.RFC3339)
		out["took_ms"] = s.org.took.Milliseconds()
	}
	if s.org.err != "" {
		out["error"] = s.org.err
	}
	return out
}

// apiRenderNow renders the org files at once (POST api/render), whatever
// the setting: Settings' Render now.
func (s *server) apiRenderNow(_ *App, _ request) (any, error) {
	if err := s.renderOrg(); err != nil {
		return nil, err
	}
	return s.renderStatus(), nil
}

// apiExport downloads org rendered fresh (GET api/export), leaving
// <desk>/render alone. scope is desk (the default), node (?path=), item
// (?id=) or inbox (?path= narrows it); each is one .org file. zip=1 instead
// packs the desk as the files <desk>/render holds.
func (s *server) apiExport(w http.ResponseWriter, r *http.Request) {
	d, err := s.a.load()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := r.URL.Query()
	if q.Get("zip") != "1" {
		name, org, err := exportOrg(d, q.Get("scope"), strings.Trim(q.Get("path"), "/"), q.Get("id"))
		if err != nil {
			jsonError(w, http.StatusNotFound, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/org; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+"-"+s.a.Now().Format("2006-01-02-1504")+`.org"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(org)
		return
	}
	files := render.Files(d)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	now := s.a.Now()
	base := "agent-desk-org-" + now.Format("2006-01-02-1504")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+base+`.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	z := zip.NewWriter(w)
	for _, name := range names {
		f, err := z.CreateHeader(&zip.FileHeader{Name: base + "/" + name, Method: zip.Deflate, Modified: now})
		if err != nil {
			return
		}
		if _, err := f.Write(files[name]); err != nil {
			return
		}
	}
	z.Close()
}

// exportOrg renders one scope of the desk as one org file, and names it.
func exportOrg(d *desk.Desk, scope, path, id string) (string, []byte, error) {
	slug := func(s string) string { return strings.NewReplacer("/", "-", " ", "-").Replace(s) }
	switch scope {
	case "", "desk":
		return "agent-desk", render.Whole(d), nil
	case "inbox":
		if path == "" {
			return "agent-desk-inbox", render.Inbox(d, ""), nil
		}
		return "agent-desk-inbox-" + slug(path), render.Inbox(d, path), nil
	case "node":
		org, ok := render.Node(d, path)
		if !ok {
			return "", nil, fmt.Errorf("no node at %q", path)
		}
		return "agent-desk-" + slug(path), org, nil
	case "item":
		e, err := d.Resolve(id)
		if err != nil {
			return "", nil, err
		}
		return "agent-desk-" + d.Display(e.ID), render.Item(d, e), nil
	}
	return "", nil, fmt.Errorf("scope is desk, node, item or inbox")
}
