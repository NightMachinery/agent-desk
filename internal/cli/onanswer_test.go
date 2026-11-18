package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An on-answer command gets the answer and handles it (exit 0), or passes
// it to the session with its output; it runs once per answer even when the
// session is unreachable and delivery is retried. The commands are inert:
// they only write files in the test's directory.
func TestOnAnswer(t *testing.T) {
	h := newHarness(t)
	h.real = map[string]bool{"env": true}
	const uuid = "22222222-0000-4000-8000-0000000000a1"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Sync coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "sync/coord")
	h.fails(env, "ctx", "--on-answer needs the command", "ask", "--blocked", "--title", "T", "--on-answer", " ")

	// Exit 0: handled, and no session is reached.
	q := h.ok(env, "ctx", "ask", "--blocked", "--title", "Which side?",
		"--on-answer", `{ printf '%s|%s|%s|' "$AGENT_DESK_QUESTION" "$AGENT_DESK_TITLE" "$PWD"; cat; } > got; echo applied`)
	h.ok(nil, "", "answer", q, "1. B: theirs")
	got, err := os.ReadFile(filepath.Join(h.cwd, "got"))
	must(t, err)
	if want := q + "|Which side?|" + h.cwd + "|1. B: theirs"; string(got) != want {
		t.Fatalf("command saw %q, want %q", got, want)
	}
	for _, c := range *h.calls {
		if strings.HasPrefix(c, "paseo") {
			t.Fatalf("a handled answer reached the session: %q", *h.calls)
		}
	}
	dv := h.delivered(t)
	if len(dv) != 1 {
		t.Fatalf("delivered marks: %d", len(dv))
	}
	var m struct {
		Via, Disposition string
		OnAnswer         onAnswerRun `json:"on_answer"`
	}
	must(t, json.Unmarshal(dv[0].Meta, &m))
	if m.Via != "on-answer" || m.Disposition != "handled" || m.OnAnswer.Exit != 0 || m.OnAnswer.Output != "applied" {
		t.Fatalf("receipt: %+v", m)
	}

	// Exit 3, session unreachable: the answer stays pending, and a retry
	// does not run the command again.
	*h.calls = nil
	q2 := h.ok(env, "ctx", "ask", "--blocked", "--title", "Merge how?",
		"--on-answer", `echo x >> runs; echo 'cannot decide "Other"'; exit 3`)
	_, errOut, _ := h.run(nil, "", "answer", q2, "Other: ask me later")
	if !strings.Contains(errOut, "not delivered") || !strings.Contains(errOut, "exited 3") {
		t.Fatalf("unreachable: %q", errOut)
	}
	h.respond = func(name string, args []string) (string, error) { return `{"disposition":"steered"}`, nil }
	h.ok(nil, "", "deliver")
	runs, err := os.ReadFile(filepath.Join(h.cwd, "runs"))
	must(t, err)
	if string(runs) != "x\n" {
		t.Fatalf("the command ran %d times", strings.Count(string(runs), "x"))
	}
	var sent string
	for _, c := range *h.calls {
		if strings.HasPrefix(c, "paseo send") {
			sent = c
		}
	}
	if !strings.Contains(sent, "Other: ask me later") || !strings.Contains(sent, "exited 3 and passed the answer to you") ||
		!strings.Contains(sent, `cannot decide "Other"`) {
		t.Fatalf("passed on as %q", sent)
	}
	dv = h.delivered(t)
	must(t, json.Unmarshal(dv[len(dv)-1].Meta, &m))
	if m.Via != "paseo" || m.OnAnswer.Exit != 3 {
		t.Fatalf("receipt: %+v", m)
	}

	// The dashboard's thread view carries the run.
	a, _, _ := h.app(nil, "")
	d, err := a.load()
	must(t, err)
	qe, err := d.Resolve(q)
	must(t, err)
	var run *onAnswerRun
	for _, e := range entries(d, d.Thread(qe.ID, qe.Path), nil) {
		if e.Delivery != nil && e.Delivery.OnAnswer != nil {
			run = e.Delivery.OnAnswer
		}
	}
	if run == nil || run.Output != "applied" {
		t.Fatalf("thread view: %+v", run)
	}
}
