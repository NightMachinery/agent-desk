package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// paseoStatus writes a Paseo record with a last status ("archived" archives it).
func paseoStatus(t *testing.T, home, uuid, native, status string) {
	rec := map[string]any{"id": uuid, "labels": map[string]string{},
		"persistence": map[string]any{"nativeHandle": native}, "lastStatus": status}
	if status == "archived" {
		rec["lastStatus"], rec["archivedAt"] = "idle", "2026-01-01T00:00:00Z"
	}
	b, _ := json.Marshal(rec)
	dir := filepath.Join(home, "agents", "Users-example-code")
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, uuid+".json"), b, 0o644))
}

// states maps each node path under the runs to its state.
func states(h *harness) map[string]string {
	out := map[string]string{}
	var walk func(ns []any)
	walk = func(ns []any) {
		for _, x := range ns {
			n := x.(map[string]any)
			out[n["path"].(string)] = n["state"].(string)
			if c, ok := n["children"].([]any); ok {
				walk(c)
			}
		}
	}
	walk(h.showJSON("", true)["runs"].([]any))
	return out
}

// A node is active while its Paseo agent runs, or for an hour after its last
// event when nothing says otherwise.
func TestNodeState(t *testing.T) {
	h := newHarness(t)
	const pa = "22222222-0000-4000-8000-00000000000a"
	const pb = "22222222-0000-4000-8000-00000000000b"
	paseoStatus(t, h.paseo, pa, sessA, "running")
	paseoStatus(t, h.paseo, pb, sessB, "idle")
	envA := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": pa}
	envB := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessB, "PASEO_AGENT_ID": pb}
	h.ok(envA, "", "join", "r1/a")
	h.ok(envB, "", "join", "r1/b")
	h.ok(h.as(sessC), "", "join", "r2/c")

	check := func(want map[string]string) {
		t.Helper()
		liveCache.Lock()
		liveCache.m = nil // read the records just written
		liveCache.Unlock()
		got := states(h)
		for p, w := range want {
			if got[p] != w {
				t.Errorf("%s: state %q, want %q (all: %v)", p, got[p], w, got)
			}
		}
	}
	// Just now: the running agent and the plain session are active; the idle
	// agent is quiet however recent its events.
	check(map[string]string{"example-proj/r1/a": "active", "example-proj/r1/b": "quiet",
		"example-proj/r1": "active", "example-proj/r2/c": "active"})
	// The tree names each node's Paseo agent, for the dashboard's copy button.
	for _, r := range h.showJSON("", true)["runs"].([]any) {
		for _, x := range r.(map[string]any)["children"].([]any) {
			for _, a := range x.(map[string]any)["children"].([]any) {
				n := a.(map[string]any)
				want := map[string]string{"example-proj/r1/a": pa, "example-proj/r1/b": pb}[n["path"].(string)]
				if got, _ := n["paseo"].(string); got != want {
					t.Errorf("%s: paseo %q, want %q", n["path"], got, want)
				}
			}
		}
	}

	// Two hours on: time alone makes the plain session quiet; the running
	// agent stays active.
	h.now = h.now.Add(2 * time.Hour)
	check(map[string]string{"example-proj/r1/a": "active", "example-proj/r1": "active",
		"example-proj/r2/c": "quiet", "example-proj/r2": "quiet"})

	// A done report from an agent that keeps running: still active.
	h.ok(envA, "Milestone one.", "done", "--title", "Phase 1 finished")
	check(map[string]string{"example-proj/r1/a": "active", "example-proj/r1": "active"})

	// Idle and archived agents: quiet and finished; the run is quiet.
	paseoStatus(t, h.paseo, pa, sessA, "idle")
	paseoStatus(t, h.paseo, pb, sessB, "archived")
	check(map[string]string{"example-proj/r1/a": "finished", "example-proj/r1/b": "finished"})
}
