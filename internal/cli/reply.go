package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/media"
	"github.com/NightMachinery/agent-desk/internal/prompts"
	"github.com/NightMachinery/agent-desk/internal/settings"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// A reply is a message from the user to the session behind an item or a
// node. It records where it goes (meta.to, meta.to_host) when written, and is
// delivered like an answer.

// replyTarget finds the session a reply to token goes to, and the event it
// is about (empty for a node).
func replyTarget(d *desk.Desk, token string) (to ident.From, host, node string, about store.Event, err error) {
	token = strings.Trim(token, "/")
	if !strings.Contains(token, "/") {
		e, rerr := d.Resolve(token)
		if rerr == nil {
			switch e.Kind {
			case "answer", "reply":
				// About what it answered or replied to, not the user's own words;
				// a message about no item roots its own thread, so a reply to it
				// is about it, and stays in that thread.
				if t, ok := d.FiledTask(e); ok {
					e = t // an older message about a filed task: about the task
				} else if e.Ref == "" {
					to, host, node, _, err = replyTarget(d, desk.NodeOf(e))
					return to, host, node, e, err
				} else {
					e = d.ByID[e.Ref]
				}
			case "read", "delivered", "undelivered", "pin", "defer", "pref", "notified", "retracted":
				return to, "", "", e, fmt.Errorf("%s is a %s mark; reply to the item it marks", d.Display(e.ID), e.Kind)
			}
			about, node, to, host = e, desk.NodeOf(e), fromOf(e), e.Host
			// A coordinator that took the node over since is the one to tell.
			if owner, ok := nodeOwner(d, node); ok && owner.ID > e.ID && sessionOf(owner) != sessionOf(e) {
				to, host = fromOf(owner), owner.Host
			}
			if e.Kind == "task" {
				to, host, node = taskRecipient(d, e, to, host, node)
			}
		} else if !strings.HasPrefix(rerr.Error(), "no event") {
			return to, "", "", about, rerr
		}
	}
	if node == "" {
		// A node no agent holds (a project, a run) goes to the coordinator
		// working under it, as a task filed there does.
		owner, ok := nodeOwner(d, token)
		if !ok {
			if c, cok := coordinatorUnder(d, token); cok {
				owner, ok = nodeOwner(d, c)
			}
		}
		if !ok {
			return to, "", "", about, fmt.Errorf("%q is neither an id nor a joined node, and no coordinator works under it", token)
		}
		node, to, host = owner.Path, fromOf(owner), owner.Host
	}
	if to.Session == "" && to.Paseo == "" && to.TmuxPane == "" {
		return to, "", "", about, errors.New("that item has no session to send to")
	}
	return to, host, node, about, nil
}

// ReplyText is the prompt typed into the session. It names the message's
// own id and the command that responds to it, so the response links without
// the agent having to remember the skill.
//
// A generated message about an item (meta.auto) names the item instead: the
// response belongs to it (see desk.Answers).
func ReplyText(d *desk.Desk, reply store.Event) string {
	id := d.Display(reply.ID)
	about, re := "to "+reply.Path, id
	if reply.Ref != "" {
		r := d.ByID[reply.Ref]
		if r.Kind == "reply" {
			about = fmt.Sprintf("following up on their message %s (\"%s\")", d.Display(r.ID), desk.ThreadTitle(r))
		} else {
			about = fmt.Sprintf("about %s \"%s\"", d.Display(r.ID), desk.ThreadTitle(r))
		}
		if desk.MetaString(reply, "auto") != "" {
			re = d.Display(r.ID)
		}
	}
	return fmt.Sprintf("Message %s from the user via agent-desk, %s: %s\n(Respond on the desk: agent-desk tell --re %s)",
		id, about, reply.Body, re)
}

// markAuto marks e as a generated message of kind auto (meta.auto): the
// one about a task the user filed, a task change, or a prepared prompt
// (its name). Nothing for an empty kind.
func markAuto(e *store.Event, auto string) {
	if auto == "" {
		return
	}
	m := desk.Meta(*e)
	m["auto"] = auto
	e.Meta = meta(m)
}

// autoKinds are the generated messages the dashboard may mark (meta.auto).
var autoKinds = map[string]bool{"task-filed": true}

