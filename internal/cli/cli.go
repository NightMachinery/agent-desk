// Package cli implements the agent-desk commands.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/config"
	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/ids"
	"github.com/NightMachinery/agent-desk/internal/render"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// App is one invocation's world; tests replace its parts.
type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Env    ident.Env
	Now    func() time.Time
	Host   string
	Home   string // store directory
	// WaitHooks makes detached hook commands synchronous, for tests.
	WaitHooks bool
	// Exec, when set, replaces running paseo and tmux for delivery, for tests.
	Exec Runner

	st  *store.Store
	cfg *config.Config
}

// New returns an App for the real environment.
func New() (*App, error) {
	home, err := store.DefaultHome()
	if err != nil {
		return nil, err
	}
	return &App{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Env: ident.System(), Now: time.Now, Host: HostName(), Home: home,
	}, nil
}

// HostName is ${AGENT_DESK_HOST}, else the short host name.
func HostName() string {
	if h := os.Getenv("AGENT_DESK_HOST"); h != "" {
		return h
	}
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return strings.ToLower(strings.SplitN(h, ".", 2)[0])
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

const usage = `agent-desk: a desk where agents file questions and state for the user.

Agent side (only coordinators write; workers report to their parent):
  agent-desk join <run>/<role>[/<role>...] [--takeover] [--project P]
  agent-desk ask --blocked --title T                  <<'Q' ... Q
  agent-desk ask --default 'what you will do' --title T <<'Q' ... Q
  agent-desk tell [--notify] --title T                <<'R' ... R
  agent-desk tell --re <m-id|a-id> [--quiet] [--title T] <<'R' ... R
                                      respond to the user's message or answer
  agent-desk whoami                   this session's node, or exit 1
  agent-desk set <slot> [text]          text or stdin; status 2 lines, next 5, others 15
  agent-desk clear <slot>
  agent-desk link <slot> <file>
  agent-desk answer <id> --from-chat 'text'   record an answer given in chat
  agent-desk done --title T [--report FILE]   <<'S' ... S   (agent-done calls this)

User side:
  agent-desk inbox [<path-prefix>] [--all]    interactive (needs fzf)
  agent-desk show [<path-prefix>|--mine] [--all] [--host H] [--session S] [--format org|md|json]
  agent-desk answer <id> 'text'       record, then type it into the asking session
  agent-desk reply <id|path> 'text'   send a message to the session behind an item or node
  agent-desk deliver                  retry answers and replies not yet delivered
  agent-desk serve [--listen ADDR|unix:PATH] [--allow-host H,...] [--allow-origin URL,...]
                   [--pid-file F] [--config F.json]
                                      the web dashboard (default 127.0.0.1:7740)
  agent-desk read <id|path-prefix>... [--host H]
  agent-desk unread <id>...
  agent-desk search <text> [--host H]
  agent-desk open <id|path>       what a notification click runs (open_command)
  agent-desk export | import [--notify] | snapshot <file> | render
  agent-desk merge <file>|ssh:<host> [--notify]

Store: ${AGENT_DESK_HOME:-~/.agents/desk}. See docs/usage.md.
`

// Run executes one command and returns the exit status.
func (a *App) Run(args []string) int {
	err := a.run(args)
	if a.st != nil {
		a.st.Close()
	}
	if err == nil {
		return 0
	}
	fmt.Fprintf(a.Stderr, "agent-desk: %v\n", err)
	var u usageError
	if errors.As(err, &u) {
		return 2
	}
	return 1
}

func (a *App) store() (*store.Store, error) {
	if a.st != nil {
		return a.st, nil
	}
	st, err := store.Open(a.Home)
	if err != nil {
		if a.Env.Getenv != nil && a.Env.Getenv("ANTIGRAVITY_CONVERSATION_ID") != "" && a.Env.Getenv("CODEX_SANDBOX") == "" {
			// Antigravity's sandbox has no writable-path setting; its shell
			// may write only temporary directories.
			return nil, fmt.Errorf("%w\nif Antigravity's sandbox runs this command, it blocks %s: "+
				"rerun the command with a request to bypass the sandbox", err, a.Home)
		}
		if a.Env.Getenv != nil && a.Env.Getenv("CODEX_SANDBOX") != "" {
			// Codex's sandbox lets a command write only its workspace, and
			// SQLite in WAL mode must write even to read.
			return nil, fmt.Errorf("%w\nCodex's sandbox blocks %s: rerun this command with escalated permissions, "+
				"or add that directory to sandbox_workspace_write.writable_roots in Codex's config.toml", err, a.Home)
		}
		return nil, err
	}
	a.st = st
	return st, nil
}

func (a *App) load() (*desk.Desk, error) {
	st, err := a.store()
	if err != nil {
		return nil, err
	}
	return desk.Load(st, a.Host, a.Now())
}

func (a *App) run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(a.Stdout, usage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(a.Stdout, usage)
		return nil
	case "join":
		return a.cmdJoin(rest)
	case "ask":
		return a.cmdAsk(rest)
	case "tell":
		return a.cmdTell(rest)
	case "set":
		return a.cmdSet(rest)
	case "whoami":
		return a.cmdWhoami(rest)
	case "reply":
		return a.cmdReply(rest)
	case "serve":
		return a.cmdServe(rest)
	case "done":
		return a.cmdDone(rest)
	case "clear":
		return a.cmdClear(rest)
	case "link":
		return a.cmdLink(rest)
	case "answer":
		return a.cmdAnswer(rest)
	case "read":
		return a.cmdRead(rest)
	case "unread":
		return a.cmdUnread(rest)
	case "show":
		return a.cmdShow(rest)
	case "search":
		return a.cmdSearch(rest)
	case "export":
		return a.cmdExport(rest)
	case "import":
		return a.cmdImport(rest)
	case "snapshot":
		return a.cmdSnapshot(rest)
	case "merge":
		return a.cmdMerge(rest)
	case "inbox":
		return a.cmdInbox(rest)
	case "inbox-list", "inbox-preview", "inbox-act":
		return a.cmdInboxHelper(cmd, rest)
	case "open":
		return a.cmdOpen(rest)
	case "deliver":
		return a.cmdDeliver(rest)
	case "render":
		if _, err := parse(rest, nil, nil, 0, 0); err != nil {
			return err
		}
		return a.render()
	}
	return usagef("unknown command %q; run agent-desk help", cmd)
}

