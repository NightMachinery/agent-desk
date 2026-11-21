package cli

import (
	"encoding/json"
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
