package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
)

// twoProjects files a blocked question and a notice in example-proj (run r1)
// and in other-proj (runs r2 and r3), and returns the question ids.
func twoProjects(t *testing.T, h *harness) (near, far string) {
	t.Helper()
	a, b, c := h.as(sessA), h.as(sessB), h.as(sessC)
	h.ok(a, "", "join", "r1/coord")
	h.ok(b, "", "join", "r2/coord", "--project", "other-proj")
	h.ok(c, "", "join", "r3/coord", "--project", "other-proj")
	near = h.ok(a, "ctx", "ask", "--blocked", "--title", "Near question?")
	h.ok(a, "body", "tell", "--title", "Near notice")
	far = h.ok(b, "ctx", "ask", "--blocked", "--title", "Far question?")
	h.ok(b, "body", "tell", "--title", "Far notice")
	h.ok(c, "body", "tell", "--title", "Third notice")
	return near, far
}

func showJSON(t *testing.T, h *harness, args ...string) (desk.View, string) {
	t.Helper()
	out, errOut, code := h.run(nil, "", append([]string{"show", "--format", "json"}, args...)...)
	if code != 0 {
		t.Fatalf("show: %s", errOut)
	}
	var v desk.View
	must(t, json.Unmarshal([]byte(out), &v))
	return v, errOut
}

func titles(gs []desk.Group) []string {
	var out []string
	for _, g := range gs {
		for _, it := range g.Items {
			out = append(out, it.Event.Title)
		}
	}
	return out
}

func TestSetsDefineReplaceDelete(t *testing.T) {
	h := newHarness(t)
	if out := h.ok(nil, "", "sets"); !strings.HasPrefix(out, "No project sets") {
		t.Fatalf("empty list: %q", out)
	}
	h.fails(nil, "", "not off", "sets", "define", "off", "x")
	h.fails(nil, "", "not \"a/b\"", "sets", "define", "a/b", "x")
	h.fails(nil, "", "at least one", "sets", "define", "W1", "/")
	h.fails(nil, "", "no project set \"W9\"", "sets", "delete", "W9")

	// Prefixes are cleaned and kept in order; a deep prefix is fine.
	_, errOut, _ := h.run(nil, "", "sets", "define", "W1", "example-proj/", "deep/sub/path", "example-proj")
	if !strings.Contains(errOut, "nothing on the desk is under deep/sub/path yet") {
		t.Fatalf("unknown prefix warning: %q", errOut)
	}
	h.ok(nil, "", "sets", "define", "B2", "other-proj")
	if out := h.ok(nil, "", "sets"); out != "B2: other-proj\n  W1: example-proj, deep/sub/path" {
		t.Fatalf("list: %q", out)
	}
	// Defining again replaces.
	if out := h.ok(nil, "", "sets", "define", "W1", "caap"); out != "W1 redefined: caap" {
		t.Fatalf("redefine: %q", out)
	}
	h.ok(nil, "", "focus", "W1")
	var sets []SetInfo
	must(t, json.Unmarshal([]byte(h.ok(nil, "", "sets", "--format", "json")), &sets))
	if len(sets) != 2 || sets[1].Name != "W1" || !sets[1].Focused || strings.Join(sets[1].Prefixes, ",") != "caap" {
		t.Fatalf("json: %+v", sets)
	}
	// Deleting the focused set turns the focus off, for good.
	if out := h.ok(nil, "", "sets", "delete", "W1"); !strings.Contains(out, "Focus: off") {
		t.Fatalf("delete: %q", out)
	}
	h.ok(nil, "", "sets", "define", "W1", "caap")
	if out := h.ok(nil, "", "focus"); out != "Focus: off (everything shows)" {
		t.Fatalf("focus came back: %q", out)
	}
	if out := h.ok(nil, "", "sets"); out != "B2: other-proj\n  W1: caap" {
		t.Fatalf("after delete: %q", out)
	}
	// Set events are bookkeeping: no project or tree node for _desk.
	v, _ := showJSON(t, h, "--all")
	for _, n := range v.Runs {
		if n.Name == desk.SystemPrefix {
			t.Fatalf("_desk in the tree: %+v", n)
		}
	}
}

