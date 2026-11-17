package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
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
				// About what it answered or replied to, not the user's own words.
				if e.Ref == "" {
					return replyTarget(d, desk.NodeOf(e))
				}
				e = d.ByID[e.Ref]
			case "read", "delivered", "pin", "pref":
				return to, "", "", e, fmt.Errorf("%s is a %s mark; reply to the item it marks", d.Display(e.ID), e.Kind)
			}
			about, node, to, host = e, desk.NodeOf(e), fromOf(e), e.Host
			// A coordinator that took the node over since is the one to tell.
			if owner, ok := nodeOwner(d, node); ok && owner.ID > e.ID && sessionOf(owner) != sessionOf(e) {
				to, host = fromOf(owner), owner.Host
			}
		} else if !strings.HasPrefix(rerr.Error(), "no event") {
			return to, "", "", about, rerr
		}
	}
	if node == "" {
		owner, ok := nodeOwner(d, token)
		if !ok {
			return to, "", "", about, fmt.Errorf("%q is neither an id nor a joined node", token)
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
func ReplyText(d *desk.Desk, reply store.Event) string {
	id := d.Display(reply.ID)
	about := "to " + reply.Path
	if reply.Ref != "" {
		r := d.ByID[reply.Ref]
		about = fmt.Sprintf("about %s \"%s\"", d.Display(r.ID), r.Title)
	}
	return fmt.Sprintf("Message %s from the user via agent-desk, %s: %s\n(Respond on the desk: agent-desk tell --re %s)",
		id, about, reply.Body, id)
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
	default:
		return "", fmt.Errorf("%s is a %s; tell --re takes a message (m-...) or an answer (a-...) sent to you", shown, e.Kind)
	}
	if root, ok := d.Root(e); ok {
		title := root.Title
		if root.Kind == "set" {
			title = desk.SlotName(root)
		}
		return "Re: " + title, nil
	}
	return "Re: " + clipLine(e.Body, 60), nil
}

func (a *App) cmdReply(args []string) error {
	o, err := parse(args, nil, []string{"behavior"}, 1, -1)
	if err != nil {
		return err
	}
	beh, err := checkBehavior(o.vals["behavior"])
	if err != nil {
		return err
	}
	text := strings.Join(o.pos[1:], " ")
	if len(o.pos) == 1 {
		if text, err = a.stdinText(); err != nil {
			return err
		}
	}
	_, err = a.recordReply(o.pos[0], text, "cli", beh)
	return err
}

// recordReply appends a reply and delivers it when its session is on this
// machine. It returns the reply.
func (a *App) recordReply(token, text, source, behavior string) (store.Event, error) {
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
	if behavior, err = checkBehavior(behavior); err != nil {
		return store.Event{}, err
	}
	if behavior == "" {
		if behavior, err = a.defaultBehavior(d, node); err != nil {
			return store.Event{}, err
		}
	}
	e := a.event("reply", node, ident.Resolve(a.Env), source, map[string]any{"to": to, "to_host": host, "behavior": behavior})
	e.Ref, e.Body = about.ID, text
	if err := a.commit1(e); err != nil {
		return e, err
	}
	if host != a.Host {
		fmt.Fprintf(a.Stderr, "agent-desk: that session is on %s; delivered there after the next merge\n", host)
		return e, nil
	}
	if _, err := a.deliver(e.ID); err != nil {
		return e, err
	}
	return e, a.renderWarn()
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
