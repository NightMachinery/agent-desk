package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/desk"
)

// Paseo 0.15.4's help, which cannot queue, and the same with queue.
const helpNoQueue = `Options:
  --image <path>         Attach image(s) to the message (default: [])
  --behavior <behavior>  Active-turn behavior (agent default: steer; otherwise:
                         interrupt) (choices: "interrupt", "steer")
  --no-wait              Return immediately without waiting for completion`

var helpQueue = strings.Replace(helpNoQueue, `"interrupt", "steer")`, `"interrupt", "steer",
                         "queue")`, 1)

func TestHelpListsQueue(t *testing.T) {
	if helpListsQueue(helpNoQueue) || !helpListsQueue(helpQueue) || helpListsQueue("") {
		t.Fatal("helpListsQueue misread the help")
	}
}

// fakePaseo answers paseo send: its help, and a receipt per behaviour.
func fakePaseo(help string, receipts map[string]string, fail map[string]bool) func(string, []string) (string, error) {
	return func(name string, args []string) (string, error) {
		if args[len(args)-1] == "--help" {
			return help, nil
		}
		for i, a := range args {
			if a == "--behavior" {
				b := args[i+1]
				if fail[b] {
					return "unsupported behavior", errors.New("exit 1")
				}
				return receipts[b], nil
			}
		}
		return "", nil
	}
}

func lastDelivered(t *testing.T, h *harness) map[string]any {
	t.Helper()
	ds := h.delivered(t)
	if len(ds) == 0 {
		t.Fatal("nothing delivered")
	}
	return desk.Meta(ds[len(ds)-1])
}

func TestBehaviors(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-000000000004"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	tl := h.ok(env, "Live.", "tell", "--title", "Deploy finished")
	sent := func() string { c := *h.calls; return c[len(c)-1] }

	// A Paseo that cannot queue: the message is steered, and says why.
	h.respond = fakePaseo(helpNoQueue, map[string]string{"steer": `{"status":"sent"}`}, nil)
	h.ok(nil, "", "reply", tl, "--behavior", "queue", "after this turn")
	if !strings.Contains(sent(), "--behavior steer") {
		t.Fatalf("queue without support: %q", sent())
	}
	m := lastDelivered(t, h)
	if m["behavior"] != "steer" || m["fallback"] != "queue unsupported" {
		t.Fatalf("fallback meta: %v", m)
	}
	_, hd := testServer(t, h)
	code, body, _ := do(t, hd, "GET", "/api/target?to="+tl, "", nil)
	var tg Target
	must(t, json.Unmarshal([]byte(body), &tg))
	if code != 200 || len(tg.Behaviors) != 3 || tg.Behaviors[1].Name != "queue" || tg.Behaviors[1].Supported ||
		tg.Behaviors[1].Why == "" || !tg.Behaviors[2].Supported || tg.DefaultBehavior != "steer" {
		t.Fatalf("target: %d %s", code, body)
	}

	// One that can: queued, with its position.
	h.respond = fakePaseo(helpQueue, map[string]string{"queue": `{"status":"queued","disposition":"queued","queuePosition":2}`}, nil)
	h.ok(nil, "", "reply", tl, "--behavior", "queue", "after this turn")
	if !strings.Contains(sent(), "--behavior queue") {
		t.Fatalf("queue: %q", sent())
	}
	if m = lastDelivered(t, h); m["disposition"] != "queued" || m["queue_position"] != float64(2) || m["fallback"] != nil {
		t.Fatalf("queued meta: %v", m)
	}
	_, body, _ = do(t, hd, "GET", "/api/target?to="+tl, "", nil)
	tg = Target{}
	must(t, json.Unmarshal([]byte(body), &tg))
	if !tg.Behaviors[1].Supported {
		t.Fatalf("queue support not reported: %s", body)
	}

	// The CLI says queue but the daemon refuses it: steered once, noted.
	h.respond = fakePaseo(helpQueue, map[string]string{"steer": `{"disposition":"steered"}`}, map[string]bool{"queue": true})
	n := len(*h.calls)
	h.ok(nil, "", "reply", tl, "--behavior", "queue", "x")
	if c := (*h.calls)[n:]; len(c) != 2 || !strings.Contains(c[1], "--behavior steer") {
		t.Fatalf("daemon fallback: %q", c)
	}
	if m = lastDelivered(t, h); m["fallback"] != "queue unsupported by the Paseo daemon" || m["disposition"] != "steered" {
		t.Fatalf("daemon fallback meta: %v", m)
	}

	// Interrupt is a per-message choice and works.
	h.respond = fakePaseo(helpNoQueue, map[string]string{"interrupt": `{"disposition":"interrupted"}`}, nil)
	h.ok(nil, "", "reply", tl, "--behavior", "interrupt", "stop")
	if !strings.Contains(sent(), "--behavior interrupt") || lastDelivered(t, h)["disposition"] != "interrupted" {
		t.Fatalf("interrupt: %q", sent())
	}
	h.fails(nil, "", "--behavior is steer, queue or interrupt", "reply", tl, "--behavior", "shout", "x")

	// The requested behaviour is on the message; answers take it too.
	d := h.loadDesk()
	var last map[string]any
	for _, e := range d.Events {
		if e.Kind == "reply" {
			last = desk.Meta(e)
		}
	}
	if last["behavior"] != "interrupt" {
		t.Fatalf("reply meta: %v", last)
	}
	q := h.ok(env, "ctx", "ask", "--blocked", "--title", "Which?")
	h.respond = fakePaseo(helpQueue, map[string]string{"queue": `{"disposition":"turn_started"}`}, nil)
	h.ok(nil, "", "answer", q, "--behavior", "queue", "B")
	if !strings.Contains(sent(), "--behavior queue") || lastDelivered(t, h)["disposition"] != "turn_started" {
		t.Fatalf("answer queue: %q", sent())
	}
	h.fails(nil, "", "one given in chat is not delivered", "answer", q, "--from-chat", "B", "--behavior", "queue")
}

