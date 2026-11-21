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
	// A message about a chat response, and the agent's response to it, are
	// that response's conversation, not the node's.
	h.respond = func(name string, args []string) (string, error) { return `{"status":"sent"}`, nil }
	m := h.ok(nil, "", "reply", r, "and seed 4?")
	h.ok(env, "Seed 4 is fine.", "tell", "--re", m)
	d := h.loadDesk()
	re, _ := d.Resolve(r)
	var kinds []string
	for _, e := range d.Thread(re.ID, "") {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, " ") != "reply tell" {
		t.Fatalf("chat response thread: %v", kinds)
	}
	for _, e := range d.Thread("", "example-proj/r1/coord") {
		if e.Kind != "tell" || e.ID != re.ID {
			t.Fatalf("node thread holds %s %s", e.Kind, d.Display(e.ID))
		}
	}
	h.ok(nil, "", "read", m)
	h.ok(nil, "", "read", d.Display(d.ResponsesTo(mustResolve(t, d, m))[0].ID))

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

// Only related messages share a thread: each message sent to a node about
// no item roots its own, and a reply to it stays there. A response to a
// generated message about a task (meta.auto) hangs from the task, and the
// message still counts as answered.
func TestMessageThreads(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-0000000000c1"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	h.respond = func(name string, args []string) (string, error) { return `{"status":"sent"}`, nil }

	m1 := h.ok(nil, "", "reply", "example-proj/r1/coord", "how are the seeds?")
	m2 := h.ok(nil, "", "reply", "example-proj/r1/coord", "unrelated: rename the repo")
	r1 := h.ok(env, "Seeds 1-4 done.", "tell", "--re", m1)
	follow := h.ok(nil, "", "reply", m1, "and seed 5?") // a reply to the message: same thread
	d := h.loadDesk()
	ids := func(evs []store.Event) string {
		var out []string
		for _, e := range evs {
			if e.Kind != "delivered" {
				out = append(out, d.Display(e.ID))
			}
		}
		return strings.Join(out, " ")
	}
	e1, _ := d.Resolve(m1)
	e2, _ := d.Resolve(m2)
	if got := ids(d.Thread(e1.ID, "")); got != m1+" "+r1+" "+follow {
		t.Fatalf("m1's thread: %s", got)
	}
	if got := ids(d.Thread(e2.ID, "")); got != m2 {
		t.Fatalf("m2's thread: %s", got)
	}
	if got := ids(d.MessageThreads("example-proj/r1/coord")); got != m1+" "+m2 {
		t.Fatalf("node threads: %s", got)
	}
	f, _ := d.Resolve(follow)
	if f.Ref != e1.ID || !strings.Contains(ReplyText(d, f), "following up on their message "+m1+` ("how are the seeds?")`) {
		t.Fatalf("follow-up: ref %s\n%s", d.Display(f.Ref), ReplyText(d, f))
	}
	if r, _ := d.Resolve(r1); r.Title != "Re: how are the seeds?" {
		t.Fatalf("response title %q", r.Title)
	}

	// A generated message about a task: the prompt names the task, and the
	// response, though it names the message, hangs from the task.
	task := h.ok(env, "", "task", "add", "--title", "Seed 5", "--brief", "Rerun seed 5.")
	_, hd := testServer(t, h)
	if code, body, _ := do(t, hd, "POST", "/api/reply", `{"to":"`+task+`","text":"The user filed a task.","auto":"task-filed"}`, dashboard); code != 200 {
		t.Fatalf("task-filed: %d %s", code, body)
	}
	if code, body, _ := do(t, hd, "POST", "/api/reply", `{"to":"`+task+`","text":"x","auto":"bogus"}`, dashboard); code != 400 {
		t.Fatalf("bogus auto: %d %s", code, body)
	}
	d = h.loadDesk()
	var auto store.Event
	for _, e := range d.Events {
		if e.Kind == "reply" && desk.MetaString(e, "auto") == "task-filed" {
			auto = e
		}
	}
	if !strings.HasSuffix(ReplyText(d, auto), "agent-desk tell --re "+task+")") {
		t.Fatalf("prompt:\n%s", ReplyText(d, auto))
	}
	resp := h.ok(env, "On it.", "tell", "--re", d.Display(auto.ID))
	direct := h.ok(env, "Seed 5 is running.", "tell", "--re", task)
	d = h.loadDesk()
	r, _ := d.Resolve(resp)
	tk, _ := d.Resolve(task)
	if r.Ref != tk.ID || desk.MetaString(r, "answers") != auto.ID || d.Awaiting(auto.ID) {
		t.Fatalf("redirected response: ref %s answers %s awaiting %v", d.Display(r.Ref), desk.MetaString(r, "answers"), d.Awaiting(auto.ID))
	}
	if got := ids(d.Thread(tk.ID, "")); got != d.Display(auto.ID)+" "+resp+" "+direct {
		t.Fatalf("task thread: %s", got)
	}
	h.fails(h.as(sessB), "x", "does not work on", "tell", "--re", task)
}