// linkResponse checks that a response from node may answer the --re target
// (respondsTo) and returns its ref and default title. A response to a
// generated message about an item refers to the item, keeping the message
// in extra["answers"], so it hangs from the item and still answers it.
func linkResponse(d *desk.Desk, re, node string, extra map[string]any) (ref, title string, err error) {
	target, err := d.Resolve(re)
	if err != nil {
		return "", "", err
	}
	// A question answered: the response is to its answer (the prompt that
	// delivers an answer names the question).
	if target.Kind == "ask" {
		aid := d.AnswerID(target.ID)
		if aid == "" {
			return "", "", fmt.Errorf("%s has no answer yet; --re takes a message (m-...), an answered question (q-...) or its answer (a-...), or a task (w-...) sent to you", d.Display(target.ID))
		}
		target = d.ByID[aid]
	}
	if title, err = respondsTo(d, target, node); err != nil {
		return "", "", err
	}
	if target.Kind == "reply" && target.Ref != "" && desk.MetaString(target, "auto") != "" {
		extra["answers"] = target.ID
		return target.Ref, title, nil
	}
	return target.ID, title, nil
}

// respondsTo checks that a response from node may refer to e: a message to
// this node, or an answer to one of this node's questions. It returns the
// default title of the response.
func respondsTo(d *desk.Desk, e store.Event, node string) (string, error) {
	shown := d.Display(e.ID)
	switch e.Kind {
	case "reply":
		if e.Path != node {
			return "", fmt.Errorf("%s is a message to %s, not to this session's node %s", shown, e.Path, node)
		}
	case "answer":
		q := d.ByID[e.Ref]
		if desk.NodeOf(q) != node {
			return "", fmt.Errorf("%s answers a question of %s, not of this session's node %s", shown, desk.NodeOf(q), node)
		}
	case "task":
		// A task this node works on: its own, or one routed to it.
		if desk.NodeOf(e) != node {
			if _, _, to, _, err := replyTarget(d, e.ID); err != nil || to != node {
				return "", fmt.Errorf("%s is a task at %s, which this session's node %s does not work on", shown, desk.NodeOf(e), node)
			}
		}
		return "Re: " + clipLine(e.Title, 60), nil
	default:
		return "", fmt.Errorf("%s is a %s; --re takes a message (m-...), an answered question (q-...) or its answer (a-...), or a task (w-...) sent to you", shown, e.Kind)
	}
	if root, ok := d.Root(e); ok {
		return "Re: " + clipLine(desk.ThreadTitle(root), 60), nil
	}
	return "Re: " + clipLine(e.Body, 60), nil
}

func (a *App) cmdReply(args []string) error {
	o, err := parse(args, nil, append([]string{"behavior", "notify-desktop", "notify-remote", "prompt"}, attachOpts...), 1, -1)
	if err != nil {
		return err
	}
	beh, err := checkBehavior(o.vals["behavior"])
	if err != nil {
		return err
	}
	notify, err := checkNotify(o.vals["notify-desktop"], o.vals["notify-remote"])
	if err != nil {
		return err
	}
	text := strings.Join(o.pos[1:], " ")
	switch {
	case o.vals["prompt"] != "":
		if len(o.pos) > 1 {
			return usagef("give a text or --prompt, not both")
		}
		if text, err = a.replyPrompt(o.pos[0], o.vals["prompt"]); err != nil {
			return err
		}
	case len(o.pos) == 1 && !o.set["attach"]:
		if text, err = a.stdinText(); err != nil {
			return err
		}
	}
	refs, err := a.attachRefs(o, false)
	if err != nil {
		return err
	}
	_, err = a.recordReply(o.pos[0], text, "cli", beh, notify, o.vals["prompt"], refs...)
	return err
}

// checkNotify validates a message's own notification overrides, for the
// response to it; nil when there are none.
func checkNotify(desktop, remote string) (map[string]string, error) {
	m := map[string]string{}
	for key, v := range map[string]string{"notify.desktop": desktop, "notify.remote": remote} {
		if v == "" {
			continue
		}
		if err := settings.Check(key, v); err != nil {
			return nil, usagef("%v", err)
		}
		m[strings.TrimPrefix(key, "notify.")] = v
	}
	if len(m) == 0 {
		return nil, nil
	}
	return m, nil
}

