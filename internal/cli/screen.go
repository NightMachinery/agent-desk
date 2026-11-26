package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/NightMachinery/agent-desk/internal/config"
	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// The screen lock (desk.Screen): an agent takes it before it drives this
// machine's GUI, so the user knows to keep off the keyboard. It is granted
// at once when the user has been idle long enough ([screen] idle_minutes,
// default 30, or the take's own --idle); otherwise the request waits in
// Needs you, where the user may approve it at any time. Once the user has
// been away long enough, the oldest request is granted unasked: by the
// dashboard server's sweep, or by any take on the machine, since a machine
// may run no server. An unanswered request lapses after
// desk.ScreenRequestLife.

const (
	screenDefault = 30 * time.Minute
	screenMax     = 2 * time.Hour
	// screenIdleMin is the shortest --idle: under a minute, a user pausing
	// to read would count as away.
	screenIdleMin = time.Minute
)

// screenPoll is how often `screen take --wait` looks again.
var screenPoll = 15 * time.Second

// screenSweepEvery is how often the background sweeper (screen sweep) looks.
var screenSweepEvery = time.Minute

// exitError ends the command with its own exit status.
type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }

// ScreenWaiting is the exit status of a take that has asked and not yet got it.
const ScreenWaiting = 3

const screenNudge = `Hold the screen only while your automation takes focus or needs the user away from the keyboard. If it can run headless, in the background or without focus, release it now. Release it the moment you are done:
    agent-desk screen release`

func (a *App) cmdScreen(args []string) error {
	if len(args) == 0 {
		return usagef("screen take|release|status|grant|deny|sweep; see agent-desk help")
	}
	rest := args[1:]
	switch args[0] {
	case "take":
		return a.screenTake(rest)
	case "release":
		return a.screenRelease(rest)
	case "status":
		return a.screenStatus(rest)
	case "sweep":
		if _, err := parse(rest, nil, nil, 0, 0); err != nil {
			return err
		}
		return a.screenSweeper()
	case "grant", "deny":
		o, err := parse(rest, nil, nil, 1, 1)
		if err != nil {
			return err
		}
		return a.screenDecide(o.pos[0], args[0] == "grant", "cli")
	}
	return usagef("unknown screen command %q: take, release, status, grant or deny", args[0])
}

