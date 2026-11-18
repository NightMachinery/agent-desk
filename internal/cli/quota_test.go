package cli

import (
	"encoding/json"
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
	if _, body, _ := do(t, hd, "GET", "/api/quota?fresh=1", "", nil); !strings.Contains(body, `"scopes":["example-proj/r1/coord"]`) {
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

// An agent out of quota that holds several desk nodes: all are listed, it
// shows under each, and both prompts name each node to take over.
func TestQuotaSeveralNodes(t *testing.T) {
	h := newHarness(t)
	const parent = "33333333-0000-4000-8000-00000000000p"
	const stopped = "33333333-0000-4000-8000-00000000000s"
	writePaseoRecord(t, h.paseo, parent, sessA, "", "Rerun coordinator")
	writePaseoRecord(t, h.paseo, stopped, sessB, parent, "Two-run worker")
	writeConfig(t, h, "[quota]\nstopped_command = 'printf STOPPED'\n[tasks]\nlaunch_command = 'printf LAUNCH'\n")
	h.respond = func(name string, args []string) (string, error) {
		switch {
		case name == "sh" && args[1] == "printf STOPPED":
			return stopped + "\n", nil
		case name == "sh" && args[1] == "printf LAUNCH":
			return "55555555-new\n", nil
		}
		return "{}", nil
	}
	env := map[string]string{"PASEO_HOME": h.paseo}
	worker := h.as(sessB)
	worker["PASEO_AGENT_ID"] = stopped
	h.ok(map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": parent}, "", "join", "r0/coord")
	h.ok(worker, "", "join", "r1/eval")
	h.ok(worker, "", "join", "r2/eval")
	h.ok(worker, "", "join", "r3/eval")
	h.ok(worker, "", "leave", "r3/eval") // left: no longer its node

	var qs []QuotaAgent
	must(t, json.Unmarshal([]byte(h.ok(env, "", "quota", "list", "--format", "json")), &qs))
	want := "example-proj/r1/eval,example-proj/r2/eval"
	if len(qs) != 1 || strings.Join(qs[0].Nodes, ",") != want || strings.Join(qs[0].Scopes, ",") != want {
		t.Fatalf("nodes: %+v", qs)
	}
	if out := h.ok(env, "", "quota", "list"); !strings.Contains(out, "nodes example-proj/r1/eval example-proj/r2/eval") {
		t.Fatalf("list: %s", out)
	}

	// Replace: take over each with --takeover, leave none, use --node.
	out := h.ok(env, "", "quota", "replace", stopped, "--dry-run")
	for _, s := range []string{
		"It held 2 agent-desk nodes",
		"agent-desk join r1/eval --project example-proj --takeover",
		"agent-desk join r2/eval --project example-proj --takeover",
		"Leave none of them", "--node PATH",
	} {
		if !strings.Contains(out, s) {
			t.Fatalf("replace prompt lacks %q:\n%s", s, out)
		}
	}
	if strings.Contains(out, "r3/eval") || strings.Contains(out, "r0/coord") {
		t.Fatalf("replace prompt names a node it does not hold:\n%s", out)
	}
	// Ask parent: names both.
	out = h.ok(env, "", "quota", "ask-parent", stopped, "--dry-run")
	if !strings.Contains(out, "nodes example-proj/r1/eval, example-proj/r2/eval") ||
		!strings.Contains(out, "agent-desk join r2/eval --project example-proj --takeover") || !strings.Contains(out, "--node PATH") {
		t.Fatalf("ask-parent prompt:\n%s", out)
	}

	// Under a focus, it is listed when either node is inside it.
	_, hd := testServer(t, h)
	h.ok(nil, "", "sets", "define", "R2", "example-proj/r2")
	h.ok(nil, "", "sets", "define", "R0", "example-proj/r0")
	h.ok(nil, "", "focus", "R2")
	if _, body, _ := do(t, hd, "GET", "/api/quota?fresh=1", "", nil); !strings.Contains(body, `"paseo":"`+stopped+`"`) ||
		!strings.Contains(body, `"nodes":["example-proj/r1/eval","example-proj/r2/eval"]`) {
		t.Fatalf("focus on its second node: %s", body)
	}
	h.ok(nil, "", "focus", "R0") // its parent's node only: it has nodes of its own
	if _, body, _ := do(t, hd, "GET", "/api/quota?fresh=1", "", nil); strings.Contains(body, stopped) {
		t.Fatalf("focus elsewhere: %s", body)
	}

	// Another session takes one over: that node is no longer the stopped
	// agent's. Once it holds none, it is scoped by its parent's node.
	h.ok(h.as(sessC), "", "join", "r1/eval", "--takeover")
	must(t, json.Unmarshal([]byte(h.ok(env, "", "quota", "list", "--format", "json")), &qs))
	if strings.Join(qs[0].Nodes, ",") != "example-proj/r2/eval" {
		t.Fatalf("after a takeover: %+v", qs[0].Nodes)
	}
	out = h.ok(env, "", "quota", "replace", stopped, "--dry-run")
	if !strings.Contains(out, "It kept the agent-desk node example-proj/r2/eval. Take it over: `agent-desk join r2/eval --project example-proj --takeover`") ||
		strings.Contains(out, "--node PATH") {
		t.Fatalf("replace prompt with one node:\n%s", out)
	}
	h.ok(h.as(sessC), "", "join", "r2/eval", "--takeover")
	must(t, json.Unmarshal([]byte(h.ok(env, "", "quota", "list", "--format", "json")), &qs))
	if len(qs[0].Nodes) != 0 || strings.Join(qs[0].Scopes, ",") != "example-proj/r0/coord" {
		t.Fatalf("no nodes left: %+v", qs[0])
	}
	if out := h.ok(env, "", "quota", "replace", stopped, "--dry-run"); strings.Contains(out, "agent-desk join") {
		t.Fatalf("replace prompt with no node:\n%s", out)
	}
}
