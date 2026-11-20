// Package render writes the plain org files a person or a recovering agent
// can read with cat: the inbox, one board per project and the ledger by
// month. Every file is replaced atomically.
package render

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// All re-renders every file under dir/render. A lock serialises concurrent
// renders, and each render reads the store after taking it, so the last
// render to finish always reflects every committed event.
func All(dir string, load func() (*desk.Desk, error)) error {
	out := filepath.Join(dir, "render")
	if err := os.MkdirAll(filepath.Join(out, "board"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(out, "ledger"), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(out, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	d, err := load()
	if err != nil {
		return err
	}

	var b bytes.Buffer
	w := &desk.Writer{W: &b, Desk: d, Org: true, Props: true}
	w.Header("agent-desk inbox")
	w.View(d.Build(desk.Filter{}))
	if err := atomicWrite(filepath.Join(out, "inbox.org"), b.Bytes()); err != nil {
		return err
	}

	for _, n := range d.Tree(desk.Filter{All: true}) {
		b.Reset()
		w.Header("agent-desk board: " + n.Name)
		w.Node(1, n)
		if err := atomicWrite(filepath.Join(out, "board", n.Name+".org"), b.Bytes()); err != nil {
			return err
		}
	}

	// The ledger lists questions, notices, done reports, deliverables and cache changes by month. A
	// conversation hangs under its root item as child headings; a message
	// whose root is not listed (a node's own thread, or a slot) is a heading
	// of its own, with its responses under it.
	months := map[string][]desk.Item{}
	qs := map[string]int{}
	for i, q := range d.Questions {
		qs[q.ID] = i
	}
	for _, e := range d.Events {
		switch {
		case e.Kind == "ask" || e.Kind == "done" || e.Kind == "deliverable" || e.Kind == "cache":
		case e.Kind == "tell" && e.Ref == "":
		case e.Kind == "reply":
			if root, ok := d.Root(e); ok && listed(root) {
				continue
			}
		default:
			continue
		}
		if len(e.TS) < 7 {
			continue
		}
		m := e.TS[:7]
		it := d.Item(e)
		it.Label, it.Age = e.Path, e.TS[:16]
		if e.Kind == "ask" {
			q := d.Questions[qs[e.ID]]
			it.Question = &q
		}
		months[m] = append(months[m], it)
	}
	keys := make([]string, 0, len(months))
	for k := range months {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, m := range keys {
		b.Reset()
		w.Header("agent-desk ledger " + m)
		for _, it := range months[m] {
			switch it.Event.Kind {
			case "ask":
				w.Question(1, it)
			case "reply":
				w.Thread(1, []store.Event{it.Event})
				w.Thread(2, d.ResponsesTo(it.Event.ID))
				continue
			default:
				w.Notice(1, it)
			}
			w.Thread(2, d.Thread(it.Event.ID, ""))
		}
		if err := atomicWrite(filepath.Join(out, "ledger", m+".org"), b.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

// listed reports whether the ledger lists e as an item of its own.
func listed(e store.Event) bool {
	return e.Kind == "ask" || e.Kind == "done" || e.Kind == "deliverable" || (e.Kind == "tell" && e.Ref == "")
}

func atomicWrite(path string, data []byte) error {
	if filepath.Ext(path) == ".org" {
		data = LinkMentions(data)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}
