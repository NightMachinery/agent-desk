package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// loadDesk reads the harness's store at its current clock.
func (h *harness) loadDesk() *desk.Desk {
	st, err := store.Open(h.desk)
	must(h.t, err)
	defer st.Close()
	d, err := desk.Load(st, h.host, h.now)
	must(h.t, err)
	return d
}

func TestResponses(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-000000000002"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	other := h.as(sessB)
	h.ok(env, "", "join", "r1/coord")
	h.ok(other, "", "join", "r2/coord")
	if got := h.ok(env, "", "whoami"); got != "example-proj/r1/coord" {
		t.Fatalf("whoami: %q", got)
	}
	h.fails(nil, "", "not an agent session", "whoami")
	h.fails(h.as(sessC), "", "has not joined", "whoami")

	tl := h.ok(env, "The site is live.", "tell", "--title", "Deploy finished")
	h.respond = func(name string, args []string) (string, error) { return `{"status":"sent"}`, nil }
	m := h.ok(nil, "", "reply", tl, "check the old feed too")

	mine := h.ok(env, "", "show", "--mine", "--format", "md")
	if !strings.Contains(mine, "Messages awaiting your response (1)") || !strings.Contains(mine, "agent-desk tell --re "+m) {
		t.Fatalf("show --mine lacks the message:\n%s", mine)
	}
	if strings.Contains(mine, "no response yet") {
		t.Fatal("flagged before 15 minutes")
	}

	// Refused: another node's session, a notice, no body, --quiet alone.
	h.fails(other, "ok", "not to this session's node", "tell", "--re", m)
	h.fails(env, "ok", "is a tell", "tell", "--re", tl)
	h.fails(env, "", "needs the response on stdin", "tell", "--re", m)
	h.fails(env, "x", "--quiet goes with --re", "tell", "--quiet", "--title", "T")

	// After 15 minutes the message is flagged.
	h.now = h.now.Add(16 * time.Minute)
	if mine = h.ok(env, "", "show", "--mine", "--format", "md"); !strings.Contains(mine, "no response yet") {
		t.Fatalf("not flagged after 15 minutes:\n%s", mine)
	}

	// The response links, takes its title from the root and clears the flag.
	resp := h.ok(env, "Done, the old feed is fine.", "tell", "--re", m)
	d := h.loadDesk()
	r, err := d.Resolve(resp)
	must(t, err)
	msg, _ := d.Resolve(m)
	root, _ := d.Resolve(tl)
	if r.Title != "Re: Deploy finished" || r.Ref != msg.ID || desk.Meta(r)["quiet"] != nil {
		t.Fatalf("response: %+v", r)
	}
	if d.Awaiting(msg.ID) || d.NoResponse(msg.ID) {
		t.Fatal("responded message still awaiting")
	}
	var kinds []string
	for _, e := range d.Thread(root.ID, "") {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, " ") != "reply delivered tell" {
		t.Fatalf("thread: %v", kinds)
	}
	if mine = h.ok(env, "", "show", "--mine", "--format", "md"); strings.Contains(mine, "awaiting your response") {
		t.Fatalf("responded message still listed:\n%s", mine)
	}

	// Reading the root item reads its responses.
	if !d.Unread(r.ID) {
		t.Fatal("a response starts unread")
	}
	h.ok(nil, "", "read", tl)
	if h.loadDesk().Unread(r.ID) {
		t.Fatal("read on the root did not read its response")
	}

	// A node message: titled from its first line; read clears its flag.
	m2 := h.ok(nil, "", "reply", "example-proj/r1/coord", "pause after seed 4\nthen report")
	h.now = h.now.Add(20 * time.Minute)
	if mine = h.ok(env, "", "show", "--mine", "--format", "md"); !strings.Contains(mine, "no response yet") {
		t.Fatalf("node message not flagged:\n%s", mine)
	}
	h.ok(nil, "", "read", m2)
	if mine = h.ok(env, "", "show", "--mine", "--format", "md"); strings.Contains(mine, "awaiting your response") {
		t.Fatalf("read did not clear the flag:\n%s", mine)
	}
	_, errOut, _ := h.run(nil, "", "read", m2)
	if !strings.Contains(errOut, "not awaiting a response") {
		t.Fatalf("second read: %q", errOut)
	}
	m3 := h.ok(nil, "", "reply", "example-proj/r1/coord", "and the next one")
	q3 := h.ok(env, "Will do.", "tell", "--re", m3, "--quiet")
	d = h.loadDesk()
	e3, _ := d.Resolve(q3)
	if e3.Title != "Re: and the next one" || desk.Meta(e3)["quiet"] != true {
		t.Fatalf("node response: %+v", e3)
	}

	// An answer: a response may refer to it, and it is never flagged.
	q := h.ok(env, "ctx", "ask", "--blocked", "--title", "Which split?")
	a := h.ok(nil, "", "answer", q, "B")
	h.now = h.now.Add(time.Hour)
	if mine = h.ok(env, "", "show", "--mine", "--format", "md"); strings.Contains(mine, "awaiting your response") {
		t.Fatalf("an answer was listed as awaiting:\n%s", mine)
	}
	ar := h.ok(env, "Using B.", "tell", "--re", a)
	d = h.loadDesk()
	if e, _ := d.Resolve(ar); e.Title != "Re: Which split?" {
		t.Fatalf("answer response title %q", e.Title)
	}
	h.fails(other, "no", "not of this session's node", "tell", "--re", a)
}