// --- argument parsing -------------------------------------------------------

type opts struct {
	bools map[string]bool
	vals  map[string]string
	set   map[string]bool
	pos   []string
}

// parse accepts --name, --name=value and --name value anywhere, and "--" to
// end options. minPos and maxPos bound the positional arguments (-1: any).
func parse(args []string, boolNames, valNames []string, minPos, maxPos int) (opts, error) {
	o := opts{bools: map[string]bool{}, vals: map[string]string{}, set: map[string]bool{}}
	isBool := map[string]bool{}
	for _, n := range boolNames {
		isBool[n] = true
	}
	isVal := map[string]bool{}
	for _, n := range valNames {
		isVal[n] = true
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			o.pos = append(o.pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "--") || arg == "-" {
			o.pos = append(o.pos, arg)
			continue
		}
		name, val, hasVal := strings.Cut(arg[2:], "=")
		switch {
		case isBool[name] && !hasVal:
			o.bools[name] = true
		case isVal[name]:
			if !hasVal {
				if i+1 >= len(args) {
					return o, usagef("--%s needs a value", name)
				}
				i++
				val = args[i]
			}
			o.vals[name] = val
		default:
			return o, usagef("unknown option --%s", name)
		}
		o.set[name] = true
	}
	if len(o.pos) < minPos || (maxPos >= 0 && len(o.pos) > maxPos) {
		return o, usagef("wrong number of arguments; run agent-desk help")
	}
	return o, nil
}

func (a *App) stdinText() (string, error) {
	if f, ok := a.Stdin.(*os.File); ok {
		if st, err := f.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
			return "", nil
		}
	}
	if a.Stdin == nil {
		return "", nil
	}
	b, err := io.ReadAll(a.Stdin)
	return strings.TrimRight(string(b), "\n"), err
}

// --- writing -----------------------------------------------------------------

var slugRE = regexp.MustCompile(`^[a-z0-9-]{1,24}$`)

func (a *App) ts() (time.Time, string) {
	t := a.Now()
	return t, t.Format("2006-01-02T15:04:05.000Z07:00")
}

func meta(m map[string]any) json.RawMessage {
	b, _ := json.Marshal(m)
	return b
}

func (a *App) event(kind, path string, from ident.From, source string, extra map[string]any) store.Event {
	t, ts := a.ts()
	m := map[string]any{"from": from, "source": source}
	for k, v := range extra {
		m[k] = v
	}
	return store.Event{ID: ids.New(t), TS: ts, Host: a.Host, Kind: kind, Path: path, Meta: meta(m)}
}

