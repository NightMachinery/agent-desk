package cli

import (
	"fmt"
	"strings"
	"testing"
)

func TestReply(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-000000000002"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	tl := h.ok(env, "The site is live.", "tell", "--title", "Deploy finished")
	h.respond = func(name string, args []string) (string, error) { return "queued", nil }

	// About an item: typed into the session that wrote it.
	m := h.ok(nil, "", "reply", tl, "Check the old feed too")
	if !strings.HasPrefix(m, "m-") {
		t.Fatalf("reply id %q", m)
	}
	want := fmt.Sprintf("paseo send --no-wait --json --behavior steer %s Message %s from the user via agent-desk, about %s \"Deploy finished\": Check the old feed too\n(Respond on the desk: agent-desk tell --re %s)", uuid, m, tl, m)
	if c := *h.calls; len(c) != 1 || c[0] != want {
		t.Fatalf("calls: %q", c)
	}
	if len(h.delivered(t)) != 1 {
		t.Fatal("reply not marked delivered")
	}

	// To a node: its coordinator. Body from stdin.
	h.ok(nil, "Two lines\nof text", "reply", "example-proj/r1/coord")
	c := *h.calls
	if len(c) != 2 || !strings.Contains(c[1], "to example-proj/r1/coord: Two lines\nof text") {
		t.Fatalf("node reply: %q", c)
	}

	// Replying to an answer replies about its question.
	q := h.ok(env, "ctx", "ask", "--blocked", "--title", "Which split?")
	a := h.ok(nil, "", "answer", q, "B")
	h.ok(nil, "", "reply", a, "and log it")
	if last := (*h.calls)[len(*h.calls)-1]; !strings.Contains(last, `about `+q+` "Which split?": and log it`) {
		t.Fatalf("reply to an answer: %q", last)
	}

	// Unreachable: recorded, retried by deliver.
	h.respond = func(name string, args []string) (string, error) { return "down", fmt.Errorf("exit 1") }
	_, errOut, code := h.run(nil, "", "reply", tl, "again")
	if code != 0 || !strings.Contains(errOut, "not delivered") {
		t.Fatalf("unreachable reply: %d %q", code, errOut)
	}
	h.respond = func(name string, args []string) (string, error) { return "queued", nil }
	n := len(h.delivered(t))
	h.ok(nil, "", "deliver")
	if len(h.delivered(t)) != n+1 {
		t.Fatal("deliver did not retry the reply")
	}

	// Errors.
	if _, errOut, code := h.run(nil, "", "reply", "example-proj/nowhere", "hi"); code == 0 || !strings.Contains(errOut, "neither an id nor a joined node") {
		t.Fatalf("unknown node: %d %q", code, errOut)
	}
	if _, _, code := h.run(nil, "", "reply", tl, " "); code == 0 {
		t.Fatal("an empty reply was accepted")
	}

	// A takeover redirects replies about older items to the new coordinator.
	h.tmux["%7"] = "work\tmain\t77"
	envB := map[string]string{"AI_AGENT": "codex", "CODEX_THREAD_ID": sessB, "TMUX_PANE": "%7"}
	h.ok(envB, "", "join", "r1/coord", "--takeover")
	h.respond = func(name string, args []string) (string, error) {
		if args[0] == "display-message" {
			return "%7\t0\t77\tcodex\t" + sessB, nil
		}
		return "", nil
	}
	*h.calls = nil
	h.ok(nil, "", "reply", tl, "over to you")
	if !strings.Contains(strings.Join(*h.calls, "\n"), "tmux paste-buffer -p -d -b agent-desk-7 -t %7") {
		t.Fatalf("takeover: %q", *h.calls)
	}

	// The session filter.
	out := h.ok(nil, "", "show", "--all", "--session", uuid, "--format", "md")
	if !strings.Contains(out, "Deploy finished") {
		t.Fatalf("session filter lost the item: %s", out)
	}
	out = h.ok(nil, "", "show", "--all", "--session", "no-such-session", "--format", "md")
	if strings.Contains(out, "Deploy finished") {
		t.Fatalf("session filter kept another session's item: %s", out)
	}
}
