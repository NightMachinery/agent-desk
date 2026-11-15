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

	months := map[string][]desk.Item{}
	qs := map[string]int{}
	for i, q := range d.Questions {
		qs[q.ID] = i
	}
	for _, e := range d.Events {
		switch e.Kind {
		case "ask", "tell", "done":
		default:
			continue
		}
		if len(e.TS) < 7 {
			continue
		}
		m := e.TS[:7]
		it := desk.Item{Event: e, ID: d.Display(e.ID), Label: e.Path, Age: e.TS[:16], Unread: d.Unread(e.ID)}
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
			if it.Event.Kind == "ask" {
				w.Question(1, it)
			} else {
				w.Notice(1, it)
			}
		}
		if err := atomicWrite(filepath.Join(out, "ledger", m+".org"), b.Bytes()); err != nil {
			return err
		}
	}
	return nil
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