func (a *App) screenTake(args []string) error {
	o, err := parse(args, []string{"wait"}, []string{"reason", "for", "timeout", "idle"}, 0, 0)
	if err != nil {
		return err
	}
	reason := strings.TrimSpace(o.vals["reason"])
	if reason == "" {
		return usagef("screen take needs --reason 'what you will do on the screen'")
	}
	hold := screenDefault
	if s := o.vals["for"]; s != "" {
		if hold, err = time.ParseDuration(s); err != nil || hold <= 0 || hold > screenMax {
			return usagef("--for is a duration up to %s, such as 20m", screenMax)
		}
	}
	timeout := time.Hour
	if s := o.vals["timeout"]; s != "" {
		if timeout, err = time.ParseDuration(s); err != nil || timeout < 0 {
			return usagef("--timeout is a duration, such as 45m")
		}
	}
	from := ident.Resolve(a.Env)
	if from.Session == "" {
		return errors.New("cannot tell which agent session this is; only agent sessions take the screen")
	}
	c, err := a.config()
	if err != nil {
		return err
	}
	// away: how long the user must have been idle for a grant without asking.
	away := time.Duration(c.Screen.Idle()) * time.Minute
	if s := o.vals["idle"]; s != "" {
		if away, err = time.ParseDuration(s); err != nil || away < screenIdleMin {
			return usagef("--idle is a duration of at least %s, such as 5m", humanDur(screenIdleMin))
		}
	}
	wait := time.Duration(0) // how long this take waits: its request lives at least that long
	if o.bools["wait"] {
		wait = timeout
	}
	mine := "" // this session's request, once filed or found
	for waited := time.Duration(0); ; waited += screenPoll {
		d, err := a.load()
		if err != nil {
			return err
		}
		st := d.Screen(a.Host)
		if marks := a.lapseMarks(st, from); len(marks) > 0 {
			if err := a.commit(marks, "-"); err != nil {
				return err
			}
		}
		if st.Holder(from.Session) {
			// Granted (perhaps by the user) or renewed: start the banner.
			request := st.Hold.Event.Ref
			if !st.Hold.Until.After(d.Now.Add(time.Minute)) || st.Hold.Reason != reason {
				g := a.screenGrant(from, from.Session, reason, hold, st.Hold.By, request)
				if err := a.commit([]store.Event{g}, "-"); err != nil {
					return err
				}
				st.Hold.Until = d.Now.Add(hold)
			}
			return a.screenGranted(c, reason, st.Hold.Until, st.Hold.By)
		}
		req, has := st.Request(from.Session)
		if has {
			mine = req.ID
		}
		if mine != "" {
			if why, denied := screenDeniedReq(d, mine); denied {
				return fmt.Errorf("the user denied the screen: %s", firstLine(why))
			}
		}
		if st.Hold == nil && (len(st.Requests) == 0 || (has && st.Requests[0].ID == req.ID)) {
			// Free, and nobody asked before this session: granted if the
			// user is away.
			idle, known := a.userAway(c, d)
			if known && idle >= away {
				g := a.screenGrant(from, from.Session, reason, hold, "auto", req.ID, d.Now.Add(-idle))
				evs := []store.Event{g}
				if has {
					evs = append(evs, a.readMark(req, from, "screen")) // out of Needs you
				}
				if err := a.commit(evs, "-"); err != nil {
					return err
				}
				fmt.Fprintf(a.Stdout, "The user has been away %s, so the screen is yours.\n", humanDur(idle))
				return a.screenGranted(c, reason, d.Now.Add(hold), "auto")
			}
		}
		if st.Hold == nil && len(st.Requests) > 0 && !(has && st.Requests[0].ID == req.ID) {
			// Free, but another session asked first: grant it the screen
			// if the user is away long enough, as the server's sweep would
			// (this machine may run no server), and wait behind it.
			if granted, err := a.screenServeOldest(); err != nil {
				return err
			} else if granted {
				continue
			}
		}
		if !has && mine == "" {
			if why, when, ok := screenLastDenial(d, from.Session); ok && d.Now.Sub(when) < screenDenyQuiet {
				return fmt.Errorf("the user denied the screen %s ago: %s; ask again after %s, or ask the user why",
					humanDur(d.Now.Sub(when)), firstLine(why), humanDur(screenDenyQuiet))
			}
			q := a.screenRequest(d, from, reason, hold, away, wait)
			if err := a.commit([]store.Event{q}, "-"); err != nil {
				return err
			}
			mine = q.ID
			a.startSweeper()
			if d, err = a.load(); err != nil {
				return err
			}
			if st.Hold != nil {
				fmt.Fprintf(a.Stdout, "Another agent holds the screen until %s (%s); asked the user (%s).\n",
					st.Hold.Until.Local().Format("15:04"), st.Hold.Reason, d.Display(q.ID))
			} else {
				fmt.Fprintf(a.Stdout, "Asked the user for the screen (%s): they are at the machine, so it needs their approval.\n", d.Display(q.ID))
			}
		}
		if !o.bools["wait"] || waited+screenPoll > timeout {
			return exitError{ScreenWaiting, "screen: not yet yours. The answer reaches you as a message; then run screen take again" +
				" (or wait for it with --wait). Once the user has been away " + humanDur(away) +
				", the oldest request is granted without asking. Unanswered, the request lapses after " +
				humanDur(max(desk.ScreenRequestLife, wait)) + "."}
		}
		time.Sleep(screenPoll)
	}
}

// screenGrant is a grant to session.
// awaySince, for an automatic grant, is when the user was last at the
// machine.
func (a *App) screenGrant(from ident.From, holder, reason string, hold time.Duration, by, request string, awaySince ...time.Time) store.Event {
	m := map[string]any{
		"action": "grant", "holder": holder, "reason": reason, "by": by,
		"until": a.Now().Add(hold).UTC().Format(time.RFC3339),
	}
	if len(awaySince) > 0 {
		m["away_since"] = awaySince[0].UTC().Format(time.RFC3339)
	}
	if request != "" {
		m["request"] = request
	}
	e := a.event("screen", desk.ScreenPath(a.Host), from, "cli", m)
	e.Ref = request
	return e
}