func sessionOf(e store.Event) string {
	var m struct {
		From ident.From `json:"from"`
	}
	_ = json.Unmarshal(e.Meta, &m)
	return m.From.Session
}

func agentOfEvent(e store.Event) string {
	var m struct {
		From ident.From `json:"from"`
	}
	_ = json.Unmarshal(e.Meta, &m)
	return m.From.Agent
}

// nodeOwner returns the latest join at a node, if any.
func nodeOwner(d *desk.Desk, node string) (store.Event, bool) {
	var last store.Event
	found := false
	for _, e := range d.Events {
		if e.Kind == "join" && e.Path == node {
			last, found = e, true
		}
	}
	return last, found
}

// commit appends events in one transaction, prints the display id of the
// event `show` (or "ok" when show is empty), and re-renders.
func (a *App) commit(evs []store.Event, show string) error {
	st, err := a.store()
	if err != nil {
		return err
	}
	if err := st.Append(evs...); err != nil {
		return err
	}
	if show == "" {
		fmt.Fprintln(a.Stdout, "ok")
	} else {
		d, err := a.load()
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Stdout, d.Display(show))
	}
	if err := a.render(); err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: warning: rendering failed: %v\n", err)
	}
	a.announce(evs, true)
	return nil
}

// commit1 commits one event and prints its display id.
func (a *App) commit1(e store.Event) error { return a.commit([]store.Event{e}, e.ID) }

func (a *App) render() error {
	if _, err := a.store(); err != nil {
		return err
	}
	return render.All(a.Home, a.load)
}

// writer resolves the node this session writes to, auto-joining
// <project>/adhoc/<slug> when a session with no parent never joined. The
// returned join event, if any, must be committed with the write.
func (a *App) writer(d *desk.Desk, project string) (ident.From, string, *store.Event, error) {
	from := ident.Resolve(a.Env)
	if from.Session == "" {
		return from, "", nil, errors.New("cannot tell which agent session this is: none of " +
			"CLAUDE_CODE_SESSION_ID, CODEX_THREAD_ID, CODEX_SESSION_ID or ANTIGRAVITY_CONVERSATION_ID is set. " +
			"Only agent sessions write to the desk")
	}
	bound, found := a.binding(d, from)
	if found {
		owner, _ := nodeOwner(d, bound.Path)
		if sessionOf(owner) != from.Session {
			return from, "", nil, fmt.Errorf("node %s was taken over by another session (%s %s, %s); "+
				"this session no longer writes there. Join another node, or report to whoever took it over",
				bound.Path, agentOfEvent(owner), shortID(sessionOf(owner)), owner.TS)
		}
		return from, bound.Path, nil, nil
	}
	if from.Parent != "" {
		return from, "", nil, fmt.Errorf("this session has a parent agent (%s): workers report to their parent; "+
			"ask it to give you a node. If you are a coordinator, join one: agent-desk join <run>/<role>",
			from.Parent)
	}
	if project == "" {
		project = ident.ProjectName(from.Cwd, a.Env.Home)
	}
	slug := ident.Slug(from.Title, 24)
	if slug == "" {
		slug = shortID(from.Session)
	}
	node := project + "/adhoc/" + slug
	if owner, ok := nodeOwner(d, node); ok && sessionOf(owner) != from.Session {
		node = project + "/adhoc/" + shortID(from.Session)
	}
	j := a.event("join", node, from, "cli", map[string]any{"auto": true})
	return from, node, &j, nil
}

// binding is this session's latest join on this host.
func (a *App) binding(d *desk.Desk, from ident.From) (store.Event, bool) {
	var bound store.Event
	found := false
	for _, e := range d.Events {
		if e.Kind == "join" && e.Host == a.Host && from.Session != "" && sessionOf(e) == from.Session {
			bound, found = e, true
		}
	}
	return bound, found
}

func shortID(s string) string {
	s = ident.Slug(s, 64)
	s = strings.ReplaceAll(s, "-", "")
	if len(s) > 8 {
		s = s[:8]
	}
	return s
}

// agentWrite writes events built for this session's node.
func (a *App) agentWrite(project string, build func(from ident.From, node string, d *desk.Desk) ([]store.Event, error)) error {
	d, err := a.load()
	if err != nil {
		return err
	}
	from, node, join, err := a.writer(d, project)
	if err != nil {
		return err
	}
	evs, err := build(from, node, d)
	if err != nil {
		return err
	}
	show := evs[0].ID
	if join != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: joined %s (this session had no node)\n", node)
		evs = append([]store.Event{*join}, evs...)
	}
	return a.commit(evs, show)
}

