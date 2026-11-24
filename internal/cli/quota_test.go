package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/keeper"
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

	// Replace: take them all over in one step, then use --node.
	out := h.ok(env, "", "quota", "replace", stopped, "--dry-run")
	for _, s := range []string{
		"It held the agent-desk nodes example-proj/r1/eval, example-proj/r2/eval",
		"agent-desk takeover " + stopped, "--node PATH",
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
		!strings.Contains(out, "agent-desk takeover "+stopped) || !strings.Contains(out, "--node PATH") {
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
	if !strings.Contains(out, "It held the agent-desk node example-proj/r2/eval. Take it over in one step: `agent-desk takeover "+stopped+"`") ||
		strings.Contains(out, "--node PATH") {
		t.Fatalf("replace prompt with one node:\n%s", out)
	}
	// takeover by Paseo id takes what it still holds (r2/eval) in one step.
	repl := h.as(sessC)
	repl["PASEO_HOME"] = h.paseo
	if out := h.ok(repl, "", "takeover", stopped); !strings.Contains(out, "took over example-proj/r2/eval") ||
		strings.Contains(out, "r1/eval") {
		t.Fatalf("takeover: %s", out)
	}
	must(t, json.Unmarshal([]byte(h.ok(env, "", "quota", "list", "--format", "json")), &qs))
	if len(qs[0].Nodes) != 0 || strings.Join(qs[0].Scopes, ",") != "example-proj/r0/coord" {
		t.Fatalf("no nodes left: %+v", qs[0])
	}
	if out := h.ok(env, "", "quota", "replace", stopped, "--dry-run"); strings.Contains(out, "agent-desk takeover") {
		t.Fatalf("replace prompt with no node:\n%s", out)
	}
	// Nothing left to take; yourself is refused; a session id works too.
	h.fails(repl, "", "holds no desk node", "takeover", stopped)
	h.fails(worker, "", "is this session", "takeover", sessB)
	if out := h.ok(worker, "", "takeover", sessC); !strings.Contains(out, "took over example-proj/r1/eval") ||
		!strings.Contains(out, "took over example-proj/r2/eval") || !strings.Contains(out, "--node PATH") {
		t.Fatalf("takeover by session id: %s", out)
	}
}

// Each stopped agent shows its account and when that has quota again, from
// the keeper, read in the background so the request never waits for it.
func TestQuotaSeat(t *testing.T) {
	h := newHarness(t)
	const blocked = "33333333-0000-4000-8000-0000000000b1"
	const usable = "33333333-0000-4000-8000-0000000000u1"
	const gemini = "33333333-0000-4000-8000-0000000000g1"
	writePaseoRecord(t, h.paseo, blocked, sessA, "", "Eval worker")
	writePaseoRecord(t, h.paseo, usable, sessB, "", "Plot helper")
	writePaseoRecord(t, h.paseo, gemini, sessC, "", "Gemini worker")
	reset, pool := 1795050000.0, 1795049000.0
	fakeKeeper(t, h, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/agents":
			if r.URL.Query().Get("all") != "1" {
				t.Errorf("seats read without all=1: %s", r.URL)
			}
			writeJSON(w, keeper.List{Enabled: true, Agents: []keeper.Agent{
				{ID: blocked, Seat: &keeper.Seat{Account: "claude:work", Verdict: "blocked"}},
				{ID: usable, Seat: &keeper.Seat{Account: "claude:default", Verdict: "usable"}},
				// An agy seat is blocked for the agent's model while the
				// account as a whole still reads usable.
				{ID: gemini, Seat: &keeper.Seat{Account: "agy:m", Verdict: "blocked", AvailableAt: &pool}},
			}})
		case "/v1/accounts":
			writeJSON(w, map[string]any{"accounts": []keeper.Account{
				{ID: "claude:work", Verdict: "blocked", BlockedUntil: &reset},
				{ID: "claude:default", Verdict: "usable"},
				{ID: "agy:m", Verdict: "usable"},
			}})
		default:
			t.Errorf("unexpected keeper request: %s", r.URL)
		}
	})
	cfg, err := os.ReadFile(filepath.Join(h.home, ".config", "agent-desk", "config.toml"))
	must(t, err)
	writeConfig(t, h, string(cfg)+"[quota]\nstopped_command = 'printf STOPPED'\n")
	h.respond = func(name string, args []string) (string, error) {
		if name == "sh" && args[1] == "printf STOPPED" {
			return blocked + "\n" + usable + "\n" + gemini + "\n", nil
		}
		return "{}", nil
	}
	s, hd := testServer(t, h)
	s.refreshSeats()
	_, body, _ := do(t, hd, "GET", "/api/quota", "", nil)
	var resp struct {
		Agents []QuotaAgent `json:"agents"`
	}
	must(t, json.Unmarshal([]byte(body), &resp))
	got := map[string]*QuotaSeat{}
	for _, a := range resp.Agents {
		got[a.Paseo] = a.Seat
	}
	if b := got[blocked]; b == nil || b.Verdict != "blocked" || b.AvailableAt == nil || *b.AvailableAt != reset {
		t.Fatalf("blocked seat: %+v (%s)", b, body)
	}
	if u := got[usable]; u == nil || u.Verdict != "usable" || u.AvailableAt != nil {
		t.Fatalf("usable seat: %+v", u)
	}
	if g := got[gemini]; g == nil || g.Verdict != "blocked" || g.AvailableAt == nil || *g.AvailableAt != pool {
		t.Fatalf("agy seat: %+v", g)
	}
}

