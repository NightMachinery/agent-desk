package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/media"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// The terminal inbox is fzf over lines this binary prints. Each line is
// "<key>\t<text>": the key is an event id, "node:<path>", or "-" for a
// header. fzf calls back into the hidden commands inbox-list, inbox-preview
// and inbox-act, which share a small state file named by
// AGENT_DESK_INBOX_STATE.

type inboxState struct {
	All     bool            `json:"all"`
	Prefix  string          `json:"prefix"`
	Host    string          `json:"host"`
	Toggled map[string]bool `json:"toggled"` // node path -> expanded, overriding the default
}

func (a *App) statePath() string { return a.Env.Getenv("AGENT_DESK_INBOX_STATE") }

func (a *App) loadState() inboxState {
	st := inboxState{Toggled: map[string]bool{}}
	if p := a.statePath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &st)
		}
	}
	if st.Toggled == nil {
		st.Toggled = map[string]bool{}
	}
	return st
}

func (a *App) saveState(st inboxState) error {
	p := a.statePath()
	if p == "" {
		return errors.New("AGENT_DESK_INBOX_STATE is not set; run agent-desk inbox")
	}
	b, _ := json.Marshal(st)
	return os.WriteFile(p, b, 0o600)
}

// filter is the inbox's view of d: its scope, under the focus.
func (st inboxState) filter(d *desk.Desk) desk.Filter {
	return desk.Filter{Prefix: st.Prefix, Host: st.Host, All: st.All, Focus: d.FocusPrefixes()}
}

// --- colours -----------------------------------------------------------------

func ansi(code, s string) string { return "\x1b[" + code + "m" + s + "\x1b[0m" }
func bold(s string) string       { return ansi("1", s) }
func dim(s string) string        { return ansi("2", s) }
func red(s string) string        { return ansi("1;31", s) }
func yellow(s string) string     { return ansi("33", s) }
func green(s string) string      { return ansi("32", s) }
func cyan(s string) string       { return ansi("36", s) }

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func stateColour(s string) string {
	switch s {
	case desk.Waiting:
		return red(s)
	case desk.Finished:
		return green(s)
	case desk.Quiet:
		return dim(s)
	}
	return s
}

// --- the list ----------------------------------------------------------------

type line struct{ key, text string }

