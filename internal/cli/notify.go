package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/NightMachinery/agent-desk/internal/config"
	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/settings"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Notification routing. A notifiable event written here is announced on up
// to two channels: desktop (this machine's card, and those of the other
// machines the notify.desktop setting names) and remote (a channel that
// reaches the user away from the desk, raised only by the machine the event
// was written on). Each notification raised is a notified event holding the
// handle the command printed; when its item stops needing the user (read,
// answered, dismissed, or a slot replaced) it is taken back with
// dismiss_command and a retracted event. Handles are events, so after a
// merge any machine can take back a remote notification.

// importedMaxAge is how old an imported event may be and still raise a
// notification here, so the first merge between two machines does not raise
// one for every old unread item.
const importedMaxAge = 24 * time.Hour

// route is where one event's notifications go.
type route struct {
	class   string
	desktop string // none, origin, all, or host names separated by commas
	remote  string // off, away or always
	dest    string
	push    string // off, away or always
}

// onDesktop reports whether host shows the event's desktop notification.
func (r route) onDesktop(host, origin string) bool {
	switch r.desktop {
	case "none", "":
		return false
	case "origin":
		return host == origin
	case "all":
		return true
	}
	for _, h := range strings.Split(r.desktop, ",") {
		h = strings.TrimSpace(h)
		if h == host || (h == "origin" && host == origin) {
			return true
		}
	}
	return false
}

// routeOf resolves e's notification settings: a message's own override
// (meta.notify on the message a response responds to), then the settings.
// A quiet response, and an event of no class, go nowhere.
func routeOf(d *desk.Desk, r *settings.Resolver, e store.Event) route {
	rt := route{class: desk.ClassOf(e), desktop: "none", remote: "off", push: "off"}
	if rt.class == "" || (desk.IsResponse(e) && e.Kind != "ask" && desk.Meta(e)["quiet"] == true) {
		return rt
	}
	node := desk.NodeOf(e)
	rt.desktop, _ = r.Resolve("notify.desktop", node, rt.class)
	rt.remote, _ = r.Resolve("notify.remote", node, rt.class)
	rt.dest, _ = r.Resolve("notify.remote.dest", node, rt.class)
	rt.push, _ = r.Resolve("notify.push", node, rt.class)
	if desk.IsResponse(e) {
		if msg, ok := d.ByID[e.Ref]; ok && (msg.Kind == "reply" || msg.Kind == "answer") {
			var m struct {
				Notify struct {
					Desktop string `json:"desktop"`
					Remote  string `json:"remote"`
				} `json:"notify"`
			}
			_ = json.Unmarshal(msg.Meta, &m)
			// A question response holds the agent up: the message's override
			// may make it louder, never quieter.
			question := e.Kind == "ask"
			if v := m.Notify.Desktop; v != "" && (!question || desktopRank(v) > desktopRank(rt.desktop)) {
				rt.desktop = v
			}
			if v := m.Notify.Remote; v != "" && (!question || remoteRank(v) > remoteRank(rt.remote)) {
				rt.remote = v
			}
		}
	}
	return rt
}

// desktopRank orders notify.desktop values by how widely they notify: none,
// then one machine or a list of them, then all.
func desktopRank(v string) int {
	switch v {
	case "none":
		return 0
	case "all":
		return 2
	}
	return 1
}

// remoteRank orders notify.remote values: off, away, always.
func remoteRank(v string) int {
	switch v {
	case "off":
		return 0
	case "always":
		return 2
	}
	return 1
}

// Notice is one notified event, read back.
type Notice struct {
	Event   store.Event
	Item    string
	Channel string
	Handle  string
	Dest    string
	Host    string
}

func noticeOf(e store.Event) Notice {
	var m struct {
		Channel string `json:"channel"`
		Handle  string `json:"handle"`
		Dest    string `json:"dest"`
		Host    string `json:"host"`
	}
	_ = json.Unmarshal(e.Meta, &m)
	return Notice{Event: e, Item: e.Ref, Channel: m.Channel, Handle: m.Handle, Dest: m.Dest, Host: m.Host}
}

