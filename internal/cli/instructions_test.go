package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

func hookJSON(session, prompt string) string {
	b, _ := json.Marshal(map[string]string{"session_id": session, "prompt": prompt, "cwd": "/tmp", "hook_event_name": "UserPromptSubmit"})
	return string(b)
}

func TestInstructionHook(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	none := map[string]string{} // hooks need not inherit the session variables
	hook := func(prompt string) {
		t.Helper()
		if out := h.ok(none, hookJSON(sessA, prompt), "hook", "prompt", "--agent", "claude"); out != "" {
			t.Fatalf("the hook printed %q; its output would join the prompt", out)
		}
	}

	hook("before joining: not recorded")
	h.ok(a, "", "join", "r1/coord")
	typed := deskMessage(t, h, a)
	before := h.count()
	for _, p := range []string{
		"Continue.",
		"/compact",
		"/clear",
		`<paseo-system kind="agent-message">from a parent</paseo-system>`,
		`Schedule "keepalive" fired (id=abc, run=1)` + "\nping",
		`Answer to q-7f3a "Which split?": B`,
		"Message from the user via agent-desk, about t-k2m9 \"x\": hi",
		typed,
		"<task-notification>done</task-notification>",
		"[agent-quotas keepalive] Automatic cache keepalive, not the user: reply with one word.",
		"   ",
	} {
		hook(p)
	}
	if h.count() != before {
		t.Fatalf("an automated prompt was recorded")
	}

	hook("  Use the big drive for worktrees.\n\nAnd keep the old cache.  ")
	hook("/delegate stronger: design the reply UX")
	if h.count() != before+2 {
		t.Fatalf("want 2 instructions, have %d new events", h.count()-before)
	}
	out := h.ok(a, "", "instructions")
	if !strings.Contains(out, "Use the big drive for worktrees.\n\nAnd keep the old cache.") ||
		!strings.Contains(out, "/delegate stronger: design the reply UX") || !strings.Contains(out, "r1/coord") ||
		strings.Contains(out, "before joining") {
		t.Fatalf("instructions:\n%s", out)
	}
	if out := h.ok(nil, "", "instructions", "other"); !strings.Contains(out, "no instructions") {
		t.Fatalf("other prefix: %q", out)
	}
	js := h.ok(nil, "", "instructions", "example-proj/r1", "--format", "json")
	if n := strings.Count(js, "\n") + 1; n != 2 || !strings.Contains(js, `"id":"u-`) {
		t.Fatalf("json: %s", js)
	}
	// Not in the user's views.
	if out := h.ok(nil, "", "show", "--all", "--format", "md"); strings.Contains(out, "big drive") {
		t.Fatalf("an instruction shows in show:\n%s", out)
	}

	// A tmux worker's prompts come from its parent.
	h.tmux["%9"] = "ag--worker\tsub\t123"
	w := map[string]string{"TMUX_PANE": "%9"}
	h.ok(map[string]string{"AI_AGENT": "claude-code_test", "CLAUDE_CODE_SESSION_ID": sessB, "TMUX_PANE": "%9"}, "", "join", "r1/coord/w1")
	n := h.count()
	h.ok(w, hookJSON(sessB, "a brief from the parent"), "hook", "prompt", "--agent", "claude")
	if h.count() != n {
		t.Fatalf("a tmux worker's prompt was recorded")
	}

	// Config: extra skips, and off.
	cfg := filepath.Join(h.home, ".config", "agent-desk", "config.toml")
	must(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
	must(t, os.WriteFile(cfg, []byte("[instructions]\nskip = ['^PING ']\n"), 0o644))
	n = h.count()
	hook("PING from a script")
	if h.count() != n {
		t.Fatalf("a configured skip was recorded")
	}
	must(t, os.WriteFile(cfg, []byte("[instructions]\nrecord = false\n"), 0o644))
	hook("a real message, but recording is off")
	if h.count() != n {
		t.Fatalf("recorded while off")
	}

	// Bad input is logged, not reported to the agent.
	if out, _, code := h.run(none, "not json", "hook", "prompt", "--agent", "claude"); code != 0 || out != "" {
		t.Fatalf("bad input: %d %q", code, out)
	}
	if b, err := os.ReadFile(filepath.Join(h.desk, "hook-errors.log")); err != nil || !strings.Contains(string(b), "not JSON") {
		t.Fatalf("hook log: %v %q", err, b)
	}
}

func TestAgentNotes(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	state := strings.Repeat("a line of run state\n", 120)
	h.fails(a, state, "at most 15 lines", "set", "findings")
	h.ok(a, state, "set", "--for", "agents", "state")
	h.ok(a, "fine", "set", "--for", "agents", "scratch")
	h.fails(a, "x", "reserved", "set", "--for", "agents", "tasks")
	h.fails(a, "x", "--for is agents", "set", "--for", "robots", "x")
	h.ok(a, "", "set", "status", "working")

	if got := h.ok(a, "", "get", "state"); got != strings.TrimRight(state, "\n") {
		t.Fatalf("get: %q", got)
	}
	if got := h.ok(nil, "", "get", "status", "--node", "example-proj/r1/coord"); got != "working" {
		t.Fatalf("get --node: %q", got)
	}
	h.fails(a, "", "no slot nope", "get", "nope")

	// Not unread, not in New; listed by name in show.
	out := h.ok(nil, "", "show", "--format", "md")
	if strings.Contains(out, "a line of run state") {
		t.Fatalf("a note's text shows:\n%s", out)
	}
	if !strings.Contains(out, "Agent notes (agent-desk get <name>): scratch (1 lines") || !strings.Contains(out, "state (120 lines") {
		t.Fatalf("show lacks the notes line:\n%s", out)
	}
	_, hd := testServer(t, h)
	_, body, _ := do(t, hd, "GET", "/api/view?path=example-proj", "", nil)
	if strings.Contains(body, `"unread":1`) || !strings.Contains(body, `"notes":[`) {
		t.Fatalf("view: %s", body)
	}
}

// deskMessage is a desk message as it is typed into a session (ReplyText).
func deskMessage(t *testing.T, h *harness, env map[string]string) string {
	t.Helper()
	tl := h.ok(env, "x", "tell", "--title", "Deploy finished")
	h.respond = func(name string, args []string) (string, error) { return `{"status":"sent"}`, nil }
	m := h.ok(nil, "", "reply", tl, "check the old feed too")
	d := h.loadDesk()
	e, err := d.Resolve(m)
	must(t, err)
	text := ReplyText(d, e)
	if !strings.HasPrefix(text, "Message "+m+" from the user via agent-desk") {
		t.Fatalf("typed text: %q", text)
	}
	return text
}

// Desk messages recorded as instructions before the pattern knew their id
// are left out of the output.
func TestInstructionsDropOldDeskMessages(t *testing.T) {
	h := newHarness(t)
	env := h.as(sessA)
	h.ok(env, "", "join", "r1/coord")
	a, _, _ := h.app(nil, "")
	for _, body := range []string{"Message m-4c2d from the user via agent-desk, about t-k2m9 \"x\": hi", "Keep the old cache."} {
		e := a.event("instruction", "example-proj/r1/coord", ident.From{}, "hook", nil)
		e.Body = body
		must(t, a.commit([]store.Event{e}, ""))
	}
	out := h.ok(nil, "", "instructions", "example-proj/r1")
	if strings.Contains(out, "via agent-desk") || !strings.Contains(out, "Keep the old cache.") {
		t.Fatalf("instructions:\n%s", out)
	}
}
