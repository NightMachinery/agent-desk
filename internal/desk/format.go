package desk

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/media"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// View is what `show` prints.
type View struct {
	Pinned   []Item  `json:"pinned"`
	NeedsYou []Group `json:"needs_you"`
	// Deliverables: what agents shipped (deployments, packages), listed
	// between Needs you and Responses.
	Deliverables []Group `json:"deliverables"`
	// DeliverablesAny counts every deliverable in scope, whatever
	// Filter.Deliverables lists: the section stays to switch to the read
	// ones when no unread one is left.
	DeliverablesAny int     `json:"deliverables_any"`
	Responses       []Group `json:"responses"`
	New             []Group `json:"new"`
	Runs            []*Node `json:"runs"`
	// Awaiting: the user's messages to this node that wait for its response.
	// Filled only for an agent's own view (show --mine).
	Awaiting []Group `json:"awaiting,omitempty"`
	// Focus: the focused project set the view is limited to, if any.
	Focus *FocusView `json:"focus,omitempty"`
}

// FocusView names the focus a view is limited to and what it hides.
type FocusView struct {
	Name     string   `json:"name"`
	Prefixes []string `json:"prefixes"`
	Hidden   Hidden   `json:"hidden"`
}

// AwaitingGroups groups the messages for node path that wait for a response.
func (d *Desk) AwaitingGroups(path string) []Group {
	var items []Item
	for _, e := range d.AwaitingAt(path) {
		items = append(items, d.item(e))
	}
	return group(items)
}

// Build assembles the three printed views for a filter.
func (d *Desk) Build(f Filter) View {
	v := View{Pinned: d.PinnedItems(f), NeedsYou: d.NeedsYou(f), Deliverables: d.DeliverableGroups(f), Responses: d.ResponseGroups(f), New: d.New(f),
		Runs: Visible(d.Tree(f)), DeliverablesAny: d.DeliverableCount(f)}
	if v.Deliverables == nil {
		v.Deliverables = []Group{}
	}
	if v.Responses == nil {
		v.Responses = []Group{}
	}
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
	Plain bool // a terminal: paths, not markdown links
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
		if kv[i] == "DESK_ID" && kv[i+1] != "" {
			// The target of [[#q-8tqk]] links (render: LinkMentions).
			fmt.Fprintf(w.W, ":CUSTOM_ID: %s\n", kv[i+1])
		}
		if kv[i+1] != "" {
			fmt.Fprintf(w.W, ":%s: %s\n", kv[i], kv[i+1])
		}
	}
	fmt.Fprintln(w.W, ":END:")
}

