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

	// A asks with the user active; the user denies it.
	if _, _, code := h.run(a, "", "screen", "take", "--reason", "Resizing windows"); code != ScreenWaiting {
		t.Fatalf("A: %d", code)
	}
	var qa string
	for _, f := range strings.Split(h.ok(nil, "", "search", "wants to drive"), "\n") {
		if fs := strings.Fields(f); len(fs) > 0 && fs[0] != q {
			qa = fs[0]
		}
	}
	h.ok(nil, "", "screen", "deny", qa)
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

// --idle sets how long the user must be away for this take: for the grant
// at once, and (kept on the request) for the server's later grant.
func TestScreenIdleFlag(t *testing.T) {
	h := newHarness(t)
	writeConfig(t, h, "[screen]\nidle_command = 'printf IDLE'\n")
	idle := "400" // 6 min 40 s: under the default 30 minutes
	h.respond = func(name string, args []string) (string, error) {
		if name == "sh" && len(args) == 2 && args[1] == "printf IDLE" {
			return idle + "\n", nil
		}
		return "", nil
	}
	a, b, c := h.as(sessA), h.as(sessB), h.as(sessC)
	h.fails(a, "", "--idle", "screen", "take", "--reason", "x", "--idle", "30s")
	h.fails(a, "", "--idle", "screen", "take", "--reason", "x", "--idle", "soon")

	// Without --idle, 6 min away is not enough, for take or the sweep.
	_, errOut, code := h.run(b, "", "screen", "take", "--reason", "Default wait")
	if code != ScreenWaiting || !strings.Contains(errOut, "away 30 min") {
		t.Fatalf("default take: %d %s", code, errOut)
	}
	srv, _ := testServer(t, h)
	must(t, srv.sweepScreenOnce())
	if st := h.loadDesk().Screen(h.host); st.Hold != nil {
		t.Fatalf("swept at 6 min with the default 30: %+v", st.Hold)
	}
	h.ok(b, "", "screen", "release") // withdraws the request

	// With --idle 5m it is granted at once.
	if out := h.ok(a, "", "screen", "take", "--reason", "Quick click", "--idle", "5m"); !strings.Contains(out, "away 6 min") || !strings.Contains(out, "is yours") {
		t.Fatalf("take --idle 5m: %s", out)
	}
	h.ok(a, "", "screen", "release")

	// The user comes back: still a request, which says 5 min; once the
	// user has been away 5 minutes, the sweep grants it.
	idle = "1"
	_, errOut, code = h.run(c, "", "screen", "take", "--reason", "Later click", "--idle", "5m")
	if code != ScreenWaiting || !strings.Contains(errOut, "away 5 min") {
		t.Fatalf("take --idle 5m with the user back: %d %s", code, errOut)
	}
	st := h.loadDesk().Screen(h.host)
	if len(st.Requests) != 1 || !strings.Contains(st.Requests[0].Body, "away 5 min") {
		t.Fatalf("request: %+v", st.Requests)
	}
	idle = "400"
	srv, _ = testServer(t, h)
	must(t, srv.sweepScreenOnce())
	if st := h.loadDesk().Screen(h.host); st.Hold == nil || st.Hold.By != "auto" || st.Hold.Reason != "Later click" {
		t.Fatalf("sweep with the request's 5 min: %+v", st)
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
		code, body, _ = do(t, hd, "POST", "/api/screen", fmt.Sprintf(`{"action":"deny","id":%q}`, question.ID), dashboard)
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
	if answers := kinds(h, "answer"); len(answers) != 1 || answers[0].Body != "No, not now: keep off the screen." {
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
	if d := time.Since(t0); d > 15*time.Second { // the stack sample takes a few seconds
		t.Fatalf("runBanner returned after %v", d)
	}
	if logs, _ := filepath.Glob(filepath.Join(a.Home, "screen", "hang-*.txt")); len(logs) != 1 {
		t.Fatalf("no stack sample of the hung banner: %v", logs)
	} else if b, _ := os.ReadFile(logs[0]); !strings.Contains(string(b), "sleep 30") {
		t.Fatalf("the sample does not name the hung process:\n%.400s", b)
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

// A request nobody answered lapses after desk.ScreenRequestLife: an agent
// that moved on never withdraws it, and it must not keep later takes from
// the grant the user's absence allows. The next take withdraws it.
func TestScreenRequestLapses(t *testing.T) {
	h := newHarness(t)
	writeConfig(t, h, "[screen]\nidle_command = 'printf IDLE'\n")
	idle := "1"
	h.respond = func(name string, args []string) (string, error) {
		if name == "sh" && len(args) == 2 && args[1] == "printf IDLE" {
			return idle + "\n", nil
		}
		return "", nil
	}
	a, b := h.as(sessA), h.as(sessB)
	if _, _, code := h.run(a, "", "screen", "take", "--reason", "Old ask"); code != ScreenWaiting {
		t.Fatalf("A: %d", code)
	}
	old := h.loadDesk().Screen(h.host).Requests[0].ID

	// An hour later A's request still holds its place. (A takes no more.)
	h.now = h.now.Add(time.Hour)
	if st := h.loadDesk().Screen(h.host); len(st.Requests) != 1 || len(st.Lapsed) != 0 {
		t.Fatalf("after 1 h: %d requests, %d lapsed", len(st.Requests), len(st.Lapsed))
	}
	// Past its life it holds none: with the user away, B gets the screen
	// at once, and A's request leaves Needs you.
	h.now = h.now.Add(desk.ScreenRequestLife)
	idle = "4000"
	if out := h.ok(b, "", "screen", "take", "--reason", "Fresh ask"); !strings.Contains(out, "is yours") {
		t.Fatalf("B behind a lapsed request: %s", out)
	}
	st := h.loadDesk().Screen(h.host)
	if len(st.Requests) != 0 || len(st.Lapsed) != 0 || !st.Holder(sessB) {
		t.Fatalf("after B: %+v", st)
	}
	if out := h.ok(nil, "", "show", "--format", "md"); strings.Contains(out, "Old ask") {
		t.Fatalf("the lapsed request %s is still in Needs you:\n%s", old, out)
	}

	// take --wait with a longer --timeout keeps its request that long.
	h.ok(b, "", "screen", "release")
	idle = "1"
	defer func(p time.Duration) { screenPoll = p }(screenPoll)
	screenPoll = 4 * time.Hour // longer than the timeout: files the request and returns at once
	if _, _, code := h.run(a, "", "screen", "take", "--reason", "Long wait", "--wait", "--timeout", "3h"); code != ScreenWaiting {
		t.Fatalf("A --wait: %d", code)
	}
	h.now = h.now.Add(desk.ScreenRequestLife + time.Minute)
	if st := h.loadDesk().Screen(h.host); len(st.Requests) != 1 {
		t.Fatalf("a 3 h wait's request lapsed at %s: %+v", desk.ScreenRequestLife, st.Requests)
	}
}

// The oldest request is granted once the user is away even where no
// dashboard server runs to sweep: any take on the machine grants it, and
// waits behind it.
func TestScreenTakeServesTheOldestRequest(t *testing.T) {
	h := newHarness(t)
	writeConfig(t, h, "[screen]\nidle_command = 'printf IDLE'\n")
	idle := "1"
	h.respond = func(name string, args []string) (string, error) {
		if name == "sh" && len(args) == 2 && args[1] == "printf IDLE" {
			return idle + "\n", nil
		}
		return "", nil
	}
	a, b := h.as(sessA), h.as(sessB)
	if _, _, code := h.run(a, "", "screen", "take", "--reason", "First ask"); code != ScreenWaiting {
		t.Fatalf("A: %d", code)
	}
	idle = "4000" // the user leaves; A does not take again
	out, _, code := h.run(b, "", "screen", "take", "--reason", "Second ask")
	if code != ScreenWaiting || !strings.Contains(out, "Another agent holds the screen") {
		t.Fatalf("B: %d %s", code, out)
	}
	st := h.loadDesk().Screen(h.host)
	if !st.Holder(sessA) || st.Hold.By != "auto" || len(st.Requests) != 1 {
		t.Fatalf("after B's take: %+v", st)
	}
	if out := h.ok(a, "", "screen", "take", "--reason", "First ask"); !strings.Contains(out, "is yours") {
		t.Fatalf("A after the grant: %s", out)
	}
}

// screen sweep, which a filed request starts in the background, grants the
// oldest request once the user is away and exits when none is left; a
// second sweeper exits at once.
func TestScreenSweeper(t *testing.T) {
	h := newHarness(t)
	writeConfig(t, h, "[screen]\nidle_command = 'printf IDLE'\n")
	idle := "1"
	h.respond = func(name string, args []string) (string, error) {
		if name == "sh" && len(args) == 2 && args[1] == "printf IDLE" {
			return idle + "\n", nil
		}
		return "", nil
	}
	defer func(e time.Duration) { screenSweepEvery = e }(screenSweepEvery)
	screenSweepEvery = time.Millisecond
	a := h.as(sessA)
	if _, _, code := h.run(a, "", "screen", "take", "--reason", "Waiting ask"); code != ScreenWaiting {
		t.Fatalf("A: %d", code)
	}
	idle = "4000"

	// Another sweeper holds the lock: this one leaves the request alone.
	must(t, os.MkdirAll(filepath.Join(h.desk, "screen"), 0o700))
	lock, err := os.OpenFile(filepath.Join(h.desk, "screen", "sweep.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	must(t, err)
	must(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	h.ok(nil, "", "screen", "sweep")
	if st := h.loadDesk().Screen(h.host); st.Hold != nil {
		t.Fatalf("granted under another sweeper's lock: %+v", st.Hold)
	}
	must(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_UN))
	lock.Close()

	h.ok(nil, "", "screen", "sweep")
	if st := h.loadDesk().Screen(h.host); !st.Holder(sessA) || st.Hold.By != "auto" || len(st.Requests) != 0 {
		t.Fatalf("after the sweeper: %+v", st)
	}
}

// The dashboard answers a request in words through the screen API, which
// says what the words decided: a grant, unless they say no.
func TestScreenAnswerSaysWhatItDecided(t *testing.T) {
	h := newHarness(t)
	writeConfig(t, h, "[screen]\nidle_command = 'printf IDLE'\nbanner_on = 'printf ON'\nbanner_off = 'printf OFF'\n")
	h.respond = func(name string, args []string) (string, error) {
		if name == "sh" && len(args) == 2 && args[1] == "printf IDLE" {
			return "1\n", nil // at the machine: a take asks
		}
		return "", nil
	}
	request := func(session, reason string) string {
		if _, _, code := h.run(h.as(session), "", "screen", "take", "--reason", reason); code != ScreenWaiting {
			t.Fatalf("take %s: %d", reason, code)
		}
		for _, f := range strings.Split(h.ok(nil, "", "search", reason), "\n") {
			if fs := strings.Fields(f); len(fs) > 0 && strings.HasPrefix(fs[0], "q-") {
				return fs[0]
			}
		}
		t.Fatalf("no request for %s", reason)
		return ""
	}
	qa := request(sessA, "Resizing windows")
	a, _, _ := h.app(nil, "")
	out, err := a.screenAnswer(qa, "Sure, go ahead.", "web")
	if err != nil || out["decision"] != "grant" || out["until"] == "" {
		t.Fatalf("yes: %v %v", out, err)
	}
	h.ok(h.as(sessA), "", "screen", "release")
	// Words never deny: "no" may open "no problem", or a question.
	qb := request(sessB, "Recording a screencast")
	a, _, _ = h.app(nil, "")
	if out, err = a.screenAnswer(qb, "No problem, but why?", "web"); err != nil || out["decision"] != "grant" {
		t.Fatalf("no: %v %v", out, err)
	}
	a, _, _ = h.app(nil, "")
	if _, err = a.screenAnswer(qb, "  ", "web"); err == nil {
		t.Fatal("an empty answer was taken")
	}
	h.ok(h.as(sessB), "", "screen", "release")

	// Dismissing a request denies it: set aside, it would still be
	// granted once the user is away.
	qc := request(sessA, "Dragging files")
	_, errOut, code := h.run(nil, "", "read", qc)
	if code != 0 || !strings.Contains(errOut, "dismissing it denies it") {
		t.Fatalf("dismiss: %d %s", code, errOut)
	}
	d := h.loadDesk()
	if why, denied := screenDeniedReq(d, mustResolve(t, d, qc)); !denied || !strings.Contains(why, "No, not now") {
		t.Fatalf("not denied: %v %q", denied, why)
	}
	if st := d.Screen(h.host); len(st.Requests) != 0 || st.Hold != nil {
		t.Fatalf("still asking: %+v", st)
	}
}
