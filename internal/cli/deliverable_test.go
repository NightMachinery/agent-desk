package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/desk"
)

// A deliverable is listed in Deliverables, above Responses and never in
// New; a newer one of the same key supersedes it; it can respond to a
// message and sit in its thread, like a done report can.
func TestDeliverables(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-0000000000d1"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "App coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "app/coord")

	h.fails(env, "x", "needs --title", "deliverable", "--url", "https://example.org")
	h.fails(env, "x", "open or download", "deliverable", "--title", "Nothing to open")
	h.fails(env, "x", "http or https", "deliverable", "--title", "Bad", "--url", "javascript:alert(1)")
	h.fails(env, "x", "--quiet goes with --re", "deliverable", "--title", "Q", "--url", "https://example.org", "--quiet")

	apk := filepath.Join(h.home, "app-debug.apk")
	must(t, os.WriteFile(apk, []byte("PK\x03\x04 not really an apk"), 0o644))
	b1 := h.ok(env, "Install it and open Settings.", "deliverable", "--title", "Android APK 1.0", "--key", "android-apk",
		"--attach", apk, "--caption", "Debug build")
	if !strings.HasPrefix(b1, "b-") {
		t.Fatalf("display id %q", b1)
	}
	site := h.ok(env, "", "deliverable", "--title", "Site on staging", "--url", "https://staging.example.org/")

	d := h.loadDesk()
	v := d.Build(desk.Filter{})
	if n := countItems(v.Deliverables); n != 2 {
		t.Fatalf("deliverables: %d", n)
	}
	if n := countItems(v.New); n != 0 {
		t.Fatalf("a deliverable is in New: %d", n)
	}
	if desk.ClassOf(d.ByID[v.Deliverables[0].Items[0].Event.ID]) != "deliverable" {
		t.Fatal("class")
	}
	md := h.ok(nil, "", "show", "--format", "md")
	if !strings.Contains(md, "Deliverables (2)") || !strings.Contains(md, "Open: https://staging.example.org/") ||
		strings.Index(md, "Deliverables (2)") > strings.Index(md, "New (") {
		t.Fatalf("show:\n%s", md)
	}

	// A newer APK supersedes the first; it stays visible with --all.
	b2 := h.ok(env, "", "deliverable", "--title", "Android APK 1.1", "--key", "android-apk", "--url", "https://example.org/app-1.1.apk")
	d = h.loadDesk()
	v = d.Build(desk.Filter{})
	ids := []string{}
	for _, g := range v.Deliverables {
		for _, it := range g.Items {
			ids = append(ids, it.ID)
		}
	}
	if strings.Join(ids, " ") != site+" "+b2 {
		t.Fatalf("after the newer APK: %v", ids)
	}
	all := d.Build(desk.Filter{All: true})
	found := false
	for _, g := range all.Deliverables {
		for _, it := range g.Items {
			if it.ID == b1 {
				found = it.SupersededBy == b2
			}
		}
	}
	if !found {
		t.Fatal("the superseded APK is not marked with --all")
	}
	// It counts as read: its project counts only the newer one (and the site).
	if e1, err := d.Resolve(b1); err != nil || d.Unread(e1.ID) {
		t.Fatal("the superseded APK still counts as unread")
	}

	// The three ways to list them: once b2 is read, unread drops it, latest
	// keeps it (and still not b1), all lists b1 too.
	h.ok(nil, "", "read", b2)
	d = h.loadDesk()
	listed := func(mode string, all bool) string {
		ids := []string{}
		for _, g := range d.Build(desk.Filter{Deliverables: mode, All: all}).Deliverables {
			for _, it := range g.Items {
				ids = append(ids, it.ID)
			}
		}
		return strings.Join(ids, " ")
	}
	for _, c := range []struct {
		mode string
		all  bool
		want string
	}{
		{"", false, site},
		{desk.DeliverUnread, false, site},
		{desk.DeliverLatest, false, site + " " + b2},
		{desk.DeliverAll, false, b1 + " " + site + " " + b2},
		{"", true, b1 + " " + site + " " + b2},
		{desk.DeliverUnread, true, site},
	} {
		if got := listed(c.mode, c.all); got != c.want {
			t.Fatalf("deliverables %q (all %v): %q, want %q", c.mode, c.all, got, c.want)
		}
	}
	if n := d.Build(desk.Filter{}).DeliverablesAny; n != 3 {
		t.Fatalf("deliverables_any = %d, want 3", n)
	}
	h.fails(nil, "", "--deliverables is", "show", "--deliverables", "newest")
	h.ok(nil, "", "unread", b2)

	// Responding: a deliverable and a done report answer a message, and
	// both sit in its thread; the done report is listed with Responses.
	h.respond = func(name string, args []string) (string, error) { return `{"status":"sent"}`, nil }
	m := h.ok(nil, "", "reply", site, "ship the APK too")
	b3 := h.ok(env, "Here.", "deliverable", "--re", m, "--title", "Android APK 1.2", "--url", "https://example.org/app-1.2.apk")
	dn := h.ok(env, "All shipped.", "done", "--re", m, "--title", "Release done")
	h.fails(env, "x", "is a deliverable", "done", "--re", b3)
	d = h.loadDesk()
	siteEv, err := d.Resolve(site)
	must(t, err)
	thread := d.Thread(siteEv.ID, "")
	got := []string{}
	for _, e := range thread {
		if e.Kind != "delivered" {
			got = append(got, d.Display(e.ID))
		}
	}
	if strings.Join(got, " ") != m+" "+b3+" "+dn {
		t.Fatalf("thread: %v", got)
	}
	v = d.Build(desk.Filter{})
	resp := []string{}
	for _, g := range v.Responses {
		for _, it := range g.Items {
			resp = append(resp, it.ID)
		}
	}
	if strings.Join(resp, " ") != dn {
		t.Fatalf("responses: %v", resp)
	}
	b3Ev, err := d.Resolve(b3)
	must(t, err)
	if !d.Item(b3Ev).Response {
		t.Fatal("the deliverable is not a response")
	}
}
