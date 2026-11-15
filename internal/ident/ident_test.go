package ident

import "testing"

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