// screenRequest is the question that asks the user for the screen. away is
// how long the user must be idle for the server to grant it unasked; wait,
// how long a take --wait will keep asking (0 without --wait), which the
// request lives at least as long as.
func (a *App) screenRequest(d *desk.Desk, from ident.From, reason string, hold, away, wait time.Duration) store.Event {
	who := from.Title
	if who == "" {
		who = from.Agent + " " + short8(from.Session)
	}
	sm := map[string]any{"host": a.Host, "reason": reason, "for": hold.String(), "minutes": int(hold.Minutes()), "idle": away.String()}
	if wait > 0 {
		sm["wait"] = wait.String()
	}
	q := a.event("ask", desk.ScreenPath(a.Host), from, "cli", map[string]any{"screen": sm})
	q.Level = "blocked"
	q.Title = fmt.Sprintf("Screen on %s: %s wants to drive the GUI", a.Host, who)
	q.Body = fmt.Sprintf("%s\n\nFor up to %s. Approve to hand it the screen now (a banner shows while it holds it), or deny. "+
		"It is granted without asking once you have been away %s.", reason, humanDur(hold), humanDur(away))
	return q
}

// bannerTimeout bounds banner_on and banner_off. Hammerspoon's hs command
// can hang when its IPC port is stuck, and an unbounded wait left screen take
// and release processes behind for days.
var bannerTimeout = 15 * time.Second

// runBanner runs a banner command for at most bannerTimeout, then kills it
// with its children (killGroupOnCancel), so the screen command always exits.
func (a *App) runBanner(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), bannerTimeout)
	defer cancel()
	if a.Exec != nil {
		_, err := a.Exec(ctx, nil, name, args...)
		return err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = envFor(name, os.Environ())
	cmd.WaitDelay = 2 * time.Second
	killGroupOnCancel(cmd)
	kill := cmd.Cancel
	var hung string
	cmd.Cancel = func() error {
		// Record where the hung command is stuck before killing it: the
		// cause of these hangs (hs, Hammerspoon's CLI) is not known yet.
		hung = a.sampleGroup(cmd.Process.Pid)
		return kill()
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		if hung != "" {
			return fmt.Errorf("gave up after %s; stacks of the hung processes are in %s", bannerTimeout, hung)
		}
		return fmt.Errorf("gave up after %s", bannerTimeout)
	}
	if err != nil && len(out) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return err
}

// screenGranted starts the banner and tells the agent the rules.
func (a *App) screenGranted(c config.Config, reason string, until time.Time, by string) error {
	secs := int(until.Sub(a.Now()).Seconds())
	if c.Screen.BannerOn != "" {
		if err := a.runBanner("env",
			"AGENT_DESK_REASON="+reason, "AGENT_DESK_SECONDS="+strconv.Itoa(secs), "sh", "-c", c.Screen.BannerOn); err != nil {
			fmt.Fprintf(a.Stderr, "agent-desk: warning: banner_on: %v\n", err)
		}
	}
	how := map[string]string{"auto": "the user is away", "user": "the user approved it"}[by]
	if how == "" {
		how = "renewed"
	}
	fmt.Fprintf(a.Stdout, "The screen on %s is yours until %s (%s).\n%s\n", a.Host, until.Local().Format("15:04"), how, screenNudge)
	return nil
}

func (a *App) screenRelease(args []string) error {
	if _, err := parse(args, nil, nil, 0, 0); err != nil {
		return err
	}
	from := ident.Resolve(a.Env)
	d, err := a.load()
	if err != nil {
		return err
	}
	st := d.Screen(a.Host)
	if !st.Holder(from.Session) {
		// Also withdraw an open request.
		if req, ok := st.Request(from.Session); ok {
			if err := a.commit([]store.Event{a.readMark(req, from, "screen")}, "-"); err != nil {
				return err
			}
			fmt.Fprintln(a.Stdout, "Withdrew your request for the screen.")
			return nil
		}
		return errors.New("you do not hold the screen on " + a.Host)
	}
	return a.screenLetGo(from, st, "agent")
}