func (a *App) inboxLines(d *desk.Desk, st inboxState) []line {
	f := st.filter(d)
	var out []line
	add := func(key, text string) { out = append(out, line{key, text}) }

	needs := d.NeedsYou(f)
	n := 0
	for _, g := range needs {
		n += len(g.Items)
	}
	add("-", bold(fmt.Sprintf("NEEDS YOU (%d)", n)))
	for _, g := range needs {
		add("node:"+g.Project+"/"+g.Run, "  "+cyan(g.Project+" / "+g.Run))
		for _, it := range g.Items {
			lvl := red("BLOCKED")
			if it.Event.Level == "default" {
				lvl = yellow("DEFAULT")
			}
			if it.Dismissed {
				lvl = dim("DISMISS")
			}
			re := ""
			if it.Re != "" {
				re = "  " + dim("re "+it.Re)
			}
			add(it.Event.ID, fmt.Sprintf("    %s %-4s %s  %s%s%s", lvl, it.Age, dim(it.Label), it.Event.Title,
				re, hostTag(d, it.Event.Host)))
		}
	}

	if resp := d.ResponseGroups(f); len(resp) > 0 {
		add("-", bold(fmt.Sprintf("RESPONSES (%d)", countItems(resp))))
		for _, g := range resp {
			add("node:"+g.Project+"/"+g.Run, "  "+cyan(g.Project+" / "+g.Run))
			for _, it := range g.Items {
				mark := " "
				if it.Unread {
					mark = yellow("•")
				}
				to := "to chat"
				if it.Re != "" {
					to = "to " + it.Re
				}
				add(it.Event.ID, fmt.Sprintf("  %s %-5s %-4s %s  %s  %s%s", mark, "re", it.Age, dim(it.Label),
					it.Event.Title, dim(to), hostTag(d, it.Event.Host)))
			}
		}
	}

	news := d.New(f)
	n = 0
	for _, g := range news {
		n += len(g.Items)
	}
	add("-", bold(fmt.Sprintf("NEW (%d)", n)))
	for _, g := range news {
		add("node:"+g.Project+"/"+g.Run, "  "+cyan(g.Project+" / "+g.Run))
		for _, it := range g.Items {
			what, title := it.Event.Kind, it.Event.Title
			label := it.Label
			if it.Event.Kind == "set" {
				what = "slot"
				_, _, label = desk.Split(desk.NodeOf(it.Event))
				title = desk.SlotName(it.Event) + ": " + firstLine(it.Event.Body)
			}
			mark := " "
			if it.Unread {
				mark = yellow("•")
			}
			add(it.Event.ID, fmt.Sprintf("  %s %-5s %-4s %s  %s%s", mark, what, it.Age, dim(label), title,
				hostTag(d, it.Event.Host)))
		}
	}

	add("-", bold("RUNS"))
	tree := desk.Visible(d.Tree(f))
	if len(tree) == 0 {
		add("-", dim("  (nothing here)"))
	}
	var walk func(n *desk.Node, depth int)
	walk = func(n *desk.Node, depth int) {
		indent := strings.Repeat("  ", depth)
		open := expanded(n, st)
		hasMore := len(n.Children) > 0 || len(n.Slots) > 0
		marker := " "
		if hasMore {
			marker = ">"
			if open {
				marker = "v"
			}
		}
		text := fmt.Sprintf("%s%s %s  %s", indent, marker, bold(n.Name), nodeSummary(n))
		if !open && n.Depth >= 3 {
			if s := n.Status(); s != "" {
				text += "  " + dim(firstLine(s))
			}
		}
		add("node:"+n.Path, text+hostTag(d, n.Host))
		if !open {
			return
		}
		for _, s := range n.Slots {
			mark := " "
			if s.Unread {
				mark = yellow("•")
			}
			body := firstLine(s.Event.Body)
			if s.File != "" {
				body = "→ " + s.File
			}
			add(s.Event.ID, fmt.Sprintf("%s  %s %s: %s", indent, mark, s.Name, body))
		}
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	for _, n := range tree {
		walk(n, 0)
	}
	return out
}

func countItems(gs []desk.Group) int {
	n := 0
	for _, g := range gs {
		n += len(g.Items)
	}
	return n
}

func hostTag(d *desk.Desk, host string) string {
	if host == "" || host == d.Host {
		return ""
	}
	return " " + dim("@"+host)
}

func nodeSummary(n *desk.Node) string {
	parts := []string{stateColour(n.State)}
	if n.State == desk.Active || n.State == desk.Quiet {
		parts[0] += " " + n.Age
	}
	if n.Open > 0 {
		parts = append(parts, red(fmt.Sprintf("%d ask", n.Open)))
	}
	if n.UnreadN > 0 {
		parts = append(parts, yellow(fmt.Sprintf("%d new", n.UnreadN)))
	}
	return strings.Join(parts, " · ")
}

// expanded: a node is open only along the way to something that needs the
// user, unless the user toggled it.
func expanded(n *desk.Node, st inboxState) bool {
	if v, ok := st.Toggled[n.Path]; ok {
		return v
	}
	return n.Attention()
}

// --- preview -----------------------------------------------------------------

const inboxHelp = `enter    answer a question, open an item, fold a node
ctrl-r   mark read (a node: everything under it)
ctrl-t   take the recommendation or the default
ctrl-o   open the session
alt-m    message the session behind the row
alt-a    show all, including read items and finished runs
esc      quit`

func (a *App) preview(d *desk.Desk, st inboxState, key string) string {
	var b bytes.Buffer
	switch {
	case key == "-" || key == "":
		b.WriteString(inboxHelp + "\n")
	case strings.HasPrefix(key, "node:"):
		path := strings.TrimPrefix(key, "node:")
		n := desk.Find(d.Tree(desk.Filter{Prefix: st.Prefix, Host: st.Host, All: true}), path)
		if n == nil {
			return "(gone)\n"
		}
		fmt.Fprintf(&b, "%s\n%s\n\n", bold(strings.ReplaceAll(path, "/", " / ")), nodeSummary(n))
		w := &desk.Writer{W: &b, Desk: d}
		for _, s := range n.Slots {
			mark := ""
			if s.Unread {
				mark = yellow(" (new)")
			}
			fmt.Fprintf(&b, "%s%s  %s\n", bold(s.Name), mark, dim(s.ID))
			if s.File != "" {
				b.WriteString(fileText(s.File))
			} else {
				w.Text(s.Event.Body)
			}
			b.WriteString("\n")
		}
		for _, c := range n.Children {
			fmt.Fprintf(&b, "%s  %s", bold(c.Name), nodeSummary(c))
			if s := c.Status(); s != "" {
				fmt.Fprintf(&b, "\n  %s", firstLine(s))
			}
			b.WriteString("\n")
		}
		b.WriteString(a.threadText(d, d.Thread("", path), "Conversation with this node", ""))
		b.WriteString("\n" + dim("ctrl-r marks everything under it read · enter folds · alt-m message") + "\n")
	default:
		e, ok := d.ByID[key]
		if !ok {
			return "(gone)\n"
		}
		project, run, rest := desk.Split(desk.NodeOf(e))
		where := project + " / " + run
		if rest != "" {
			where += " / " + strings.ReplaceAll(rest, "/", " / ")
		}
		age := desk.Age(d.Now.Sub(desk.Time(e)))
		head := d.Display(e.ID)
		w := &desk.Writer{W: &b, Desk: d, Plain: true}
		hint := ""
		switch e.Kind {
		case "ask":
			lvl := red("BLOCKED")
			if e.Level == "default" {
				lvl = yellow("DEFAULT")
			}
			fmt.Fprintf(&b, "%s  %s  %s%s\n%s\n\n%s\n\n", head, lvl, age, hostTag(d, e.Host), dim(where), bold(e.Title))
			w.Text(e.Body)
			if c, ok := desk.ChoicesOf(e); ok {
				fmt.Fprintln(&b)
				w.Text(c.Text())
			}
			w.Media(e)
			if def := desk.MetaString(e, "default"); def != "" {
				fmt.Fprintf(&b, "\n%s %s\n", yellow("Default:"), def)
			}
			hint = "enter answer · ctrl-t take " + takeWhat(e) + " · alt-m message · ctrl-o open session"
		default:
			what := e.Kind
			title := e.Title
			if desk.IsSlotKind(e.Kind) {
				what, title = "slot", desk.SlotName(e)
			}
			if desk.IsResponse(e) {
				what = "re"
			}
			state := ""
			if d.Unread(e.ID) {
				state = "  " + yellow("NEW")
			}
			fmt.Fprintf(&b, "%s  %s  %s%s%s\n%s\n\n%s\n\n", head, what, age, state, hostTag(d, e.Host), dim(where), bold(title))
			if f := desk.MetaString(e, "file"); f != "" {
				fmt.Fprintf(&b, "%s\n\n%s", dim(f), fileText(f))
			} else {
				w.Text(e.Body)
			}
			w.Media(e)
			if r := desk.MetaString(e, "report"); r != "" {
				fmt.Fprintf(&b, "\nReport: %s\n", r)
			}
			hint = "enter open in $EDITOR · ctrl-r mark read · alt-m message · ctrl-o open session"
		}
		thread, title := itemThread(d, e)
		b.WriteString(a.threadText(d, thread, title, e.ID))
		b.WriteString("\n" + dim(hint) + "\n")
	}
	return b.String()
}

// itemThread is the conversation e belongs to: its own when it is an item,
// its root item's when it is a message, an answer or a response, or else
// its node's.
func itemThread(d *desk.Desk, e store.Event) ([]store.Event, string) {
	if e.Kind != "reply" && e.Kind != "answer" && !desk.IsResponse(e) {
		return d.Thread(e.ID, ""), "Conversation"
	}
	if root, ok := d.Root(e); ok {
		title := root.Title
		if desk.IsSlotKind(root.Kind) {
			title = desk.SlotName(root)
		}
		return d.Thread(root.ID, ""), fmt.Sprintf("Conversation on %s %s", d.Display(root.ID), title)
	}
	return d.Thread("", desk.NodeOf(e)), "Conversation with " + desk.NodeOf(e)
}

// deliveryText says what happened to a delivered message, as the dashboard
// does.
func deliveryText(del *Delivery) string {
	words := map[string]string{
		"turn_started": "started a new turn",
		"steered":      "joined the running turn",
		"interrupted":  "interrupted the turn",
		"out_of_band":  "answered without a turn",
		"typed":        "typed into its pane",
		"command":      "handed to deliver_command",
	}
	t := words[del.Disposition]
	switch {
	case del.Disposition == "queued" && del.QueuePosition > 0:
		t = fmt.Sprintf("queued, position %d", del.QueuePosition)
	case del.Disposition == "queued":
		t = "queued"
	case t == "" && del.Via == "tmux":
		t = "typed into its pane"
	case t == "":
		t = "delivered"
	}
	if del.Revived {
		t += " · revived the session"
	}
	return t
}

// threadText prints a conversation for the preview, oldest first, marking
// the entry highlight.
func (a *App) threadText(d *desk.Desk, thread []store.Event, title, highlight string) string {
	var pushed map[string]string
	if c, err := a.config(); err == nil {
		pushed = c.SSHHosts
	}
	ens := entries(d, thread, pushed)
	if len(ens) == 0 {
		return ""
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "\n%s\n", bold(title))
	for _, en := range ens {
		mark := " "
		if en.Event.ID == highlight {
			mark = cyan("▸")
		}
		var head string
		switch en.Kind {
		case "response":
			re := ""
			if en.Re != "" {
				re = " to " + en.Re
			}
			head = fmt.Sprintf("%s responded%s", en.By, re)
		case "answer":
			head = "You answered " + en.By
			if en.Source == "chat" {
				head += ", in its chat"
			}
		default:
			head = "You → " + en.By
		}
		fmt.Fprintf(&b, "%s %s  %s · %s%s\n", mark, green(en.ID), head, en.Age, hostTag(d, en.Event.Host))
		lines := strings.Split(strings.TrimSpace(en.Event.Body), "\n")
		if len(lines) > 8 {
			lines = append(lines[:8], "…")
		}
		for _, l := range lines {
			fmt.Fprintf(&b, "    %s\n", l)
		}
		for _, r := range media.Of(en.Event) {
			where := dim("(not on this host)")
			if r.Present(a.Home) {
				where = r.Path(a.Home)
			}
			fmt.Fprintf(&b, "    %s %s: %s  %s\n", r.Kind(), r.Name, r.Caption, where)
		}
		var state []string
		if en.Delivery != nil {
			state = append(state, deliveryText(en.Delivery))
			if en.Delivery.Fallback != "" {
				state = append(state, en.Delivery.Fallback)
			}
		} else if en.Pending != "" {
			state = append(state, yellow(en.Pending))
		}
		switch en.Status {
		case "responded":
			state = append(state, green("responded"))
		case "awaiting":
			state = append(state, "waiting for a response")
		case "no_response":
			state = append(state, red("no response yet"))
		case "cleared":
			state = append(state, "marked read")
		}
		if len(state) > 0 {
			fmt.Fprintf(&b, "    %s\n", dim(strings.Join(state, " · ")))
		}
		if f := en.Failure; f != nil {
			fmt.Fprintf(&b, "    %s\n", red(fmt.Sprintf("%s ago: %s", f.Age, f.Error)))
			if f.Resume != "" {
				fmt.Fprintf(&b, "    %s\n", dim("resume it: "+f.Resume))
			}
		}
	}
	return b.String()
}

func fileText(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "(cannot read: " + err.Error() + ")\n"
	}
	const limit = 64 << 10
	if len(b) > limit {
		b = append(b[:limit], []byte("\n…")...)
	}
	return string(b) + "\n"
}

