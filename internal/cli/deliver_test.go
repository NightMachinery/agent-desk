package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// TestMain lets the test binary act as agent-desk itself, for merge ssh:.
func TestMain(m *testing.M) {
	if os.Getenv("AGENT_DESK_AS_CLI") == "1" {
		app, err := New()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(app.Run(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func (h *harness) delivered(t *testing.T) []store.Event {
	t.Helper()
	st, err := store.Open(h.desk)
	must(t, err)
	defer st.Close()
	evs, err := st.Query("events", "kind = 'delivered'")
	must(t, err)
	return evs
}

func TestDeliverToPaseo(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-000000000001"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	q := h.ok(env, "ctx", "ask", "--blocked", "--title", "Which split?")

	// Unreachable first: the answer is recorded and stays pending.
	_, errOut, code := h.run(nil, "", "answer", q, "B, keep the cache")
	if code != 0 || !strings.Contains(errOut, "not delivered") || !strings.Contains(errOut, "claude --resume") {
		t.Fatalf("unreachable: %d %q", code, errOut)
	}
	if len(h.delivered(t)) != 0 {
		t.Fatal("a failed delivery was marked delivered")
	}
	want := fmt.Sprintf(`paseo send --no-wait %s Answer to %s "Which split?": B, keep the cache`, uuid, q)
	if c := *h.calls; len(c) != 1 || c[0] != want {
		t.Fatalf("calls: %q", c)
	}

	// deliver retries, now that paseo answers.
	h.respond = func(name string, args []string) (string, error) { return "queued", nil }
	h.ok(nil, "", "deliver")
	if d := h.delivered(t); len(d) != 1 || !strings.Contains(string(d[0].Meta), `"via":"paseo"`) {
		t.Fatalf("delivered: %+v", d)
	}
	// Nothing left to deliver; nothing is typed twice.
	n := len(*h.calls)
	h.ok(nil, "", "deliver")
	if len(*h.calls) != n {
		t.Fatalf("delivered twice: %q", (*h.calls)[n:])
	}
	// An answer recorded from chat already reached its session.
	q2 := h.ok(env, "ctx", "ask", "--blocked", "--title", "Second?")
	h.ok(env, "", "answer", q2, "--from-chat", "yes")
	h.ok(nil, "", "deliver")
	if len(*h.calls) != n {
		t.Fatalf("a chat answer was delivered: %q", (*h.calls)[n:])
	}
}

func TestDeliverToTmux(t *testing.T) {
	h := newHarness(t)
	h.tmux["%5"] = "work\tmain\t4242"
	env := map[string]string{"AI_AGENT": "codex", "CODEX_THREAD_ID": sessB, "TMUX_PANE": "%5"}
	h.ok(env, "", "join", "r1/coord")
	q := h.ok(env, "ctx", "ask", "--default", "seed 7", "--title", "Which seed?")

	pid := "4242"
	h.respond = func(name string, args []string) (string, error) {
		if args[0] == "display-message" {
			return "%5\t0\t" + pid + "\tcodex\t" + sessB + "\t/tmp/example.jsonl", nil
		}
		return "", nil
	}
	h.ok(nil, "", "answer", q, "seed 3\nand log it")
	c := strings.Join(*h.calls, "\n")
	for _, want := range []string{
		"tmux load-buffer -b agent-desk-5 - <<< Answer to " + q + ` "Which seed?": seed 3` + "\nand log it",
		"tmux paste-buffer -p -d -b agent-desk-5 -t %5",
		"tmux send-keys -t %5 Enter",
	} {
		if !strings.Contains(c, want) {
			t.Fatalf("missing %q in:\n%s", want, c)
		}
	}
	if len(h.delivered(t)) != 1 {
		t.Fatal("not marked delivered")
	}

	// A pane now running another process is not typed into.
	q2 := h.ok(env, "ctx", "ask", "--blocked", "--title", "Again?")
	pid = "999"
	*h.calls = nil
	_, errOut, _ := h.run(nil, "", "answer", q2, "no")
	if !strings.Contains(errOut, "another process") {
		t.Fatalf("reused pane: %q", errOut)
	}
	for _, c := range *h.calls {
		if strings.Contains(c, "paste-buffer") {
			t.Fatal("typed into a reused pane")
		}
	}
}

func TestDeliverCommand(t *testing.T) {
	h := newHarness(t)
	log := filepath.Join(t.TempDir(), "delivered.jsonl")
	cfg := filepath.Join(h.home, ".config", "agent-desk", "config.toml")
	must(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
	must(t, os.WriteFile(cfg, []byte(`deliver_command = "cat >> '`+log+`'"`), 0o644))
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	q := h.ok(a, "ctx", "ask", "--blocked", "--title", "Which?")
	h.ok(nil, "", "answer", q, "A")
	got := readHookLog(t, log)
	if len(got) != 1 || got[0].Action != "deliver" || got[0].Text != `Answer to `+q+` "Which?": A` ||
		got[0].Event.Kind != "ask" || got[0].Cause.Kind != "answer" {
		t.Fatalf("deliver_command payload: %+v", got)
	}
	if len(h.delivered(t)) != 1 {
		t.Fatal("not marked delivered")
	}
}

func TestMergeSSHDeliversRemoteAnswers(t *testing.T) {
	h := newHarness(t)
	self, err := os.Executable()
	must(t, err)
	remoteHome := filepath.Join(t.TempDir(), "remote-desk")
	// A fake ssh: drop "-o BatchMode=yes host" and run the remote command.
	fake := filepath.Join(t.TempDir(), "fake-ssh")
	must(t, os.WriteFile(fake, []byte("#!/bin/sh\nshift 3\nexec sh -c \"$1\"\n"), 0o755))
	sshEnv := map[string]string{
		"AGENT_DESK_SSH": fake,
		"AGENT_DESK_REMOTE_COMMAND": "AGENT_DESK_AS_CLI=1 AGENT_DESK_HOME='" + remoteHome +
			"' AGENT_DESK_HOST=host-b AGENT_DESK_CONFIG=/nonexistent '" + self + "'",
	}

	// The remote machine has a question; this one has a notice.
	hb := *h
	hb.desk, hb.host = remoteHome, "host-b"
	hb.ok(h.as(sessB), "", "join", "far/coord")
	q := hb.ok(h.as(sessB), "ctx", "ask", "--blocked", "--title", "Remote question?")
	h.ok(h.as(sessA), "", "join", "near/coord")
	h.ok(h.as(sessA), "body", "tell", "--title", "Local notice")

	_, errOut, code := h.run(sshEnv, "", "merge", "ssh:other")
	if code != 0 {
		t.Fatalf("merge ssh: %s", errOut)
	}
	if h.ok(nil, "", "export") != hb.ok(nil, "", "export") {
		t.Fatal("stores differ after merge ssh")
	}
	// Answered here, the remote question is delivered there after the next
	// merge; the remote side's delivery fails (no reply address) but runs.
	_, errOut, _ = h.run(nil, "", "answer", q, "yes")
	if !strings.Contains(errOut, "asked on host-b") {
		t.Fatalf("remote answer: %q", errOut)
	}
	_, errOut, code = h.run(sshEnv, "", "merge", "ssh:other")
	if code != 0 || !strings.Contains(errOut, "other: agent-desk: a-") || !strings.Contains(errOut, "not delivered") {
		t.Fatalf("second merge: %d %q", code, errOut)
	}
	if h.ok(nil, "", "export") != hb.ok(nil, "", "export") {
		t.Fatal("stores differ after the second merge")
	}
}
