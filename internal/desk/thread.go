package desk

import (
	"regexp"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// Threads are derived from ref, never stored: a message (reply) refers to
// what it is about, an answer to its question, and a response to the
// message or answer it responds to. Following ref upwards from any of them
// reaches the root item. A message with no ref (sent to a node, about no
// item) is the root of its own thread: only related messages share one.
//
// A generated message (meta.auto: the message about a task the user filed,
// a task change, a prepared prompt) is how the desk reaches an agent, not
// something the user wrote. A response to one that is about an item refers
// to that item, with the message in meta.answers (see Answers): it hangs
// from the item, and the message still counts as answered. Any later
// response in that item's thread (tell --re <task>) answers it too.
//
// Every kind an agent files for the user (AgentItem: a notice, a question,
// a done report, a deliverable) can be a response: given a ref (--re) or
// what was asked in chat (--re-chat), it sits in that thread, whatever its
// kind. A new agent item kind gets this by being added to AgentItem.

// NoResponseAfter is how long a delivered message may wait for a response
// before every view flags it.
const NoResponseAfter = 15 * time.Minute

// AgentItem reports whether kind is one an agent files for the user: a
// notice (tell), a question (ask), a done report or a deliverable. Each
// can respond to a message and so be part of a thread.
func AgentItem(kind string) bool {
	switch kind {
	case "tell", "ask", "done", "deliverable":
		return true
	}
	return false
}

// IsResponse reports whether e is an agent's response to the user: to a
// message or an answer (ref), or to something the user asked in chat
// (meta.asked). It is any agent item (AgentItem).
func IsResponse(e store.Event) bool {
	return AgentItem(e.Kind) && (e.Ref != "" || MetaString(e, "asked") != "")
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

// Root is the item e's thread hangs from: an item, or a message sent with
// no ref, which roots its own thread. A response to a question asked in chat
// has no ref either, but it is an item of its own: messages about it hang
// from it. ok is false only for what leads nowhere (a missing ref).
func (d *Desk) Root(e store.Event) (root store.Event, ok bool) {
	for i := 0; i < 64 && inThread(e); i++ {
		if e.Ref == "" {
			if t, ok := d.FiledTask(e); ok {
				return t, true
			}
			if e.Kind == "reply" || (i > 0 && IsResponse(e)) {
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

// filedRE matches the text of the message the dashboard sent about a task
// the user filed, before that message went about the task and carried
// meta.auto: it went to the node, and names the task only in its text.
var filedRE = regexp.MustCompile(`^The user filed an? [a-z]+ (?:at \S+ on the desk, and you are the coordinator working under it|for you on the desk): (w-[a-z0-9]+)`)

// FiledTask is the task such an older message about a filed task names, so
// that it and the responses to it sit in the task's thread like a newer
// one's. Only a message with no ref and no meta.auto, filed after the task.
func (d *Desk) FiledTask(e store.Event) (store.Event, bool) {
	if e.Kind != "reply" || e.Ref != "" || MetaString(e, "auto") != "" {
		return store.Event{}, false
	}
	m := filedRE.FindStringSubmatch(e.Body)
	if m == nil {
		return store.Event{}, false
	}
	t, err := d.Resolve(m[1])
	if err != nil || t.Kind != "task" || t.ID > e.ID {
		return store.Event{}, false
	}
	return t, true
}

// AutoKind is the kind of generated message e is (meta.auto, or task-filed
// for an older message about a filed task), or "" for the user's words.
func (d *Desk) AutoKind(e store.Event) string {
	if a := MetaString(e, "auto"); a != "" {
		return a
	}
	if _, ok := d.FiledTask(e); ok {
		return "task-filed"
	}
	return ""
}

// The thread indexes hold positions in Events. Root IDs and node paths have
// separate keys because an imported node path can also look like an ID.
type threadKey struct {
	id, path string
}

type threadIndex struct {
	entries   map[threadKey][]int
	nodeRoots map[string][]string // node path: ids of the messages with no ref sent to it, oldest first
	responses map[string][]int
	delivered map[string]int
	failed    map[string]int
	progress  map[string]int
}

func (d *Desk) indexThreads() {
	d.threads = threadIndex{
		entries:   map[threadKey][]int{},
		nodeRoots: map[string][]string{},
		responses: map[string][]int{},
		delivered: map[string]int{},
		failed:    map[string]int{},
		progress:  map[string]int{},
	}
	d.latestDeliverable = map[string]string{}
	about := map[string][]string{} // thread root id: the generated messages in its thread so far
	for i, e := range d.Events {
		if k := deliverableKey(e); k != "" {
			d.latestDeliverable[k] = e.ID
		}
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
			to := Answers(e)
			d.threads.responses[to] = append(d.threads.responses[to], i)
		}
		root, ok := d.Root(x)
		// A response in an item's thread answers every generated message
		// about that item before it: the message about a filed task asks
		// for tell --re <task>, the agent may answer the user's follow-up
		// instead, and two such messages want one report.
		if ok && x.ID == e.ID && root.ID != e.ID {
			if IsResponse(e) {
				for _, m := range about[root.ID] {
					if m != Answers(e) {
						d.threads.responses[m] = append(d.threads.responses[m], i)
					}
				}
			} else if e.Kind == "reply" && d.AutoKind(e) != "" {
				about[root.ID] = append(about[root.ID], e.ID)
			}
		}
		k := threadKey{id: root.ID}
		if !ok {
			k = threadKey{path: NodeOf(x)}
		} else if root.ID == x.ID && x.Kind == "reply" && x.ID == e.ID {
			d.threads.nodeRoots[x.Path] = append(d.threads.nodeRoots[x.Path], x.ID)
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

// MessageThreads are the messages sent to node path with no ref, oldest
// first: each roots a thread of its own (Thread(id, "")).
func (d *Desk) MessageThreads(path string) []store.Event {
	ids := d.threads.nodeRoots[path]
	out := make([]store.Event, 0, len(ids))
	for _, id := range ids {
		out = append(out, d.ByID[id])
	}
	return out
}

// Answers is the message or answer response e answers: meta.answers when
// its ref was moved from a generated message to that message's item, else
// its ref.
func Answers(e store.Event) string {
	if m := MetaString(e, "answers"); m != "" {
		return m
	}
	return e.Ref
}

// ThreadTitle names the thread root heads: a slot's name, an item's title,
// or a message's first line (a message has no title).
func ThreadTitle(root store.Event) string {
	switch {
	case IsSlotKind(root.Kind):
		return SlotName(root)
	case root.Title != "":
		return root.Title
	case root.Kind == "reply":
		line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(root.Body), "\n", 2)[0])
		if r := []rune(line); len(r) > 80 {
			line = string(r[:79]) + "…"
		}
		return line
	}
	return ""
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

// ReadSince lists the events marked read at or after t whose mark stands.
func (d *Desk) ReadSince(t time.Time) []string {
	var out []string
	for id, at := range d.readAt {
		if !at.Before(t) {
			out = append(out, id)
		}
	}
	return out
}

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
// tell-notify with --notify, else tell; a done report is done; a deliverable
// is deliverable, responding or not. Other events have no class.
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
	case "deliverable":
		return "deliverable"
	}
	return ""
}

// IsMark reports whether kind is bookkeeping about other events (a read,
// a pin, a setting, a project set or the focus, a notification raised or
// taken back) rather than something an agent or the user said. Delivered
// marks count as activity and are not included.
func IsMark(kind string) bool {
	switch kind {
	case "read", "pin", "defer", "pref", "notified", "retracted", "undelivered", "projectset", "focus", "cache":
		return true
	}
	return false
}
