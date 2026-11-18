package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// eventsAt returns the events of kind at path.
func (h *harness) eventsAt(kind, path string) []store.Event {
	h.t.Helper()
	st, err := store.Open(h.desk)
	must(h.t, err)
	defer st.Close()
	evs, err := st.Query("events", "kind = '"+kind+"' AND path = '"+path+"'")
	must(h.t, err)
	return evs
}

func TestHeldNodes(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	// One node: every command works as before.
	h.ok(a, "", "set", "status", "one node")
	if got := h.ok(a, "", "whoami"); got != "example-proj/r1/coord" {
		t.Fatalf("whoami with one node: %q", got)
	}
	h.ok(a, "", "join", "r2/coord")
	h.ok(a, "", "join", "r1/coord") // re-joining a held node is harmless
	if got := h.ok(a, "", "whoami"); got != "example-proj/r2/coord\nexample-proj/r1/coord" {
		t.Fatalf("whoami with two nodes: %q", got)
	}

	// Every write that means "my node" refuses without --node, listing both.
	must(t, os.WriteFile(filepath.Join(h.cwd, "notes.org"), []byte("* x\n"), 0o644))
	for _, args := range [][]string{
		{"set", "status", "x"},
		{"clear", "status"},
		{"tell", "--title", "x"},
		{"ask", "--blocked", "--title", "x"},
		{"done", "--title", "x"},
		{"task", "add", "--title", "x"},
		{"link", "notes", "notes.org"},
	} {
		_, errOut, code := h.run(a, "ctx", args...)
		if code == 0 || !strings.Contains(errOut, "holds 2 nodes") || !strings.Contains(errOut, "--node PATH") ||
			!strings.Contains(errOut, "example-proj/r1/coord") || !strings.Contains(errOut, "example-proj/r2/coord") ||
			!strings.Contains(errOut, "agent-desk leave") {
			t.Fatalf("%v: %d %q", args, code, errOut)
		}
	}
	// Reads of "my node" ask too.
	h.fails(a, "", "holds 2 nodes", "get", "status")
	h.fails(a, "", "holds 2 nodes", "show", "--mine")
	h.fails(a, "", "holds 2 nodes", "instructions")

	// --node, full or relative to the project, lands on that node.
	h.ok(a, "", "set", "status", "second run", "--node", "r2/coord")
	h.ok(a, "", "set", "status", "first run", "--node", "example-proj/r1/coord")
	if got := h.ok(a, "", "get", "status", "--node", "r2/coord"); got != "second run" {
		t.Fatalf("r2 status: %q", got)
	}
	if got := h.ok(a, "", "get", "status", "--node", "example-proj/r1/coord"); got != "first run" {
		t.Fatalf("r1 status: %q", got)
	}
	h.ok(a, "ctx", "ask", "--blocked", "--title", "Second-run question?", "--node", "r2/coord")
	if evs := h.eventsAt("ask", "example-proj/r2/coord"); len(evs) != 1 || evs[0].Title != "Second-run question?" {
		t.Fatalf("ask --node: %+v", evs)
	}
	h.ok(a, "", "task", "add", "--title", "a task", "--node", "r2/coord")
	if evs := h.eventsAt("task", "example-proj/r2/coord"); len(evs) != 1 {
		t.Fatalf("task add --node: %+v", evs)
	}
	if !strings.Contains(h.ok(a, "", "show", "--mine", "--node", "r2/coord", "--format", "json"), "Second-run question?") {
		t.Fatal("show --mine --node does not show the node")
	}

	// done --node finishes only that node.
	h.ok(a, "", "done", "--title", "Run 2 finished", "--node", "r2/coord")
	if len(h.eventsAt("done", "example-proj/r2/coord")) != 1 || len(h.eventsAt("done", "example-proj/r1/coord")) != 0 {
		t.Fatal("done --node finished the wrong node")
	}
	if got := h.ok(a, "", "get", "status", "--node", "r1/coord"); got != "first run" {
		t.Fatalf("done --node touched r1's status: %q", got)
	}

	// --node to a node this session does not hold is refused.
	h.fails(a, "", "not a node this session holds", "set", "status", "x", "--node", "r3/coord")
	h.fails(a, "", "not a node this session holds", "set", "status", "x", "--node", "other-proj/r1/coord")

	// leave drops one; single-node behaviour resumes.
	if out := h.ok(a, "", "leave", "r2/coord"); !strings.Contains(out, "holds example-proj/r1/coord") {
		t.Fatalf("leave: %q", out)
	}
	if got := h.ok(a, "", "whoami"); got != "example-proj/r1/coord" {
		t.Fatalf("whoami after leave: %q", got)
	}
	h.ok(a, "", "set", "status", "back to one")
	if got := h.ok(a, "", "get", "status"); got != "back to one" {
		t.Fatalf("after leave: %q", got)
	}
	h.fails(a, "", "not a node this session holds", "leave", "r2/coord")
	// A node its holder left is free to join without --takeover.
	h.ok(h.as(sessB), "", "join", "r2/coord")

	// Leaving the last node makes the session nodeless again.
	h.ok(a, "", "leave", "example-proj/r1/coord")
	h.fails(a, "", "has not joined", "whoami")
}

