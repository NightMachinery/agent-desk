package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeliveryFailureIsKept: why an answer did not reach its session is
// kept on the desk (once per distinct reason), shown by the dashboard and
// the org ledger, logged in deliver.log, and cleared by a later delivery, which the
// dashboard's Retry can make.
func TestDeliveryFailureIsKept(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-0000000000f1"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	q := h.ok(env, "ctx", "ask", "--blocked", "--title", "Which split?")

	reason := "failed to load configuration: Failed to synchronize managed preferences"
	h.respond = func(name string, args []string) (string, error) {
		return "{\n  \"error\": {\n    \"code\": \"SEND_FAILED\",\n    \"message\": \"Failed to send message: " + reason + "\"\n  }\n}", errors.New("exit status 1")
	}
	h.run(nil, "", "answer", q, "B")
	h.run(nil, "", "deliver") // the same failure again
	fails := kinds(h, "undelivered")
	if len(fails) != 1 {
		t.Fatalf("undelivered marks: %+v", fails)
	}
	var m struct{ Error, Resume string }
	must(t, json.Unmarshal(fails[0].Meta, &m))
	if m.Error != "paseo send: Failed to send message: "+reason+" (exit status 1)" || !strings.Contains(m.Resume, "claude --resume") {
		t.Fatalf("meta: %s", fails[0].Meta)
	}

	// The dashboard shows it beside the answer.
	_, hd := testServer(t, h)
	item := func() Entry {
		t.Helper()
		_, body, _ := do(t, hd, "GET", "/api/item?id="+q, "", nil)
		var v struct{ Thread []Entry }
		must(t, json.Unmarshal([]byte(body), &v))
		for _, e := range v.Thread {
			if e.Event.Kind == "answer" {
				return e
			}
		}
		t.Fatalf("no answer in %s", body)
		return Entry{}
	}
	if e := item(); e.Failure == nil || e.Failure.Error != m.Error || e.Pending != "not delivered" {
		t.Fatalf("entry: %+v", e)
	}
	h.ok(nil, "", "render")
	ledgers, _ := filepath.Glob(filepath.Join(h.desk, "render", "ledger", "*.org"))
	if len(ledgers) != 1 {
		t.Fatalf("ledgers: %v", ledgers)
	}
	if b, _ := os.ReadFile(ledgers[0]); !strings.Contains(string(b), ":undelivered:") || !strings.Contains(string(b), "Not delivered: paseo send: Failed") {
		t.Fatalf("ledger:\n%s", b)
	}

	// A different reason is a new mark.
	h.respond = func(string, []string) (string, error) { return "", errors.New("exit status 2") }
	h.run(nil, "", "deliver")
	if n := len(kinds(h, "undelivered")); n != 2 {
		t.Fatalf("a new reason was not kept: %d marks", n)
	}

	// Retry from the dashboard: refused cross-site, then delivered.
	if code, _, _ := do(t, hd, "POST", "/api/deliver", `{"id":"`+q+`"}`, nil); code != 403 {
		t.Fatalf("a cross-site retry was not refused: %d", code)
	}
	if code, body, _ := do(t, hd, "POST", "/api/deliver", `{"id":"`+q+`"}`, dashboard); code != 422 || !strings.Contains(body, "exit status 2") {
		t.Fatalf("failed retry: %d %s", code, body)
	}
	h.respond = func(string, []string) (string, error) { return `{"status":"sent","disposition":"turn_started"}`, nil }
	ans := item().ID
	if code, body, _ := do(t, hd, "POST", "/api/deliver", `{"id":"`+ans+`"}`, dashboard); code != 200 {
		t.Fatalf("retry: %d %s", code, body)
	}
	if e := item(); e.Failure != nil || e.Delivery == nil {
		t.Fatalf("after delivery: %+v", e)
	}

	log, err := os.ReadFile(filepath.Join(h.desk, "deliver.log"))
	must(t, err)
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 5 || !strings.Contains(lines[0], "-> "+q+" failed: paseo send: Failed to send message") ||
		!strings.Contains(lines[4], "delivered: paseo turn_started") {
		t.Fatalf("deliver.log:\n%s", log)
	}
}