func takeWhat(q store.Event) string {
	if q.Level == "default" {
		return "the default"
	}
	return "the recommendation"
}

// TakeText is the fixed answer for "take the recommendation or default".
func TakeText(q store.Event) string {
	if c, ok := desk.ChoicesOf(q); ok {
		if text, all := c.Recommendations(); all {
			return text
		}
	}
	if def := desk.MetaString(q, "default"); def != "" && q.Level == "default" {
		return "Go ahead with your default: " + def
	}
	return "Take your recommendation."
}

// --- actions -----------------------------------------------------------------

func (a *App) cmdInboxHelper(cmd string, args []string) error {
	d, err := a.load()
	if err != nil {
		return err
	}
	st := a.loadState()
	switch cmd {
	case "inbox-list":
		for _, l := range a.inboxLines(d, st) {
			fmt.Fprintf(a.Stdout, "%s\t%s\n", l.key, l.text)
		}
		return nil
	case "inbox-preview":
		key := ""
		if len(args) > 0 {
			key = args[0]
		}
		fmt.Fprint(a.Stdout, a.preview(d, st, key))
		return nil
	}
	if len(args) < 1 {
		return usagef("inbox-act <action> [key]")
	}
	key := ""
	if len(args) > 1 {
		key = args[1]
	}
	return a.inboxAct(d, st, args[0], key)
}

