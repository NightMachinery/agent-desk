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
//
// An approval while another agent holds the screen queues the request
// (desk.ScreenQueued): it is granted when the hold ends, by the holder's
// release, the next sweep, or its own take, and its agent is told. A
// request filed behind a hold while the user is away notifies nobody
// (meta.screen.quiet): it will be granted without them. If it still waits
// once the screen is free, because the user came back, the sweep wakes it
// (a screen event "wake"), which raises its notifications.

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
		out, err := a.screenDecide(o.pos[0], args[0] == "grant", "cli")
		if err == nil && out["decision"] == "queue" {
			fmt.Fprintf(a.Stdout, "Approved: %s holds the screen until %s, so this request goes next.\n", out["after"], localHM(out["at"]))
		}
		return err
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
			if len(st.Queued) > 0 {
				// The user approved another agent to go next: no renewal.
				fmt.Fprintf(a.Stdout, "The user approved %s to have the screen next, so your hold is not renewed.\n",
					screenAgentName(d, st.Queued[0].Session))
			} else if !st.Hold.Until.After(d.Now.Add(time.Minute)) || st.Hold.Reason != reason {
				g := a.screenGrant(from, from.Session, reason, hold, st.Hold.By, request)
				if err := a.commit([]store.Event{g}, "-"); err != nil {
					return err
				}
				st.Hold.Until = d.Now.Add(hold)
			}
			return a.screenGranted(c, reason, st.Hold.Until, st.Hold.By)
		}
		if x, queued := st.QueuedFor(from.Session); queued {
			if st.Hold == nil && st.Queued[0].Session == from.Session {
				// Approved while another agent held it, and now free.
				h := x.For
				if h <= 0 || h > screenMax {
					h = hold
				}
				g := a.screenGrant(from, from.Session, reason, h, "user", x.Request)
				if err := a.commit([]store.Event{g}, "-"); err != nil {
					return err
				}
				return a.screenGranted(c, reason, d.Now.Add(h), "user")
			}
			if st.Hold == nil { // free, and another approved request is first: its turn
				if granted, err := a.screenServeQueued(); err != nil {
					return err
				} else if granted {
					continue
				}
			}
			if waited == 0 {
				fmt.Fprintf(a.Stdout, "The user approved your request (%s): you are next once %s.\n", d.Display(x.Request), screenTurn(d, st, from.Session))
			}
			if !o.bools["wait"] || waited+screenPoll > timeout {
				return exitError{ScreenWaiting, "screen: approved, not yet yours. You get a message when the screen is yours, or wait for it with --wait."}
			}
			time.Sleep(screenPoll)
			continue
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
		if st.Hold == nil && len(st.Queued) == 0 && (len(st.Requests) == 0 || (has && st.Requests[0].ID == req.ID)) {
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
		if st.Hold == nil && len(st.Queued) > 0 {
			// Free, and the user approved another request to go next:
			// hand it the screen, as the server's sweep would.
			if granted, err := a.screenServeQueued(); err != nil {
				return err
			} else if granted {
				continue
			}
		}
		if st.Hold == nil && len(st.Queued) == 0 && len(st.Requests) > 0 && !(has && st.Requests[0].ID == req.ID) {
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
			q := a.screenRequest(d, c, st, from, reason, hold, away, wait)
			if err := a.commit([]store.Event{q}, "-"); err != nil {
				return err
			}
			mine = q.ID
			a.startSweeper()
			if d, err = a.load(); err != nil {
				return err
			}
			if st.Hold != nil {
				fmt.Fprintf(a.Stdout, "%s holds the screen until %s (%s); asked the user (%s). If they approve, it is yours once that hold ends.\n",
					screenAgentName(d, st.Hold.Session), st.Hold.Until.Local().Format("15:04"), st.Hold.Reason, d.Display(q.ID))
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

// screenRequest is the question that asks the user for the screen, with
// st the screen as it is. away is how long the user must be idle for the
// server to grant it unasked; wait, how long a take --wait will keep asking
// (0 without --wait), which the request lives at least as long as. Filed
// behind another agent's hold, it says so; and if the user is away long
// enough already, it is quiet (meta.screen.quiet): it will be granted
// without them when the hold ends, so it notifies nobody unless it is still
// waiting then.
func (a *App) screenRequest(d *desk.Desk, c config.Config, st desk.ScreenState, from ident.From, reason string, hold, away, wait time.Duration) store.Event {
	who := from.Title
	if who == "" {
		who = from.Agent + " " + short8(from.Session)
	}
	sm := map[string]any{"host": a.Host, "reason": reason, "for": hold.String(), "minutes": int(hold.Minutes()), "idle": away.String()}
	if wait > 0 {
		sm["wait"] = wait.String()
	}
	body := fmt.Sprintf("%s\n\nFor up to %s. Approve to hand it the screen now (a banner shows while it holds it), or deny. "+
		"It is granted without asking once you have been away %s.", reason, humanDur(hold), humanDur(away))
	if st.Hold != nil {
		ahead := ""
		if n := len(st.Queued); n > 0 {
			ahead = fmt.Sprintf(", and after the %d request(s) you approved before it", n)
		}
		body = fmt.Sprintf("%s\n\nFor up to %s. It waits behind %s, which holds the screen until %s (%s)%s. "+
			"Approve to give it the screen next, when that hold ends, or deny. ",
			reason, humanDur(hold), screenAgentName(d, st.Hold.Session), st.Hold.Until.Local().Format("15:04"), st.Hold.Reason, ahead)
		if idle, known := a.userAway(c, d); known && idle >= away {
			sm["quiet"] = true
			body += fmt.Sprintf("You have been away %s, so it gets the screen then without asking; "+
				"you are told only if it is still waiting then.", humanDur(idle))
		} else {
			body += fmt.Sprintf("Unanswered, it gets the screen then only once you have been away %s.", humanDur(away))
		}
	}
	q := a.event("ask", desk.ScreenPath(a.Host), from, "cli", map[string]any{"screen": sm})
	q.Level = "blocked"
	q.Title = fmt.Sprintf("Screen on %s: %s wants to drive the GUI", a.Host, who)
	q.Body = body
	return q
}

// screenAgentName names session's agent for the user: the title its latest
// word carries, else its agent and a short id.
func screenAgentName(d *desk.Desk, session string) string {
	name := ""
	for _, e := range d.Events {
		if sessionOf(e) == session {
			if t := agentTitle(e); t != "" {
				name = t
			}
		}
	}
	if name == "" {
		return "agent " + short8(session)
	}
	return name
}

// screenTurn says when queued session gets the screen.
func screenTurn(d *desk.Desk, st desk.ScreenState, session string) string {
	turn := "the screen is free"
	if st.Hold != nil {
		turn = fmt.Sprintf("%s releases the screen or its hold ends at %s", screenAgentName(d, st.Hold.Session), st.Hold.Until.Local().Format("15:04"))
	}
	for i, x := range st.Queued {
		if x.Session == session && i > 0 {
			turn += fmt.Sprintf(", after the %d request(s) approved before yours", i)
		}
	}
	return turn
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
		// Also withdraw an open request, or a place on the queue.
		if req, ok := st.Request(from.Session); ok {
			if err := a.commit([]store.Event{a.readMark(req, from, "screen")}, "-"); err != nil {
				return err
			}
			fmt.Fprintln(a.Stdout, "Withdrew your request for the screen.")
			return nil
		}
		if x, ok := st.QueuedFor(from.Session); ok {
			e := a.event("screen", desk.ScreenPath(a.Host), from, "cli", map[string]any{"action": "unqueue", "request": x.Request})
			e.Ref = x.Request
			if err := a.commit([]store.Event{e}, "-"); err != nil {
				return err
			}
			fmt.Fprintln(a.Stdout, "Gave up your place next in line for the screen.")
			return nil
		}
		return errors.New("you do not hold the screen on " + a.Host)
	}
	_, err = a.screenLetGo(from, st, "agent")
	return err
}

// screenLetGo releases the screen and takes the banner down. The first
// request the user approved while it was held gets it at once, and its
// agent is told; next names that agent ("" for none).
func (a *App) screenLetGo(from ident.From, st desk.ScreenState, by string) (next string, err error) {
	e := a.event("screen", desk.ScreenPath(a.Host), from, "cli", map[string]any{"action": "release", "by": by})
	e.Ref = st.Hold.Event.ID
	d, err := a.load()
	if err != nil {
		return "", err
	}
	free := st
	free.Hold = nil
	why := "it released it"
	if by == "user" {
		why = "the user took the screen back from it"
	}
	evs := append([]store.Event{e}, a.screenNextEvents(d, free, why)...)
	if err := a.commit(evs, "-"); err != nil {
		return "", err
	}
	c, err := a.config()
	if err == nil && c.Screen.BannerOff != "" && st.Host == a.Host {
		if err := a.runBanner("sh", "-c", c.Screen.BannerOff); err != nil {
			fmt.Fprintf(a.Stderr, "agent-desk: warning: banner_off: %v\n", err)
		}
	}
	fmt.Fprintf(a.Stdout, "Released the screen on %s.\n", a.Host)
	if len(evs) > 1 {
		next = screenAgentName(d, free.Queued[0].Session)
		fmt.Fprintf(a.Stdout, "It goes to %s, which the user approved next.\n", next)
		if evs[1].Kind == "reply" {
			if _, err := a.deliver(evs[1].ID); err != nil {
				fmt.Fprintf(a.Stderr, "agent-desk: telling %s the screen is its: %v\n", next, err)
			}
		}
	}
	return next, nil
}

// screenNextEvents hand the free screen st to its first queued request: a
// message telling its agent (first, so a guarded append counts the screen
// path), and the grant. why says how the last hold ended.
func (a *App) screenNextEvents(d *desk.Desk, st desk.ScreenState, why string) []store.Event {
	if st.Hold != nil || len(st.Queued) == 0 {
		return nil
	}
	x := st.Queued[0]
	q, ok := d.ByID[x.Request]
	if !ok {
		return nil
	}
	hold := x.For
	if hold <= 0 || hold > screenMax {
		hold = screenDefault
	}
	g := a.screenGrant(ident.From{}, x.Session, x.Reason, hold, "user", x.Request)
	text := fmt.Sprintf("The screen on %s is yours now, until %s: the user approved your request while another agent held the screen, and %s. "+
		"Run `agent-desk screen take --reason %s` again to put up the banner, and release it as soon as you are done.",
		st.Host, a.Now().Add(hold).Local().Format("15:04"), why, shellQuote(x.Reason))
	r, err := a.replyTo(d, fromOf(q), q.Host, q.Path, q, text, "screen", "", nil)
	if err != nil {
		return []store.Event{g}
	}
	markAuto(&r, "screen-granted")
	return []store.Event{r, g}
}

// screenServeQueued hands the free screen to the first queued request and
// tells its agent; granted reports whether it did. The guarded append keeps
// it from granting twice when a sweep runs at the same moment.
func (a *App) screenServeQueued() (granted bool, err error) {
	d, err := a.load()
	if err != nil {
		return false, err
	}
	evs := a.screenNextEvents(d, d.Screen(a.Host), "its time ran out")
	if len(evs) == 0 {
		return false, nil
	}
	st, err := a.store()
	if err != nil {
		return false, err
	}
	if granted, err = st.AppendScreenSweep(evs, eventsAt(d, evs[0].Path)); err != nil || !granted {
		return false, err
	}
	a.announce(evs, false, false)
	if evs[0].Kind == "reply" {
		if _, err := a.deliver(evs[0].ID); err != nil {
			fmt.Fprintf(a.Stderr, "agent-desk: telling the agent its approved request has the screen: %v\n", err)
		}
	}
	return true, nil
}

// eventsAt counts the events at path, for a guarded append.
func eventsAt(d *desk.Desk, path string) int {
	n := 0
	for _, e := range d.Events {
		if e.Path == path {
			n++
		}
	}
	return n
}

// screenWake raises the notifications of the quiet requests (see
// screenRequest) still waiting on a free screen that nobody was granted:
// the user came back before the hold they waited behind ended.
func (a *App) screenWake() error {
	d, err := a.load()
	if err != nil {
		return err
	}
	st := d.Screen(a.Host)
	if st.Hold != nil || len(st.Queued) > 0 {
		return nil
	}
	var wakes, woken []store.Event
	for _, q := range st.Requests {
		if screenQuiet(d, q) {
			w := a.event("screen", q.Path, ident.From{}, "cli", map[string]any{"action": "wake"})
			w.Ref = q.ID
			wakes, woken = append(wakes, w), append(woken, q)
		}
	}
	if len(wakes) == 0 {
		return nil
	}
	if err := a.appendQuiet(wakes); err != nil {
		return err
	}
	a.announce(woken, false, false)
	return nil
}

// screenQuiet reports whether request q notifies nobody yet: filed quiet,
// and not woken since.
func screenQuiet(d *desk.Desk, q store.Event) bool {
	sm, ok := desk.Meta(q)["screen"].(map[string]any)
	if !ok || sm["quiet"] != true {
		return false
	}
	for _, e := range d.Events {
		if e.Kind == "screen" && e.Ref == q.ID && desk.MetaString(e, "action") == "wake" {
			return false
		}
	}
	return true
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
	for _, x := range st.Queued {
		fmt.Fprintf(a.Stdout, "Next, approved: %s (%s): %s\n", screenAgentName(d, x.Session), d.Display(x.Request), x.Reason)
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

// screenRequestOf resolves token to a request for the screen.
func (a *App) screenRequestOf(token string) (*desk.Desk, store.Event, error) {
	d, err := a.load()
	if err != nil {
		return nil, store.Event{}, err
	}
	q, err := d.Resolve(token)
	if err != nil {
		return nil, store.Event{}, err
	}
	if q.Kind != "ask" || desk.Meta(q)["screen"] == nil {
		return nil, store.Event{}, usagef("%s is not a request for the screen", d.Display(q.ID))
	}
	return d, q, nil
}

// screenDecide grants or denies a request (a screen question) as the user,
// and answers it, so its agent hears at once (the delivered answer says
// what the desk did: screenNote). Only this denies: an answer in words
// grants whatever it says (screenDecision). It returns the outcome
// (screenOutcome).
func (a *App) screenDecide(token string, grant bool, source string) (map[string]string, error) {
	d, q, err := a.screenRequestOf(token)
	if err != nil {
		return nil, err
	}
	text := "No, not now: keep off the screen."
	if grant {
		text = "Approved."
	}
	evs, err := a.answerEvents(d, q, text, source, "", nil)
	if err != nil {
		return nil, err
	}
	if !grant {
		evs[len(evs)-1] = a.screenDeny(q)
	}
	if err := a.sendAnswer(d, q, evs, source); err != nil {
		return nil, err
	}
	return screenOutcome(d, evs[len(evs)-1]), nil
}

// screenAnswer answers a request in the user's own words, which grant it
// (screenDecision), and returns the outcome (screenOutcome).
func (a *App) screenAnswer(token, text, source string) (map[string]string, error) {
	d, q, err := a.screenRequestOf(token)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, usagef("an answer needs words: approve or deny it instead")
	}
	evs, err := a.answerEvents(d, q, text, source, "", nil)
	if err != nil {
		return nil, err
	}
	if err := a.sendAnswer(d, q, evs, source); err != nil {
		return nil, err
	}
	return screenOutcome(d, evs[len(evs)-1]), nil
}

// screenOutcome describes decision se for the dashboard: {decision: grant,
// until}, {decision: queue, after: the holder's name, at: when its hold
// ends}, or {decision: deny}.
func screenOutcome(d *desk.Desk, se store.Event) map[string]string {
	out := map[string]string{"decision": desk.MetaString(se, "action")}
	if u := desk.MetaString(se, "until"); u != "" {
		out["until"] = u
	}
	if out["decision"] == "queue" {
		st := d.Screen(strings.TrimPrefix(se.Path, "_desk/screen/"))
		if st.Hold != nil {
			out["after"], out["at"] = screenAgentName(d, st.Hold.Session), st.Hold.Until.Format(time.RFC3339)
		}
	}
	return out
}

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
	if st.Hold != nil || len(st.Queued) > 0 || len(st.Requests) == 0 {
		return nil, 0, nil // held, or an approved request goes first (screenServeQueued)
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
	count := eventsAt(d, q.Path)
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

// screenServeNext gives the free screen to whoever is next: the first
// request the user approved, else the oldest once the user is away long
// enough. If it goes to nobody, quiet requests start notifying.
func (a *App) screenServeNext() error {
	if granted, err := a.screenServeQueued(); err != nil || granted {
		return err
	}
	if granted, err := a.screenServeOldest(); err != nil || granted {
		return err
	}
	return a.screenWake()
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
		if len(st.Requests) == 0 && len(st.Queued) == 0 {
			return nil
		}
		if st.Hold == nil {
			if err := a.screenServeNext(); err != nil {
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

// screenDecision is the screen event an answer to request q implies: a
// grant, whatever the answer says. A typed answer never denies, so a
// question back, or a "no" meant as "no problem", cannot cost the agent the
// screen; only Deny (screenDecide, or dismissing the request) denies. While
// another agent holds the screen (or an earlier approval waits for it), the
// approval queues the request instead: it never takes the screen from an
// agent that may be mid-click.
func (a *App) screenDecision(d *desk.Desk, q store.Event, source string) (store.Event, bool) {
	sm, ok := desk.Meta(q)["screen"].(map[string]any)
	if !ok {
		return store.Event{}, false
	}
	host, _ := sm["host"].(string)
	hold, err := time.ParseDuration(fmt.Sprint(sm["for"]))
	if err != nil || hold <= 0 || hold > screenMax {
		hold = screenDefault
	}
	reason, _ := sm["reason"].(string)
	session := sessionOf(q)
	if st := d.Screen(host); (st.Hold != nil && st.Hold.Session != session) || (len(st.Queued) > 0 && st.Queued[0].Session != session) {
		e := a.event("screen", desk.ScreenPath(host), ident.Resolve(a.Env), "answer", map[string]any{
			"action": "queue", "holder": session, "reason": reason, "for": hold.String(), "by": "user", "request": q.ID,
		})
		e.Ref = q.ID
		return e, true
	}
	m := map[string]any{
		"action": "grant", "holder": session, "reason": reason, "by": "user", "request": q.ID,
		"until": a.Now().Add(hold).UTC().Format(time.RFC3339),
	}
	if source == "auto" {
		m["by"] = "auto"
	}
	e := a.event("screen", desk.ScreenPath(host), ident.Resolve(a.Env), "answer", m)
	e.Ref = q.ID
	return e, true
}

// screenDeny is the screen event denying request q.
func (a *App) screenDeny(q store.Event) store.Event {
	sm, _ := desk.Meta(q)["screen"].(map[string]any)
	host, _ := sm["host"].(string)
	e := a.event("screen", desk.ScreenPath(host), ident.Resolve(a.Env), "answer", map[string]any{"action": "deny", "request": q.ID})
	e.Ref = q.ID
	return e
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

// localHM is RFC 3339 time s as local hours and minutes.
func localHM(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Local().Format("15:04")
}

// screenNote is what the desk adds to an answer to screen request q when
// it is delivered: what the answer did, which the user's words may not say.
func screenNote(d *desk.Desk, q store.Event) string {
	sm, ok := desk.Meta(q)["screen"].(map[string]any)
	if !ok {
		return ""
	}
	host, _ := sm["host"].(string)
	var last store.Event
	for _, e := range d.Events {
		if e.Kind == "screen" && e.Ref == q.ID {
			if a := desk.MetaString(e, "action"); a == "grant" || a == "queue" || a == "deny" {
				last = e
			}
		}
	}
	switch desk.MetaString(last, "action") {
	case "queue":
		st := d.Screen(host)
		if x, ok := st.QueuedFor(desk.MetaString(last, "holder")); ok && x.Request == q.ID {
			return fmt.Sprintf("(From the desk: this approves your request, but another agent holds the screen on %s, so you are next: "+
				"you get the screen once %s, and a message then tells you. Do not drive the GUI before that.)", host, screenTurn(d, st, x.Session))
		}
	case "grant":
		if desk.MetaString(last, "by") == "user" {
			return fmt.Sprintf("(From the desk: this approves your request: the screen on %s is yours until %s. "+
				"Run `agent-desk screen take` again to put up the banner, and release it as soon as you are done.)", host, localHM(desk.MetaString(last, "until")))
		}
	}
	return ""
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