func TestResponseSectionAndChat(t *testing.T) {
	h := newHarness(t)
	env := h.as(sessA)
	h.ok(env, "", "join", "r1/coord")
	h.ok(env, "Live.", "tell", "--title", "Deploy finished")
	r := h.ok(env, "Seed 2 drew shard 17, which has duplicated rows.", "tell", "--re-chat", "Why is seed 2 slow?\nAnd seed 3?")
	h.fails(env, "x", "not both", "tell", "--re-chat", "q", "--re", r)
	h.fails(env, "", "needs the response on stdin", "tell", "--re-chat", "q")

	v := h.showJSON("", false)
	resp := v["responses"].([]any)
	if len(resp) != 1 {
		t.Fatalf("responses: %v", resp)
	}
	it := resp[0].(map[string]any)["items"].([]any)[0].(map[string]any)
	if it["display_id"] != r || it["asked_via"] != "chat" || it["asked"] != "Why is seed 2 slow?\nAnd seed 3?" ||
		it["event"].(map[string]any)["title"] != "Re: Why is seed 2 slow?" {
		t.Fatalf("chat response: %v", it)
	}
	for _, g := range v["new"].([]any) {
		for _, x := range g.(map[string]any)["items"].([]any) {
			if x.(map[string]any)["display_id"] == r {
				t.Fatal("a response is also in New")
			}
		}
	}
	md := h.ok(nil, "", "show", "--format", "md")
	if !strings.Contains(md, "Responses to you (1)") || !strings.Contains(md, "You asked (chat): Why is seed 2 slow?") {
		t.Fatalf("show md:\n%s", md)
	}
	h.ok(nil, "", "read", r)
	if len(h.showJSON("", false)["responses"].([]any)) != 0 {
		t.Fatal("a read response stays in the section")
	}
}

func TestPins(t *testing.T) {
	h := newHarness(t)
	env := h.as(sessA)
	h.ok(env, "", "join", "r1/coord")
	tl := h.ok(env, "Live.", "tell", "--title", "Deploy finished")
	s1 := h.ok(env, "", "set", "findings", "first")
	h.ok(nil, "", "pin", tl, s1)
	h.ok(nil, "", "read", tl)
	_, errOut, _ := h.run(nil, "", "pin", tl)
	if !strings.Contains(errOut, "already pinned") {
		t.Fatalf("second pin: %q", errOut)
	}
	pinned := func() []string {
		var ids []string
		for _, x := range h.showJSON("", false)["pinned"].([]any) {
			ids = append(ids, x.(map[string]any)["display_id"].(string))
		}
		return ids
	}
	// Most recently pinned first; a read item stays pinned.
	if p := pinned(); len(p) != 2 || p[0] != s1 || p[1] != tl {
		t.Fatalf("pinned: %v", p)
	}
	// A pinned slot follows the slot's current text, and leaves while cleared.
	s2 := h.ok(env, "", "set", "findings", "second")
	if p := pinned(); p[0] != s2 {
		t.Fatalf("slot pin did not follow: %v", p)
	}
	h.ok(env, "", "clear", "findings")
	if p := pinned(); len(p) != 1 || p[0] != tl {
		t.Fatalf("cleared slot still pinned: %v", p)
	}
	h.ok(nil, "", "unpin", tl)
	if p := pinned(); len(p) != 0 {
		t.Fatalf("unpin: %v", p)
	}
	h.fails(nil, "", "pin the item itself", "pin", lastOf(t, h, "pin"))
}

// lastOf is the display id of the latest event of kind k.
func lastOf(t *testing.T, h *harness, k string) string {
	d := h.loadDesk()
	id := ""
	for _, e := range d.Events {
		if e.Kind == k {
			id = d.Display(e.ID)
		}
	}
	return id
}
