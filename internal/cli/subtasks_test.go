package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// A task's page lists its steps (task add --parent), finished ones too.
func TestItemSubtasks(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	parent := h.ok(a, "", "task", "add", "--title", "Ship the report", "--brief", "Two parts.")
	s1 := h.ok(a, "", "task", "add", "--parent", parent, "--title", "Draft it")
	s2 := h.ok(a, "", "task", "add", "--parent", parent, "--title", "Proofread it")
	h.ok(a, "", "task", "done", s1, "--note", "drafted")
	h.ok(a, "", "task", "add", "--title", "Unrelated", "--brief", "b")

	_, hd := testServer(t, h)
	_, raw, _ := do(t, hd, "GET", "/api/item?id="+parent, "", nil)
	var resp struct {
		Subtasks []struct {
			ID    string `json:"display_id"`
			State string `json:"state"`
		} `json:"subtasks"`
	}
	must(t, json.Unmarshal([]byte(raw), &resp))
	got := map[string]string{}
	for _, s := range resp.Subtasks {
		got[s.ID] = s.State
	}
	if len(got) != 2 || got[s1] != "done" || got[s2] != "open" {
		t.Fatalf("subtasks: %+v", resp.Subtasks)
	}
	_, raw, _ = do(t, hd, "GET", "/api/item?id="+s2, "", nil)
	resp.Subtasks = nil
	must(t, json.Unmarshal([]byte(raw), &resp))
	if len(resp.Subtasks) != 0 {
		t.Fatalf("a step has no steps: %+v", resp.Subtasks)
	}
}

// taskStates are the tasks on the desk by display id: state and parent.
func taskStates(t *testing.T, h *harness) map[string][2]string {
	t.Helper()
	out := map[string][2]string{}
	for _, line := range strings.Split(h.ok(nil, "", "task", "list", "--all", "--format", "json"), "\n") {
		var x struct {
			ID     string `json:"display_id"`
			State  string `json:"state"`
			Parent string `json:"parent_display"`
			Agent  string `json:"agent"`
			Event  struct {
				Title string `json:"title"`
			} `json:"event"`
		}
		must(t, json.Unmarshal([]byte(line), &x))
		out[x.ID] = [2]string{x.State, x.Parent}
		out[x.Event.Title] = [2]string{x.ID, x.Agent}
	}
	return out
}

// task add --steps files the task and a step under it per line of stdin,
// list marks taken off, and prints the task's id, then each step's.
func TestTaskAddSteps(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	out := h.ok(a, "- Draft it\n\n2. Proofread it\n[ ] Send it\n", "task", "add", "--title", "Ship the report", "--brief", "Three parts.", "--steps")
	lines := strings.Split(out, "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "w-") || !strings.HasSuffix(lines[1], " Draft it") || !strings.HasSuffix(lines[3], " Send it") {
		t.Fatalf("output:\n%s", out)
	}
	parent := lines[0]
	got := taskStates(t, h)
	for _, title := range []string{"Draft it", "Proofread it", "Send it"} {
		id := got[title][0]
		if got[id] != [2]string{"open", parent} || !strings.Contains(out, id+" "+title) {
			t.Fatalf("step %q: %v (output %q)", title, got[id], out)
		}
	}
	// Parked with the task when it is filed deferred.
	out = h.ok(a, "one\ntwo\n", "task", "add", "--defer", "--title", "Later work", "--brief", "Not now.", "--steps")
	if got := taskStates(t, h); got[got["two"][0]][0] != "later" {
		t.Fatalf("a deferred task's step: %v", got[got["two"][0]])
	}
	h.fails(a, "one\n", "--brief TEXT", "task", "add", "--title", "T", "--brief", "-", "--steps")
	h.fails(a, "one\n", "--brief TEXT", "task", "add", "--defer", "--title", "T", "--steps")
	h.fails(a, "one\n", "does not go with --quota", "task", "add", "--quota", "--title", "T", "--brief", "b", "--steps")
	h.fails(a, "\n  \n", "no steps", "task", "add", "--title", "T", "--brief", "b", "--steps")
}

// task done --start finishes a task and starts the next in one write.
func TestTaskDoneStart(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	one := h.ok(a, "", "task", "add", "--title", "One", "--brief", "b")
	two := h.ok(a, "", "task", "add", "--title", "Two", "--brief", "b")
	three := h.ok(a, "", "task", "add", "--title", "Three", "--brief", "b")
	h.ok(a, "", "task", "start", one)
	h.ok(a, "", "task", "done", one, "--note", "checked", "--start", two)
	got := taskStates(t, h)
	if got[one][0] != "done" || got[two][0] != "started" || got["Two"][1] == "" {
		t.Fatalf("after done --start: one %v, two %v (agent %q)", got[one], got[two], got["Two"][1])
	}
	h.fails(a, "", "goes with task done", "task", "start", three, "--start", two)
	h.fails(a, "", "which this command finishes", "task", "done", two, "--note", "n", "--start", two)
	h.fails(a, "", "reopen it first", "task", "done", two, "--note", "n", "--start", one)
	h.ok(a, "", "task", "drop", two, "--note", "superseded", "--start", three)
	if got := taskStates(t, h); got[two][0] != "dropped" || got[three][0] != "started" {
		t.Fatalf("after drop --start: two %v, three %v", got[two], got[three])
	}
}