// replyEvent prepares a reply to an item or node.
func (a *App) replyEvent(token, text, source, behavior string, notify map[string]string, refs ...media.Ref) (store.Event, error) {
	if strings.TrimSpace(text) == "" && len(refs) > 0 {
		text = attachedOnly
	}
	if strings.TrimSpace(text) == "" {
		return store.Event{}, usagef("reply needs the message's text")
	}
	d, err := a.load()
	if err != nil {
		return store.Event{}, err
	}
	to, host, node, about, err := replyTarget(d, token)
	if err != nil {
		return store.Event{}, err
	}
	return a.replyTo(d, to, host, node, about, text, source, behavior, notify, refs...)
}

// replyTo is the reply to a recipient already resolved: to on host, at node,
// about an item (none for a message to the node itself).
func (a *App) replyTo(d *desk.Desk, to ident.From, host, node string, about store.Event, text, source, behavior string, notify map[string]string, refs ...media.Ref) (store.Event, error) {
	var err error
	if behavior, err = checkBehavior(behavior); err != nil {
		return store.Event{}, err
	}
	if behavior == "" {
		if behavior, err = a.defaultBehavior(d, node); err != nil {
			return store.Event{}, err
		}
	}
	extra := map[string]any{"to": to, "to_host": host, "behavior": behavior}
	if notify != nil {
		extra["notify"] = notify
	}
	if len(refs) > 0 {
		extra["media"] = refs
	}
	e := a.event("reply", node, ident.Resolve(a.Env), source, extra)
	e.Ref, e.Body = about.ID, text
	return e, nil
}

// recordReply appends a reply and delivers it for the CLI; auto marks a
// generated one (markAuto).
func (a *App) recordReply(token, text, source, behavior string, notify map[string]string, auto string, refs ...media.Ref) (store.Event, error) {
	e, err := a.replyEvent(token, text, source, behavior, notify, refs...)
	if err != nil {
		return e, err
	}
	markAuto(&e, auto)
	_, host := toOf(e)
	if err := a.commit1(e); err != nil {
		return e, err
	}
	if host != a.Host {
		a.pushTo(host, "that session is on "+host)
		return e, nil
	}
	_, err = a.deliver(e.ID)
	return e, err
}

// toOf reads where a reply goes.
func toOf(e store.Event) (ident.From, string) {
	var m struct {
		To     ident.From `json:"to"`
		ToHost string     `json:"to_host"`
	}
	_ = json.Unmarshal(e.Meta, &m)
	return m.To, m.ToHost
}

// ReplyPrompts are the prepared messages `reply --prompt` sends, each a
// template in internal/prompts that the user may override.
var ReplyPrompts = map[string]string{
	"velocity-report": "velocity-report.md", // an executive report on velocity
	"task-status":     "task-status.md",     // an executive summary of where a task stands
}

// replyPrompt renders the prepared message name for the node behind target
// (task-status: for the task target is).
func (a *App) replyPrompt(target, name string) (string, error) {
	file, ok := ReplyPrompts[name]
	if !ok {
		names := make([]string, 0, len(ReplyPrompts))
		for n := range ReplyPrompts {
			names = append(names, n)
		}
		sort.Strings(names)
		return "", usagef("no prepared message %q; there is: %s", name, strings.Join(names, ", "))
	}
	d, err := a.load()
	if err != nil {
		return "", err
	}
	_, _, node, _, err := replyTarget(d, target)
	if err != nil {
		return "", err
	}
	data := struct{ Node, Velocity, Task, Title, State string }{Node: node}
	for _, sl := range d.Slots() {
		if sl.Path == node+"/velocity" && sl.Kind == "set" {
			data.Velocity = sl.Body
		}
	}
	if name == "task-status" {
		t, err := a.task(d, target)
		if err != nil {
			return "", usagef("task-status is for a task: %v", err)
		}
		data.Task, data.Title, data.State = t.ID, t.Event.Title, t.State
	}
	return prompts.Render(a.promptsDir(), file, data)
}

