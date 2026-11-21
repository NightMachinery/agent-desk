package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The org files: writes leave them alone; the server renders them once the
// desk is quiet for the debounce, Render now renders at once, the setting
// is validated, and Export packs the same files in a zip.
func TestOrgFiles(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-0000000000f1"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "App coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "app/coord")
	firstID := h.ok(env, "Built.", "tell", "--title", "First build")
	inbox := filepath.Join(h.desk, "render", "inbox.org")
	if _, err := os.Stat(inbox); err == nil {
		t.Fatal("a write rendered the org files")
	}

	s, hd := testServer(t, h)
	status := func() map[string]any {
		t.Helper()
		_, body, _ := do(t, hd, "GET", "/api/render", "", nil)
		var m map[string]any
		must(t, json.Unmarshal([]byte(body), &m))
		return m
	}
	if m := status(); m["enabled"] != true || m["debounce"] != float64(2) || m["last"] != nil {
		t.Fatalf("defaults: %v", m)
	}
	for _, bad := range []string{
		`{"key":"render","data":{"enabled":true,"debounce":0}}`,
		`{"key":"render","data":{"enabled":true,"debounce":601}}`,
		`{"key":"render","data":"often"}`,
	} {
		if code, _, _ := do(t, hd, "POST", "/api/dashboard-prefs", bad, dashboard); code != 400 {
			t.Fatalf("%s: %d, want 400", bad, code)
		}
	}
	if code, body, _ := do(t, hd, "POST", "/api/dashboard-prefs", `{"key":"render","data":{"enabled":false,"debounce":1}}`, dashboard); code != 200 {
		t.Fatalf("set: %d %s", code, body)
	}
	if m := status(); m["enabled"] != false || m["debounce"] != float64(1) {
		t.Fatalf("read back: %v", m)
	}

	// Render now, whatever the setting.
	if code, body, _ := do(t, hd, "POST", "/api/render", `{}`, dashboard); code != 200 || !strings.Contains(body, `"last"`) {
		t.Fatalf("render now: %d %s", code, body)
	}
	if b, err := os.ReadFile(inbox); err != nil || !bytes.Contains(b, []byte("First build")) {
		t.Fatalf("render now wrote no inbox: %v %s", err, b)
	}

	// Export: by default the whole desk as one .org file, inbox, boards
	// and ledger as top-level headings; a scope narrows it.
	code, body, hdr := do(t, hd, "GET", "/api/export", "", nil)
	if code != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/org") || !strings.Contains(hdr.Get("Content-Disposition"), `.org"`) {
		t.Fatalf("export: %d %v", code, hdr)
	}
	for _, h := range []string{"\n* Inbox\n", "\n* Boards\n", "\n* Ledger\n", "First build"} {
		if !strings.Contains(body, h) {
			t.Fatalf("export lacks %q:\n%s", h, body)
		}
	}
	first, err := h.loadDesk().Resolve(firstID)
	must(t, err)
	for _, q := range []string{"scope=inbox", "scope=node&path=" + first.Path, "scope=item&id=" + firstID} {
		if code, body, _ := do(t, hd, "GET", "/api/export?"+q, "", nil); code != 200 || !strings.Contains(body, "First build") {
			t.Fatalf("export %s: %d\n%s", q, code, body)
		}
	}
	if code, _, _ := do(t, hd, "GET", "/api/export?scope=node&path=nowhere", "", nil); code != 404 {
		t.Fatalf("export of no node: %d", code)
	}
	// The same from a shell, to stdout.
	if out := h.ok(nil, "", "render", "--print", "item", firstID); !strings.Contains(out, "First build") || strings.Contains(out, "#+TITLE: agent-desk [[") {
		t.Fatalf("render --print item:\n%s", out)
	}
	h.fails(nil, "", "render --print [desk", "render", "--print", "bogus")
	h.fails(nil, "", "only with --print", "render", "node", "x")

	// zip=1: the same files as <desk>/render, in a zip, rendered fresh.
	code, body, hdr = do(t, hd, "GET", "/api/export?zip=1", "", nil)
	if code != 200 || hdr.Get("Content-Type") != "application/zip" || !strings.Contains(hdr.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d %v", code, hdr)
	}
	z, err := zip.NewReader(bytes.NewReader([]byte(body)), int64(len(body)))
	must(t, err)
	var names []string
	for _, f := range z.File {
		names = append(names, f.Name)
	}
	joined := strings.Join(names, " ")
	if !strings.Contains(joined, "/inbox.org") || !strings.Contains(joined, "/board/") || !strings.Contains(joined, "/ledger/") {
		t.Fatalf("export holds: %v", names)
	}

	// The loop: on, it renders at start, then once the desk has been quiet
	// for the debounce (1 s), not at the write itself.
	st, err := s.a.store()
	must(t, err)
	must(t, st.PutDashboardPref("render", json.RawMessage(`{"enabled":true,"debounce":1}`), time.Now()))
	must(t, os.RemoveAll(filepath.Join(h.desk, "render")))
	s.poll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.renderLoop(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	waitFor := func(what string, within time.Duration, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(within); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatalf("%s: not within %v", what, within)
	}
	has := func(text string) func() bool {
		return func() bool {
			b, err := os.ReadFile(inbox)
			return err == nil && bytes.Contains(b, []byte(text))
		}
	}
	waitFor("render at start", 2*time.Second, has("First build"))
	h.ok(env, "Built again.", "tell", "--title", "Second build")
	time.Sleep(400 * time.Millisecond)
	if has("Second build")() {
		t.Fatal("rendered before the desk was quiet for the debounce")
	}
	waitFor("render after the debounce", 3*time.Second, has("Second build"))
}
