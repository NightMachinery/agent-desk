package desk

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// View is what `show` prints.
type View struct {
	NeedsYou []Group `json:"needs_you"`
	New      []Group `json:"new"`
	Runs     []*Node `json:"runs"`
}

// Build assembles the three printed views for a filter.
func (d *Desk) Build(f Filter) View {
	v := View{NeedsYou: d.NeedsYou(f), New: d.New(f), Runs: Visible(d.Tree(f))}
	// Empty views are empty lists, not null, for JSON readers.
	if v.NeedsYou == nil {
		v.NeedsYou = []Group{}
	}
	if v.New == nil {
		v.New = []Group{}
	}
	if v.Runs == nil {
		v.Runs = []*Node{}
	}
	return v
}

func count(gs []Group) int {
	n := 0
	for _, g := range gs {
		n += len(g.Items)
	}
	return n
}

// Writer prints views as org or markdown.
type Writer struct {
	W     io.Writer
	Desk  *Desk
	Org   bool // org, else markdown
	Props bool // org property drawers
}

func (w *Writer) h(level int, text string) {
	mark := "#"
	if w.Org {
		mark = "*"
	}
	fmt.Fprintf(w.W, "%s %s\n", strings.Repeat(mark, level), text)
}

func (w *Writer) props(kv ...string) {
	if !w.Org || !w.Props {
		return
	}
	fmt.Fprintln(w.W, ":PROPERTIES:")
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			fmt.Fprintf(w.W, ":%s: %s\n", kv[i], kv[i+1])
		}
	}
	fmt.Fprintln(w.W, ":END:")
}

// text prints a body indented, so that no line can start a heading.
func (w *Writer) text(s string) {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return
	}
	for _, line := range strings.Split(s, "\n") {
		if line == "" {
			fmt.Fprintln(w.W)
			continue
		}
		fmt.Fprintf(w.W, "  %s\n", line)
	}
}

func (w *Writer) hostTag(host string) string {
	if host == "" || host == w.Desk.Host {
		return ""
	}
	return " @" + host
}

func kwd(org bool, k string) string {
	if k == "" {
		return ""
	}
	if org {
		return k + " "
	}
	return "**" + k + "** "
}

// Header starts an org file with the keywords the items use.
func (w *Writer) Header(title string) {
	if w.Org {
		fmt.Fprintf(w.W, "#+TITLE: %s\n#+TODO: ASK NEW | ANSWERED\n#+STARTUP: overview\n\n", title)
	}
}

// Question prints one question at a heading level.
func (w *Writer) Question(level int, it Item) {
	q := it.Question
	k := "ASK"
	if q != nil && !q.Open() {
		k = "ANSWERED"
	}
	label := it.Label
	if label == "" {
		label = it.Event.Path
	}
	w.h(level, fmt.Sprintf("%s%s %s  (%s · %s · %s%s)", kwd(w.Org, k), it.ID, it.Event.Title,
		label, it.Event.Level, it.Age, w.hostTag(it.Event.Host)))
	w.props("DESK_ID", it.ID, "DESK_PATH", it.Event.Path, "DESK_TS", it.Event.TS, "DESK_HOST", it.Event.Host)
	w.text(it.Event.Body)
	if def := MetaString(it.Event, "default"); def != "" {
		w.text("Default: " + def)
	}
	if q != nil && !q.Open() {
		ans := w.Desk.ByID[q.AnswerID]
		w.text(fmt.Sprintf("Answer (%s, %s): %s", w.Desk.Display(q.AnswerID), MetaString(ans, "source"), q.AnswerBody))
	}
}

// Notice prints one tell, done or slot item.
func (w *Writer) Notice(level int, it Item) {
	k := ""
	if it.Unread {
		k = "NEW"
	}
	what := it.Event.Kind
	title := it.Event.Title
	if it.Event.Kind == "set" {
		what, title = "slot", SlotName(it.Event)
	}
	label := it.Label
	if it.Event.Kind == "set" {
		label = NodeOf(it.Event)
		if _, _, rest := Split(label); rest != "" {
			label = rest
		}
	}
	w.h(level, fmt.Sprintf("%s%s %s: %s  (%s · %s%s)", kwd(w.Org, k), it.ID, what, title, label, it.Age,
		w.hostTag(it.Event.Host)))
	w.props("DESK_ID", it.ID, "DESK_PATH", it.Event.Path, "DESK_TS", it.Event.TS, "DESK_HOST", it.Event.Host)
	w.text(it.Event.Body)
	if r := MetaString(it.Event, "report"); r != "" {
		w.text("Report: " + r)
	}
}

func (w *Writer) groups(level int, gs []Group, each func(int, Item)) {
	project := ""
	for _, g := range gs {
		if g.Project != project {
			w.h(level, g.Project)
			project = g.Project
		}
		w.h(level+1, g.Run)
		for _, it := range g.Items {
			each(level+2, it)
		}
	}
}

func (n *Node) summary() string {
	var parts []string
	if n.State == Active || n.State == Quiet {
		parts = append(parts, n.State+" "+n.Age)
	} else {
		parts = append(parts, n.State)
	}
	if n.Open > 0 {
		parts = append(parts, fmt.Sprintf("%d ask", n.Open))
	}
	if n.UnreadN > 0 {
		parts = append(parts, fmt.Sprintf("%d new", n.UnreadN))
	}
	return strings.Join(parts, " · ")
}

// Node prints a node and its visible slots and children.
func (w *Writer) Node(level int, n *Node) {
	w.h(level, fmt.Sprintf("%s  [%s]%s", n.Name, n.summary(), w.hostTag(n.Host)))
	w.props("DESK_PATH", n.Path)
	for _, s := range n.Slots {
		k := ""
		if s.Unread {
			k = "NEW"
		}
		w.h(level+1, fmt.Sprintf("%s%s %s  (%s)", kwd(w.Org, k), s.ID, s.Name, Age(w.Desk.Now.Sub(Time(s.Event)))))
		w.props("DESK_ID", s.ID, "DESK_PATH", s.Event.Path, "DESK_TS", s.Event.TS)
		if s.File != "" {
			w.text("File: " + s.File)
			if b, err := os.ReadFile(s.File); err == nil {
				w.text(string(b))
			} else {
				w.text("(cannot read: " + err.Error() + ")")
			}
		} else {
			w.text(s.Event.Body)
		}
	}
	for _, c := range n.Children {
		w.Node(level+1, c)
	}
}

// View prints the three views.
func (w *Writer) View(v View) {
	w.h(1, fmt.Sprintf("Needs you (%d)", count(v.NeedsYou)))
	w.groups(2, v.NeedsYou, w.Question)
	w.h(1, fmt.Sprintf("New (%d)", count(v.New)))
	w.groups(2, v.New, w.Notice)
	w.h(1, "Runs")
	for _, n := range v.Runs {
		w.Node(2, n)
	}
}

// JSON prints a view as JSON.
func JSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