func (a *App) cmdJoin(args []string) error {
	o, err := parse(args, []string{"takeover"}, []string{"project"}, 1, 1)
	if err != nil {
		return err
	}
	segs := strings.Split(strings.Trim(o.pos[0], "/"), "/")
	if len(segs) < 2 {
		return usagef("join takes <run>/<role>[/<role>...], such as rerun-nov/coord")
	}
	for _, s := range segs {
		if !slugRE.MatchString(s) {
			return usagef("%q is not a slug: use 1 to 24 of a-z, 0-9 and -", s)
		}
	}
	from := ident.Resolve(a.Env)
	if from.Session == "" {
		return errors.New("cannot tell which agent session this is; only agent sessions join")
	}
	project := o.vals["project"]
	if project == "" {
		project = ident.ProjectName(from.Cwd, a.Env.Home)
	} else if ident.Slug(project, 40) != project {
		return usagef("project %q is not a slug", project)
	}
	node := project + "/" + strings.Join(segs, "/")
	d, err := a.load()
	if err != nil {
		return err
	}
	if len(segs) > 2 {
		parent := project + "/" + strings.Join(segs[:len(segs)-1], "/")
		if _, ok := nodeOwner(d, parent); !ok {
			return fmt.Errorf("no coordinator has joined %s; a nested coordinator sits under its parent's node", parent)
		}
	}
	if owner, ok := nodeOwner(d, node); ok && sessionOf(owner) != from.Session && !o.bools["takeover"] {
		return fmt.Errorf("%s is bound to another session (%s %s, joined %s); if you are replacing that "+
			"coordinator, add --takeover", node, agentOfEvent(owner), shortID(sessionOf(owner)), owner.TS)
	}
	extra := map[string]any{}
	if o.bools["takeover"] {
		extra["takeover"] = true
	}
	return a.commit1(a.event("join", node, from, "cli", extra))
}

func (a *App) cmdAsk(args []string) error {
	o, err := parse(args, []string{"blocked"}, []string{"default", "title", "project"}, 0, 0)
	if err != nil {
		return err
	}
	if o.bools["blocked"] == o.set["default"] {
		return usagef("ask takes exactly one of --blocked or --default 'what you will do'")
	}
	title := strings.TrimSpace(o.vals["title"])
	if title == "" {
		return usagef("ask needs --title")
	}
	body, err := a.stdinText()
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return usagef("ask needs the question's context, options and recommendation on stdin")
	}
	level, extra := "blocked", map[string]any{}
	if o.set["default"] {
		if strings.TrimSpace(o.vals["default"]) == "" {
			return usagef("--default needs the choice you will act on")
		}
		level, extra["default"] = "default", o.vals["default"]
	}
	return a.agentWrite(o.vals["project"], func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
		e := a.event("ask", node, from, "cli", extra)
		e.Level, e.Title, e.Body = level, title, body
		return []store.Event{e}, nil
	})
}

func (a *App) cmdTell(args []string) error {
	o, err := parse(args, []string{"notify", "quiet"}, []string{"title", "project", "re"}, 0, 0)
	if err != nil {
		return err
	}
	re := strings.TrimSpace(o.vals["re"])
	if o.set["re"] && re == "" {
		return usagef("--re needs the id of the message or answer you respond to")
	}
	if o.bools["quiet"] && re == "" {
		return usagef("--quiet goes with --re: it marks a bare acknowledgement")
	}
	title := strings.TrimSpace(o.vals["title"])
	if title == "" && re == "" {
		return usagef("tell needs --title")
	}
	body, err := a.stdinText()
	if err != nil {
		return err
	}
	if re != "" && strings.TrimSpace(body) == "" {
		return usagef("tell --re needs the response on stdin, even one line such as \"Done.\"")
	}
	level := "fyi"
	if o.bools["notify"] {
		level = "notify"
	}
	return a.agentWrite(o.vals["project"], func(from ident.From, node string, d *desk.Desk) ([]store.Event, error) {
		var extra map[string]any
		ref := ""
		if re != "" {
			target, err := d.Resolve(re)
			if err != nil {
				return nil, err
			}
			def, err := respondsTo(d, target, node)
			if err != nil {
				return nil, err
			}
			if title == "" {
				title = def
			}
			ref = target.ID
			if o.bools["quiet"] {
				extra = map[string]any{"quiet": true}
			}
		}
		e := a.event("tell", node, from, "cli", extra)
		e.Level, e.Title, e.Body, e.Ref = level, title, body, ref
		return []store.Event{e}, nil
	})
}