// Replace offers every enabled, available Paseo provider with its models
// and the quota of the account it runs on, learnt from the agents on it.
func TestProviderChoices(t *testing.T) {
	h := newHarness(t)
	rec := func(id, provider string) {
		b, _ := json.Marshal(map[string]any{"id": id, "title": id, "provider": provider, "labels": map[string]string{},
			"config": map[string]string{"model": "m", "thinkingOptionId": "high"}})
		dir := filepath.Join(h.paseo, "agents", "Users-example-code")
		must(t, os.MkdirAll(dir, 0o755))
		must(t, os.WriteFile(filepath.Join(dir, id+".json"), b, 0o644))
	}
	rec("a1", "claude")
	rec("a2", "work-claude")
	rec("a3", "work-claude")
	rec("a4", "codex")
	reset := 1795050000.0
	fakeKeeper(t, h, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/agents":
			writeJSON(w, keeper.List{Enabled: true, Agents: []keeper.Agent{
				{ID: "a1", Seat: &keeper.Seat{Account: "claude:default"}},
				{ID: "a2", Seat: &keeper.Seat{Account: "claude:work"}},
				{ID: "a3", Seat: &keeper.Seat{Account: "claude:work"}},
			}})
		case "/v1/accounts":
			writeJSON(w, map[string]any{"accounts": []keeper.Account{
				{ID: "claude:default", Label: "default", Verdict: "blocked", BlockedUntil: &reset, BlockedReasons: []string{"weekly at 100%"}},
				{ID: "claude:work", Label: "work", Verdict: "usable"},
			}})
		}
	})
	h.respond = func(name string, args []string) (string, error) {
		if name != "paseo" {
			return "{}", nil
		}
		switch strings.Join(args, " ") {
		case "provider ls --json":
			return `[{"provider":"claude","label":"Claude","status":"available","enabled":"Enabled"},
				{"provider":"codex","label":"Codex","status":"unavailable","enabled":"Disabled"},
				{"provider":"work-claude","label":"Claude (Work)","status":"available","enabled":"Enabled"},
				{"provider":"broken","label":"Broken","status":"available","enabled":"Enabled"}]`, nil
		case "provider models claude --json", "provider models work-claude --json":
			return `[{"model":"Opus","id":"opus","description":"d","thinkingOptionIds":["low","high"],"defaultThinkingOptionId":"high"},
				{"model":"Haiku","id":"haiku","thinkingOptionIds":null}]`, nil
		}
		return "", os.ErrNotExist
	}
	_, hd := testServer(t, h)
	code, body, _ := do(t, hd, "GET", "/api/providers", "", nil)
	if code != 200 {
		t.Fatalf("api/providers: %d %s", code, body)
	}
	var resp struct {
		Providers []ProviderChoice `json:"providers"`
	}
	must(t, json.Unmarshal([]byte(body), &resp))
	ps := resp.Providers
	if len(ps) != 3 || ps[0].ID != "claude" || ps[1].ID != "work-claude" || ps[2].ID != "broken" {
		t.Fatalf("providers: %s", body)
	}
	if c := ps[0]; c.Account != "claude:default" || c.Verdict != "blocked" || c.AvailableAt == nil || len(c.Reasons) != 1 ||
		len(c.Models) != 2 || c.Models[0].DefaultThinking != "high" || len(c.Models[1].Thinking) != 0 || c.Models[1].Thinking == nil {
		t.Fatalf("claude: %+v", c)
	}
	if w := ps[1]; w.Account != "claude:work" || w.AccountLabel != "work" || w.Verdict != "usable" || w.AvailableAt != nil {
		t.Fatalf("work-claude: %+v", w)
	}
	if b := ps[2]; len(b.Models) != 0 || b.Account != "" {
		t.Fatalf("a provider whose models fail is still offered, without models: %+v", b)
	}
}

// Archive from the dashboard passes paseo only an agent this machine has a
// record of, and says so when it is archived already.
func TestArchiveAgent(t *testing.T) {
	h := newHarness(t)
	const id = "11111111-0000-4000-8000-0000000000aa"
	writePaseoRecord(t, h.paseo, id, sessA, "", "Old coordinator")
	a, _, _ := h.app(map[string]string{}, "")
	var calls [][]string
	a.Exec = func(_ context.Context, _ []byte, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "", nil
	}
	note, err := a.archiveAgent(id)
	must(t, err)
	if note != "Archived Old coordinator." || len(calls) != 1 || strings.Join(calls[0], " ") != "paseo archive "+id {
		t.Fatalf("note %q calls %v", note, calls)
	}
	for _, bad := range []string{"", "nope", "../x", "*"} {
		if _, err := a.archiveAgent(bad); err == nil {
			t.Fatalf("archived %q", bad)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("paseo ran for an unknown id: %v", calls)
	}
}