func TestHeldNodeWorkerCannotWriteAsParent(t *testing.T) {
	h := newHarness(t)
	h.ok(h.as(sessA), "", "join", "r1/coord")
	w := h.as(sessB) // a worker its coordinator gave a nested node
	h.ok(w, "", "join", "r1/coord/eval")
	h.fails(w, "", "not a node this session holds", "set", "status", "x", "--node", "r1/coord")
	h.fails(w, "", "not a node this session holds", "tell", "--title", "x", "--node", "example-proj/r1/coord")
	h.ok(w, "", "set", "status", "evaluating", "--node", "r1/coord/eval")
}

func TestHeldNodeAmbiguousRelative(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	h.ok(a, "", "join", "r1/coord", "--project", "other-proj")
	h.fails(a, "", "matches more than one", "set", "status", "x", "--node", "r1/coord")
	h.ok(a, "", "set", "status", "x", "--node", "other-proj/r1/coord")
}

func TestHeldNodeTakeover(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	h.ok(a, "", "join", "r2/coord")
	h.ok(h.as(sessC), "", "join", "r2/coord", "--takeover")
	if got := h.ok(a, "", "whoami"); got != "example-proj/r1/coord" {
		t.Fatalf("whoami after takeover: %q", got)
	}
	h.fails(a, "", "not a node this session holds", "set", "status", "x", "--node", "r2/coord")
	h.ok(a, "", "set", "status", "x") // one node left
}

func TestHeldNodeDelivery(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-000000000002"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Two-run coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	h.ok(env, "", "join", "r2/coord")
	h.respond = func(name string, args []string) (string, error) { return "queued", nil }

	h.ok(nil, "", "reply", "example-proj/r2/coord", "About the second run")
	q := h.ok(env, "ctx", "ask", "--blocked", "--title", "Which seed?", "--node", "r2/coord")
	h.ok(nil, "", "answer", q, "seed 3")
	c := *h.calls
	if len(c) != 2 || !strings.Contains(c[0], uuid+" Message") || !strings.Contains(c[0], "to example-proj/r2/coord: About the second run") ||
		!strings.Contains(c[1], uuid) || !strings.Contains(c[1], "seed 3") {
		t.Fatalf("delivery to the second node: %q", c)
	}
}

func TestHeldNodeInstructions(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	h.ok(a, "", "join", "r2/coord")
	h.ok(map[string]string{}, hookJSON(sessA, "Use seed 3 in both runs."), "hook", "prompt", "--agent", "claude")
	for _, p := range []string{"r1", "r2"} {
		if got := h.ok(a, "", "instructions", "--node", p+"/coord"); strings.Count(got, "Use seed 3") != 1 {
			t.Fatalf("instructions for %s: %q", p, got)
		}
	}
	if got := h.ok(a, "", "instructions", "example-proj"); strings.Count(got, "Use seed 3") != 1 {
		t.Fatalf("the copies show once across the project: %q", got)
	}
}

func TestLeaveMergesAcrossDesks(t *testing.T) {
	h := newHarness(t)
	hb := *h
	hb.desk, hb.host = filepath.Join(t.TempDir(), "desk-b"), "host-b"
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	h.ok(a, "", "join", "r2/coord")
	hb.now = h.now.Add(time.Minute)
	hb.ok(nil, "", "merge", filepath.Join(h.desk, "desk.db"))
	hb.fails(hb.as(sessC), "", "add --takeover", "join", "r2/coord")

	h.ok(a, "", "leave", "r2/coord")
	hb.ok(nil, "", "merge", filepath.Join(h.desk, "desk.db"))
	// The other desk sees the node is free again.
	hb.ok(hb.as(sessC), "", "join", "r2/coord")
	h.ok(nil, "", "merge", filepath.Join(hb.desk, "desk.db"))
	if got := h.ok(a, "", "whoami"); got != "example-proj/r1/coord" {
		t.Fatalf("whoami after merging back: %q", got)
	}
	if n := len(h.eventsAt("leave", "example-proj/r2/coord")); n != 1 {
		t.Fatalf("leave events after the round trip: %d", n)
	}
}