// cmdWhoami prints the node the calling session writes to.
func (a *App) cmdWhoami(args []string) error {
	if _, err := parse(args, nil, nil, 0, 0); err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	from := ident.Resolve(a.Env)
	if from.Session == "" {
		return errors.New("not an agent session")
	}
	bound, ok := a.binding(d, from)
	if !ok {
		return errors.New("this session has not joined a node")
	}
	if owner, _ := nodeOwner(d, bound.Path); sessionOf(owner) != from.Session {
		return fmt.Errorf("this session joined %s, but another session took it over", bound.Path)
	}
	fmt.Fprintln(a.Stdout, bound.Path)
	return nil
}

// SlotCaps are the line caps of the conventional slots. Every agent keeps
// status, next and done; a coordinator also keeps velocity.
var SlotCaps = map[string]int{"status": 2, "next": 5, "done": 15, "velocity": 15, "findings": 15, "artifacts": 15}

// DefaultCap is the cap of any other slot.
const DefaultCap = 15

// Reserved slots only ever link a file.
var Reserved = map[string]bool{"tasks": true, "user-verbatim": true, "state": true}

func slotArg(name string) error {
	if !slugRE.MatchString(name) {
		return usagef("slot %q is not a slug: use 1 to 24 of a-z, 0-9 and -", name)
	}
	return nil
}

func (a *App) cmdSet(args []string) error {
	o, err := parse(args, nil, []string{"project"}, 1, -1)
	if err != nil {
		return err
	}
	name := o.pos[0]
	if err := slotArg(name); err != nil {
		return err
	}
	if Reserved[name] {
		return usagef("slot %q is reserved for a linked file: agent-desk link %s <file>", name, name)
	}
	text := strings.Join(o.pos[1:], " ")
	if len(o.pos) == 1 {
		if text, err = a.stdinText(); err != nil {
			return err
		}
	}
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return usagef("set needs text; to empty a slot use agent-desk clear %s", name)
	}
	if t := desk.SlotText(text); t != text {
		text = t
		fmt.Fprintf(a.Stderr, "agent-desk: took each \\n as a line break; pass real ones with $'...' or stdin\n")
	}
	limit, ok := SlotCaps[name]
	if !ok {
		limit = DefaultCap
	}
	if n := strings.Count(text, "\n") + 1; n > limit {
		return fmt.Errorf("slot %s takes at most %d lines; this text has %d. Nothing was written. "+
			"State only what is true now", name, limit, n)
	}
	return a.agentWrite(o.vals["project"], func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
		e := a.event("set", node+"/"+name, from, "cli", nil)
		e.Body = text
		return []store.Event{e}, nil
	})
}

func (a *App) cmdClear(args []string) error {
	o, err := parse(args, nil, []string{"project"}, 1, 1)
	if err != nil {
		return err
	}
	if err := slotArg(o.pos[0]); err != nil {
		return err
	}
	return a.agentWrite(o.vals["project"], func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
		return []store.Event{a.event("clear", node+"/"+o.pos[0], from, "cli", nil)}, nil
	})
}

func (a *App) cmdLink(args []string) error {
	o, err := parse(args, nil, []string{"project"}, 2, 2)
	if err != nil {
		return err
	}
	name := o.pos[0]
	if err := slotArg(name); err != nil {
		return err
	}
	file := o.pos[1]
	if !filepath.IsAbs(file) {
		file = filepath.Join(a.Env.Cwd, file)
	}
	if st, err := os.Stat(file); err != nil {
		return err
	} else if st.IsDir() {
		return fmt.Errorf("%s is a directory; link a file", file)
	}
	return a.agentWrite(o.vals["project"], func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
		return []store.Event{a.event("link", node+"/"+name, from, "cli", map[string]any{"file": file})}, nil
	})
}

