package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPref(t *testing.T) {
	h := newHarness(t)
	agent := h.as(sessA)
	h.ok(agent, "", "join", "r1/coord")

	// Settings are the user's.
	h.fails(agent, "", "an agent session cannot change them", "pref", "set", "notify.remote", "always")
	// Interrupt is never a default.
	h.fails(nil, "", "cannot be interrupt", "pref", "set", "deliver.behavior", "interrupt")
	h.fails(nil, "", "drop --class", "pref", "set", "deliver.behavior", "queue", "--class", "done")
	h.fails(nil, "", "no setting", "pref", "set", "colour", "red")
	h.fails(nil, "", "no class", "pref", "set", "notify.remote", "off", "--class", "loud")

	h.ok(nil, "", "pref", "set", "notify.remote", "always", "--scope", "example-proj/r1", "--class", "ask-blocked")
	h.ok(nil, "", "pref", "set", "notify.desktop", "host-a, host-b")
	out := h.ok(nil, "", "pref", "show", "example-proj/r1/coord")
	for _, want := range []string{
		"deliver.behavior = steer  (built in)",
		"ask-blocked  always  (set at example-proj/r1, class ask-blocked)",
		"ask-default  off     (built in)",
		"done         host-a,host-b (set globally, any class)",
		"notify.desktop = host-a,host-b  globally",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("pref show lacks %q:\n%s", want, out)
		}
	}
	var v SettingsView
	must(t, json.Unmarshal([]byte(h.ok(nil, "", "pref", "show", "example-proj/r1/coord", "--class", "ask-blocked", "--format", "json")), &v))
	if len(v.Settings) != 4 || len(v.Overrides) != 2 || v.Settings[2].Origin.Scope != "example-proj/r1" {
		t.Fatalf("json: %+v", v)
	}

	// Unset restores inheritance; unsetting nothing writes nothing.
	n := h.count()
	_, errOut, code := h.run(nil, "", "pref", "unset", "notify.remote", "--scope", "example-proj/r1")
	if code != 0 || !strings.Contains(errOut, "no override") || h.count() != n {
		t.Fatalf("unset of nothing: %d %q", code, errOut)
	}
	h.ok(nil, "", "pref", "unset", "notify.remote", "--scope", "example-proj/r1", "--class", "ask-blocked")
	if out := h.ok(nil, "", "pref", "show", "example-proj/r1/coord", "--class", "ask-blocked"); !strings.Contains(out, "away    (built in)") {
		t.Fatalf("after unset:\n%s", out)
	}
	// Pref events stay out of the views.
	if got := h.showJSON("", true); len(got["new"].([]any)) != 0 {
		t.Fatalf("pref shown as an item: %v", got["new"])
	}
}

func TestDefaultBehaviorSetting(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-000000000009"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	tl := h.ok(env, "Live.", "tell", "--title", "Deploy finished")
	h.respond = fakePaseo(helpQueue, map[string]string{"steer": `{"disposition":"steered"}`, "queue": `{"disposition":"queued","queuePosition":1}`}, nil)
	sent := func() string { c := *h.calls; return c[len(c)-1] }

	// A config default applies.
	cfg := filepath.Join(h.home, ".config", "agent-desk", "config.toml")
	must(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
	must(t, os.WriteFile(cfg, []byte("[defaults]\n\"deliver.behavior\" = \"queue\"\n"), 0o644))
	h.ok(nil, "", "reply", tl, "after this turn")
	if !strings.Contains(sent(), "--behavior queue") {
		t.Fatalf("config default: %q", sent())
	}
	// A pref on the run beats it; --behavior beats both.
	h.ok(nil, "", "pref", "set", "deliver.behavior", "steer", "--scope", "example-proj/r1")
	h.ok(nil, "", "reply", tl, "now")
	if !strings.Contains(sent(), "--behavior steer") {
		t.Fatalf("pref: %q", sent())
	}
	h.ok(nil, "", "reply", tl, "--behavior", "queue", "later")
	if !strings.Contains(sent(), "--behavior queue") {
		t.Fatalf("per message: %q", sent())
	}
	_, hd := testServer(t, h)
	_, body, _ := do(t, hd, "GET", "/api/target?to="+tl, "", nil)
	var tg Target
	must(t, json.Unmarshal([]byte(body), &tg))
	if tg.DefaultBehavior != "steer" {
		t.Fatalf("default_behavior: %s", body)
	}

	// A bad config default is an error naming the table.
	must(t, os.WriteFile(cfg, []byte("[defaults.class.done]\n\"deliver.behavior\" = \"queue\"\n"), 0o644))
	h.fails(nil, "", "[defaults.class.done]", "reply", tl, "x")
}
