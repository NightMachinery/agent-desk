package desk

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// TasksOrg prints tasks as an org checklist, one heading per node (at level)
// and subtasks indented under their parent: what a TASKS.org file would hold.
// Dropped tasks are left out unless all; typ keeps one type.
func TasksOrg(w io.Writer, ts []Task, typ string, all bool, level int) {
	keep := func(t Task) bool {
		return (all || t.State != TaskDropped) && (typ == "" || t.Kind() == typ)
	}
	byNode := map[string][]Task{}
	var nodes []string
	children := map[string][]Task{}
	shown := map[string]bool{}
	for _, t := range ts {
		if keep(t) {
			shown[t.Event.ID] = true
		}
	}
	for _, t := range ts {
		if !shown[t.Event.ID] {
			continue
		}
		if t.Parent != "" && shown[t.Parent] {
			children[t.Parent] = append(children[t.Parent], t)
			continue
		}
		if _, ok := byNode[t.Event.Path]; !ok {
			nodes = append(nodes, t.Event.Path)
		}
		byNode[t.Event.Path] = append(byNode[t.Event.Path], t)
	}
	sort.Strings(nodes)
	box := map[string]string{TaskDone: "[X]", TaskStarted: "[-]"}
	var item func(t Task, depth int)
	item = func(t Task, depth int) {
		b := box[t.State]
		if b == "" {
			b = "[ ]"
		}
		title := t.Event.Title
		if t.Type != "" {
			title = t.Type + ": " + title
		}
		if t.State == TaskDropped {
			title = "+" + title + "+"
		}
		for _, g := range t.Tags {
			title += " #" + g
		}
		note := []string{t.ID}
		if t.State != TaskOpen && t.State != TaskDone {
			note = append(note, StateLabel(t.State))
		}
		if t.Priority != 0 {
			note = append(note, fmt.Sprintf("p%d", t.Priority))
		}
		fmt.Fprintf(w, "%s- %s %s (%s)\n", strings.Repeat("  ", depth), b, title, strings.Join(note, ", "))
		for _, c := range children[t.Event.ID] {
			item(c, depth+1)
		}
	}
	for _, n := range nodes {
		fmt.Fprintf(w, "%s %s\n", strings.Repeat("*", level), n)
		for _, t := range byNode[n] {
			item(t, 0)
		}
	}
}