// Text prints a body indented, so that no line can start a heading.
func (w *Writer) Text(s string) {
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

// Media lists an event's attachments, each with its caption and a link to
// the stored file, or why it is not on this host.
func (w *Writer) Media(e store.Event) {
	refs := media.Of(e)
	if len(refs) == 0 {
		return
	}
	var lines []string
	for _, r := range refs {
		where := "not on this host"
		if w.Desk.Home != "" && r.Present(w.Desk.Home) {
			where = r.Path(w.Desk.Home)
			if w.Org {
				where = "[[file:" + where + "][" + r.Name + "]]"
			} else if w.Plain {
				where = r.Name + "  " + where
			} else {
				where = "[" + r.Name + "](<" + where + ">)"
			}
		} else {
			where = r.Name + " (" + where + ")"
		}
		lines = append(lines, fmt.Sprintf("- %s %s: %s", r.Kind(), where, r.Caption))
	}
	w.Text("Attachments:\n" + strings.Join(lines, "\n"))
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
		fmt.Fprintf(w.W, "#+TITLE: %s\n#+TODO: ASK NEW MSG RE | ANSWERED\n#+STARTUP: overview\n\n", title)
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
	lvl := it.Event.Level
	if it.Dismissed {
		lvl += ", dismissed"
	}
	if it.Deferred {
		lvl += ", deferred"
	}
	w.h(level, fmt.Sprintf("%s%s %s  (%s · %s · %s%s)", kwd(w.Org, k), it.ID, it.Event.Title,
		label, lvl, it.Age, w.hostTag(it.Event.Host)))
	w.props("DESK_ID", it.ID, "DESK_PATH", it.Event.Path, "DESK_TS", it.Event.TS, "DESK_HOST", it.Event.Host)
	w.Text(it.Event.Body)
	if c, ok := ChoicesOf(it.Event); ok {
		w.Text("Choices:\n" + c.Text())
	}
	w.Media(it.Event)
	if def := MetaString(it.Event, "default"); def != "" {
		w.Text("Default: " + def)
	}
	if q != nil && !q.Open() {
		ans := w.Desk.ByID[q.AnswerID]
		w.Text(fmt.Sprintf("Answer (%s, %s): %s", w.Desk.Display(q.AnswerID), MetaString(ans, "source"), q.AnswerBody))
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
	age := it.Age
	if it.Deferred {
		age += " · deferred"
	}
	w.h(level, fmt.Sprintf("%s%s %s: %s  (%s · %s%s)", kwd(w.Org, k), it.ID, what, title, label, age,
		w.hostTag(it.Event.Host)))
	w.props("DESK_ID", it.ID, "DESK_PATH", it.Event.Path, "DESK_TS", it.Event.TS, "DESK_HOST", it.Event.Host)
	if it.Response {
		w.Text(fmt.Sprintf("You asked (%s): %s", it.AskedVia, firstLine(it.Asked)))
	}
	if u := MetaString(it.Event, "url"); u != "" {
		if w.Org && OpenableURL(u) {
			// A bracket link, so an app link (myapp://...) is followed too,
			// by whatever handler Emacs has for its link type. An unsafe one
			// (from another host or an older version) stays plain text.
			u = "[[" + strings.NewReplacer("[", "%5B", "]", "%5D").Replace(u) + "][Open]]"
		}
		w.Text("Open: " + u)
	}
	if it.SupersededBy != "" {
		w.Text("Superseded by " + it.SupersededBy)
	}
	w.Text(it.Event.Body)
	w.Media(it.Event)
	if r := MetaString(it.Event, "report"); r != "" {
		w.Text("Report: " + r)
	}
}

// Message prints one message from the user, with the command that
// responds to it.
func (w *Writer) Message(level int, it Item) {
	about := "to " + it.Event.Path
	if it.Event.Ref != "" {
		r := w.Desk.ByID[it.Event.Ref]
		about = fmt.Sprintf("about %s %q", w.Desk.Display(r.ID), r.Title)
	}
	flag := ""
	if it.NoResponse {
		flag = " · no response yet"
	}
	w.h(level, fmt.Sprintf("%s%s message %s  (%s%s%s)", kwd(w.Org, "MSG"), it.ID, about, it.Age, flag,
		w.hostTag(it.Event.Host)))
	w.props("DESK_ID", it.ID, "DESK_PATH", it.Event.Path, "DESK_TS", it.Event.TS, "DESK_HOST", it.Event.Host)
	w.Text(it.Event.Body)
	w.Text("Respond: agent-desk tell --re " + it.ID)
}

// DeliveryTag is the org tag for a message's or an answer's delivery:
// pending, undelivered (the last attempt failed), delivered, in_chat (an
// answer given in the agent's chat), plus no_response once a delivered
// message has waited too long.
func (d *Desk) DeliveryTag(e store.Event) string {
	if MetaString(e, "source") == "chat" {
		return ":in_chat:"
	}
	if _, ok := d.Delivery(e.ID); !ok {
		if _, failed := d.DeliveryFailure(e.ID); failed {
			return ":undelivered:"
		}
		return ":pending:"
	}
	if d.NoResponse(e.ID) {
		return ":delivered:no_response:"
	}
	return ":delivered:"
}

// Thread prints a thread's messages, answers and responses as headings at
// one level: MSG for what the user sent, with its delivery state as a tag,
// and RE for an agent's response.
func (w *Writer) Thread(level int, thread []store.Event) {
	d := w.Desk
	for _, e := range thread {
		if e.Kind == "delivered" {
			continue
		}
		// Files keep a timestamp, which does not go stale; views an age.
		age := Age(d.Now.Sub(Time(e)))
		if w.Props && len(e.TS) >= 16 {
			age = e.TS[:16]
		}
		if IsResponse(e) {
			re := ""
			if e.Ref != "" {
				re = " to " + d.Display(e.Ref)
			}
			key, what := "RE", "response"
			if e.Kind == "ask" {
				key, what = "ASK", "question"
				if !d.QuestionOpen(e.ID) {
					key = "ANSWERED"
				}
			}
			w.h(level, fmt.Sprintf("%s%s %s%s: %s  (%s · %s%s)", kwd(w.Org, key), d.Display(e.ID), what, re,
				e.Title, agentLabel(e), age, w.hostTag(e.Host)))
		} else {
			what := "message"
			if e.Kind == "answer" {
				what = "answer"
			}
			tag := ""
			if w.Org {
				tag = "  " + d.DeliveryTag(e)
			} else {
				tag = " · " + strings.ReplaceAll(strings.Trim(d.DeliveryTag(e), ":"), ":", ", ")
			}
			if level == 1 {
				// Not under its root item, so say where it went.
				age = e.Path + " · " + age
			}
			w.h(level, fmt.Sprintf("%s%s %s: %s  (%s%s)%s", kwd(w.Org, "MSG"), d.Display(e.ID), what,
				firstLine(e.Body), age, w.hostTag(e.Host), tag))
		}
		w.props("DESK_ID", d.Display(e.ID), "DESK_PATH", e.Path, "DESK_TS", e.TS, "DESK_HOST", e.Host)
		w.Text(e.Body)
		w.Media(e)
		if f, ok := d.DeliveryFailure(e.ID); ok {
			w.Text("Not delivered: " + MetaString(f, "error"))
			if r := MetaString(f, "resume"); r != "" {
				w.Text("Resume its session: " + r)
			}
		}
	}
}

// agentLabel names the session that wrote e, briefly.
func agentLabel(e store.Event) string {
	var m struct {
		From struct{ Agent, Session, Title string } `json:"from"`
	}
	_ = json.Unmarshal(e.Meta, &m)
	switch {
	case m.From.Title != "":
		return m.From.Title
	case m.From.Agent != "":
		return m.From.Agent
	}
	return NodeOf(e)
}

func firstLine(s string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
	if r := []rune(line); len(r) > 100 {
		line = string(r[:99]) + "…"
	}
	return line
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
			w.Text("File: " + s.File)
			w.Text(LinkText(s.File))
		} else {
			w.Text(s.Event.Body)
		}
	}
	if len(n.Tasks) > 0 {
		done, active := 0, []string{}
		for _, t := range n.Tasks {
			if t.State == TaskDone {
				done++
			}
			if t.Active() {
				active = append(active, fmt.Sprintf("- %s %s %s", t.State, t.ID, t.Event.Title))
			}
		}
		w.Text(fmt.Sprintf("Tasks: %d of %d done\n%s", done, len(n.Tasks), strings.Join(active, "\n")))
	}
	if len(n.Notes) > 0 {
		var names []string
		for _, s := range n.Notes {
			names = append(names, fmt.Sprintf("%s (%d lines, %s)", s.Name, s.Lines, Age(w.Desk.Now.Sub(Time(s.Event)))))
		}
		w.Text("Agent notes (agent-desk get <name>): " + strings.Join(names, ", "))
	}
	for _, c := range n.Children {
		w.Node(level+1, c)
	}
}

