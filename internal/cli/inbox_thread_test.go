package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// G7: the terminal inbox shows conversations and marks responses, and the
// org ledger hangs them under their root item.
func TestInboxAndLedgerThreads(t *testing.T) {
	h := newHarness(t)
	state := filepath.Join(t.TempDir(), "state.json")
	must(t, os.WriteFile(state, []byte(`{}`), 0o600))
	const uuid = "22222222-0000-4000-8000-000000000021"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	tl := h.ok(env, "The site is live.", "tell", "--title", "Deploy finished")
	h.respond = func(name string, args []string) (string, error) { return `{"status":"sent"}`, nil }
	m := h.ok(nil, "", "reply", tl, "check the old feed too")
	r := h.ok(env, "The old feed is fine.", "tell", "--re", m)
	nm := h.ok(nil, "", "reply", "example-proj/r1/coord", "how are the seeds?")
	h.now = h.now.Add(16 * time.Minute)

	list := h.inbox(t, state, "inbox-list")
	if !strings.Contains(list, "RESPONSES (1)") || !strings.Contains(list, "re  ") ||
		!strings.Contains(list, "Re: Deploy finished") || !strings.Contains(list, "to "+m) {
		t.Fatalf("list:\n%s", list)
	}
	key := func(want string) string {
		for _, l := range strings.Split(h.ok(map[string]string{"AGENT_DESK_INBOX_STATE": state}, "", "inbox-list"), "\n") {
			if strings.Contains(ansiRE.ReplaceAllString(l, ""), want) {
				return strings.SplitN(l, "\t", 2)[0]
			}
		}
		t.Fatalf("no line with %q", want)
		return ""
	}
	// The response's preview shows the root's conversation, itself marked.
	p := h.inbox(t, state, "inbox-preview", key("Re: Deploy finished"))
	if !strings.Contains(p, "Conversation on "+tl+" Deploy finished") || !strings.Contains(p, m+"  You → Rerun coordinator") ||
		!strings.Contains(p, "delivered · responded") || !strings.Contains(p, r+"  re  ") ||
		!strings.Contains(p, "▸ "+r+"  Rerun coordinator responded to "+m) || !strings.Contains(p, "alt-m message") {
		t.Fatalf("response preview:\n%s", p)
	}
	// The node's preview shows its own conversation, flagged after 15 minutes.
	p = h.inbox(t, state, "inbox-preview", "node:example-proj/r1/coord")
	if !strings.Contains(p, "Conversation with this node") || !strings.Contains(p, nm) ||
		!strings.Contains(p, "how are the seeds?") || !strings.Contains(p, "no response yet") {
		t.Fatalf("node preview:\n%s", p)
	}

	// The files are rendered at each commit, so the flag shows after one.
	h.ok(env, "x", "tell", "--title", "Later")
	ledger, err := os.ReadFile(filepath.Join(h.desk, "render", "ledger", h.now.Format("2006-01")+".org"))
	must(t, err)
	l := string(ledger)
	for _, want := range []string{
		"#+TODO: ASK NEW MSG RE | ANSWERED",
		" " + tl + " tell: Deploy finished",
		"** MSG " + m + " message: check the old feed too",
		":delivered:\n",
		"** RE " + r + " response to " + m + ": Re: Deploy finished",
		"* MSG " + nm + " message: how are the seeds?",
		":delivered:no_response:\n",
	} {
		if !strings.Contains(l, want) {
			t.Fatalf("ledger lacks %q:\n%s", want, l)
		}
	}
	if strings.Count(l, "Re: Deploy finished") != 1 {
		t.Fatalf("the response is listed twice:\n%s", l)
	}
}
