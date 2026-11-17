package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// notifyConfig writes a config whose notify and dismiss commands log their
// payloads to log; notify prints a handle, the log's line count.
func notifyConfig(t *testing.T, h *harness, log, extra string) {
	t.Helper()
	cfg := filepath.Join(h.home, ".config", "agent-desk", "config.toml")
	must(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
	must(t, os.WriteFile(cfg, []byte(`notify_command = "cat >> '`+log+`'; printf 'H%s\n' $(wc -l < '`+log+`')"
dismiss_command = "cat >> '`+log+`'"
base_url = "https://desk.example.net/"
`+extra), 0o644))
}

func kinds(h *harness, kind string) []store.Event {
	var out []store.Event
	for _, e := range h.loadDesk().Events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// Spec acceptance 7.
func TestNotifyChannelsAndTakeBack(t *testing.T) {
	h := newHarness(t)
	log := filepath.Join(t.TempDir(), "hooks.jsonl")
	notifyConfig(t, h, log, "")
	h.ok(nil, "", "pref", "set", "notify.remote", "always", "--class", "ask-blocked")
	h.ok(nil, "", "pref", "set", "notify.remote.dest", "example-chat")
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	q := h.ok(a, "ctx", "ask", "--blocked", "--title", "Which split?")

	got := readHookLog(t, log)
	if len(got) != 2 || got[0].Channel != "desktop" || got[1].Channel != "remote" || got[1].Dest != "example-chat" {
		t.Fatalf("raised: %+v", got)
	}
	if got[0].Class != "ask-blocked" || got[0].URL != "https://desk.example.net/#/p/example-proj/r1/coord?item="+q ||
		got[0].Project != "example-proj" || !strings.HasPrefix(got[0].AgentTitle, "claude ") {
		t.Fatalf("payload: %+v", got[0])
	}
	ns := kinds(h, "notified")
	if len(ns) != 2 || desk.Meta(ns[0])["handle"] != "H1" || desk.Meta(ns[1])["handle"] != "H2" ||
		desk.Meta(ns[0])["host"] != "host-a" {
		t.Fatalf("notified: %+v", ns)
	}

	h.ok(nil, "", "answer", q, "B")
	got = readHookLog(t, log)[2:]
	if len(got) != 2 || got[0].Action != "dismiss" || got[0].Handle != "H1" || got[1].Handle != "H2" ||
		got[1].Channel != "remote" || got[0].Cause == nil || got[0].Cause.Kind != "answer" {
		t.Fatalf("taken back: %+v", got)
	}
	if xs := kinds(h, "retracted"); len(xs) != 2 || xs[0].Ref != ns[0].ID || xs[1].Ref != ns[1].ID {
		t.Fatalf("retracted: %+v", xs)
	}
	// Nothing is taken back twice.
	h.ok(nil, "", "read", "example-proj")
	if n := len(readHookLog(t, log)); n != 4 {
		t.Fatalf("taken back again: %d hooks", n)
	}

	// A newer set takes back the old slot's notification.
	h.ok(a, "", "set", "findings", "first")
	old := kinds(h, "set")[0]
	n := store.Event{ID: "01ZZZZZZZZZZZZZZZZZZZZZZZZ", TS: "2026-03-04T10:00:00+01:00", Host: "host-a", Kind: "notified",
		Path: old.Path, Ref: old.ID, Meta: json.RawMessage(`{"channel":"desktop","handle":"H-slot","host":"host-a"}`)}
	st, err := store.Open(h.desk)
	must(t, err)
	must(t, st.Append(n))
	st.Close()
	h.ok(a, "", "set", "findings", "second")
	if got = readHookLog(t, log); len(got) != 5 || got[4].Handle != "H-slot" || got[4].Cause.Kind != "set" {
		t.Fatalf("slot take-back: %+v", got[4:])
	}
}

// Spec acceptance 8: two machines.
func TestNotifyAcrossMachines(t *testing.T) {
	ha, hb := newHarness(t), newHarness(t)
	hb.host = "host-b"
	logA, logB := filepath.Join(t.TempDir(), "a.jsonl"), filepath.Join(t.TempDir(), "b.jsonl")
	notifyConfig(t, ha, logA, "")
	notifyConfig(t, hb, logB, "")
	ha.ok(nil, "", "pref", "set", "notify.desktop", "all")
	ha.ok(nil, "", "pref", "set", "notify.remote", "always")
	a := ha.as(sessA)
	ha.ok(a, "", "join", "r1/coord")
	tn := ha.ok(a, "x", "tell", "--notify", "--title", "Look")
	if got := readHookLog(t, logA); len(got) != 2 {
		t.Fatalf("on A: %+v", got)
	}
	snap := func(h *harness) string {
		p := filepath.Join(t.TempDir(), "s.db")
		h.ok(nil, "", "snapshot", p)
		return p
	}

	// B raises its own desktop notification at import, once, and no remote.
	hb.ok(nil, "", "merge", snap(ha))
	hb.ok(nil, "", "merge", snap(ha))
	got := readHookLog(t, logB)
	if len(got) != 1 || got[0].Channel != "desktop" || got[0].DisplayID != tn || got[0].Local {
		t.Fatalf("on B: %+v", got)
	}

	// Read on B: B takes back its own and the remote one, not A's desktop.
	hb.ok(nil, "", "read", tn)
	got = readHookLog(t, logB)[1:]
	if len(got) != 2 || got[0].Channel != "desktop" || got[1].Channel != "remote" {
		t.Fatalf("B takes back: %+v", got)
	}
	// After the merge A takes back its desktop one only.
	ha.ok(nil, "", "merge", snap(hb))
	got = readHookLog(t, logA)[2:]
	if len(got) != 1 || got[0].Channel != "desktop" || got[0].Handle != "H1" {
		t.Fatalf("A takes back: %+v", got)
	}

	// Both read before merging: each takes back the remote one.
	t2 := ha.ok(a, "y", "tell", "--notify", "--title", "Again")
	hb.ok(nil, "", "merge", snap(ha))
	ha.ok(nil, "", "read", t2)
	hb.ok(nil, "", "read", t2)
	remote := func(log string) int {
		n := 0
		for _, p := range readHookLog(t, log) {
			if p.Action == "dismiss" && p.Channel == "remote" && p.DisplayID == t2 {
				n++
			}
		}
		return n
	}
	if remote(logA) != 1 || remote(logB) != 1 {
		t.Fatalf("remote take-backs: A %d, B %d", remote(logA), remote(logB))
	}
	ha.ok(nil, "", "merge", snap(hb))
	hb.ok(nil, "", "merge", snap(ha))
	if remote(logA) != 1 || remote(logB) != 1 {
		t.Fatal("a merge took the remote one back again")
	}
}

// Spec acceptance 9, and the old behaviour without settings.
func TestNotifyResponses(t *testing.T) {
	h := newHarness(t)
	log := filepath.Join(t.TempDir(), "hooks.jsonl")
	notifyConfig(t, h, log, "away_command = \"exit 1\"\n")
	const uuid = "22222222-0000-4000-8000-000000000011"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	tl := h.ok(env, "Live.", "tell", "--title", "Deploy finished")
	h.respond = func(name string, args []string) (string, error) { return `{"status":"sent"}`, nil }
	if n := len(readHookLog(t, log)); n != 0 {
		t.Fatalf("a plain tell notified: %d", n)
	}

	m1 := h.ok(nil, "", "reply", tl, "check the feed")
	r1 := h.ok(env, "Done.", "tell", "--re", m1)
	got := readHookLog(t, log)
	// Remote is away for responses, and away_command says the user is here.
	if len(got) != 1 || got[0].Class != "response" || got[0].Channel != "desktop" || got[0].DisplayID != r1 ||
		got[0].AgentTitle != "Rerun coordinator" {
		t.Fatalf("response: %+v", got)
	}
	m2 := h.ok(nil, "", "reply", tl, "ok")
	h.ok(env, "Noted.", "tell", "--re", m2, "--quiet")
	if n := len(readHookLog(t, log)); n != 1 {
		t.Fatalf("--quiet notified: %d", n)
	}
	h.fails(nil, "", "notify.remote is off, away or always", "reply", tl, "--notify-remote", "loud", "x")
	m3 := h.ok(nil, "", "reply", tl, "--notify-desktop", "none", "--notify-remote", "always", "tell me on the phone")
	h.ok(env, "Will do.", "tell", "--re", m3)
	if got = readHookLog(t, log)[1:]; len(got) != 1 || got[0].Channel != "remote" {
		t.Fatalf("per-message override: %+v", got)
	}
}

func TestOpenURL(t *testing.T) {
	h := newHarness(t)
	notifyConfig(t, h, filepath.Join(t.TempDir(), "l"), "")
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	tl := h.ok(a, "x", "tell", "--title", "T")
	h.respond = func(name string, args []string) (string, error) { return "", nil }
	h.ok(nil, "", "open", tl)
	c := *h.calls
	if len(c) == 0 || !strings.HasSuffix(c[len(c)-1], " https://desk.example.net/#/p/example-proj/r1/coord?item="+tl) {
		t.Fatalf("open: %q", c)
	}
}

// A desktop target in ssh_hosts gets the event pushed at once.
func TestNotifyPushesToSSHHosts(t *testing.T) {
	h := newHarness(t)
	self, err := os.Executable()
	must(t, err)
	remoteHome := filepath.Join(t.TempDir(), "remote-desk")
	fake := filepath.Join(t.TempDir(), "fake-ssh")
	must(t, os.WriteFile(fake, []byte("#!/bin/sh\nshift 3\nexec sh -c \"$1\"\n"), 0o755))
	notifyConfig(t, h, filepath.Join(t.TempDir(), "l"), "[ssh_hosts]\nhost-b = \"host-b.example.net\"\n")
	agent := h.as(sessA)
	agent["AGENT_DESK_SSH"] = fake
	agent["AGENT_DESK_REMOTE_COMMAND"] = "AGENT_DESK_AS_CLI=1 AGENT_DESK_HOME='" + remoteHome +
		"' AGENT_DESK_HOST=host-b AGENT_DESK_CONFIG=/nonexistent '" + self + "'"
	h.ok(nil, "", "pref", "set", "notify.desktop", "origin,host-b", "--class", "ask-blocked")
	h.ok(agent, "", "join", "r1/coord")
	q := h.ok(agent, "ctx", "ask", "--blocked", "--title", "Which?")
	hb := *h
	hb.desk, hb.host = remoteHome, "host-b"
	if !strings.Contains(hb.ok(nil, "", "show", "--format", "md"), "Which?") {
		t.Fatalf("%s was not pushed to host-b", q)
	}
}
