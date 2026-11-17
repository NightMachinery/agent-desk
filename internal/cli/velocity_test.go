package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A prepared message: reply --prompt velocity-report, from the CLI and the API.
func TestReplyPrompt(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	vel := h.ok(a, "3 of 5 seeds; a day behind.", "set", "velocity")
	h.respond = func(name string, args []string) (string, error) { return `{"disposition":"steered"}`, nil }

	h.ok(nil, "", "reply", vel, "--prompt", "velocity-report")
	out := h.ok(nil, "", "search", "executive report")
	if !strings.Contains(out, "example-proj/r1/coord") {
		t.Fatalf("search: %s", out)
	}
	h.fails(nil, "", "no prepared message", "reply", vel, "--prompt", "nope")
	h.fails(nil, "", "not both", "reply", vel, "--prompt", "velocity-report", "and text")

	// The user's own version replaces the built-in one.
	dir := filepath.Join(h.home, ".config", "agent-desk", "prompts")
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "velocity-report.md"), []byte("How fast, {{.Node}}? Now: {{.Velocity}}"), 0o644))
	_, hd := testServer(t, h)
	code, body, _ := do(t, hd, "POST", "/api/reply", `{"to":"example-proj/r1/coord","prompt":"velocity-report"}`, dashboard)
	if code != 200 {
		t.Fatalf("api: %d %s", code, body)
	}
	if out := h.ok(nil, "", "search", "How fast"); !strings.Contains(out, "a day behind") {
		t.Fatalf("override: %s", out)
	}
	if code, body, _ = do(t, hd, "POST", "/api/reply", `{"to":"example-proj/r1/coord","prompt":"velocity-report","text":"x"}`, dashboard); code != 400 {
		t.Fatalf("both: %d %s", code, body)
	}
}