func (a *App) cmdAnswer(args []string) error {
	o, err := parse(args, nil, []string{"from-chat"}, 1, -1)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	q, err := d.Resolve(o.pos[0])
	if err != nil {
		return err
	}
	if q.Kind != "ask" {
		return fmt.Errorf("%s is a %s, not a question", d.Display(q.ID), q.Kind)
	}
	source := "cli"
	text := strings.Join(o.pos[1:], " ")
	if o.set["from-chat"] {
		source = "chat"
		if text != "" {
			return usagef("give the answer once: agent-desk answer <id> --from-chat 'text'")
		}
		text = o.vals["from-chat"]
	} else if len(o.pos) == 1 {
		if text, err = a.stdinText(); err != nil {
			return err
		}
	}
	return a.recordAnswer(d, q, text, source)
}

// recordAnswer appends an answer to question q and prints its display id.
func (a *App) recordAnswer(d *desk.Desk, q store.Event, text, source string) error {
	if strings.TrimSpace(text) == "" {
		return usagef("answer needs the answer's text")
	}
	for _, x := range d.Questions {
		if x.ID == q.ID && !x.Open() {
			fmt.Fprintf(a.Stderr, "agent-desk: %s was already answered (%s); this answer replaces it\n",
				d.Display(q.ID), d.Display(x.AnswerID))
		}
	}
	e := a.event("answer", q.Path, ident.Resolve(a.Env), source, nil)
	e.Ref, e.Body = q.ID, text
	if err := a.commit1(e); err != nil {
		return err
	}
	if source == "chat" {
		return nil
	}
	if q.Host != a.Host {
		fmt.Fprintf(a.Stderr, "agent-desk: asked on %s; delivered there after the next merge\n", q.Host)
		return nil
	}
	if _, err := a.deliver(q.ID); err != nil {
		return err
	}
	return a.renderWarn()
}

func (a *App) cmdRead(args []string) error {
	o, err := parse(args, nil, []string{"host"}, 1, -1)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	from := ident.Resolve(a.Env)
	var evs []store.Event
	seen := map[string]bool{}
	add := func(e store.Event) {
		if !seen[e.Ref] {
			seen[e.Ref] = true
			evs = append(evs, e)
		}
	}
	for _, arg := range o.pos {
		if !strings.Contains(arg, "/") {
			e, err := d.Resolve(arg)
			if err == nil {
				switch {
				case e.Kind == "ask" && !isOpen(d, e.ID):
					fmt.Fprintf(a.Stderr, "agent-desk: %s is already answered\n", d.Display(e.ID))
				case e.Kind == "ask" && d.Dismissed(e.ID):
					fmt.Fprintf(a.Stderr, "agent-desk: %s is already dismissed\n", d.Display(e.ID))
				case e.Kind == "ask":
					add(a.readMark(e, from, "cli"))
					then := "its agent stays blocked until you answer"
					if e.Level == "default" {
						then = "its agent goes ahead with its default unless you answer"
					}
					fmt.Fprintf(a.Stderr, "agent-desk: %s leaves Needs you but stays open: %s\n", d.Display(e.ID), then)
				case e.Kind == "reply" && d.Awaiting(e.ID):
					add(a.readMark(e, from, "cli"))
				case e.Kind == "reply":
					fmt.Fprintf(a.Stderr, "agent-desk: %s is not awaiting a response\n", d.Display(e.ID))
				case !d.Unread(e.ID):
					if len(unreadResponses(d, e.ID)) == 0 {
						fmt.Fprintf(a.Stderr, "agent-desk: %s is not unread\n", d.Display(e.ID))
					}
				default:
					add(a.readMark(e, from, "cli"))
				}
				// Reading an item reads the responses in its thread.
				for _, r := range unreadResponses(d, e.ID) {
					add(a.readMark(r, from, "cli"))
				}
				continue
			} else if !strings.HasPrefix(err.Error(), "no event") {
				return err
			}
		}
		marks := a.readMarks(d, from, desk.Filter{Prefix: arg, Host: o.vals["host"]})
		if len(marks) == 0 {
			known := false
			for _, e := range d.Events {
				if desk.Under(e.Path, arg) {
					known = true
					break
				}
			}
			if !known {
				return fmt.Errorf("%q is neither an id nor a path in the store", arg)
			}
			fmt.Fprintf(a.Stderr, "agent-desk: nothing unread under %s\n", arg)
		}
		for _, m := range marks {
			add(m)
		}
	}
	if len(evs) == 0 {
		fmt.Fprintln(a.Stdout, "ok")
		return nil
	}
	if len(evs) > 1 {
		fmt.Fprintf(a.Stderr, "agent-desk: marked %d item(s) read\n", len(evs))
	}
	return a.commit(evs, "")
}

