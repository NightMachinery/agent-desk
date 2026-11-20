package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/config"
	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// The screen lock (desk.Screen): an agent takes it before it drives this
// machine's GUI, so the user knows to keep off the keyboard. It is granted
// at once when the user has been idle long enough ([screen] idle_minutes,
// default 30); otherwise the request waits in Needs you, where the user may
// approve it at any time.

const (
	screenDefault = 30 * time.Minute
	screenMax     = 2 * time.Hour
)

// screenPoll is how often `screen take --wait` looks again.
var screenPoll = 15 * time.Second

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
		return usagef("screen take|release|status|grant|deny; see agent-desk help")
	}
	rest := args[1:]
	switch args[0] {
	case "take":
		return a.screenTake(rest)
	case "release":
		return a.screenRelease(rest)
	case "status":
		return a.screenStatus(rest)
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
	o, err := parse(args, []string{"wait"}, []string{"reason", "for", "timeout"}, 0, 0)
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
	mine := "" // this session's request, once filed or found
	for waited := time.Duration(0); ; waited += screenPoll {
		d, err := a.load()
		if err != nil {
			return err
		}
		st := d.Screen(a.Host)
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
			if known && idle >= time.Duration(c.Screen.Idle())*time.Minute {
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
		if !has && mine == "" {
			if why, when, ok := screenLastDenial(d, from.Session); ok && d.Now.Sub(when) < screenDenyQuiet {
				return fmt.Errorf("the user denied the screen %s ago: %s; ask again after %s, or ask the user why",
					humanDur(d.Now.Sub(when)), firstLine(why), humanDur(screenDenyQuiet))
			}
			q := a.screenRequest(d, from, reason, hold, c.Screen.Idle())
			if err := a.commit([]store.Event{q}, "-"); err != nil {
				return err
			}
			mine = q.ID
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
				" (or wait for it with --wait). It is granted without asking once the user has been away " +
				strconv.Itoa(c.Screen.Idle()) + " minutes."}
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

// screenRequest is the question that asks the user for the screen.
func (a *App) screenRequest(d *desk.Desk, from ident.From, reason string, hold time.Duration, idleMin int) store.Event {
	who := from.Title
	if who == "" {
		who = from.Agent + " " + short8(from.Session)
	}
	q := a.event("ask", desk.ScreenPath(a.Host), from, "cli", map[string]any{
		"screen": map[string]any{"host": a.Host, "reason": reason, "for": hold.String(), "minutes": int(hold.Minutes())},
	})
	q.Level = "blocked"
	q.Title = fmt.Sprintf("Screen on %s: %s wants to drive the GUI", a.Host, who)
	q.Body = fmt.Sprintf("%s\n\nFor up to %s. Approve to hand it the screen now (a banner shows while it holds it), or deny. "+
		"It is granted without asking once you have been away %d minutes.", reason, humanDur(hold), idleMin)
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
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
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
	away, ok := a.userAway(c, d)
	if !ok || away < time.Duration(c.Screen.Idle())*time.Minute {
		return nil, 0, nil
	}
	text := fmt.Sprintf("Approved automatically: the user has been away %s, so the screen is yours. "+
		"Run `agent-desk screen take` again to put up the banner, and release it as soon as you are done.", humanDur(away))
	q := st.Requests[0]
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
