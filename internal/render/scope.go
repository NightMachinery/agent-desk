package render

// One scope of the desk as one org file, for the dashboard's Export commands
// (api/export?scope=...) and `agent-desk export`: the whole desk, a node, an
// item or the inbox. The files under <desk>/render stay as they are.

import (
	"bytes"
	"fmt"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/store"
)

func writer(b *bytes.Buffer, d *desk.Desk) *desk.Writer {
	return &desk.Writer{W: b, Desk: d, Org: true, Props: true}
}

// Whole is the whole desk in one file: the inbox, the boards, the tasks and
// the ledger by month, each a top-level heading.
func Whole(d *desk.Desk) []byte {
	var b bytes.Buffer
	w := writer(&b, d)
	w.Header("agent-desk")
	b.WriteString("* Inbox\n")
	w.ViewAt(2, d.Build(desk.Filter{}))
	b.WriteString("* Boards\n")
	for _, n := range d.Tree(desk.Filter{All: true}) {
		w.Node(2, n)
	}
	if ts := d.Tasks(desk.Filter{All: true}); len(ts) > 0 {
		b.WriteString("* Tasks\n")
		desk.TasksOrg(&b, ts, "", false, 2)
	}
	b.WriteString("* Ledger\n")
	months, byMonth := ledger(d, func(store.Event) bool { return true })
	for _, m := range months {
		fmt.Fprintf(&b, "** %s\n", m)
		for _, it := range byMonth[m] {
			ledgerItem(w, 3, it)
		}
	}
	return LinkMentions(b.Bytes())
}

// Node is the node at path and everything under it: its board (slots and
// children), its tasks, and its items with their conversations, oldest
// first. ok is false when no node is there.
func Node(d *desk.Desk, path string) (out []byte, ok bool) {
	n := desk.Find(d.Tree(desk.Filter{All: true}), path)
	if n == nil {
		return nil, false
	}
	var b bytes.Buffer
	w := writer(&b, d)
	w.Header("agent-desk: " + path)
	b.WriteString("* Board\n")
	w.Node(2, n)
	if ts := d.Tasks(desk.Filter{Prefix: path, All: true}); len(ts) > 0 {
		b.WriteString("* Tasks\n")
		desk.TasksOrg(&b, ts, "", false, 2)
	}
	months, byMonth := ledger(d, func(e store.Event) bool { return desk.Under(e.Path, path) })
	if len(months) > 0 {
		b.WriteString("* Items\n")
		for _, m := range months {
			for _, it := range byMonth[m] {
				ledgerItem(w, 2, it)
			}
		}
	}
	return LinkMentions(b.Bytes()), true
}

// Item is one item with the conversation it belongs to: a message or a
// response comes with its whole conversation, a task with its subtasks.
func Item(d *desk.Desk, e store.Event) []byte {
	if root, ok := d.Root(e); ok && root.ID != e.ID {
		e = root
	}
	var b bytes.Buffer
	w := writer(&b, d)
	w.Header("agent-desk " + d.Display(e.ID) + ": " + desk.ThreadTitle(e))
	it := d.Item(e)
	it.Label, it.Age = e.Path, e.TS[:min(16, len(e.TS))]
	for i := range d.Questions {
		if d.Questions[i].ID == e.ID {
			q := d.Questions[i]
			it.Question = &q
		}
	}
	if e.Kind != "task" {
		ledgerItem(w, 1, it)
		return LinkMentions(b.Bytes())
	}
	w.Notice(1, it)
	var task []desk.Task
	for _, t := range d.Tasks(desk.Filter{All: true}) {
		if t.Event.ID == e.ID || t.Parent == e.ID {
			task = append(task, t)
		}
	}
	desk.TasksOrg(&b, task, "", true, 2)
	w.Thread(2, d.Thread(e.ID, ""))
	return LinkMentions(b.Bytes())
}

// Inbox is the inbox under prefix (all of it when empty): what needs you,
// deliverables, pinned items, responses, what is new, and the runs.
func Inbox(d *desk.Desk, prefix string) []byte {
	var b bytes.Buffer
	w := writer(&b, d)
	title := "agent-desk inbox"
	if prefix != "" {
		title += ": " + prefix
	}
	w.Header(title)
	w.View(d.Build(desk.Filter{Prefix: prefix}))
	return LinkMentions(b.Bytes())
}