func TestFocusFiltersViews(t *testing.T) {
	h := newHarness(t)
	near, far := twoProjects(t, h)
	// A set may name a run: other-proj/r3 without other-proj/r2.
	h.ok(nil, "", "sets", "define", "W1", "example-proj", "other-proj/r3")
	h.fails(h.as(sessA), "", "the focus is the user's", "focus", "W1")
	h.fails(nil, "", "no project set \"W2\"", "focus", "W2")
	if out := h.ok(nil, "", "focus", "W1"); out != "Focus: W1 (example-proj, other-proj/r3)" {
		t.Fatalf("focus: %q", out)
	}

	v, note := showJSON(t, h)
	if got := strings.Join(titles(v.NeedsYou), "|"); got != "Near question?" {
		t.Fatalf("needs you: %q", got)
	}
	if got := strings.Join(titles(v.New), "|"); got != "Near notice|Third notice" {
		t.Fatalf("new: %q", got)
	}
	if len(v.Runs) != 2 || v.Runs[0].Name != "example-proj" || v.Runs[1].Name != "other-proj" ||
		len(v.Runs[1].Children) != 1 || v.Runs[1].Children[0].Name != "r3" || v.Runs[1].Open != 0 {
		t.Fatalf("runs: %+v", v.Runs)
	}
	if v.Focus == nil || v.Focus.Name != "W1" || v.Focus.Hidden != (desk.Hidden{NeedsYou: 1, Unread: 1}) {
		t.Fatalf("focus: %+v", v.Focus)
	}
	if !strings.Contains(note, "focus W1 (example-proj, other-proj/r3); 2 hidden: 1 waiting on you; --unfocused") {
		t.Fatalf("note: %q", note)
	}
	if out := h.ok(nil, "", "focus"); !strings.Contains(out, "Hidden: 2 hidden: 1 waiting on you") {
		t.Fatalf("focus status: %q", out)
	}

	// --unfocused shows everything; --mine is the agent's own view.
	v, note = showJSON(t, h, "--unfocused")
	if len(titles(v.NeedsYou)) != 2 || v.Focus != nil || note != "" {
		t.Fatalf("unfocused: %v %+v %q", titles(v.NeedsYou), v.Focus, note)
	}
	out, _, _ := h.run(h.as(sessB), "", "show", "--mine", "--format", "json")
	var mine desk.View
	must(t, json.Unmarshal([]byte(out), &mine))
	if strings.Join(titles(mine.NeedsYou), "|") != "Far question?" {
		t.Fatalf("mine: %v", titles(mine.NeedsYou))
	}
	// An agent outside the focus still writes, and the user can still
	// answer it by id.
	h.ok(h.as(sessB), "more", "tell", "--title", "Far again")
	h.ok(nil, "", "answer", far, "yes")
	_ = near

	// Off: everything again.
	h.ok(nil, "", "focus", "off")
	v, note = showJSON(t, h)
	if len(titles(v.New)) != 4 || v.Focus != nil || note != "" {
		t.Fatalf("off: %v %q", titles(v.New), note)
	}

	// The desk's own items (a screen request) stay in view under a focus.
	if !desk.InFocus(desk.ScreenPath("host-a"), []string{"example-proj"}) ||
		desk.InFocus("example-project", []string{"example-proj"}) ||
		!desk.InFocus("example-proj/r1/coord/next", []string{"example-proj/r1"}) {
		t.Fatal("InFocus")
	}
}

func TestFocusSuppressesNotifications(t *testing.T) {
	h := newHarness(t)
	log := filepath.Join(t.TempDir(), "hooks.jsonl")
	notifyConfig(t, h, log, "")
	h.ok(nil, "", "pref", "set", "notify.desktop", "origin", "--class", "tell")
	a, b := h.as(sessA), h.as(sessB)
	h.ok(a, "", "join", "r1/coord")
	h.ok(b, "", "join", "r2/coord", "--project", "other-proj")
	h.ok(nil, "", "sets", "define", "W1", "example-proj")
	h.ok(nil, "", "focus", "W1")

	far := h.ok(b, "ctx", "ask", "--blocked", "--title", "Far question?")
	h.ok(b, "body", "tell", "--title", "Far notice")
	if got := readHookLog(t, log); len(got) != 0 {
		t.Fatalf("raised outside the focus: %+v", got)
	}
	h.ok(a, "ctx", "ask", "--blocked", "--title", "Near question?")
	got := readHookLog(t, log)
	if len(got) != 1 || got[0].Event.Title != "Near question?" {
		t.Fatalf("inside the focus: %+v", got)
	}
	// Nothing is lost: outside the focus it is open and unread.
	d := h.loadDesk()
	if !isOpen(d, d.ByID[mustResolve(t, d, far)].ID) {
		t.Fatal("far question closed")
	}
	if v, _ := showJSON(t, h, "--unfocused"); strings.Join(titles(v.New), "|") != "Far notice" {
		t.Fatalf("far notice not unread: %v", titles(v.New))
	}
	// Leaving the focus does not raise what it held back, and new events
	// outside it notify again.
	h.ok(nil, "", "focus", "off")
	if n := len(readHookLog(t, log)); n != 1 {
		t.Fatalf("retroactive notifications: %d", n)
	}
	h.ok(b, "body", "tell", "--title", "Far later")
	if got := readHookLog(t, log); len(got) != 2 || got[1].Event.Title != "Far later" {
		t.Fatalf("after focus off: %+v", got)
	}
}

