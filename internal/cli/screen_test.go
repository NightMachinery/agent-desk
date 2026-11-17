package cli

import (
	"strings"
	"testing"
)

// The screen lock: granted at once when the user is idle long enough,
// otherwise a request in Needs you that the user approves or denies.
func TestScreen(t *testing.T) {
	h := newHarness(t)
	writeConfig(t, h, "[screen]\nidle_command = 'printf IDLE'\nbanner_on = 'printf ON'\nbanner_off = 'printf OFF'\n")
	idle := "4000" // seconds
	h.respond = func(name string, args []string) (string, error) {
		if name == "sh" && len(args) == 2 && args[1] == "printf IDLE" {
			return idle + "\n", nil
		}
		return "", nil
	}
	called := func(sub string) bool {
		for _, c := range *h.calls {
			if strings.Contains(c, sub) {
				return true
			}
		}
		return false
	}
	a, b := h.as(sessA), h.as(sessB)

	// Idle for over an hour: A gets it at once, with the banner and the nudge.
	out := h.ok(a, "", "screen", "take", "--reason", "Clicking through the settings app", "--for", "20m")
	if !strings.Contains(out, "is yours until") || !strings.Contains(out, "release it now") {
		t.Fatalf("take: %s", out)
	}
	if !called("env AGENT_DESK_REASON=Clicking through the settings app AGENT_DESK_SECONDS=1200 sh -c printf ON") {
		t.Fatalf("banner: %v", *h.calls)
	}
	if out := h.ok(nil, "", "screen", "status"); !strings.Contains(out, "held until") || !strings.Contains(out, "granted auto") {
		t.Fatalf("status: %s", out)
	}
	h.fails(a, "", "--for", "screen", "take", "--reason", "x", "--for", "3h")
	h.fails(a, "", "--reason", "screen", "take")

	// B asks while A holds it: a request in Needs you, exit 3.
	out, _, code := h.run(b, "", "screen", "take", "--reason", "Recording a screencast")
	if code != ScreenWaiting || !strings.Contains(out, "Another agent holds the screen") {
		t.Fatalf("B: %d %s", code, out)
	}
	if out := h.ok(nil, "", "show", "--format", "md"); !strings.Contains(out, "wants to drive the GUI") {
		t.Fatalf("needs you lacks the request:\n%s", out)
	}
	n := h.count()
	if _, _, code := h.run(b, "", "screen", "take", "--reason", "Recording a screencast"); code != ScreenWaiting || h.count() != n {
		t.Fatalf("a second take filed a second request: %d", code)
	}

	// A lets go; the user is at the keyboard, so B still waits.
	if out := h.ok(a, "", "screen", "release"); !strings.Contains(out, "Released") || !called("sh -c printf OFF") {
		t.Fatalf("release: %s", out)
	}
	h.fails(a, "", "do not hold", "screen", "release")
	idle = "12"
	if _, _, code := h.run(b, "", "screen", "take", "--reason", "Recording a screencast"); code != ScreenWaiting {
		t.Fatalf("B took it while the user was active: %d", code)
	}

	// The user approves: B holds it on its next take.
	q := strings.Fields(h.ok(nil, "", "search", "wants to drive"))[0]
	h.ok(nil, "", "screen", "grant", q)
	if out := h.ok(b, "", "screen", "take", "--reason", "Recording a screencast"); !strings.Contains(out, "is yours") {
		t.Fatalf("after grant: %s", out)
	}
	h.ok(b, "", "screen", "release")

	// A asks with the user active; the user answers no in words.
	if _, _, code := h.run(a, "", "screen", "take", "--reason", "Resizing windows"); code != ScreenWaiting {
		t.Fatalf("A: %d", code)
	}
	var qa string
	for _, f := range strings.Split(h.ok(nil, "", "search", "wants to drive"), "\n") {
		if fs := strings.Fields(f); len(fs) > 0 && fs[0] != q {
			qa = fs[0]
		}
	}
	h.ok(nil, "", "answer", qa, "No, I am using it.")
	h.fails(a, "", "denied the screen", "screen", "take", "--reason", "Resizing windows", "--wait", "--timeout", "0s")
	h.fails(nil, "", "not a request for the screen", "screen", "grant", strings.Fields(h.ok(a, "x", "tell", "--title", "t"))[0])

	// The dashboard: the view lists screens; approve and take back.
	idle = "5"
	if _, _, code := h.run(b, "", "screen", "take", "--reason", "Testing the menu bar"); code != ScreenWaiting {
		t.Fatalf("B again: %d", code)
	}
	_, hd := testServer(t, h)
	_, body, _ := do(t, hd, "GET", "/api/view", "", nil)
	if !strings.Contains(body, `"screens":[{"host":"`+h.host+`","held":false`) {
		t.Fatalf("view screens: %s", body[strings.Index(body, `"screens"`):])
	}
	var qb string
	for _, f := range strings.Split(h.ok(nil, "", "search", "wants to drive"), "\n") {
		if fs := strings.Fields(f); len(fs) > 0 && fs[0] != q && fs[0] != qa {
			qb = fs[0]
		}
	}
	if code, body, _ := do(t, hd, "POST", "/api/screen", `{"action":"grant","id":"`+qb+`"}`, dashboard); code != 200 {
		t.Fatalf("api grant: %d %s", code, body)
	}
	_, hd = testServer(t, h)
	if _, body, _ := do(t, hd, "GET", "/api/view", "", nil); !strings.Contains(body, `"held":true`) || !strings.Contains(body, "Testing the menu bar") {
		t.Fatalf("held: %s", body[strings.Index(body, `"screens"`):])
	}
	if code, body, _ := do(t, hd, "POST", "/api/screen", `{"action":"release"}`, dashboard); code != 200 {
		t.Fatalf("api release: %d %s", code, body)
	}
	if out := h.ok(nil, "", "screen", "status"); !strings.Contains(out, "is free") {
		t.Fatalf("after take-back: %s", out)
	}
}
