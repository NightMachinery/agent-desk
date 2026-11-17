package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, h *harness, body string) {
	cfg := filepath.Join(h.home, ".config", "agent-desk", "config.toml")
	must(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
	must(t, os.WriteFile(cfg, []byte(body), 0o644))
}

func TestTasks(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")

	// An agent's own list.
	w1 := h.ok(a, "", "task", "add", "--title", "Rerun seed 4")
	w2 := h.ok(a, "never read", "task", "add", "--title", "Write the table", "--priority", "1", "--brief", "Seeds 1-5, one row each.")
	h.ok(a, "", "task", "start", w1)
	h.ok(a, "", "task", "done", w2, "--note", "in results.md")
	if p := h.ok(nil, "", "task", "prompt", w2); !strings.Contains(p, "Seeds 1-5, one row each.") {
		t.Fatalf("inline brief: %s", p)
	}
	out := h.ok(nil, "", "task", "list")
	if !strings.Contains(out, w1+"  started  Rerun seed 4") || strings.Contains(out, "Write the table") {
		t.Fatalf("list:\n%s", out)
	}
	if out := h.ok(nil, "", "task", "list", "--all"); !strings.Contains(out, "done") {
		t.Fatalf("list --all:\n%s", out)
	}

	// Deferred tasks wait for quota.
	h.fails(a, "", "needs a brief", "task", "add", "--defer", "--title", "x")
	h.fails(a, "b", "--size is S, M or L", "task", "add", "--defer", "--title", "x", "--size", "XL")
	d1 := h.ok(a, "Profile the loader.", "task", "add", "--defer", "--title", "Profile", "--size", "M", "--priority", "3")
	d2 := h.ok(a, "Port the plots to the new style.", "task", "add", "--defer", "--title", "Plots", "--profile", "codex-high", "--priority", "1")
	d3 := h.ok(nil, "Tidy the README.", "task", "add", "--defer", "--title", "README", "--node", "example-proj/r1/coord")
	if out := h.ok(nil, "", "task", "launch", "--deferred", "--dry-run", "--max", "2"); out != d2+"  Plots\n"+d3+"  README" {
		t.Fatalf("pick order:\n%s", out)
	}
	prompt := h.ok(nil, "", "task", "prompt", d2)
	if !strings.Contains(prompt, "Port the plots to the new style.") ||
		!strings.Contains(prompt, "agent-desk join r1/task-"+strings.TrimPrefix(d2, "w-")+" --project example-proj") ||
		!strings.Contains(prompt, "agent-desk task done "+d2) {
		t.Fatalf("prompt:\n%s", prompt)
	}
	h.fails(nil, "", "no launch_command", "task", "launch", d1)

	writeConfig(t, h, "[tasks]\nlaunch_command = 'my-launcher'\nmax_launch = 2\n")
	n := 0
	h.respond = func(name string, args []string) (string, error) {
		if name == "sh" && args[1] == "my-launcher" {
			n++
			return fmt.Sprintf("a warning\nagent-%d", n), nil
		}
		return "", fmt.Errorf("unexpected %s", name)
	}
	h.fails(nil, "", "max_launch is 2", "task", "launch", d1, d2, d3)
	out = h.ok(nil, "", "task", "launch", "--deferred")
	if !strings.Contains(out, d2+"  launched  agent-1") || !strings.Contains(out, d3+"  launched  agent-2") {
		t.Fatalf("launch:\n%s", out)
	}
	if !strings.Contains((*h.calls)[0], `"prompt":"This is a task`) || !strings.Contains((*h.calls)[0], `"profile":"codex-high"`) {
		t.Fatalf("payload: %s", (*h.calls)[0])
	}
	out = h.ok(nil, "", "task", "list", "--deferred")
	if out != d1+"  deferred Profile  [M p3]  example-proj/r1/coord" {
		t.Fatalf("after launch:\n%s", out)
	}
	h.fails(nil, "", "is started", "task", "launch", d2)

	// The dashboard: deferred tasks in the view, and actions.
	_, hd := testServer(t, h)
	_, body, _ := do(t, hd, "GET", "/api/view?path=example-proj", "", nil)
	if !strings.Contains(body, `"deferred":[{`) || !strings.Contains(body, `"launch":{"ready":true,"max":2}`) ||
		!strings.Contains(body, `"tasks":[`) {
		t.Fatalf("view: %s", body)
	}
	if code, body, _ := do(t, hd, "POST", "/api/task", `{"action":"drop","id":"`+d1+`"}`, dashboard); code != 200 {
		t.Fatalf("drop: %d %s", code, body)
	}
	if code, _, _ := do(t, hd, "POST", "/api/task", `{"action":"explode","id":"`+d1+`"}`, dashboard); code != 400 {
		t.Fatalf("bad action: %d", code)
	}
	if out := h.ok(nil, "", "task", "list", "--deferred"); out != "" {
		t.Fatalf("after drop: %q", out)
	}
	h.ok(nil, "", "task", "defer", d1)

	// The nudge: a weekly window about to reset with quota unused.
	reset := h.now.Add(10*3600e9 + 1800e9).Unix()
	writeConfig(t, h, "[tasks]\nquota_command = 'quota'\n")
	h.respond = func(name string, args []string) (string, error) {
		return fmt.Sprintf(`note: partial
{"gauges": {"claude.default.7d": {"pct": 35, "reset": %d}, "claude.default.5h": {"pct": 1, "reset": %d},
 "codex.a.7d": {"pct": 90, "reset": %d}}}`, reset, reset, reset), fmt.Errorf("exit status 1")
	}
	out = h.ok(nil, "", "task", "nudge")
	if !strings.HasPrefix(out, "t-") {
		t.Fatalf("nudge: %q", out)
	}
	show := h.ok(nil, "", "show", "--format", "md")
	if !strings.Contains(show, "Quota to spare: claude.default.7d resets in 10h at 35% used") || strings.Contains(show, "codex.a.7d") ||
		!strings.Contains(show, d1+" Profile") {
		t.Fatalf("show:\n%s", show)
	}
	if out := h.ok(nil, "", "task", "nudge"); out != "no nudge due" {
		t.Fatalf("second nudge: %q", out)
	}
}
