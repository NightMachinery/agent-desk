package desk

import (
	"time"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// Threads are derived from ref, never stored: a message (reply) refers to
// what it is about, an answer to its question, and a response (a tell or an
// ask with a ref) to the message or answer it responds to. Following ref upwards from
// any of them reaches the root item; a message with no ref belongs to its
// node's thread.

// NoResponseAfter is how long a delivered message may wait for a response
// before every view flags it.
const NoResponseAfter = 15 * time.Minute

// IsResponse reports whether e is an agent's response to the user: to a
// message or an answer (ref), or to something the user asked in chat
// (meta.asked). It is a tell, or an ask (a question response).
func IsResponse(e store.Event) bool {
	return (e.Kind == "tell" || e.Kind == "ask") && (e.Ref != "" || MetaString(e, "asked") != "")
}

// IsQuestionResponse reports whether e is a response that is also a
// question: it needs the user's answer, and is listed as a question.
func IsQuestionResponse(e store.Event) bool {
	return e.Kind == "ask" && IsResponse(e)
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
// message with no ref, or anything leading back to one. A response to a
// question asked in chat has no ref either, but it is an item of its own:
// messages about it hang from it.
func (d *Desk) Root(e store.Event) (root store.Event, ok bool) {
	for i := 0; i < 64 && inThread(e); i++ {
		if e.Ref == "" {
			if i > 0 && IsResponse(e) {
				return e, true
			}
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

// The thread indexes hold positions in Events. Root IDs and node paths have
// separate keys because an imported node path can also look like an ID.
type threadKey struct {
	id, path string
}

type threadIndex struct {
	entries   map[threadKey][]int
	responses map[string][]int
	delivered map[string]int
	failed    map[string]int
	progress  map[string]int
}

func (d *Desk) indexThreads() {
	d.threads = threadIndex{
		entries:   map[threadKey][]int{},
		responses: map[string][]int{},
		delivered: map[string]int{},
		failed:    map[string]int{},
		progress:  map[string]int{},
	}
	for i, e := range d.Events {
		x := e
		switch e.Kind {
		case "delivered":
			d.threads.delivered[e.Ref] = i
			delete(d.threads.failed, e.Ref)
			delete(d.threads.progress, e.Ref)
			x = d.ByID[e.Ref]
		case "undelivered", "delivery-state":
			if _, delivered := d.threads.delivered[e.Ref]; delivered {
				continue
			}
			d.threads.progress[e.Ref] = i
			if e.Kind == "undelivered" {
				d.threads.failed[e.Ref] = i
			} else if MetaString(e, "status") == "queued" {
				delete(d.threads.failed, e.Ref)
			}
		}
		if !inThread(x) {
			continue
		}
		if IsResponse(e) {
			d.threads.responses[e.Ref] = append(d.threads.responses[e.Ref], i)
		}
		root, ok := d.Root(x)
		k := threadKey{id: root.ID}
		if !ok {
			k = threadKey{path: NodeOf(x)}
		}
		d.threads.entries[k] = append(d.threads.entries[k], i)
	}
}

func (d *Desk) threadEvents(indices []int) []store.Event {
	if len(indices) == 0 {
		return nil
	}
	out := make([]store.Event, len(indices))
	for i, at := range indices {
		out[i] = d.Events[at]
	}
	return out
}

// Thread is every message, answer, delivery mark and response whose root is
// the event id (or, with id empty, the node thread of path), in order.
func (d *Desk) Thread(id, path string) []store.Event {
	if id != "" {
		path = ""
	}
	return d.threadEvents(d.threads.entries[threadKey{id: id, path: path}])
}

// ResponsesTo are the responses to message or answer id, oldest first.
func (d *Desk) ResponsesTo(id string) []store.Event {
	return d.threadEvents(d.threads.responses[id])
}

// Delivery is the delivered mark of message id, if any.
func (d *Desk) Delivery(id string) (store.Event, bool) {
	if i, ok := d.threads.delivered[id]; ok {
		return d.Events[i], true
	}
	return store.Event{}, false
}

// DeliveryProgress is the latest queue action or failure of a message
// still awaiting delivery.
func (d *Desk) DeliveryProgress(id string) (store.Event, bool) {
	if i, ok := d.threads.progress[id]; ok {
		return d.Events[i], true
	}
	return store.Event{}, false
}

// DeliveryFailure is the latest failed attempt in the current retry window.
func (d *Desk) DeliveryFailure(id string) (store.Event, bool) {
	if i, ok := d.threads.failed[id]; ok {
		return d.Events[i], true
	}
	return store.Event{}, false
}

// Marked reports whether the user's latest read mark on id stands.
func (d *Desk) Marked(id string) bool { return d.marked[id] }

// Awaiting reports whether message id was delivered, has no response and
// has not been marked read: it waits for its agent to respond.
func (d *Desk) Awaiting(id string) bool {
	e := d.ByID[id]
	if e.Kind != "reply" || d.marked[id] || len(d.threads.responses[id]) > 0 {
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

// ClassOf is the settings class of a notifiable event: a question is
// ask-blocked or ask-default; a tell is response when it responds, else
// tell-notify with --notify, else tell; a done report is done. Other events
// have no class.
func ClassOf(e store.Event) string {
	switch e.Kind {
	case "ask":
		if e.Level == "blocked" {
			return "ask-blocked"
		}
		return "ask-default"
	case "tell":
		if IsResponse(e) {
			return "response"
		}
		if e.Level == "notify" {
			return "tell-notify"
		}
		return "tell"
	case "done":
		return "done"
	}
	return ""
}

// IsMark reports whether kind is bookkeeping about other events (a read,
// a pin, a setting, a project set or the focus, a notification raised or
// taken back) rather than something an agent or the user said. Delivered
// marks count as activity and are not included.
func IsMark(kind string) bool {
	switch kind {
	case "read", "pin", "pref", "notified", "retracted", "undelivered", "projectset", "focus", "cache":
		return true
	}
	return false
}