func (a *App) inboxAct(d *desk.Desk, st inboxState, action, key string) error {
	if action == "toggle-all" {
		st.All = !st.All
		return a.saveState(st)
	}
	if key == "-" || key == "" {
		return nil
	}
	from := ident.Resolve(a.Env)
	if strings.HasPrefix(key, "node:") {
		path := strings.TrimPrefix(key, "node:")
		switch action {
		case "enter":
			n := desk.Find(d.Tree(st.filter(d)), path)
			if n == nil {
				return nil
			}
			st.Toggled[path] = !expanded(n, st)
			return a.saveState(st)
		case "read":
			evs := a.readMarks(d, from, desk.Filter{Prefix: path, Host: st.Host})
			if len(evs) == 0 {
				return nil
			}
			return a.commit(evs, "")
		case "message":
			return a.messagePrompt(d, path, key)
		case "open":
			owner, ok := nodeOwner(d, path)
			if !ok {
				return a.pause("no session has joined " + path)
			}
			return a.openSession(owner)
		}
		return nil
	}
	e, ok := d.ByID[key]
	if !ok {
		return nil
	}
	switch action {
	case "enter":
		if e.Kind == "ask" {
			return a.answerPrompt(d, e)
		}
		return a.openItem(d, e)
	case "read":
		if e.Kind == "ask" {
			if !d.Dismissed(e.ID) && isOpen(d, e.ID) {
				return a.commit([]store.Event{a.readMark(e, from, "cli")}, "")
			}
			return nil
		}
		if !d.Unread(e.ID) {
			return nil
		}
		r := a.event("read", e.Path, from, "cli", nil)
		r.Ref = e.ID
		return a.commit([]store.Event{r}, "")
	case "take":
		if e.Kind != "ask" {
			return nil
		}
		return a.recordAnswer(d, e, TakeText(e), "cli", "", nil)
	case "message":
		return a.messagePrompt(d, e.ID, key)
	case "open":
		return a.openSession(e)
	}
	return usagef("unknown inbox action %q", action)
}

