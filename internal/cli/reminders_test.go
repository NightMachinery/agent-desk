package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setReminders sets the shared "reminders" setting as the dashboard does.
func setReminders(t *testing.T, h *harness, data string) {
	t.Helper()
	_, hd := testServer(t, h)
	if code, body, _ := do(t, hd, "POST", "/api/dashboard-prefs", `{"key":"reminders","data":`+data+`}`, dashboard); code != 200 {
		t.Fatalf("set reminders: %d %s", code, body)
	}
}

func TestPromptOccasion(t *testing.T) {
	for prompt, want := range map[string]string{
		"Fix the loader, then rerun.":                                          OnUser,
		"/delegate stronger: design it":                                        OnUser,
		`Answer to q-7f3a "Which split?": B`:                                   OnDesk,
		"Message m-4c2d from the user via agent-desk, about t-k2m9 \"x\": hi":  OnDesk,
		`Schedule "heartbeat" fired (id=abc, run=1)` + "\nCheck the children.": OnHeartbeat,
		`<paseo-system kind="heartbeat">wake</paseo-system>`:                   OnHeartbeat,
		`<paseo-system kind="agent-message">from a parent</paseo-system>`:      OnAgent,
		"<agent-message from=\"x\">done</agent-message>":                       OnAgent,
		"[agent-quotas keepalive] Automatic cache keepalive, not the user.":    "",
		"/compact":  "",
		"/clear":    "",
		"Continue.": "",
		"<task-notification>done</task-notification>": "",
		"[Request interrupted by user]":               "",
		"   ":                                         "",
	} {
		if got := promptOccasion(prompt, nil); got != want {
			t.Errorf("%q: %q, want %q", prompt, got, want)
		}
	}
}

// The prompt hook reminds an agent with no started task at its node, on
// the occasions the setting names, and says nothing once a task is started.
func TestNoTaskReminder(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	none := map[string]string{}
	hook := func(prompt string) string {
		t.Helper()
		return h.ok(none, hookJSON(sessA, prompt), "hook", "prompt", "--agent", "claude")
	}
	if out := hook("before joining"); out != "" {
		t.Fatalf("a session off the desk got %q", out)
	}
	h.ok(a, "", "join", "r1/coord")
	open := h.ok(a, "", "task", "add", "--title", "Rerun seeds", "--brief", "b")
	for _, p := range []string{"Fix the loader.", `Answer to q-7f3a "Which split?": B`, `Schedule "hb" fired (id=a, run=1)` + "\nnext"} {
		out := hook(p)
		if !strings.Contains(out, "no started task at example-proj/r1/coord") || !strings.Contains(out, open+" Rerun seeds") || strings.Contains(out, "\n") {
			t.Fatalf("%q: reminder %q", p, out)
		}
	}
	for _, p := range []string{`<paseo-system kind="agent-message">hi</paseo-system>`, "[agent-quotas keepalive] ping", "/compact", "Continue."} {
		if out := hook(p); out != "" {
			t.Fatalf("%q reminded: %q", p, out)
		}
	}
	// Agent messages when asked for; nothing on what is turned off.
	setReminders(t, h, `{"no_task": {"on": true, "user": false, "desk": true, "heartbeat": true, "agent": true}, "velocity": {"on": true, "hours": 3}}`)
	if out := hook(`<paseo-system kind="agent-message">hi</paseo-system>`); out == "" {
		t.Fatal("no reminder on an agent message with agent on")
	}
	if out := hook("Fix the loader."); out != "" {
		t.Fatalf("user off, but reminded: %q", out)
	}
	// A started task: no reminder.
	setReminders(t, h, "null")
	h.ok(a, "", "task", "start", open)
	if out := hook("Fix the loader."); out != "" {
		t.Fatalf("with a started task: %q", out)
	}
	// Recording off in config.toml: the reminder still comes.
	h.ok(a, "", "task", "done", open, "--note", "n")
	cfg := filepath.Join(h.home, ".config", "agent-desk")
	must(t, os.MkdirAll(cfg, 0o755))
	must(t, os.WriteFile(filepath.Join(cfg, "config.toml"), []byte("[instructions]\nrecord = false\n"), 0o644))
	before := h.count()
	if out := hook("Fix the loader."); !strings.Contains(out, "no started task") || h.count() != before {
		t.Fatalf("recording off: reminder %q, %d new events", out, h.count()-before)
	}
}