// readMark is a read marker for one event. On an open question it is a
// dismissal.
func (a *App) readMark(e store.Event, from ident.From, source string) store.Event {
	r := a.event("read", e.Path, from, source, nil)
	r.Ref = e.ID
	return r
}

// unreadResponses are the unread responses in the thread of item id.
func unreadResponses(d *desk.Desk, id string) []store.Event {
	var out []store.Event
	for _, e := range d.Thread(id, "") {
		if desk.IsResponse(e) && d.Unread(e.ID) {
			out = append(out, e)
		}
	}
	return out
}

// isOpen reports whether question id has no answer yet.
func isOpen(d *desk.Desk, id string) bool {
	for _, q := range d.Questions {
		if q.ID == id {
			return q.Open()
		}
	}
	return false
}

// readMarks builds one read marker per unread item in scope.
func (a *App) readMarks(d *desk.Desk, from ident.From, f desk.Filter) []store.Event {
	var evs []store.Event
	for _, e := range d.Events {
		if d.Unread(e.ID) && f.Keep(e) {
			evs = append(evs, a.readMark(e, from, "cli"))
		}
	}
	return evs
}

func (a *App) renderWarn() error {
	if err := a.render(); err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: warning: rendering failed: %v\n", err)
	}
	return nil
}

func (a *App) cmdUnread(args []string) error {
	o, err := parse(args, nil, nil, 1, -1)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	from := ident.Resolve(a.Env)
	var evs []store.Event
	seen := map[string]bool{}
	for _, tok := range o.pos {
		e, err := d.Resolve(tok)
		if err != nil {
			return err
		}
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		var last store.Event
		for _, r := range d.Events {
			if r.Kind == "read" && r.Ref == e.ID {
				last = r
			}
		}
		if last.ID == "" || desk.Meta(last)["cancel"] == true {
			fmt.Fprintf(a.Stderr, "agent-desk: %s is not marked read\n", d.Display(e.ID))
			continue
		}
		r := a.event("read", e.Path, from, "cli", map[string]any{"cancel": true, "supersedes": last.ID})
		r.Ref = e.ID
		evs = append(evs, r)
	}
	if len(evs) == 0 {
		fmt.Fprintln(a.Stdout, "ok")
		return nil
	}
	return a.commit(evs, "")
}

// --- reading -----------------------------------------------------------------

func (a *App) cmdShow(args []string) error {
	o, err := parse(args, []string{"all", "mine"}, []string{"format", "host", "session"}, 0, 1)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	f := desk.Filter{All: o.bools["all"], Host: o.vals["host"], Session: o.vals["session"]}
	if len(o.pos) == 1 {
		f.Prefix = strings.Trim(o.pos[0], "/")
	}
	if o.bools["mine"] {
		if len(o.pos) == 1 {
			return usagef("give a path or --mine, not both")
		}
		bound, ok := a.binding(d, ident.Resolve(a.Env))
		if !ok {
			return errors.New("this session has not joined a node")
		}
		f.Prefix, f.Dismissed = bound.Path, true
	}
	v := d.Build(f)
	if o.bools["mine"] {
		v.Awaiting = d.AwaitingGroups(f.Prefix)
	}
	switch o.vals["format"] {
	case "", "org":
		(&desk.Writer{W: a.Stdout, Desk: d, Org: true}).View(v)
	case "md":
		(&desk.Writer{W: a.Stdout, Desk: d}).View(v)
	case "json":
		return desk.JSON(a.Stdout, v)
	default:
		return usagef("--format is org, md or json")
	}
	return nil
}

func (a *App) cmdSearch(args []string) error {
	o, err := parse(args, nil, []string{"host"}, 1, -1)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	needle := strings.ToLower(strings.Join(o.pos, " "))
	var hits []store.Event
	for _, e := range d.Events {
		if e.Kind == "read" || e.Kind == "join" || e.Kind == "delivered" {
			continue
		}
		if o.vals["host"] != "" && e.Host != o.vals["host"] {
			continue
		}
		hay := strings.ToLower(e.Title + "\n" + e.Body + "\n" + e.Path + "\n" + desk.MetaString(e, "default"))
		if strings.Contains(hay, needle) {
			hits = append(hits, e)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].ID > hits[j].ID })
	for _, e := range hits {
		line := e.Title
		if line == "" {
			line = strings.SplitN(e.Body, "\n", 2)[0]
		}
		host := ""
		if e.Host != a.Host {
			host = " @" + e.Host
		}
		ts := e.TS
		if len(ts) >= 16 {
			ts = strings.Replace(ts[:16], "T", " ", 1)
		}
		fmt.Fprintf(a.Stdout, "%-8s %-6s %s  %s%s  %s\n", d.Display(e.ID), e.Kind, ts, e.Path, host, line)
	}
	if len(hits) == 0 {
		return errors.New("no matches")
	}
	return nil
}