// tty opens the terminal fzf suspended for an execute() binding.
func tty() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }

func (a *App) pause(msg string) error {
	t, err := tty()
	if err != nil {
		fmt.Fprintln(a.Stderr, msg)
		return nil
	}
	defer t.Close()
	fmt.Fprintf(t, "%s\n\npress enter to return ", msg)
	bufio.NewReader(t).ReadString('\n')
	return nil
}

func editor() string {
	for _, k := range []string{"VISUAL", "EDITOR"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "vi"
}

func runEditor(path string) error {
	t, err := tty()
	if err != nil {
		return err
	}
	defer t.Close()
	cmd := exec.Command("sh", "-c", editor()+` "$1"`, "agent-desk", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = t, t, t
	return cmd.Run()
}

func (a *App) answerPrompt(d *desk.Desk, q store.Event) error {
	comment := fmt.Sprintf("Answer to %s %q.", d.Display(q.ID), q.Title)
	context := q.Body
	if c, ok := desk.ChoicesOf(q); ok {
		context += "\n\n" + c.Text() + "\n\nAnswer each by number and key, such as \"1. B  2. Other: ...\", with a note after any pick."
	}
	text, err := a.prompt(d, q.ID, "Answer "+d.Display(q.ID), comment, context)
	if err != nil || text == "" {
		return err
	}
	return a.recordAnswer(d, q, text, "cli", "", nil)
}

// messagePrompt asks for a message to the session behind token (an item or
// a node path) and sends it like reply does; key is the row to preview.
func (a *App) messagePrompt(d *desk.Desk, token, key string) error {
	to, _, node, about, err := replyTarget(d, token)
	if err != nil {
		return a.pause(err.Error())
	}
	who := fromTitle(to)
	if who == "" {
		who = node
	}
	comment := "Message to " + who + " at " + node + "."
	context := ""
	if about.ID != "" {
		comment = fmt.Sprintf("Message to %s about %s %q.", who, d.Display(about.ID), about.Title)
		context = about.Body
	}
	text, err := a.prompt(d, key, "Message to "+who, comment, context)
	if err != nil || text == "" {
		return err
	}
	if _, err := a.recordReply(token, text, "cli", "", nil); err != nil {
		return a.pause(err.Error())
	}
	return a.pause("Sent.")
}

// prompt shows key's preview on the terminal and reads one line; an empty
// line opens $EDITOR on a file whose # lines (comment, then context) are
// dropped. An empty result means cancelled.
func (a *App) prompt(d *desk.Desk, key, label, comment, context string) (string, error) {
	t, err := tty()
	if err != nil {
		return "", err
	}
	fmt.Fprintf(t, "\x1b[2J\x1b[H%s\n\n", a.preview(d, inboxState{}, key))
	fmt.Fprintf(t, "%s (empty: write it in $EDITOR, ctrl-c: cancel)\n> ", bold(label))
	text, err := bufio.NewReader(t).ReadString('\n')
	t.Close()
	if err != nil {
		return "", nil
	}
	if text = strings.TrimSpace(text); text != "" {
		return text, nil
	}
	f, err := os.CreateTemp("", "agent-desk-text-*.txt")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	fmt.Fprintf(f, "\n# %s Lines starting with # are dropped; an empty text cancels.\n", comment)
	if context != "" {
		for _, l := range strings.Split(context, "\n") {
			fmt.Fprintf(f, "# %s\n", l)
		}
	}
	f.Close()
	if err := runEditor(f.Name()); err != nil {
		return "", err
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		return "", err
	}
	var keep []string
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(l, "#") {
			keep = append(keep, l)
		}
	}
	return strings.TrimSpace(strings.Join(keep, "\n")), nil
}

// openItem opens what an item points to in $EDITOR: a linked file, a done
// report, or else the item's own text.
func (a *App) openItem(d *desk.Desk, e store.Event) error {
	for _, k := range []string{"file", "report"} {
		if p := desk.MetaString(e, k); p != "" {
			if _, err := os.Stat(p); err == nil {
				return runEditor(p)
			}
		}
	}
	f, err := os.CreateTemp("", "agent-desk-"+d.Display(e.ID)+"-*.org")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	w := &desk.Writer{W: f, Desk: d, Org: true, Props: true}
	it := desk.Item{Event: e, ID: d.Display(e.ID), Label: e.Path, Age: e.TS, Unread: d.Unread(e.ID)}
	w.Notice(1, it)
	thread, _ := itemThread(d, e)
	w.Thread(2, thread)
	f.Close()
	return runEditor(f.Name())
}

// --- the fzf front end -------------------------------------------------------

func isTerminal(w any) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (a *App) cmdInbox(args []string) error {
	o, err := parse(args, []string{"all"}, []string{"host"}, 0, 1)
	if err != nil {
		return err
	}
	fzf, err := exec.LookPath("fzf")
	if err != nil {
		return errors.New("inbox needs fzf on PATH; agent-desk show prints the same views")
	}
	if !isTerminal(a.Stdout) {
		return errors.New("inbox is interactive and needs a terminal; agent-desk show prints the same views")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if _, err := a.store(); err != nil {
		return err
	}
	st := inboxState{All: o.bools["all"], Host: o.vals["host"], Toggled: map[string]bool{}}
	if len(o.pos) == 1 {
		st.Prefix = strings.Trim(o.pos[0], "/")
	}
	sf, err := os.CreateTemp("", "agent-desk-inbox-*.json")
	if err != nil {
		return err
	}
	sf.Close()
	defer os.Remove(sf.Name())
	env := append(os.Environ(), "AGENT_DESK_INBOX_STATE="+sf.Name(), "AGENT_DESK_HOME="+a.Home)
	a.Env.Getenv = func(k string) string {
		if k == "AGENT_DESK_INBOX_STATE" {
			return sf.Name()
		}
		return os.Getenv(k)
	}
	if err := a.saveState(st); err != nil {
		return err
	}

	q := shellQuote(self)
	reload := "reload(" + q + " inbox-list)"
	act := func(name string) string { return q + " inbox-act " + name + " {1}" }
	title := "agent-desk"
	if st.Prefix != "" {
		title += ": " + st.Prefix
	}
	cmd := exec.Command(fzf,
		"--ansi", "--delimiter=\t", "--with-nth=2..", "--no-sort", "--layout=reverse",
		"--track", "--id-nth=1",
		"--prompt="+title+"> ",
		"--header=enter answer/open/fold · ctrl-r read · ctrl-t take · ctrl-o session · alt-m message · alt-a all · esc quit",
		"--preview="+q+" inbox-preview {1}",
		"--preview-window=right,55%,wrap,<50(down,60%,wrap)",
		"--bind=enter:execute("+act("enter")+")+"+reload,
		"--bind=ctrl-r:execute-silent("+act("read")+")+"+reload,
		"--bind=ctrl-t:execute("+act("take")+")+"+reload,
		"--bind=ctrl-o:execute("+act("open")+")",
		"--bind=alt-m:execute("+act("message")+")+"+reload,
		"--bind=alt-a:execute-silent("+q+" inbox-act toggle-all -)+"+reload,
		"--bind=ctrl-l:"+reload,
	)
	cmd.Env = env
	d, err := a.load()
	if err != nil {
		return err
	}
	var in bytes.Buffer
	for _, l := range a.inboxLines(d, st) {
		fmt.Fprintf(&in, "%s\t%s\n", l.key, l.text)
	}
	cmd.Stdin = &in
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && (ee.ExitCode() == 130 || ee.ExitCode() == 1) {
		return nil
	}
	return err
}