// The user's copy of the template is used; a broken one falls back to the
// built-in one and logs why.
func TestReminderTemplateOverride(t *testing.T) {
	h := newHarness(t)
	h.ok(h.as(sessA), "", "join", "r1/coord")
	_, hd := testServer(t, h)
	code, body, _ := do(t, hd, "GET", "/api/prompts", "", nil)
	var list struct {
		Dir     string `json:"dir"`
		Prompts []struct {
			Name       string `json:"name"`
			Builtin    string `json:"builtin"`
			Overridden bool   `json:"overridden"`
		} `json:"prompts"`
	}
	must(t, json.Unmarshal([]byte(body), &list))
	if code != 200 || len(list.Prompts) < 3 || list.Prompts[0].Name != "nag-no-task.md" || list.Prompts[1].Name != "nag-velocity.md" || list.Prompts[0].Overridden {
		t.Fatalf("api/prompts: %d %s", code, body)
	}
	if code, body, _ := do(t, hd, "POST", "/api/prompt", `{"name":"nag-no-task.md","text":"{{.Nope}}"}`, dashboard); code != 400 || !strings.Contains(body, "Nope") {
		t.Fatalf("a field that does not exist: %d %s", code, body)
	}
	if code, _, _ := do(t, hd, "POST", "/api/prompt", `{"name":"../config.toml","text":"x"}`, dashboard); code != 400 {
		t.Fatalf("a name outside the prompts: %d", code)
	}
	if code, body, _ := do(t, hd, "POST", "/api/prompt", `{"name":"nag-no-task.md","text":"Start a task at {{.Node}}."}`, dashboard); code != 200 || !strings.Contains(body, `"overridden":true`) {
		t.Fatalf("save: %d %s", code, body)
	}
	hook := func() string {
		return h.ok(map[string]string{}, hookJSON(sessA, "Go."), "hook", "prompt", "--agent", "claude")
	}
	if out := hook(); out != "Start a task at example-proj/r1/coord." {
		t.Fatalf("with the user's copy: %q", out)
	}
	// Broken by hand: the built-in one, and a line in the log.
	must(t, os.WriteFile(filepath.Join(list.Dir, "nag-no-task.md"), []byte("{{.Nope}}"), 0o644))
	if out := hook(); !strings.Contains(out, "no started task") {
		t.Fatalf("broken copy: %q", out)
	}
	if b, _ := os.ReadFile(filepath.Join(h.desk, "hook-errors.log")); !strings.Contains(string(b), "nag-no-task.md") {
		t.Fatalf("hook-errors.log: %q", b)
	}
	if code, body, _ := do(t, hd, "POST", "/api/prompt", `{"name":"nag-no-task.md","reset":true}`, dashboard); code != 200 || strings.Contains(body, `"overridden":true`) {
		t.Fatalf("reset: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(list.Dir, "nag-no-task.md")); !os.IsNotExist(err) {
		t.Fatalf("reset left the file: %v", err)
	}
}

// task done reminds a coordinator whose velocity slot is missing or older
// than the setting's hours; a session working alone is not reminded.
func TestVelocityReminder(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	done := func(env map[string]string) string {
		t.Helper()
		id := h.ok(env, "", "task", "add", "--title", "T", "--brief", "b")
		_, errOut, code := h.run(env, "", "task", "done", id, "--note", "checked")
		if code != 0 {
			t.Fatalf("task done: %s", errOut)
		}
		return errOut
	}
	if out := done(a); !strings.Contains(out, "example-proj/r1/coord has no velocity slot yet") {
		t.Fatalf("no slot: %q", out)
	}
	h.ok(a, "", "set", "velocity", "Pace: fine.")
	if out := done(a); out != "" {
		t.Fatalf("fresh slot: %q", out)
	}
	h.now = h.now.Add(4 * time.Hour)
	if out := done(a); !strings.Contains(out, "was last set 4h") {
		t.Fatalf("old slot: %q", out)
	}
	setReminders(t, h, `{"no_task": {"on": true, "user": true, "desk": true, "heartbeat": true}, "velocity": {"on": false, "hours": 3}}`)
	if out := done(a); out != "" {
		t.Fatalf("velocity off: %q", out)
	}
	setReminders(t, h, "null")
	// Alone (adhoc): no velocity to keep.
	b := h.as(sessB)
	h.ok(b, "hi", "tell", "--title", "Hello")
	if out := done(b); strings.Contains(out, "velocity") {
		t.Fatalf("adhoc session: %q", out)
	}
}