func mustResolve(t *testing.T, d *desk.Desk, token string) string {
	t.Helper()
	e, err := d.Resolve(token)
	must(t, err)
	return e.ID
}

func TestSetsMergeAcrossDesks(t *testing.T) {
	h := newHarness(t)
	hb := *h
	hb.desk, hb.host = filepath.Join(t.TempDir(), "desk-b"), "host-b"
	h.ok(nil, "", "sets", "define", "W1", "example-proj")
	h.ok(nil, "", "focus", "W1")
	hb.now = h.now.Add(time.Minute)
	hb.ok(nil, "", "sets", "define", "B2", "other-proj")
	hb.ok(nil, "", "focus", "B2") // later: it wins after the merge

	h.ok(nil, "", "merge", filepath.Join(hb.desk, "desk.db"))
	hb.ok(nil, "", "merge", filepath.Join(h.desk, "desk.db"))
	for _, x := range []*harness{h, &hb} {
		if out := x.ok(nil, "", "sets"); out != "* B2: other-proj\n  W1: example-proj" {
			t.Fatalf("%s sets: %q", x.host, out)
		}
	}
	// A delete merges too, and so does the focus it ends.
	hb.ok(nil, "", "sets", "delete", "B2")
	h.ok(nil, "", "merge", filepath.Join(hb.desk, "desk.db"))
	if out := h.ok(nil, "", "sets"); out != "W1: example-proj" {
		t.Fatalf("after delete: %q", out)
	}
	if out := h.ok(nil, "", "focus"); out != "Focus: off (everything shows)" {
		t.Fatalf("focus after delete: %q", out)
	}
}

func TestServeSetsAndFocus(t *testing.T) {
	h := newHarness(t)
	twoProjects(t, h)
	_, hd := testServer(t, h)
	post := func(body string) {
		t.Helper()
		if code, out, _ := do(t, hd, "POST", "/api/sets", body, dashboard); code != 200 {
			t.Fatalf("POST %s: %d %s", body, code, out)
		}
	}
	post(`{"action":"define","name":"W1","prefixes":["example-proj"]}`)
	post(`{"action":"focus","name":"W1"}`)
	if code, out, _ := do(t, hd, "POST", "/api/sets", `{"action":"focus","name":"nope"}`, dashboard); code == 200 || !strings.Contains(out, "no project set") {
		t.Fatalf("unknown set: %d %s", code, out)
	}

	var v struct {
		Sets  []SetInfo       `json:"sets"`
		Focus *desk.FocusView `json:"focus"`
		Paths []string        `json:"paths"`
		View  desk.View       `json:"view"`
	}
	view := func(target string) {
		t.Helper()
		v.Focus = nil
		code, body, _ := do(t, hd, "GET", target, "", nil)
		if code != 200 {
			t.Fatalf("%s: %d %s", target, code, body)
		}
		must(t, json.Unmarshal([]byte(body), &v))
	}
	view("/api/view")
	if len(v.Sets) != 1 || !v.Sets[0].Focused || v.Focus == nil || v.Focus.Hidden.Total() != 3 ||
		strings.Join(titles(v.View.NeedsYou), "|") != "Near question?" {
		t.Fatalf("focused view: %+v %+v %v", v.Sets, v.Focus, titles(v.View.NeedsYou))
	}
	if strings.Join(v.Paths, ",") != "example-proj,example-proj/r1,other-proj,other-proj/r2,other-proj/r3" {
		t.Fatalf("paths: %v", v.Paths)
	}
	view("/api/view?unfocused=1")
	if len(titles(v.View.NeedsYou)) != 2 {
		t.Fatalf("unfocused view: %v", titles(v.View.NeedsYou))
	}
	post(`{"action":"focus","name":""}`)
	view("/api/view")
	if v.Focus != nil || len(titles(v.View.NeedsYou)) != 2 {
		t.Fatalf("focus off: %+v", v.Focus)
	}
	post(`{"action":"delete","name":"W1"}`)
	view("/api/view")
	if len(v.Sets) != 0 {
		t.Fatalf("deleted: %+v", v.Sets)
	}
}