// coordinatorUnder is the node to tell about something filed at a node no
// agent holds, a project or a run: the coordinator working under it. A
// coordinator holds a node three deep (<project>/<run>/<role>); of those
// with an agent joined, one that has not filed a done report since it
// joined comes first, then the one whose work (its own events or its
// workers') is newest. ok is false when no agent ever joined under it.
func coordinatorUnder(d *desk.Desk, prefix string) (string, bool) {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" || strings.HasPrefix(prefix, "_desk") {
		return "", false
	}
	type cand struct {
		last     int  // index of the newest event at or below it
		joined   int  // index of its latest join, -1 for none
		finished bool // a done at the node after that join
	}
	cands := map[string]*cand{}
	for i, e := range d.Events {
		if !desk.Under(e.Path, prefix) {
			continue
		}
		parts := strings.SplitN(desk.NodeOf(e), "/", 4)
		if len(parts) < 3 {
			continue
		}
		key := strings.Join(parts[:3], "/")
		c := cands[key]
		if c == nil {
			c = &cand{joined: -1}
			cands[key] = c
		}
		c.last = i
		at := desk.NodeOf(e) == key
		switch {
		case e.Kind == "join" && at:
			c.joined, c.finished = i, false
		case e.Kind == "done" && at && c.joined >= 0:
			c.finished = true
		}
	}
	best, bestC := "", (*cand)(nil)
	for key, c := range cands {
		if c.joined < 0 {
			continue
		}
		if bestC == nil || (bestC.finished && !c.finished) || (bestC.finished == c.finished && c.last > bestC.last) {
			best, bestC = key, c
		}
	}
	return best, bestC != nil
}

// taskRecipient is who a message about task goes to: the agent that last
// changed its state (started it, noted progress, finished it), else one
// launched on it (meta.agent "paseo:<id>") once it has joined a node, else
// its author (to, host, node as resolved for any item), else for a task
// filed from the dashboard the node's agent or the coordinator working
// under it. A node taken over since goes to whoever holds it now.
func taskRecipient(d *desk.Desk, task store.Event, to ident.From, host, node string) (ident.From, string, string) {
	for i := len(d.Events) - 1; i >= 0; i-- {
		e := d.Events[i]
		if e.Kind != "taskstate" || e.Ref != task.ID || (desk.MetaString(e, "state") == "" && desk.MetaString(e, "agent") == "") {
			continue // a tag or title edit is not working on it
		}
		if f := fromOf(e); f.Session != "" || f.Paseo != "" {
			if j, ok := latestJoin(d, func(j store.Event) bool { return sessionOf(j) == f.Session && f.Session != "" }); ok {
				return heldBy(d, j)
			}
			return f, e.Host, node
		}
		if p, ok := strings.CutPrefix(desk.MetaString(e, "agent"), "paseo:"); ok && p != "" {
			if j, ok := latestJoin(d, func(j store.Event) bool { return fromOf(j).Paseo == p }); ok {
				return heldBy(d, j)
			}
		}
	}
	if to.Session != "" || to.Paseo != "" || to.TmuxPane != "" {
		return to, host, node
	}
	if owner, ok := nodeOwner(d, node); ok {
		return fromOf(owner), owner.Host, node
	}
	if c, ok := coordinatorUnder(d, node); ok {
		owner, _ := nodeOwner(d, c)
		return fromOf(owner), owner.Host, c
	}
	return to, host, node
}

// latestJoin is the newest join event match accepts.
func latestJoin(d *desk.Desk, match func(store.Event) bool) (store.Event, bool) {
	for i := len(d.Events) - 1; i >= 0; i-- {
		if e := d.Events[i]; e.Kind == "join" && match(e) {
			return e, true
		}
	}
	return store.Event{}, false
}

// heldBy is the agent holding join's node now: the joiner, or one that took
// the node over since.
func heldBy(d *desk.Desk, join store.Event) (ident.From, string, string) {
	if owner, ok := nodeOwner(d, join.Path); ok && owner.ID > join.ID {
		return fromOf(owner), owner.Host, join.Path
	}
	return fromOf(join), join.Host, join.Path
}