// screenLetGo releases the screen and takes the banner down.
func (a *App) screenLetGo(from ident.From, st desk.ScreenState, by string) error {
	e := a.event("screen", desk.ScreenPath(a.Host), from, "cli", map[string]any{"action": "release", "by": by})
	e.Ref = st.Hold.Event.ID
	if err := a.commit([]store.Event{e}, "-"); err != nil {
		return err
	}
	c, err := a.config()
	if err == nil && c.Screen.BannerOff != "" && st.Host == a.Host {
		if err := a.runBanner("sh", "-c", c.Screen.BannerOff); err != nil {
			fmt.Fprintf(a.Stderr, "agent-desk: warning: banner_off: %v\n", err)
		}
	}
	fmt.Fprintf(a.Stdout, "Released the screen on %s.\n", a.Host)
	return nil
}

func (a *App) screenStatus(args []string) error {
	o, err := parse(args, nil, []string{"host"}, 0, 0)
	if err != nil {
		return err
	}
	host := o.vals["host"]
	if host == "" {
		host = a.Host
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	st := d.Screen(host)
	if st.Hold == nil {
		fmt.Fprintf(a.Stdout, "The screen on %s is free.\n", host)
	} else {
		fmt.Fprintf(a.Stdout, "The screen on %s is held until %s (%s, granted %s): %s\n", host,
			st.Hold.Until.Local().Format("15:04"), short8(st.Hold.Session), st.Hold.By, st.Hold.Reason)
	}
	for _, q := range st.Requests {
		fmt.Fprintf(a.Stdout, "Asking: %s %s\n", d.Display(q.ID), q.Title)
	}
	if c, err := a.config(); err == nil && host == a.Host {
		if idle, ok := a.userAway(c, d); ok {
			fmt.Fprintf(a.Stdout, "The user has been idle %s (granted without asking from %d minutes).\n", humanDur(idle), c.Screen.Idle())
		}
	}
	return nil
}

// screenDecide grants or denies a request (a screen question) as the user,
// and answers it, so its agent hears at once.
func (a *App) screenDecide(token string, grant bool, source string) error {
	d, err := a.load()
	if err != nil {
		return err
	}
	q, err := d.Resolve(token)
	if err != nil {
		return err
	}
	if q.Kind != "ask" || desk.Meta(q)["screen"] == nil {
		return usagef("%s is not a request for the screen", d.Display(q.ID))
	}
	text := "No, not now: keep off the screen."
	if grant {
		text = "Approved: the screen is yours. Run `agent-desk screen take` again to put up the banner, and release it as soon as you are done."
	}
	return a.recordAnswer(d, q, text, source, "", nil)
}

// screenDecision is the screen event an answer to a screen request implies:
// a grant, unless the answer says no.
var screenNo = regexp.MustCompile(`(?i)^\s*(no|deny|denied|not now|don'?t|do not)\b`)

// screenSweep prepares an automatic answer to the oldest waiting request.
// The server checks the screen path again when it commits the grant, since
// sampling the user's idle time may take a while.
func (a *App) screenSweep() ([]store.Event, int, error) {
	c, err := a.config()
	if err != nil {
		return nil, 0, err
	}
	d, err := a.load()
	if err != nil {
		return nil, 0, err
	}
	st := d.Screen(a.Host)
	if st.Hold != nil || len(st.Requests) == 0 {
		return nil, 0, nil
	}
	q := st.Requests[0]
	away, ok := a.userAway(c, d)
	if !ok || away < requestIdle(q, c) {
		return nil, 0, nil
	}
	text := fmt.Sprintf("Approved automatically: the user has been away %s, so the screen is yours. "+
		"Run `agent-desk screen take` again to put up the banner, and release it as soon as you are done.", humanDur(away))
	events, err := a.answerEvents(d, q, text, "auto", "", nil)
	if err != nil {
		return nil, 0, err
	}
	count := 0
	for _, event := range d.Events {
		if event.Path == q.Path {
			count++
		}
	}
	if len(events) > 1 {
		grant := desk.Meta(events[1])
		grant["away_since"] = d.Now.Add(-away).UTC().Format(time.RFC3339)
		events[1].Meta = meta(grant)
	}
	return events, count, nil
}

// lapseMarks withdraw the requests of st that outlived
// desk.ScreenRequestLife, so they leave Needs you.
func (a *App) lapseMarks(st desk.ScreenState, from ident.From) []store.Event {
	var marks []store.Event
	for _, q := range st.Lapsed {
		marks = append(marks, a.readMark(q, from, "screen-lapsed"))
	}
	return marks
}

// screenServeOldest grants the oldest waiting request, as the server's
// sweep does, when the user has been away long enough for it, and tells
// its agent. granted reports whether it did. The guarded append keeps it
// from granting twice when a sweep runs at the same moment.
func (a *App) screenServeOldest() (granted bool, err error) {
	events, count, err := a.screenSweep()
	if err != nil || len(events) == 0 {
		return false, err
	}
	st, err := a.store()
	if err != nil {
		return false, err
	}
	if granted, err = st.AppendScreenSweep(events, count); err != nil || !granted {
		return false, err
	}
	a.announce(events, false, false)
	if events[0].Ref != "" {
		if _, err := a.deliver(events[0].Ref); err != nil {
			fmt.Fprintf(a.Stderr, "agent-desk: telling the agent its screen request was granted: %v\n", err)
		}
	}
	return true, nil
}

// startSweeper starts `screen sweep` in the background, so the request just
// filed is granted once the user is away even when no dashboard server runs
// on this machine and its agent does not take again. Tests (WaitHooks) skip
// it: run in-process, it would wait for as long as the request lives.
func (a *App) startSweeper() {
	if a.WaitHooks {
		return
	}
	if err := a.spawn([]string{"screen", "sweep"}); err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: warning: starting the screen sweeper: %v\n", err)
	}
}

