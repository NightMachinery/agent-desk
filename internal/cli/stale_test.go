package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// staleOf is the stale? reason task list gives the task id.
func staleOf(t *testing.T, h *harness, id string) string {
	t.Helper()
	for _, line := range strings.Split(h.ok(nil, "", "task", "list", "--format", "json"), "\n") {
		var x struct {
			ID    string `json:"display_id"`
			Stale string `json:"stale"`
		}
		must(t, json.Unmarshal([]byte(line), &x))
		if x.ID == id {
			return x.Stale
		}
	}
	t.Fatalf("%s not listed", id)
	return ""
}

// A started task is marked stale? when nothing touched it for a day; a
// message about it, or a response to one, counts as a touch.
func TestStaleUntouched(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	id := h.ok(a, "", "task", "add", "--title", "Long one", "--brief", "b")
	open := h.ok(a, "", "task", "add", "--title", "Not started", "--brief", "b")
	h.ok(a, "", "task", "start", id)
	if s := staleOf(t, h, id); s != "" {
		t.Fatalf("fresh: %q", s)
	}
	h.now = h.now.Add(25 * time.Hour)
	if s := staleOf(t, h, id); !strings.HasPrefix(s, "untouched for ") {
		t.Fatalf("after a day: %q", s)
	}
	if s := staleOf(t, h, open); s != "" {
		t.Fatalf("an open task is never stale: %q", s)
	}
	if out := h.ok(nil, "", "task", "list"); !strings.Contains(out, "stale? untouched for") {
		t.Fatalf("task list:\n%s", out)
	}
	h.ok(a, "The loader is fixed.", "tell", "--re", id, "--title", "Halfway")
	if s := staleOf(t, h, id); s != "" {
		t.Fatalf("after a response on it: %q", s)
	}
}

// A started task is marked stale? once the agent that started it no longer
// holds the task's node: another agent took it over.
func TestStaleTakenOver(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	id := h.ok(a, "", "task", "add", "--title", "Mine", "--brief", "b")
	h.ok(a, "", "task", "start", id)
	b := h.as(sessB)
	h.ok(b, "", "join", "r1/coord", "--takeover")
	if s := staleOf(t, h, id); s != "its agent no longer holds this node" {
		t.Fatalf("after a takeover: %q", s)
	}
	h.ok(b, "", "task", "start", id)
	if s := staleOf(t, h, id); s != "" {
		t.Fatalf("started again by the new holder: %q", s)
	}
}

// A started task whose Paseo agent is archived is marked stale?.
func TestStaleArchivedAgent(t *testing.T) {
	h := newHarness(t)
	const uuid = "11111111-0000-4000-8000-000000000009"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Coordinator")
	a := h.as(sessA)
	a["PASEO_AGENT_ID"] = uuid
	a["PASEO_HOME"] = h.paseo
	h.ok(a, "", "join", "r1/coord")
	id := h.ok(a, "", "task", "add", "--title", "Paseo's", "--brief", "b")
	h.ok(a, "", "task", "start", id)
	expireLive()
	if s := staleOf(t, h, id); s != "" {
		t.Fatalf("live agent: %q", s)
	}
	f := filepath.Join(h.paseo, "agents", "Users-example-code", uuid+".json")
	b, err := os.ReadFile(f)
	must(t, err)
	var rec map[string]any
	must(t, json.Unmarshal(b, &rec))
	rec["archivedAt"] = "2026-11-28T10:00:00Z"
	b, _ = json.Marshal(rec)
	must(t, os.WriteFile(f, b, 0o644))
	expireLive()
	if s := staleOf(t, h, id); s != "its agent is archived" {
		t.Fatalf("archived agent: %q", s)
	}
}