// notices are the notifications raised for each item, and whether each has
// been taken back.
func notices(d *desk.Desk) (byItem map[string][]Notice, retracted map[string]bool) {
	byItem, retracted = map[string][]Notice{}, map[string]bool{}
	for _, e := range d.Events {
		switch e.Kind {
		case "notified":
			byItem[e.Ref] = append(byItem[e.Ref], noticeOf(e))
		case "retracted":
			retracted[e.Ref] = true
		}
	}
	return byItem, retracted
}

// closer is the event that ends item's need for the user, if it has ended:
// its answer or dismissal, its read mark, or (for a slot) the newer event at
// its path. An answer or read names its item, so it counts whatever its id;
// another machine's clock may be behind. A slot is replaced by a higher id,
// as in the slot_current view.
func closer(d *desk.Desk, item store.Event) (store.Event, bool) {
	var found store.Event
	for _, e := range d.Events {
		switch {
		case item.Kind == "ask" && e.Kind == "answer" && e.Ref == item.ID:
			return e, true
		case e.Kind == "read" && e.Ref == item.ID && found.ID == "":
			found = e
		case item.Kind == "set" && e.ID > item.ID && e.Path == item.Path &&
			(e.Kind == "set" || e.Kind == "clear" || e.Kind == "link"):
			return e, true
		}
	}
	if found.ID == "" {
		return found, false
	}
	// The read mark that counts is the latest; an unread undoes it.
	if item.Kind == "ask" {
		return found, d.Dismissed(item.ID)
	}
	return found, !d.Unread(item.ID)
}

// takeBacks are the notifications this host should take back now: its own
// desktop cards and pushes, and any remote notification (its handle is in
// the event), each when this host has the means.
func (a *App) takeBacks(d *desk.Desk, c config.Config) []Notice {
	byItem, retracted := notices(d)
	var out []Notice
	for id, ns := range byItem {
		var mine []Notice
		for _, n := range ns {
			ours := false
			switch n.Channel {
			case "push":
				ours = n.Host == a.Host && !c.Push.Off
			case "desktop":
				ours = n.Host == a.Host && c.DismissCommand != ""
			default:
				ours = c.DismissCommand != ""
			}
			if ours && !retracted[n.Event.ID] {
				mine = append(mine, n)
			}
		}
		// Most items' notifications were taken back long ago; only look for
		// a closer when one is still out.
		item, ok := d.ByID[id]
		if len(mine) == 0 || !ok {
			continue
		}
		if _, closed := closer(d, item); closed {
			out = append(out, mine...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Event.ID < out[j].Event.ID })
	return out
}

// raisable reports whether e may raise notifications here: still needing
// the user, inside the focus when a project set is focused, and for an
// imported event, recent and written elsewhere (one written here was
// announced when it was written). An event outside the focus stays unread
// on the desk; turning the focus off later does not raise it.
func (a *App) raisable(d *desk.Desk, e store.Event, imported, force bool) bool {
	if desk.ClassOf(e) == "" || !desk.InFocus(e.Path, d.FocusPrefixes()) {
		return false
	}
	if _, closed := closer(d, e); closed {
		return false
	}
	if e.Kind == "ask" && !isOpen(d, e.ID) {
		return false
	}
	if !imported || force {
		return true
	}
	return e.Host != a.Host && a.Now().Sub(desk.Time(e)) < importedMaxAge
}

// announce starts what new events call for. Written here (imported false),
// a notifiable event is routed by its settings; imported, it raises this
// machine's desktop notification when the settings name this machine (or,
// with force, always). Either way, notifications whose items no longer need
// the user are taken back. The work runs in a detached notify-run, so a slow
// notifier never holds up an agent's write.
func (a *App) announce(evs []store.Event, imported, force bool) {
	c, err := a.config()
	if err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: warning: %v\n", err)
		return
	}
	pushOn := a.pushOn(c)
	if c.NotifyCommand == "" && c.DismissCommand == "" && !pushOn {
		return
	}
	d, err := a.load()
	if err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: warning: %v\n", err)
		return
	}
	r := settings.New(d.Events, c.Defaults)
	var raise []string
	byItem, _ := notices(d)
	for _, e := range evs {
		if a.raisable(d, e, imported, force) {
			rt := routeOf(d, r, e)
			notify := c.NotifyCommand != "" &&
				(force || rt.onDesktop(a.Host, e.Host) || (!imported && (rt.remote != "off" || rt.desktop != "none")))
			if notify || (pushOn && rt.push != "off") {
				raise = append(raise, e.ID)
			}
		}
		// Items raised before notified events existed are dismissed as
		// before: once per item, with no handle.
		if (e.Kind == "answer" || (e.Kind == "read" && desk.Meta(e)["cancel"] != true)) && c.DismissCommand != "" {
			target, ok := d.ByID[e.Ref]
			if !ok || len(byItem[target.ID]) > 0 {
				continue
			}
			cause := e
			p := a.payload(d, "dismiss", target)
			p.Cause = &cause
			if err := a.runHook(c.DismissCommand, p, false); err != nil {
				fmt.Fprintf(a.Stderr, "agent-desk: warning: dismiss command: %v\n", err)
			}
		}
	}
	if len(raise) == 0 && len(a.takeBacks(d, c)) == 0 {
		return
	}
	args := []string{"notify-run"}
	if imported {
		args = append(args, "--imported")
	}
	if force {
		args = append(args, "--force")
	}
	args = append(args, raise...)
	if err := a.spawn(args); err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: warning: notify-run: %v\n", err)
	}
}

