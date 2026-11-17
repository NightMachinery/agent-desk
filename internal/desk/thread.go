package desk

import (
	"time"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// Threads are derived from ref, never stored: a message (reply) refers to
// what it is about, an answer to its question, and a response (a tell with a
// ref) to the message or answer it responds to. Following ref upwards from
// any of them reaches the root item; a message with no ref belongs to its
// node's thread.

// NoResponseAfter is how long a delivered message may wait for a response
// before every view flags it.
const NoResponseAfter = 15 * time.Minute

// IsResponse reports whether e is an agent's response to the user: to a
// message or an answer (ref), or to something the user asked in chat
// (meta.asked).
func IsResponse(e store.Event) bool {
	return e.Kind == "tell" && (e.Ref != "" || MetaString(e, "asked") != "")
}

// Asked is what a response responds to, and how it was asked: "message"
// (from the desk), "answer" (the user's answer to a question) or "chat".
func (d *Desk) Asked(e store.Event) (text, via string) {
	if !IsResponse(e) {
		return "", ""
	}
	if q := MetaString(e, "asked"); q != "" {
		return q, "chat"
	}
	r := d.ByID[e.Ref]
	if r.Kind == "answer" {
		return r.Body, "answer"
	}
	return r.Body, "message"
}

// inThread reports whether e is a message, an answer or a response: the
// events a thread is made of, as opposed to its root.
func inThread(e store.Event) bool {
	return e.Kind == "reply" || e.Kind == "answer" || IsResponse(e)
}

// Root is the item e's thread hangs from. ok is false for a node thread: a
// message with no ref, or anything leading back to one.
func (d *Desk) Root(e store.Event) (root store.Event, ok bool) {
	for i := 0; i < 64 && inThread(e); i++ {
		if e.Ref == "" {
			return store.Event{}, false
		}
		next, found := d.ByID[e.Ref]
		if !found {
			return store.Event{}, false
		}
		e = next
	}
	return e, e.ID != ""
}

// Thread is every message, answer, delivery mark and response whose root is
// the event id (or, with id empty, the node thread of path), in order.
func (d *Desk) Thread(id, path string) []store.Event {
	var out []store.Event
	for _, e := range d.Events {
		x := e
		if e.Kind == "delivered" {
			x = d.ByID[e.Ref]
		}
		if !inThread(x) {
			continue
		}
		root, ok := d.Root(x)
		if (id != "" && ok && root.ID == id) || (id == "" && !ok && NodeOf(x) == path) {
			out = append(out, e)
		}
	}
	return out
}

// ResponsesTo are the responses to message or answer id, oldest first.
func (d *Desk) ResponsesTo(id string) []store.Event {
	var out []store.Event
	for _, e := range d.Events {
		if IsResponse(e) && e.Ref == id {
			out = append(out, e)
		}
	}
	return out
}

// Delivery is the delivered mark of message id, if any.
func (d *Desk) Delivery(id string) (store.Event, bool) {
	var found store.Event
	for _, e := range d.Events {
		if e.Kind == "delivered" && e.Ref == id {
			found = e
		}
	}
	return found, found.ID != ""
}

// Marked reports whether the user's latest read mark on id stands.
func (d *Desk) Marked(id string) bool { return d.marked[id] }

// Awaiting reports whether message id was delivered, has no response and
// has not been marked read: it waits for its agent to respond.
func (d *Desk) Awaiting(id string) bool {
	e := d.ByID[id]
	if e.Kind != "reply" || d.marked[id] || len(d.ResponsesTo(id)) > 0 {
		return false
	}
	_, ok := d.Delivery(id)
	return ok
}

// NoResponse reports whether message id has waited longer than
// NoResponseAfter since it was delivered. Answers are never flagged.
func (d *Desk) NoResponse(id string) bool {
	if !d.Awaiting(id) {
		return false
	}
	del, _ := d.Delivery(id)
	return d.Now.Sub(Time(del)) >= NoResponseAfter
}

// AwaitingAt lists the messages for node path that wait for a response.
func (d *Desk) AwaitingAt(path string) []store.Event {
	var out []store.Event
	for _, e := range d.Events {
		if e.Kind == "reply" && e.Path == path && d.Awaiting(e.ID) {
			out = append(out, e)
		}
	}
	return out
}
