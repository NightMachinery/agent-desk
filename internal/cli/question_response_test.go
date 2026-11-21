package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/desk"
)

// A question response: an ask that is also the response to a message
// (docs/spec-conversation.md, "Question responses").
func TestQuestionResponse(t *testing.T) {
	h := newHarness(t)
	log := filepath.Join(t.TempDir(), "hooks.jsonl")
	notifyConfig(t, h, log, "away_command = \"exit 1\"\n")
	const uuid = "22222222-0000-4000-8000-000000000031"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	other := h.as(sessB)
	h.ok(env, "", "join", "r1/coord")
	h.ok(other, "", "join", "r2/coord")
	tl := h.ok(env, "The site is live.", "tell", "--title", "Deploy finished")
	h.respond = func(name string, args []string) (string, error) { return `{"status":"sent"}`, nil }
	m := h.ok(nil, "", "reply", tl, "--notify-desktop", "none", "--notify-remote", "off", "drop the old feed")

	// 1. Refused from another node's session, on a notice, and with --quiet.
	h.fails(other, "ctx", "not to this session's node", "ask", "--re", m, "--blocked")
	h.fails(env, "ctx", "is a tell", "ask", "--re", tl, "--blocked")
	h.fails(env, "ctx", "no --quiet", "ask", "--re", m, "--blocked", "--quiet")
	h.fails(env, "ctx", "not both", "ask", "--re", m, "--re-chat", "x", "--blocked")
	before := len(readHookLog(t, log))
	q := h.ok(env, "There are two: v1 and v2-legacy. Which one?", "ask", "--re", m, "--blocked")

	// 2. It links, responds at once, is listed in Needs you and not in
	// Responses, and sits in the thread.
	d := h.loadDesk()
	qe, err := d.Resolve(q)
	must(t, err)
	me, _ := d.Resolve(m)
	root, _ := d.Resolve(tl)
	if qe.Ref != me.ID || qe.Title != "Re: Deploy finished" || !desk.IsQuestionResponse(qe) {
		t.Fatalf("question response: %+v", qe)
	}
	if d.Awaiting(me.ID) {
		t.Fatal("the message still awaits a response")
	}
	has := func(gs []desk.Group, id string) bool {
		for _, g := range gs {
			for _, it := range g.Items {
				if it.Event.ID == id {
					return true
				}
			}
		}
		return false
	}
	if !has(d.NeedsYou(desk.Filter{}), qe.ID) || has(d.ResponseGroups(desk.Filter{}), qe.ID) {
		t.Fatal("not in Needs you, or listed in Responses")
	}

	// 4. It notifies as ask-blocked; the message's "off" cannot silence it.
	got := readHookLog(t, log)[before:]
	if len(got) != 1 || got[0].Class != "ask-blocked" || got[0].DisplayID != q || got[0].Channel != "desktop" {
		t.Fatalf("notification: %+v", got)
	}

	// 3. Reading the root reads a plain response, not the open question.
	r := h.ok(env, "Removing the feed config now.", "tell", "--re", m)
	h.ok(nil, "", "read", tl)
	d = h.loadDesk()
	re, _ := d.Resolve(r)
	if d.Unread(re.ID) || d.Dismissed(qe.ID) || !d.QuestionOpen(qe.ID) || !has(d.NeedsYou(desk.Filter{}), qe.ID) {
		t.Fatal("read on the root: the response should be read and the question left open in Needs you")
	}

	// 5. The answer goes to the asking session and joins the thread.
	calls := len(*h.calls)
	a := h.ok(nil, "", "answer", q, "v2-legacy")
	if len(*h.calls) == calls {
		t.Fatal("the answer was not delivered")
	}
	d = h.loadDesk()
	ae, _ := d.Resolve(a)
	if _, ok := d.Delivery(ae.ID); !ok {
		t.Fatal("no delivered mark on the answer")
	}
	var kinds []string
	for _, e := range d.Thread(root.ID, "") {
		kinds = append(kinds, e.Kind)
	}
	if want := "reply delivered ask tell answer delivered"; strings.Join(kinds, " ") != want {
		t.Fatalf("thread: %v, want %s", kinds, want)
	}
	h.ok(env, "x", "tell", "--title", "Later")
	h.ok(nil, "", "render") // writes no longer render; the server or this does
	ledger, err := os.ReadFile(filepath.Join(h.desk, "render", "ledger", h.now.Format("2006-01")+".org"))
	must(t, err)
	if !strings.Contains(string(ledger), "** ANSWERED "+q+" question to "+m+": Re: Deploy finished") {
		t.Fatalf("ledger lacks the question in its thread:\n%s", ledger)
	}

	// 4, louder: an override above the class is used.
	m2 := h.ok(nil, "", "reply", tl, "--notify-remote", "always", "and the cache?")
	before = len(readHookLog(t, log))
	h.ok(env, "Clear it, or keep it a week?", "ask", "--re", m2, "--default", "keep it a week")
	got = readHookLog(t, log)[before:]
	remote := false
	for _, p := range got {
		remote = remote || (p.Channel == "remote" && p.Class == "ask-default")
	}
	if !remote {
		t.Fatalf("louder override not used: %+v", got)
	}

	// --re-chat works too and takes its title from the chat question.
	c := h.ok(env, "Which seed first?", "ask", "--re-chat", "can you rerun the seeds?", "--blocked")
	if e, _ := h.loadDesk().Resolve(c); e.Title != "Re: can you rerun the seeds?" || desk.MetaString(e, "asked") == "" {
		t.Fatalf("chat question response: %+v", e)
	}
}