// View prints the three views.
func (w *Writer) View(v View) { w.ViewAt(1, v) }

// ViewAt prints the three views with their sections at heading level l.
func (w *Writer) ViewAt(l int, v View) {
	if len(v.Awaiting) > 0 {
		w.h(l, fmt.Sprintf("Messages awaiting your response (%d)", count(v.Awaiting)))
		w.groups(l+1, v.Awaiting, w.Message)
	}
	w.h(l, fmt.Sprintf("Needs you (%d)", count(v.NeedsYou)))
	w.groups(l+1, v.NeedsYou, w.Question)
	if len(v.Deliverables) > 0 {
		w.h(l, fmt.Sprintf("Deliverables (%d)", count(v.Deliverables)))
		w.groups(l+1, v.Deliverables, w.Notice)
	}
	if len(v.Pinned) > 0 {
		w.h(l, fmt.Sprintf("Pinned (%d)", len(v.Pinned)))
		for _, it := range v.Pinned {
			if it.Event.Kind == "ask" {
				w.Question(l+1, it)
			} else {
				w.Notice(l+1, it)
			}
		}
	}
	if len(v.Responses) > 0 {
		w.h(l, fmt.Sprintf("Responses to you (%d)", count(v.Responses)))
		w.groups(l+1, v.Responses, w.Notice)
	}
	w.h(l, fmt.Sprintf("New (%d)", count(v.New)))
	w.groups(l+1, v.New, w.Notice)
	w.h(l, "Runs")
	for _, n := range v.Runs {
		w.Node(l+1, n)
	}
}

// JSON prints a view as JSON.
func JSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// OpenableURL is what a deliverable's Open button may carry: an http or
// https address with a host, or any other absolute URL (an app link a local
// program has registered, such as myapp://open?x=1) whose scheme is not one
// the dashboard's Markdown renderer refuses.
func OpenableURL(link string) bool {
	u, err := url.Parse(link)
	if err != nil || u.Scheme == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	case "javascript", "vbscript", "file", "data":
		return false
	}
	return true
}