// screenSweeper is `screen sweep`: every screenSweepEvery, withdraw lapsed
// requests and grant the oldest one once the user is away long enough, until
// no request is left. One per desk (screen/sweep.lock); a second exits.
func (a *App) screenSweeper() error {
	dir := filepath.Join(a.Home, "screen")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "sweep.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil // another sweeper runs
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	for {
		d, err := a.load()
		if err != nil {
			return err
		}
		st := d.Screen(a.Host)
		if marks := a.lapseMarks(st, ident.From{}); len(marks) > 0 {
			if err := a.commit(marks, "-"); err != nil {
				return err
			}
		}
		if len(st.Requests) == 0 {
			return nil
		}
		if st.Hold == nil {
			if _, err := a.screenServeOldest(); err != nil {
				return err
			}
		}
		time.Sleep(screenSweepEvery)
	}
}

// requestIdle is how long the user must be away before screen request q is
// granted unasked: its take's --idle, else [screen] idle_minutes.
func requestIdle(q store.Event, c config.Config) time.Duration {
	if sm, ok := desk.Meta(q)["screen"].(map[string]any); ok {
		if d, err := time.ParseDuration(fmt.Sprint(sm["idle"])); err == nil && d >= screenIdleMin {
			return d
		}
	}
	return time.Duration(c.Screen.Idle()) * time.Minute
}

func (a *App) screenDecision(q store.Event, answer, source string) (store.Event, bool) {
	sm, ok := desk.Meta(q)["screen"].(map[string]any)
	if !ok {
		return store.Event{}, false
	}
	from := ident.Resolve(a.Env)
	host, _ := sm["host"].(string)
	path := desk.ScreenPath(host)
	if screenNo.MatchString(answer) {
		e := a.event("screen", path, from, "answer", map[string]any{"action": "deny", "request": q.ID})
		e.Ref = q.ID
		return e, true
	}
	hold, err := time.ParseDuration(fmt.Sprint(sm["for"]))
	if err != nil || hold <= 0 || hold > screenMax {
		hold = screenDefault
	}
	reason, _ := sm["reason"].(string)
	m := map[string]any{
		"action": "grant", "holder": sessionOf(q), "reason": reason, "by": "user", "request": q.ID,
		"until": a.Now().Add(hold).UTC().Format(time.RFC3339),
	}
	if source == "auto" {
		m["by"] = "auto"
	}
	e := a.event("screen", path, from, "answer", m)
	e.Ref = q.ID
	return e, true
}

// screenDenyQuiet is how long after a denial a session may not ask again.
const screenDenyQuiet = 10 * time.Minute

