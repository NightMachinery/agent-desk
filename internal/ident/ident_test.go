package ident

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentPrecedence(t *testing.T) {
	env := func(m map[string]string) Env {
		return Env{Getenv: func(k string) string { return m[k] }, Home: t.TempDir(), Cwd: t.TempDir()}
	}
	// A Codex session started from a Claude shell inherits Claude's id;
	// AI_AGENT, set by the innermost agent, decides.
	f := Resolve(env(map[string]string{
		"AI_AGENT": "codex_0", "CODEX_THREAD_ID": "thread-1", "CLAUDE_CODE_SESSION_ID": "claude-1",
	}))
	if f.Agent != "codex" || f.Session != "thread-1" {
		t.Fatalf("got %+v", f)
	}
	f = Resolve(env(map[string]string{"CODEX_SESSION_ID": "old-1"}))
	if f.Agent != "codex" || f.Session != "old-1" {
		t.Fatalf("older codex variable: %+v", f)
	}
	f = Resolve(env(map[string]string{"AI_AGENT": "agy", "ANTIGRAVITY_CONVERSATION_ID": "conv-1"}))
	if f.Agent != "agy" || f.Session != "conv-1" {
		t.Fatalf("antigravity: %+v", f)
	}
	f = Resolve(env(map[string]string{}))
	if f.Agent != "" || f.Session != "" {
		t.Fatalf("no agent: %+v", f)
	}
}

// The model and effort come from the Paseo record (what Paseo saw run, then
// what it was started with), and what it lacks from the transcript.
func TestModelAndEffort(t *testing.T) {
	home := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vars := map[string]string{}
	env := Env{Getenv: func(k string) string { return vars[k] }, Home: home, Cwd: home}

	// Claude in Paseo: the record says model and effort.
	write(filepath.Join(home, ".paseo", "agents", "proj", "p1.json"),
		`{"id":"p1","persistence":{"nativeHandle":"s1"},"config":{"model":"claude-opus-5-5","thinkingOptionId":"xhigh"},"runtimeInfo":{"model":"claude-fable-5-2"}}`)
	vars["CLAUDE_CODE_SESSION_ID"], vars["PASEO_AGENT_ID"] = "s1", "p1"
	if f := Resolve(env); f.Model != "claude-fable-5-2" || f.Effort != "xhigh" {
		t.Fatalf("paseo: %q %q", f.Model, f.Effort)
	}

	// Claude outside Paseo: the latest assistant message, skipping a
	// synthetic one for the model.
	delete(vars, "PASEO_AGENT_ID")
	vars["CLAUDE_CODE_SESSION_ID"] = "s2"
	write(filepath.Join(home, ".claude", "projects", "proj", "s2.jsonl"),
		`{"type":"assistant","effort":"low","message":{"model":"claude-haiku-5-5"}}`+"\n"+
			`{"type":"assistant","effort":"high","message":{"model":"claude-opus-5-5"}}`+"\n"+
			`{"type":"user","message":{"content":"hi"}}`+"\n"+
			`{"type":"assistant","effort":"high","message":{"model":"<synthetic>"}}`+"\n")
	if f := Resolve(env); f.Model != "claude-opus-5-5" || f.Effort != "high" {
		t.Fatalf("claude transcript: %q %q", f.Model, f.Effort)
	}

	// Codex: the latest turn_context.
	vars = map[string]string{"AI_AGENT": "codex", "CODEX_THREAD_ID": "th1"}
	write(filepath.Join(home, ".codex", "sessions", "2026", "11", "26", "rollout-x-th1.jsonl"),
		`{"type":"turn_context","payload":{"model":"gpt-6.1-terra","effort":"medium"}}`+"\n"+
			`{"type":"turn_context","payload":{"model":"gpt-6.2-sol","effort":"xhigh"}}`+"\n"+
			`{"type":"response_item","payload":{"type":"message"}}`+"\n")
	if f := Resolve(env); f.Model != "gpt-6.2-sol" || f.Effort != "xhigh" {
		t.Fatalf("codex transcript: %q %q", f.Model, f.Effort)
	}
}
