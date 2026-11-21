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
	files := Files(d)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := atomicWrite(filepath.Join(out, filepath.FromSlash(name)), files[name]); err != nil {
			return err
		}
	}
	return nil
}

// Files renders the desk's org files, by path under the render directory
// (inbox.org, board/<project>.org, ledger/<yyyy-mm>.org): what All writes
// and the dashboard's Export packs.
func Files(d *desk.Desk) map[string][]byte {
	files := map[string][]byte{}
	var b bytes.Buffer
	w := &desk.Writer{W: &b, Desk: d, Org: true, Props: true}
	w.Header("agent-desk inbox")
	w.View(d.Build(desk.Filter{}))
	files["inbox.org"] = bytes.Clone(b.Bytes())

	for _, n := range d.Tree(desk.Filter{All: true}) {
		b.Reset()
		w.Header("agent-desk board: " + n.Name)
		w.Node(1, n)
		files["board/"+n.Name+".org"] = bytes.Clone(b.Bytes())
	}

	months, byMonth := ledger(d, func(store.Event) bool { return true })
	for _, m := range months {
		b.Reset()
		w.Header("agent-desk ledger " + m)
		for _, it := range byMonth[m] {
			ledgerItem(w, 1, it)
		}
		files["ledger/"+m+".org"] = bytes.Clone(b.Bytes())
	}
	for name, b := range files {
		files[name] = LinkMentions(b)
	}
	return files
}

// ledger is what the ledger lists, of the events keep keeps, by month (in
// order): questions, notices, done reports, deliverables and cache changes.
// A conversation hangs under its root item; a message whose root is not
// listed (a node's own thread, or a slot) is an item of its own, with its
// responses under it.
func ledger(d *desk.Desk, keep func(store.Event) bool) ([]string, map[string][]desk.Item) {
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
		if len(e.TS) < 7 || !keep(e) {
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
	return keys, months
}

// ledgerItem writes one ledger item at level, its conversation under it.
func ledgerItem(w *desk.Writer, level int, it desk.Item) {
	d := w.Desk
	switch it.Event.Kind {
	case "ask":
		w.Question(level, it)
	case "reply":
		w.Thread(level, []store.Event{it.Event})
		w.Thread(level+1, d.ResponsesTo(it.Event.ID))
		return
	default:
		w.Notice(level, it)
	}
	w.Thread(level+1, d.Thread(it.Event.ID, ""))
}

// listed reports whether the ledger lists e as an item of its own.
func listed(e store.Event) bool {
	return e.Kind == "ask" || e.Kind == "done" || e.Kind == "deliverable" || (e.Kind == "tell" && e.Ref == "")
}

func atomicWrite(path string, data []byte) error {
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