// spawn runs agent-desk with args detached, its output in hooks.log; under
// WaitHooks (tests) it runs in this process instead.
func (a *App) spawn(args []string) error {
	if a.WaitHooks {
		return a.run(args)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(a.Home, "hooks.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	fmt.Fprintf(logf, "%s %s\n", time.Now().Format(time.RFC3339), strings.Join(args, " "))
	cmd := exec.Command(self, args...)
	cmd.Env = append(os.Environ(), "AGENT_DESK_HOME="+a.Home)
	cmd.Stdout, cmd.Stderr = logf, logf
	// A new session, so no controlling terminal: a hook that touches
	// /dev/tty cannot be stopped by job control (SIGTTIN/SIGTTOU), which
	// once froze notify-run, holding notify.lock while every later run
	// queued behind it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// lockNotify serializes notify-run processes, so two of them never raise or
// take back the same notification. It returns the unlock. A run waits at
// most notifyLockWait, then fails naming the holder (in hooks.log), so a
// stuck holder shows instead of silently queueing every later run.
var notifyLockWait = 3 * time.Minute

func orUnknown(s string) string {
	if s == "" {
		return "an unknown process"
	}
	return s
}

func (a *App) lockNotify() (func(), error) {
	if a.WaitHooks {
		return func() {}, nil
	}
	f, err := os.OpenFile(filepath.Join(a.Home, "notify.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(notifyLockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			holder, _ := io.ReadAll(f)
			f.Close()
			return nil, fmt.Errorf("notify.lock held by %s; gave up after %v", orUnknown(strings.TrimSpace(string(holder))), notifyLockWait)
		}
		time.Sleep(250 * time.Millisecond)
	}
	// Say who holds it, for the message above.
	if err := f.Truncate(0); err == nil {
		fmt.Fprintf(f, "pid %d since %s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// cmdNotifyRun raises the notifications of the given events and takes back
// those whose items no longer need the user. announce starts it.
func (a *App) cmdNotifyRun(args []string) error {
	o, err := parse(args, []string{"imported", "force"}, nil, 0, -1)
	if err != nil {
		return err
	}
	unlock, err := a.lockNotify()
	if err != nil {
		return err
	}
	defer unlock()
	c, err := a.config()
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	r := settings.New(d.Events, c.Defaults)
	byItem, _ := notices(d)
	has := func(item, channel, host string) bool {
		for _, n := range byItem[item] {
			if n.Channel == channel && (host == "" || n.Host == host) {
				return true
			}
		}
		return false
	}
	imported, force := o.bools["imported"], o.bools["force"]
	var made []store.Event
	push := map[string]bool{}
	away, awayKnown := false, false
	isAway := func() bool {
		if !awayKnown {
			away, awayKnown = a.away(c), true
		}
		return away
	}
	pushOn := a.pushOn(c)
	notify := c.NotifyCommand != ""
	for _, id := range o.pos {
		e, ok := d.ByID[id]
		if !ok || !a.raisable(d, e, imported, force) {
			continue
		}
		rt := routeOf(d, r, e)
		if notify && (force || rt.onDesktop(a.Host, e.Host)) && !has(e.ID, "desktop", a.Host) {
			if n, ok := a.raise(d, c, e, rt, "desktop", ""); ok {
				made = append(made, n)
			}
		}
		// Pushes go from the machine the devices subscribed on, for events
		// written there and those it imports.
		if pushOn && (rt.push == "always" || (rt.push == "away" && isAway())) && !has(e.ID, "push", a.Host) {
			if n, ok := a.pushRaise(d, c, e, rt); ok {
				made = append(made, n)
			}
		}
		if imported || !notify {
			continue
		}
		for host := range c.SSHHosts {
			if host != a.Host && rt.onDesktop(host, e.Host) {
				push[host] = true
			}
		}
		if (rt.remote == "always" || (rt.remote == "away" && isAway())) && !has(e.ID, "remote", "") {
			if n, ok := a.raise(d, c, e, rt, "remote", rt.dest); ok {
				made = append(made, n)
			}
		}
	}
	if err := a.appendQuiet(made); err != nil {
		return err
	}
	// Push to the other machines whose desktops show these; each raises its
	// own when it imports them. An unreachable one catches up at its next
	// merge.
	hosts := make([]string, 0, len(push))
	for h := range push {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	for _, h := range hosts {
		if err := a.mergeSSH(c.SSHHosts[h], false); err != nil {
			fmt.Fprintf(a.Stderr, "agent-desk: pushing to %s: %v\n", h, err)
		}
	}
	return a.takeBack(c)
}

// takeBack runs dismiss_command for each notification this host should take
// back, and records it.
func (a *App) takeBack(c config.Config) error {
	d, err := a.load()
	if err != nil {
		return err
	}
	var made []store.Event
	for _, n := range a.takeBacks(d, c) {
		item := d.ByID[n.Item]
		if n.Channel == "push" {
			// One try: a device that missed it keeps the card until the
			// user clears it.
			if err := a.pushClose(c, n); err != nil {
				fmt.Fprintf(a.Stderr, "agent-desk: warning: taking back push %s: %v\n", n.Handle, err)
			}
			x := a.event("retracted", item.Path, ident.From{}, "notify", map[string]any{"host": a.Host})
			x.Ref = n.Event.ID
			made = append(made, x)
			continue
		}
		p := a.payload(d, "dismiss", item)
		if cause, ok := closer(d, item); ok {
			p.Cause = &cause
		}
		p.Channel, p.Handle, p.Dest = n.Channel, n.Handle, n.Dest
		if _, err := a.hookOutput(c.DismissCommand, p); err != nil {
			fmt.Fprintf(a.Stderr, "agent-desk: warning: dismiss command (%s %s): %v\n", n.Channel, d.Display(item.ID), err)
			continue
		}
		x := a.event("retracted", item.Path, ident.From{}, "notify", map[string]any{"host": a.Host})
		x.Ref = n.Event.ID
		made = append(made, x)
	}
	return a.appendQuiet(made)
}

// raise runs notify_command for one channel and returns the notified event
// holding the handle it printed.
func (a *App) raise(d *desk.Desk, c config.Config, e store.Event, rt route, channel, dest string) (store.Event, bool) {
	p := a.payload(d, "notify", e)
	p.Channel, p.Dest, p.Class = channel, dest, rt.class
	out, err := a.hookOutput(c.NotifyCommand, p)
	if err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: warning: notify command (%s %s): %v\n", channel, d.Display(e.ID), err)
		return store.Event{}, false
	}
	handle := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	n := a.event("notified", e.Path, ident.From{}, "notify",
		map[string]any{"channel": channel, "handle": handle, "dest": dest, "host": a.Host})
	n.Ref = e.ID
	return n, true
}

// appendQuiet stores bookkeeping events without printing or announcing.
func (a *App) appendQuiet(evs []store.Event) error {
	if len(evs) == 0 {
		return nil
	}
	st, err := a.store()
	if err != nil {
		return err
	}
	return st.Append(evs...)
}

// hookOutput runs a configured command with the payload on stdin and
// returns what it printed.
func (a *App) hookOutput(command string, p Payload) (string, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	killGroupOnCancel(cmd)
	cmd.Stdin = bytes.NewReader(append(data, '\n'))
	cmd.Env = append(os.Environ(), "AGENT_DESK_HOME="+a.Home)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, a.Stderr
	err = cmd.Run()
	return out.String(), err
}

// hookTimeout bounds a notify or dismiss command.
var hookTimeout = time.Minute

// killGroupOnCancel runs cmd in a process group of its own and, when its
// context ends, kills the whole group: the command's children too, which
// would otherwise live on and hold its output open.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
}

// away runs away_command: exit 0 means the user is away from this machine.
func (a *App) away(c config.Config) bool {
	if c.AwayCommand == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", c.AwayCommand)
	killGroupOnCancel(cmd)
	cmd.Stdout, cmd.Stderr = nil, a.Stderr
	return cmd.Run() == nil
}

// itemURL is e's page in the web dashboard, or "" without base_url.
func itemURL(c config.Config, d *desk.Desk, e store.Event) string {
	if c.BaseURL == "" {
		return ""
	}
	return strings.TrimRight(c.BaseURL, "/") + "/" + itemHash(d, e)
}

// itemHash is e's page relative to the dashboard.
func itemHash(d *desk.Desk, e store.Event) string {
	var segs []string
	for _, s := range strings.Split(desk.NodeOf(e), "/") {
		segs = append(segs, url.PathEscape(s))
	}
	return "#/p/" + strings.Join(segs, "/") + "?item=" + url.QueryEscape(d.Display(e.ID))
}

// agentTitle names the agent behind e: its Paseo title when recorded, else
// the agent and a short session id.
func agentTitle(e store.Event) string {
	f := fromOf(e)
	if f.Title != "" {
		return f.Title
	}
	if f.Agent == "" {
		return ""
	}
	if f.Session == "" {
		return f.Agent
	}
	return f.Agent + " " + shortID(f.Session)
}

// pushTo sends this store to host at once when ssh_hosts names it, so a
// message or answer for a session there is delivered now; otherwise it
// waits for the next merge.
func (a *App) pushTo(host, lead string) {
	c, _ := a.config()
	dest, ok := c.SSHHosts[host]
	if !ok {
		fmt.Fprintf(a.Stderr, "agent-desk: %s; delivered there after the next merge "+
			"(add %s to [ssh_hosts] in the config to push it at once)\n", lead, host)
		return
	}
	if err := a.spawn([]string{"merge", "ssh:" + dest}); err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: pushing to %s: %v; delivered there after the next merge\n", host, err)
		return
	}
	fmt.Fprintf(a.Stderr, "agent-desk: %s; pushing it there now\n", lead)
}
