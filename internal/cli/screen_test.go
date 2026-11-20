package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
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
	idle = "1" // touched after A let go: the user's input
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
	idle = "1"
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

// Input while an agent holds the screen is the agent's: an agent granted
// the screen because the user was away, asking again just after its hold
// ran out, finds the user still away, though its own clicks reset the
// machine's idle time.
func TestScreenAgentInputIsNotTheUser(t *testing.T) {
	h := newHarness(t)
	writeConfig(t, h, "[screen]\nidle_command = 'printf IDLE'\nbanner_on = 'printf ON'\nbanner_off = 'printf OFF'\n")
	idle := "4000"
	h.respond = func(name string, args []string) (string, error) {
		if name == "sh" && len(args) == 2 && args[1] == "printf IDLE" {
			return idle + "\n", nil
		}
		return "", nil
	}
	a := h.as(sessA)
	if out := h.ok(a, "", "screen", "take", "--reason", "Filling a form", "--for", "5m"); !strings.Contains(out, "is yours") {
		t.Fatalf("take: %s", out)
	}
	// The hold runs out; the agent clicked 30 s before it did.
	h.now = h.now.Add(5*time.Minute + 20*time.Second)
	idle = "50"
	out := h.ok(a, "", "screen", "take", "--reason", "Submitting the form", "--for", "3m")
	if !strings.Contains(out, "away 1 h") || !strings.Contains(out, "is yours") {
		t.Fatalf("second take: %s", out)
	}
	if out := h.ok(nil, "", "screen", "status"); !strings.Contains(out, "idle 1 h") {
		t.Fatalf("status: %s", out)
	}

	// Released, then the machine is touched: that is the user.
	h.ok(a, "", "screen", "release")
	idle = "1"
	if _, _, code := h.run(a, "", "screen", "take", "--reason", "One more click"); code != ScreenWaiting {
		t.Fatalf("taken while the user was back: %d", code)
	}

	// The request waits; once the user has been away long enough, the
	// server's sweep grants it and answers the request.
	srv, _ := testServer(t, h)
	must(t, srv.sweepScreenOnce())
	if st := h.loadDesk().Screen(h.host); st.Hold != nil || len(st.Requests) != 1 {
		t.Fatalf("granted while the user is here: %+v", st)
	}
	idle = "2000"
	srv, _ = testServer(t, h)
	must(t, srv.sweepScreenOnce())
	d := h.loadDesk()
	st := d.Screen(h.host)
	if st.Hold == nil || st.Hold.By != "auto" || st.Hold.Reason != "One more click" || len(st.Requests) != 0 {
		t.Fatalf("after the sweep: %+v", st)
	}
	if desk.MetaString(st.Hold.Event, "away_since") == "" {
		t.Fatalf("no away_since: %s", st.Hold.Event.Meta)
	}
	answer := d.ByID[d.AnswerID(st.Hold.Event.Ref)]
	if !queuedForDelivery(answer) {
		t.Fatalf("automatic answer was not queued: %+v", answer)
	}
	select {
	case <-srv.sendWake:
	default:
		t.Fatal("automatic answer did not wake the delivery worker")
	}
	if out := h.ok(a, "", "screen", "take", "--reason", "One more click"); !strings.Contains(out, "is yours") {
		t.Fatalf("take after the sweep: %s", out)
	}
}

func TestScreenSweepDoesNotBlockWritesOrOverrideDenial(t *testing.T) {
	h, _, note := queueFixture(t)
	writeConfig(t, h, "[screen]\nidle_command = 'printf IDLE'\n")
	h.respond = func(_ string, _ []string) (string, error) { return "1", nil }
	if _, _, code := h.run(h.as(sessA), "", "screen", "take", "--reason", "Fixture screen request"); code != ScreenWaiting {
		t.Fatalf("screen take: %d", code)
	}
	d := h.loadDesk()
	question := d.Screen(h.host).Requests[0]
	s, hd := testServer(t, h)
	probing := make(chan struct{})
	resume := make(chan struct{})
	s.a.Exec = func(_ context.Context, _ []byte, name string, args ...string) (string, error) {
		if name == "sh" && len(args) == 2 && args[1] == "printf IDLE" {
			close(probing)
			<-resume
			return "2000", nil
		}
		return "", nil
	}
	probeDone := make(chan error, 1)
	go func() { probeDone <- s.sweepScreenOnce() }()
	defer close(resume)
	select {
	case <-probing:
	case <-time.After(3 * time.Second):
		t.Fatal("idle probe did not start")
	}
	writes := make(chan string, 1)
	go func() {
		code, body, _ := do(t, hd, "POST", "/api/read", fmt.Sprintf(`{"target":%q}`, note), dashboard)
		if code != 200 {
			writes <- body
			return
		}
		code, body, _ = do(t, hd, "POST", "/api/answer", fmt.Sprintf(`{"id":%q,"text":"No, keep off the screen."}`, question.ID), dashboard)
		if code != 200 {
			writes <- body
			return
		}
		writes <- ""
	}()
	select {
	case err := <-writes:
		if err != "" {
			t.Fatalf("write during idle probe: %s", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writes waited for the idle probe")
	}
	resume <- struct{}{}
	must(t, <-probeDone)
	if answers := kinds(h, "answer"); len(answers) != 1 || answers[0].Body != "No, keep off the screen." {
		t.Fatalf("sweep overrode a newer decision: %+v", answers)
	}
	if hold := h.loadDesk().Screen(h.host).Hold; hold != nil {
		t.Fatalf("sweep granted a denied request: %+v", hold)
	}
}

// A banner command that hangs (hs with a stuck Hammerspoon) is killed with
// its children after bannerTimeout, so screen take and release still exit.
func TestBannerTimeoutKillsGroup(t *testing.T) {
	a := &App{Home: t.TempDir(), Stderr: io.Discard}
	old := bannerTimeout
	bannerTimeout = 300 * time.Millisecond
	defer func() { bannerTimeout = old }()
	pidf := filepath.Join(a.Home, "child.pid")
	t0 := time.Now()
	if err := a.runBanner("sh", "-c", "sleep 30 & echo $! > "+pidf+"; wait"); err == nil {
		t.Fatal("a hung banner reported success")
	}
	if d := time.Since(t0); d > 3*time.Second {
		t.Fatalf("runBanner returned after %v", d)
	}
	b, err := os.ReadFile(pidf)
	must(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	must(t, err)
	time.Sleep(100 * time.Millisecond)
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("the banner's child %d survived the timeout", pid)
	}
}
