package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// New task at a project or a run tells the coordinator working under it:
// api/target?coordinator=1 resolves the node no agent holds to the newest
// unfinished coordinator there. Without the flag a project is still not a
// node to message.
func TestCoordinatorUnder(t *testing.T) {
	h := newHarness(t)
	const pA, pB = "33333333-0000-4000-8000-0000000000a1", "33333333-0000-4000-8000-0000000000b1"
	writePaseoRecord(t, h.paseo, pA, sessA, "", "Old coordinator")
	writePaseoRecord(t, h.paseo, pB, sessB, "", "New coordinator")
	a := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": pA}
	b := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessB, "PASEO_AGENT_ID": pB}
	h.ok(a, "", "join", "r1/coord")
	h.ok(b, "", "join", "r2/coord")
	h.ok(a, "", "set", "status", "Still going.")           // r1 is newer...
	h.ok(a, "All done.", "done", "--title", "r1 finished") // ...but finished

	_, hd := testServer(t, h)
	target := func(q string) (int, Target) {
		t.Helper()
		code, body, _ := do(t, hd, "GET", "/api/target?"+q, "", nil)
		var tg Target
		if code == 200 {
			must(t, json.Unmarshal([]byte(body), &tg))
		}
		return code, tg
	}
	if code, _ := target("to=example-proj"); code != 404 {
		t.Fatalf("a project without the flag: %d, want 404", code)
	}
	if code, tg := target("to=example-proj&coordinator=1"); code != 200 || tg.Node != "example-proj/r2/coord" || tg.For != "example-proj" || tg.Paseo != pB {
		t.Fatalf("project: %d %+v", code, tg)
	}
	if code, tg := target("to=example-proj/r1&coordinator=1"); code != 200 || tg.Node != "example-proj/r1/coord" || tg.For != "example-proj/r1" {
		t.Fatalf("run with only a finished coordinator: %d %+v", code, tg)
	}
	if code, tg := target("to=example-proj/r2/coord&coordinator=1"); code != 200 || tg.Node != "example-proj/r2/coord" || tg.For != "" {
		t.Fatalf("a held node is itself: %d %+v", code, tg)
	}
	if code, _ := target("to=nobody&coordinator=1"); code != 404 {
		t.Fatalf("no agent under it: %d, want 404", code)
	}
}

// A message about a task goes to whoever works on it: the agent that last
// changed its state, else its author, else (filed from the dashboard) the
// node's agent or the coordinator working under the project.
func TestTaskMessageRecipient(t *testing.T) {
	h := newHarness(t)
	const pA, pB = "44444444-0000-4000-8000-0000000000a1", "44444444-0000-4000-8000-0000000000b1"
	writePaseoRecord(t, h.paseo, pA, sessA, "", "Coordinator A")
	writePaseoRecord(t, h.paseo, pB, sessB, "", "Coordinator B")
	a := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": pA}
	b := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessB, "PASEO_AGENT_ID": pB}
	h.ok(a, "", "join", "r1/coord")
	h.ok(b, "", "join", "r2/coord")
	mine := h.ok(a, "", "task", "add", "--title", "A's own", "--brief", "b")
	taken := h.ok(a, "", "task", "add", "--title", "B picks it up", "--brief", "b")
	h.ok(b, "", "task", "start", taken)

	_, hd := testServer(t, h)
	add := func(to string) string {
		t.Helper()
		_, body, _ := do(t, hd, "POST", "/api/task", `{"action":"add","to":"`+to+`","title":"From the dashboard"}`, dashboard)
		var m map[string]string
		must(t, json.Unmarshal([]byte(body), &m))
		return m["id"]
	}
	atNode := add("example-proj/r1/coord")
	// B's start above is an event at r1 (the task's node); B reporting at
	// its own node makes r2 the coordinator most recently at work.
	h.ok(b, "Working.", "tell", "--title", "B at work")
	atProject := add("example-proj")
	for _, c := range []struct{ id, node, paseo string }{
		{mine, "example-proj/r1/coord", pA},
		{taken, "example-proj/r2/coord", pB},
		{atNode, "example-proj/r1/coord", pA},
		{atProject, "example-proj/r2/coord", pB}, // the newest coordinator under it
	} {
		code, body, _ := do(t, hd, "GET", "/api/target?to="+c.id, "", nil)
		var tg Target
		if code == 200 {
			must(t, json.Unmarshal([]byte(body), &tg))
		}
		if code != 200 || tg.Node != c.node || tg.Paseo != c.paseo {
			t.Errorf("%s: %d node %q paseo %q, want %s %s (%s)", c.id, code, tg.Node, tg.Paseo, c.node, c.paseo, body)
		}
	}

	// Status: the prepared message asks the worker for a summary of the task.
	h.respond = func(name string, args []string) (string, error) { return `{"disposition":"steered"}`, nil }
	if code, body, _ := do(t, hd, "POST", "/api/reply", `{"to":"`+taken+`","prompt":"task-status"}`, dashboard); code != 200 {
		t.Fatalf("status: %d %s", code, body)
	}
	out := h.ok(nil, "", "search", "executive summary")
	if !strings.Contains(out, "example-proj/r2/coord") || !strings.Contains(out, "B picks it up") {
		t.Fatalf("status went: %s", out)
	}
	h.fails(nil, "", "is for a task", "reply", "example-proj/r1/coord", "--prompt", "task-status")
}
