package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// setPaseoFailure makes a stored Paseo record show a failed turn (lastError
// empty: the agent has run since), last active at active.
func setPaseoFailure(t *testing.T, home, uuid, lastErr, turn string, active time.Time) {
	t.Helper()
	path := filepath.Join(home, "agents", "Users-example-code", uuid+".json")
	b, err := os.ReadFile(path)
	must(t, err)
	rec := map[string]any{}
	must(t, json.Unmarshal(b, &rec))
	rec["lastError"], rec["lastUserMessageAt"], rec["lastActivityAt"] = lastErr, turn, active.UTC().Format(time.RFC3339Nano)
	if lastErr == "" {
		delete(rec, "lastError")
	}
	b, _ = json.Marshal(rec)
	must(t, os.WriteFile(path, b, 0o644))
}

// stoppedNoticesNow runs the server's look for stopped agents once, and
// returns every stopped notice on the desk with whether it is unread.
func stoppedNoticesNow(t *testing.T, h *harness, env map[string]string) (filed []string, notices []store.Event, unread []bool) {
	t.Helper()
	a, _, _ := h.app(env, "")
	filed, err := a.stoppedNotices()
	must(t, err)
	d, err := a.load()
	must(t, err)
	for _, e := range d.Events {
		if desk.StoppedNotice(e) {
			notices = append(notices, e)
			unread = append(unread, d.Unread(e.ID))
		}
	}
	return filed, notices, unread
}

// Stopped by an error: an agent whose last turn failed is listed with its
// error, can be resumed, and gets one notice per failed turn, read once it
// runs again.
func TestStoppedByError(t *testing.T) {
	h := newHarness(t)
	const coord = "66666666-0000-4000-8000-000000000001"
	const worker = "66666666-0000-4000-8000-000000000002"
	writePaseoRecord(t, h.paseo, coord, sessA, "", "Coordinator")
	writePaseoRecord(t, h.paseo, worker, sessB, coord, "Worker")
	h.respond = func(name string, args []string) (string, error) { return "{}", nil }
	env := map[string]string{"PASEO_HOME": h.paseo}
	h.ok(map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": coord}, "", "join", "r1/coord")

	if out := h.ok(env, "", "quota", "list"); !strings.Contains(out, "no agent is stopped") {
		t.Fatalf("nothing failed yet: %s", out)
	}
	if filed, _, _ := stoppedNoticesNow(t, h, env); len(filed) != 0 {
		t.Fatalf("filed with nothing failed: %v", filed)
	}

	// The worker's account logs out.
	const login = "Not logged in · Please run /login"
	setPaseoFailure(t, h.paseo, worker, login, "2026-03-04T08:58:00Z", h.now)
	out := h.ok(env, "", "quota", "list")
	if !strings.Contains(out, worker+"  Worker") || !strings.Contains(out, "stopped by an error: "+login) || !strings.Contains(out, "parent Coordinator") {
		t.Fatalf("list: %s", out)
	}
	var qs []QuotaAgent
	must(t, json.Unmarshal([]byte(h.ok(env, "", "quota", "list", "--format", "json")), &qs))
	if len(qs) != 1 || qs[0].Why != StoppedError || !qs[0].Login || qs[0].Error != login {
		t.Fatalf("json: %+v", qs)
	}
	out = h.ok(env, "", "quota", "resume", worker, "--dry-run")
	if !strings.Contains(out, `stopped with an error ("`+login+`"), and the user has logged in again`) {
		t.Fatalf("resume prompt: %s", out)
	}
	out = h.ok(env, "", "quota", "replace", worker, "--dry-run")
	if !strings.Contains(out, `which stopped with an error: "`+login+`"`) {
		t.Fatalf("replace prompt: %s", out)
	}
	out = h.ok(env, "", "quota", "ask-parent", worker, "--dry-run")
	if !strings.Contains(out, "Its account is logged out") || strings.Contains(out, "quota to reset") {
		t.Fatalf("ask-parent prompt: %s", out)
	}
	h.ok(env, "", "quota", "resume", worker)
	sent := false
	for _, c := range *h.calls {
		sent = sent || strings.HasPrefix(c, "paseo send --no-wait --json "+worker+" Continue: your last turn stopped with an error")
	}
	if !sent {
		t.Fatalf("resume did not send: %v", *h.calls)
	}

	// One notice, under the parent's node (the worker holds none), from
	// the session holding it, in the stopped class.
	filed, notices, unread := stoppedNoticesNow(t, h, env)
	if len(filed) != 1 || len(notices) != 1 || !unread[0] {
		t.Fatalf("filed %v, notices %v", filed, notices)
	}
	n := notices[0]
	if n.Path != "example-proj/r1/coord" || sessionOf(n) != sessA || desk.ClassOf(n) != "stopped" ||
		n.Title != "Worker is logged out: "+login || !strings.Contains(n.Body, "codex login") {
		t.Fatalf("notice: path %s session %s class %s title %q body %q", n.Path, sessionOf(n), desk.ClassOf(n), n.Title, n.Body)
	}
	if filed, _, _ := stoppedNoticesNow(t, h, env); len(filed) != 0 {
		t.Fatalf("told twice: %v", filed)
	}

	// It runs again: the notice is read.
	setPaseoFailure(t, h.paseo, worker, "", "2026-03-04T09:10:00Z", h.now)
	if _, notices, unread = stoppedNoticesNow(t, h, env); len(notices) != 1 || unread[0] {
		t.Fatalf("not read once it ran: %v", unread)
	}

	// Another failure is another notice; a usage limit is left to Out of
	// quota, and a failure from days ago is listed but not told.
	setPaseoFailure(t, h.paseo, worker, "The stream was interrupted.", "2026-03-04T09:20:00Z", h.now)
	if filed, notices, _ = stoppedNoticesNow(t, h, env); len(filed) != 1 || notices[1].Title != "Worker stopped: The stream was interrupted." {
		t.Fatalf("second failure: %v %v", filed, notices)
	}
	setPaseoFailure(t, h.paseo, worker, "You've hit your usage limit", "2026-03-04T09:30:00Z", h.now)
	if filed, _, _ = stoppedNoticesNow(t, h, env); len(filed) != 0 {
		t.Fatalf("usage limit told: %v", filed)
	}
	setPaseoFailure(t, h.paseo, coord, "Overloaded", "2026-03-01T09:00:00Z", h.now.Add(-72*time.Hour))
	if filed, _, _ = stoppedNoticesNow(t, h, env); len(filed) != 0 {
		t.Fatalf("old failure told: %v", filed)
	}
	if out := h.ok(env, "", "quota", "list"); !strings.Contains(out, coord+"  Coordinator") {
		t.Fatalf("old failure not listed: %s", out)
	}
}

func TestLoginError(t *testing.T) {
	for _, s := range []string{
		"Not logged in · Please run /login",
		"Invalid API key · Please run /login",
		`API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth token has expired."}}`,
		"Your access token could not be refreshed because your refresh token was already used. Please log out and sign in again.",
	} {
		if !LoginError.MatchString(s) {
			t.Errorf("not a login error: %s", s)
		}
	}
	for _, s := range []string{"The stream was interrupted.", "Overloaded", "You've hit your usage limit"} {
		if LoginError.MatchString(s) {
			t.Errorf("a login error: %s", s)
		}
	}
}
