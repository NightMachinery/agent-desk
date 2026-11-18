package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Spec acceptance 10, server side: threads with delivery and response
// state, the node's conversation, the target's notify values, and settings.
func TestServeThreads(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-000000000021"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	tl := h.ok(env, "Live.", "tell", "--title", "Deploy finished")
	h.respond = func(name string, args []string) (string, error) { return `{"disposition":"steered"}`, nil }
	_, hd := testServer(t, h)

	code, body, _ := do(t, hd, "POST", "/api/reply",
		`{"to":"`+tl+`","text":"check the feed","notify":{"remote":"always"}}`, dashboard)
	if code != 200 {
		t.Fatalf("reply: %d %s", code, body)
	}
	var res map[string]string
	must(t, json.Unmarshal([]byte(body), &res))
	m1 := res["id"]
	if code, body, _ = do(t, hd, "POST", "/api/reply", `{"to":"`+tl+`","text":"x","notify":{"remote":"loud"}}`, dashboard); code != 400 {
		t.Fatalf("bad notify: %d %s", code, body)
	}
	r1 := h.ok(env, "Done.", "tell", "--re", m1)
	m2 := h.ok(nil, "", "reply", "example-proj/r1/coord", "and the node?")

	item := func(id string) itemResponse {
		t.Helper()
		_, hd := testServer(t, h)
		code, body, _ := do(t, hd, "GET", "/api/item?id="+id, "", nil)
		if code != 200 {
			t.Fatalf("item %s: %d %s", id, code, body)
		}
		var it itemResponse
		must(t, json.Unmarshal([]byte(body), &it))
		return it
	}
	it := item(tl)
	if len(it.Thread) != 2 {
		t.Fatalf("thread: %+v", it.Thread)
	}
	msg, re := it.Thread[0], it.Thread[1]
	if msg.Kind != "message" || msg.ID != m1 || msg.Behavior != "steer" || msg.Delivery == nil ||
		msg.Delivery.Disposition != "steered" || msg.Status != "responded" || len(msg.Responses) != 1 ||
		msg.Responses[0] != r1 || msg.Notify["remote"] != "always" || msg.By != "Rerun coordinator" {
		t.Fatalf("message entry: %+v", msg)
	}
	if re.Kind != "response" || re.Re != m1 || re.By != "Rerun coordinator" {
		t.Fatalf("response entry: %+v", re)
	}
	// A response's page shows the thread it belongs to.
	if it = item(r1); it.ThreadOf != tl || it.ThreadTitle != "Deploy finished" || len(it.Thread) != 2 ||
		it.Item.Re != m1 || it.Item.Root != tl {
		t.Fatalf("response page: of %q %q, %d entries, item %+v", it.ThreadOf, it.ThreadTitle, len(it.Thread), it.Item)
	}

	// The node page: its own conversation, and the target's notify values.
	h.now = h.now.Add(16 * time.Minute)
	_, hd = testServer(t, h)
	var v viewResponse
	_, body, _ = do(t, hd, "GET", "/api/view?path=example-proj/r1/coord", "", nil)
	must(t, json.Unmarshal([]byte(body), &v))
	if len(v.Conversation) != 1 || v.Conversation[0].ID != m2 || v.Conversation[0].Status != "no_response" {
		t.Fatalf("conversation: %+v", v.Conversation)
	}
	if v.Target == nil || v.Target.Notify["desktop"] != "origin" || v.Target.Notify["remote"] != "away" || v.Target.Finished != "" {
		t.Fatalf("target: %+v", v.Target)
	}

	// Settings from the dashboard, though the server runs in an agent's shell.
	pref := func(js string) {
		t.Helper()
		if code, body, _ := do(t, hd, "POST", "/api/pref", js, dashboard); code != 200 {
			t.Fatalf("pref %s: %d %s", js, code, body)
		}
	}
	pref(`{"scope":"example-proj","key":"notify.remote","value":"always"}`)
	pref(`{"scope":"example-proj/r1","class":"response","key":"notify.remote","value":"off"}`)
	var pv PrefsView
	prefs := func(q string) PrefKey {
		t.Helper()
		_, hd := testServer(t, h)
		code, body, _ := do(t, hd, "GET", "/api/prefs?"+q, "", nil)
		if code != 200 {
			t.Fatalf("prefs %s: %d %s", q, code, body)
		}
		pv = PrefsView{}
		must(t, json.Unmarshal([]byte(body), &pv))
		for _, k := range pv.Keys {
			if k.Key == "notify.remote" {
				return k
			}
		}
		t.Fatalf("no notify.remote in %s", body)
		return PrefKey{}
	}
	k := prefs("path=example-proj/r1/coord")
	if k.Value != "always" || k.Origin.Scope != "example-proj" || k.Here || len(k.Differs) != 1 ||
		k.Differs[0] != (ClassValue{"response", "off"}) || len(pv.Overrides) != 2 {
		t.Fatalf("all kinds: %+v", k)
	}
	if k = prefs("path=example-proj/r1&class=response"); k.Value != "off" || !k.Here {
		t.Fatalf("class response: %+v", k)
	}
	pref(`{"scope":"example-proj/r1","class":"response","key":"notify.remote","value":null}`)
	if k = prefs("path=example-proj/r1&class=response"); k.Value != "always" || k.Here {
		t.Fatalf("after reset: %+v", k)
	}
	if code, body, _ := do(t, hd, "POST", "/api/pref", `{"key":"deliver.behavior","value":"interrupt"}`, dashboard); code != 400 ||
		!strings.Contains(body, "cancels") {
		t.Fatalf("interrupt default: %d %s", code, body)
	}

	// A done report from a live Paseo agent is a milestone: not finished.
	// From a session Paseo does not know, or once the Paseo agent is
	// archived, sending revives it.
	h.ok(env, "", "done", "--title", "Milestone")
	_, hd = testServer(t, h)
	v = viewResponse{}
	_, body, _ = do(t, hd, "GET", "/api/view?path=example-proj/r1/coord", "", nil)
	must(t, json.Unmarshal([]byte(body), &v))
	if v.Target == nil || v.Target.Finished != "" {
		t.Fatalf("a live Paseo agent's milestone: %+v", v.Target)
	}
	plain := h.as(sessB)
	h.ok(plain, "", "join", "r2/coord")
	h.ok(plain, "", "done", "--title", "Finished")
	_, hd = testServer(t, h)
	v = viewResponse{}
	_, body, _ = do(t, hd, "GET", "/api/view?path=example-proj/r2/coord", "", nil)
	must(t, json.Unmarshal([]byte(body), &v))
	if v.Target == nil || v.Target.Finished != "done" {
		t.Fatalf("finished: %+v", v.Target)
	}
	rec := filepath.Join(h.paseo, "agents", "Users-example-code", uuid+".json")
	b, err := os.ReadFile(rec)
	must(t, err)
	must(t, os.WriteFile(rec, []byte(strings.Replace(string(b), "{", `{"archivedAt":"2026-03-04T09:00:00Z",`, 1)), 0o644))
	h.ok(env, "", "join", "r1/coord")
	_, hd = testServer(t, h)
	_, body, _ = do(t, hd, "GET", "/api/target?to=example-proj/r1/coord", "", nil)
	var tg Target
	must(t, json.Unmarshal([]byte(body), &tg))
	if tg.Finished != "archived" {
		t.Fatalf("archived: %s", body)
	}
}

// An answer's notify override routes the response to it.
func TestAnswerNotifyOverride(t *testing.T) {
	h := newHarness(t)
	log := filepath.Join(t.TempDir(), "hooks.jsonl")
	notifyConfig(t, h, log, "")
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	q := h.ok(a, "ctx", "ask", "--blocked", "--title", "Which?")
	h.respond = func(name string, args []string) (string, error) { return "", nil }
	ans := h.ok(nil, "", "answer", q, "--notify-desktop", "none", "--notify-remote", "always", "B")
	n := len(readHookLog(t, log))
	h.ok(a, "Going with B.", "tell", "--re", ans)
	got := readHookLog(t, log)[n:]
	if len(got) != 1 || got[0].Channel != "remote" || got[0].Class != "response" {
		t.Fatalf("response to the answer: %+v", got)
	}
	h.fails(nil, "", "--notify-*", "answer", q, "--from-chat", "x", "--notify-remote", "always")
}