// --- moving events ---------------------------------------------------------

func (a *App) cmdExport(args []string) error {
	if _, err := parse(args, nil, nil, 0, 0); err != nil {
		return err
	}
	st, err := a.store()
	if err != nil {
		return err
	}
	return st.Export(a.Stdout)
}

func (a *App) imported(added []store.Event, notify bool) error {
	fmt.Fprintln(a.Stdout, "ok")
	fmt.Fprintf(a.Stderr, "agent-desk: %d new event(s)\n", len(added))
	if len(added) == 0 {
		return nil
	}
	err := a.renderWarn()
	a.announce(added, notify)
	return err
}

func (a *App) cmdImport(args []string) error {
	o, err := parse(args, []string{"notify"}, nil, 0, 0)
	if err != nil {
		return err
	}
	st, err := a.store()
	if err != nil {
		return err
	}
	added, err := st.Import(a.Stdin)
	if err != nil {
		return err
	}
	return a.imported(added, o.bools["notify"])
}

func (a *App) cmdSnapshot(args []string) error {
	o, err := parse(args, nil, nil, 1, 1)
	if err != nil {
		return err
	}
	st, err := a.store()
	if err != nil {
		return err
	}
	if err := st.Snapshot(o.pos[0]); err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, "ok")
	return nil
}

func (a *App) cmdMerge(args []string) error {
	o, err := parse(args, []string{"notify"}, nil, 1, 1)
	if err != nil {
		return err
	}
	src := o.pos[0]
	if strings.HasPrefix(src, "ssh:") {
		return a.mergeSSH(strings.TrimPrefix(src, "ssh:"), o.bools["notify"])
	}
	st, err := a.store()
	if err != nil {
		return err
	}
	added, err := st.Merge(src)
	if err != nil {
		return err
	}
	if err := a.imported(added, o.bools["notify"]); err != nil {
		return err
	}
	_, err = a.deliver("")
	return err
}

// cmdOpen opens an item or node for the user: what a notification click
// runs. With open_command configured it hands over the item; otherwise, in
// a terminal, it opens the inbox scoped to the item's node.
func (a *App) cmdOpen(args []string) error {
	o, err := parse(args, nil, nil, 1, 1)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	var p Payload
	if strings.Contains(o.pos[0], "/") {
		self, _ := os.Executable()
		p = Payload{Action: "open", Path: strings.Trim(o.pos[0], "/"), AgentDesk: self, DeskHome: a.Home, Local: true}
	} else {
		e, err := d.Resolve(o.pos[0])
		if err != nil {
			return err
		}
		p = a.payload(d, "open", e)
	}
	c, err := a.config()
	if err != nil {
		return err
	}
	if c.OpenCommand != "" {
		return a.runHook(c.OpenCommand, p, true)
	}
	if isTerminal(a.Stdout) {
		return a.cmdInbox([]string{p.Path})
	}
	return fmt.Errorf("no open_command in %s; run agent-desk inbox %s", config.Path(a.Env.Getenv, a.Env.Home), p.Path)
}

// cmdDone files a finished session's summary and report path, and sets its
// status slot to say so, in one transaction.
func (a *App) cmdDone(args []string) error {
	o, err := parse(args, nil, []string{"title", "report", "project"}, 0, 0)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(o.vals["title"])
	if title == "" {
		title = "Session finished"
	}
	body, err := a.stdinText()
	if err != nil {
		return err
	}
	extra := map[string]any{}
	if r := o.vals["report"]; r != "" {
		if !filepath.IsAbs(r) {
			r = filepath.Join(a.Env.Cwd, r)
		}
		extra["report"] = r
	}
	return a.agentWrite(o.vals["project"], func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
		e := a.event("done", node, from, "cli", extra)
		e.Title, e.Body = title, body
		status := "Finished."
		if extra["report"] != nil {
			status = "Finished; see report."
		}
		st := a.event("set", node+"/status", from, "cli", nil)
		st.Body = status
		return []store.Event{e, st}, nil
	})
}
