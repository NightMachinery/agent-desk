package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func (h *harness) inbox(t *testing.T, state string, args ...string) string {
	t.Helper()
	env := map[string]string{"AGENT_DESK_INBOX_STATE": state}
	return ansiRE.ReplaceAllString(h.ok(env, "", args...), "")
}

func TestInboxFoldingAndActions(t *testing.T) {
	h := newHarness(t)
	state := filepath.Join(t.TempDir(), "state.json")
	must(t, os.WriteFile(state, []byte(`{}`), 0o600))
	a, b := h.as(sessA), h.as(sessB)
	h.ok(a, "", "join", "r1/coord")
	h.ok(a, "", "set", "status", "Seeds 3/5 done.")
	h.ok(b, "", "join", "r1/coord/eval-lead")
	h.ok(b, "", "set", "status", "Evaluating seed 2.")
	q := h.ok(a, "ctx", "ask", "--default", "reuse the cache", "--title", "Rebuild the cache?")

	list := h.inbox(t, state, "inbox-list")
	// The path to the open question is expanded; eval-lead has nothing for
	// the user and is one line carrying its status.
	if !strings.Contains(list, "v coord") || !strings.Contains(list, "eval-lead  active") ||
		!strings.Contains(list, "Evaluating seed 2.") || strings.Contains(list, "v eval-lead") {
		t.Fatalf("folding:\n%s", list)
	}
	keyOf := func(list, want string) string {
		for _, l := range strings.Split(h.ok(map[string]string{"AGENT_DESK_INBOX_STATE": state}, "", "inbox-list"), "\n") {
			if strings.Contains(ansiRE.ReplaceAllString(l, ""), want) {
				return strings.SplitN(l, "\t", 2)[0]
			}
		}
		t.Fatalf("no line with %q", want)
		return ""
	}
	// Enter on a node unfolds it.
	h.ok(map[string]string{"AGENT_DESK_INBOX_STATE": state}, "", "inbox-act", "enter", keyOf(list, "eval-lead"))
	if l := h.inbox(t, state, "inbox-list"); !strings.Contains(l, "v eval-lead") {
		t.Fatalf("enter did not unfold:\n%s", l)
	}
	// Preview of the question names its default and the keys.
	qkey := keyOf(list, "Rebuild the cache?")
	if p := h.inbox(t, state, "inbox-preview", qkey); !strings.Contains(p, "Default: reuse the cache") ||
		!strings.Contains(p, "ctrl-t take the default") || !strings.Contains(p, q) {
		t.Fatalf("preview:\n%s", p)
	}
	// ctrl-t answers with the fixed sentence.
	h.ok(map[string]string{"AGENT_DESK_INBOX_STATE": state}, "", "inbox-act", "take", qkey)
	if out := h.ok(nil, "", "search", "Go ahead with your default: reuse the cache"); !strings.Contains(out, "answer") {
		t.Fatalf("take: %s", out)
	}
	// ctrl-r on a node marks its subtree read.
	h.ok(b, "", "set", "findings", "seed 2 diverges")
	h.ok(a, "body", "tell", "--title", "Notice")
	if l := h.inbox(t, state, "inbox-list"); !strings.Contains(l, "NEW (2)") {
		t.Fatalf("before read:\n%s", l)
	}
	h.ok(map[string]string{"AGENT_DESK_INBOX_STATE": state}, "", "inbox-act", "read", "node:example-proj/r1")
	if l := h.inbox(t, state, "inbox-list"); !strings.Contains(l, "NEW (0)") {
		t.Fatalf("after read:\n%s", l)
	}
	// alt-a shows read items again.
	h.ok(map[string]string{"AGENT_DESK_INBOX_STATE": state}, "", "inbox-act", "toggle-all", "-")
	if l := h.inbox(t, state, "inbox-list"); !strings.Contains(l, "NEW (2)") {
		t.Fatalf("show all:\n%s", l)
	}
}

func TestInboxNeedsTerminal(t *testing.T) {
	h := newHarness(t)
	_, errOut, code := h.run(nil, "", "inbox")
	if code == 0 || !strings.Contains(errOut, "needs") {
		t.Fatalf("inbox without a terminal: %d %q", code, errOut)
	}
}
