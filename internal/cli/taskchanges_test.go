package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// The user's task changes reach the agents working on the tasks: after the
// debounce from the last change, one message per agent, each task's net
// change (an undone change sends nothing), told once; an agent's own change
// and a disabled setting tell nobody.
func TestTaskChangesTold(t *testing.T) {
	h := newHarness(t)
	const pA, pB = "55555555-0000-4000-8000-0000000000a1", "55555555-0000-4000-8000-0000000000b1"
	writePaseoRecord(t, h.paseo, pA, sessA, "", "Coordinator A")
	writePaseoRecord(t, h.paseo, pB, sessB, "", "Coordinator B")
	a := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": pA}
	b := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessB, "PASEO_AGENT_ID": pB}
	h.ok(a, "", "join", "r1/coord")
	h.ok(b, "", "join", "r2/coord")
	one := h.ok(a, "", "task", "add", "--title", "Comparison table", "--brief", "b")
	two := h.ok(a, "", "task", "add", "--title", "Old feed", "--brief", "b")
	three := h.ok(b, "", "task", "add", "--title", "Seed 5", "--brief", "b")
	undone := h.ok(a, "", "task", "add", "--title", "Undone", "--brief", "b")
	own := h.ok(a, "", "task", "add", "--title", "Its own", "--brief", "b")
	h.ok(a, "", "task", "start", one)
	h.ok(a, "", "task", "drop", own, "--note", "not needed") // the agent's own change

	s, hd := testServer(t, h)
	t0 := s.a.Now()
	act := func(body string) map[string]any {
		t.Helper()
		code, out, _ := do(t, hd, "POST", "/api/task", body, dashboard)
		if code != 200 {
			t.Fatalf("%s: %d %s", body, code, out)
		}
		var m map[string]any
		must(t, json.Unmarshal([]byte(out), &m))
		return m
	}
	if m := act(`{"action":"done","id":"` + one + `"}`); m["tell"] != "example-proj/r1/coord" || m["after"] != float64(10) {
		t.Fatalf("done answers %v, want who hears and when", m)
	}
	act(`{"action":"drop","id":"` + two + `"}`)
	act(`{"action":"done","id":"` + three + `"}`)
	act(`{"action":"done","id":"` + undone + `"}`)
	act(`{"action":"reopen","id":"` + undone + `"}`) // Undo

	told := func() []store.Event {
		t.Helper()
		d, err := s.a.load()
		must(t, err)
		var out []store.Event
		for _, e := range d.Events {
			if e.Kind == "reply" && strings.Contains(string(e.Meta), "task_changes") {
				out = append(out, e)
			}
		}
		return out
	}
	tell := func(after time.Duration) int {
		t.Helper()
		n, err := s.tellTaskChanges(t0.Add(after))
		must(t, err)
		return n
	}
	if n := tell(5 * time.Second); n != 0 {
		t.Fatalf("told %d before the debounce", n)
	}
	if n := tell(11 * time.Second); n != 2 {
		t.Fatalf("told %d messages, want 2 (A: two tasks, B: one)", n)
	}
	msgs := told()
	if len(msgs) != 2 {
		t.Fatalf("%d messages", len(msgs))
	}
	for _, e := range msgs {
		to, _ := toOf(e)
		switch to.Paseo {
		case pA:
			if e.Ref != "" || !strings.Contains(e.Body, "I changed 2 tasks") || !strings.Contains(e.Body, `"Comparison table": marked done (it was started)`) ||
				!strings.Contains(e.Body, `"Old feed": dropped (it was open)`) || strings.Contains(e.Body, "Undone") || strings.Contains(e.Body, "Its own") {
				t.Errorf("to A: ref %q\n%s", e.Ref, e.Body)
			}
		case pB:
			if e.Path != "example-proj/r2/coord" || !strings.HasPrefix(e.Body, "I marked this task done (it was open). Stop working on it") {
				t.Errorf("to B at %s:\n%s", e.Path, e.Body)
			}
		default:
			t.Errorf("a message to %+v", to)
		}
	}
	if n := tell(20 * time.Second); n != 0 {
		t.Fatalf("told again: %d", n)
	}

	// Reopen with a note: one task, so it threads under the task.
	act(`{"action":"reopen","id":"` + one + `","text":"The table lacks seed 5."}`)
	if n := tell(30 * time.Second); n != 1 {
		t.Fatalf("reopen: told %d", n)
	}
	last := told()[2]
	d, err := s.a.load()
	must(t, err)
	if d.Display(last.Ref) != one || !strings.HasPrefix(last.Body, "I reopened this task (it was done). My note: The table lacks seed 5. Pick it up again and deal with what the note says") {
		t.Fatalf("reopen: ref %s\n%s", d.Display(last.Ref), last.Body)
	}
	if desk.MetaString(d.ByID[last.Ref], "state") != "" {
		t.Fatal("ref is not the task")
	}

	// A change from a shell outside any agent counts too.
	h.ok(nil, "", "task", "done", undone)
	if n := tell(time.Minute); n != 1 {
		t.Fatalf("shell: told %d", n)
	}

	// Off, nothing goes, and the answer names nobody.
	if code, out, _ := do(t, hd, "POST", "/api/dashboard-prefs", `{"key":"task-changes","data":{"enabled":true,"debounce":0}}`, dashboard); code != 400 {
		t.Fatalf("debounce 0: %d %s", code, out)
	}
	if code, out, _ := do(t, hd, "POST", "/api/dashboard-prefs", `{"key":"task-changes","data":{"enabled":false,"debounce":30}}`, dashboard); code != 200 {
		t.Fatalf("off: %d %s", code, out)
	}
	if m := act(`{"action":"drop","id":"` + one + `"}`); m["tell"] != nil {
		t.Fatalf("off, the answer still names %v", m["tell"])
	}
	h.ok(nil, "", "task", "done", three)
	if n := tell(time.Hour); n != 0 {
		t.Fatalf("off: told %d", n)
	}
	_, out, _ := do(t, hd, "GET", "/api/task-changes", "", nil)
	if !strings.Contains(out, `"enabled":false`) || !strings.Contains(out, `"debounce":30`) {
		t.Fatalf("api/task-changes: %s", out)
	}
}
