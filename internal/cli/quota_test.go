package cli

import (
	"strings"
	"testing"
)

// Out of quota: list the stopped agents, toggle auto-continue, ask a parent
// to reassign the role, and start a replacement.
func TestQuota(t *testing.T) {
	h := newHarness(t)
	const parent = "33333333-0000-4000-8000-00000000000p"
	const stopped = "33333333-0000-4000-8000-00000000000s"
	const child = "33333333-0000-4000-8000-00000000000c"
	writePaseoRecord(t, h.paseo, parent, sessA, "", "Rerun coordinator")
	writePaseoRecord(t, h.paseo, stopped, sessB, parent, "Eval worker")
	writePaseoRecord(t, h.paseo, child, sessC, stopped, "Plot helper")
	writeConfig(t, h, "[quota]\nstopped_command = 'printf STOPPED'\nauto_continue_command = 'printf AUTO'\n[tasks]\nlaunch_command = 'printf LAUNCH'\n")
	h.respond = func(name string, args []string) (string, error) {
		switch {
		case name == "sh" && args[1] == "printf STOPPED":
			return stopped + "\n44444444-gone\n", nil
		case name == "sh" && args[1] == "printf AUTO" && args[3] == "list":
			return stopped + "\t~/.paseo\tclaude/default\twatching\n", nil
		case name == "sh" && args[1] == "printf LAUNCH":
			return "55555555-new\n", nil
		}
		return "{}", nil
	}
	env := map[string]string{"PASEO_HOME": h.paseo}
	called := func(sub string) bool {
		for _, c := range *h.calls {
			if strings.Contains(c, sub) {
				return true
			}
		}
		return false
	}

	// The parent joined a desk node: its stopped subagent is shown there.
	h.ok(map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": parent}, "", "join", "r1/coord")

	out := h.ok(env, "", "quota", "list")
	if !strings.Contains(out, stopped+"  Eval worker") || !strings.Contains(out, "parent Rerun coordinator") ||
		!strings.Contains(out, "1 subagents") || !strings.Contains(out, "auto-continue on") || strings.Contains(out, "gone") {
		t.Fatalf("list: %s", out)
	}

	out = h.ok(env, "", "quota", "ask-parent", stopped, "--dry-run")
	if !strings.Contains(out, "Your subagent Eval worker ("+stopped+")") || !strings.Contains(out, "Plot helper ("+child+")") {
		t.Fatalf("ask-parent prompt:\n%s", out)
	}
	h.ok(env, "", "quota", "ask-parent", stopped)
	if !called("paseo send --no-wait --json " + parent + " Your subagent Eval worker") {
		t.Fatalf("send: %v", *h.calls)
	}
	h.fails(env, "", "not an agent stopped", "quota", "ask-parent", parent)
	out = h.ok(env, "", "quota", "replace", stopped, "--profile", "claude/opus@high")
	if !strings.Contains(out, "Started 55555555-new to take over") ||
		!called(`"profile":"claude/opus@high"`) || !called("paseo archive "+stopped) {
		t.Fatalf("replace: %s\n%v", out, *h.calls)
	}

	_, hd := testServer(t, h)
	if _, body, _ := do(t, hd, "GET", "/api/quota", "", nil); !strings.Contains(body, `"paseo":"`+stopped+`"`) || !strings.Contains(body, `"auto_continue":true`) {
		t.Fatalf("api: %s", body)
	}
	if code, body, _ := do(t, hd, "POST", "/api/quota", `{"action":"auto-continue-off","id":"`+stopped+`"}`, dashboard); code != 200 || !called("sh -c printf AUTO -- off "+stopped) {
		t.Fatalf("api auto-continue: %d %s", code, body)
	}
	if _, body, _ := do(t, hd, "GET", "/api/quota?fresh=1", "", nil); !strings.Contains(body, `"scope":"example-proj/r1/coord"`) {
		t.Fatalf("scope: %s", body)
	}
	if _, body, _ := do(t, hd, "GET", "/api/auto-continue?paseo="+stopped, "", nil); body != `{"available":true,"on":true}`+"\n" {
		t.Fatalf("api auto-continue state: %q", body)
	}

	// Dismissed: hidden until it runs again; undismiss shows it.
	h.ok(env, "", "quota", "dismiss", stopped)
	if out := h.ok(env, "", "quota", "list"); !strings.Contains(out, "dismissed") {
		t.Fatalf("dismissed: %s", out)
	}
	if code, body, _ := do(t, hd, "POST", "/api/quota", `{"action":"undismiss","id":"`+stopped+`"}`, dashboard); code != 200 {
		t.Fatalf("api undismiss: %d %s", code, body)
	}
	if _, body, _ := do(t, hd, "GET", "/api/quota?fresh=1", "", nil); strings.Contains(body, `"dismissed":true`) {
		t.Fatalf("undismissed still hidden: %s", body)
	}
	h.fails(env, "", "not an agent stopped", "quota", "dismiss", parent)
}