// screenLastDenial is the denial of session's latest request, if it was denied.
func screenLastDenial(d *desk.Desk, session string) (string, time.Time, bool) {
	last := ""
	for _, q := range d.Questions {
		if strings.HasPrefix(q.Path, "_desk/screen/") && sessionOf(q.Event) == session {
			last = q.ID
		}
	}
	if last == "" {
		return "", time.Time{}, false
	}
	for _, e := range d.Events {
		if e.Kind == "screen" && e.Ref == last && desk.MetaString(e, "action") == "deny" {
			why, _ := screenDeniedReq(d, last)
			return why, desk.Time(e), true
		}
	}
	return "", time.Time{}, false
}

// screenDeniedReq reports whether request id was denied, with the answer.
func screenDeniedReq(d *desk.Desk, id string) (string, bool) {
	for _, e := range d.Events {
		if e.Kind == "screen" && e.Ref == id && desk.MetaString(e, "action") == "deny" {
			for _, q := range d.Questions {
				if q.ID == id && q.AnswerBody != "" {
					return q.AnswerBody, true
				}
			}
			return "denied", true
		}
	}
	return "", false
}

// userAway is how long the user has been away from this machine. Input
// while an agent held the screen was the agent's, not the user's: an agent
// that held the screen and asks again a minute later finds the user as long
// away as when it was first granted, not freshly back.
func (a *App) userAway(c config.Config, d *desk.Desk) (time.Duration, bool) {
	idle, ok := a.userIdle(c)
	if !ok {
		return 0, false
	}
	last := d.Now.Add(-idle)
	if user, held := d.ScreenUserLast(a.Host, last, time.Duration(c.Screen.Idle())*time.Minute); held && user.Before(last) {
		return d.Now.Sub(user), true
	}
	return idle, true
}

// userIdle is how long nobody has touched this machine (the user, or an
// agent driving it), and whether it is known.
func (a *App) userIdle(c config.Config) (time.Duration, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c.Screen.IdleCommand != "" {
		out, err := a.runExt(ctx, nil, "sh", "-c", c.Screen.IdleCommand)
		if err != nil {
			return 0, false
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(out), 64)
		if err != nil {
			return 0, false
		}
		return time.Duration(f * float64(time.Second)), true
	}
	out, err := a.runExt(ctx, nil, "ioreg", "-c", "IOHIDSystem", "-d", "4")
	if err != nil {
		return 0, false
	}
	m := regexp.MustCompile(`"HIDIdleTime" = (\d+)`).FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	ns, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(ns), true
}

// humanDur writes a duration as people do: "45 s", "12 min", "2 h 5 min".
func humanDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	if m == 0 {
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, m)
}

func short8(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// sampleGroup writes a stack sample of every process in process group pgid
// (macOS sample(1)) to <desk>/screen/hang-<time>.txt, with a line in
// <desk>/screen/log, and returns the file, or "" when it could not.
func (a *App) sampleGroup(pgid int) string {
	sampler, err := exec.LookPath("sample")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ps, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,pgid=,etime=,command=").Output()
	if err != nil {
		return ""
	}
	dir := filepath.Join(a.Home, "screen")
	if os.MkdirAll(dir, 0o700) != nil {
		return ""
	}
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	file := filepath.Join(dir, "hang-"+now.UTC().Format("20060102T150405Z")+".txt")
	var b strings.Builder
	var members []string
	for _, line := range strings.Split(string(ps), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[1] == strconv.Itoa(pgid) {
			members = append(members, f[0])
			fmt.Fprintf(&b, "%s\n", strings.TrimSpace(line))
		}
	}
	for _, pid := range members {
		out, _ := exec.CommandContext(ctx, sampler, pid, "1").CombinedOutput()
		fmt.Fprintf(&b, "\n===== sample %s\n%s", pid, out)
	}
	if os.WriteFile(file, []byte(b.String()), 0o600) != nil {
		return ""
	}
	if f, err := os.OpenFile(filepath.Join(dir, "log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		fmt.Fprintf(f, "%s banner hung after %s: %d processes, stacks in %s\n", now.UTC().Format(time.RFC3339), bannerTimeout, len(members), file)
		f.Close()
	}
	return file
}