func TestTmuxBehaviors(t *testing.T) {
	h := newHarness(t)
	saved := interruptSettle
	interruptSettle = 0
	t.Cleanup(func() { interruptSettle = saved })
	h.tmux["%7"] = "work\tmain\t77"
	envClaude := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessB, "TMUX_PANE": "%7"}
	h.ok(envClaude, "", "join", "r1/coord")
	tl := h.ok(envClaude, "Live.", "tell", "--title", "Deploy finished")
	h.respond = func(name string, args []string) (string, error) {
		if args[0] == "display-message" {
			return "%7\t0\t77\tclaude-code\t" + sessB, nil
		}
		return "", nil
	}
	*h.calls = nil
	h.ok(nil, "", "reply", tl, "--behavior", "interrupt", "stop")
	all := strings.Join(*h.calls, "\n")
	esc, paste := strings.Index(all, "send-keys -t %7 Escape"), strings.Index(all, "paste-buffer")
	if esc < 0 || paste < esc {
		t.Fatalf("interrupt in tmux: %q", *h.calls)
	}
	if m := lastDelivered(t, h); m["behavior"] != "interrupt" || m["disposition"] != "interrupted" {
		t.Fatalf("tmux interrupt meta: %v", m)
	}
	*h.calls = nil
	h.ok(nil, "", "reply", tl, "--behavior", "queue", "later")
	if strings.Contains(strings.Join(*h.calls, "\n"), "Escape") {
		t.Fatal("queue sent Escape")
	}
	if m := lastDelivered(t, h); m["behavior"] != "steer" || m["fallback"] != "queue unsupported" || m["disposition"] != "typed" {
		t.Fatalf("tmux queue meta: %v", m)
	}
	if !tmuxInterruptible("claude") || !tmuxInterruptible("codex") || tmuxInterruptible("agy") {
		t.Fatal("tmuxInterruptible")
	}
}
