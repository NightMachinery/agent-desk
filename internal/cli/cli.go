// Package cli implements the agent-desk commands.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/NightMachinery/agent-desk/internal/config"
	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/ids"
	"github.com/NightMachinery/agent-desk/internal/media"
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
	// HTTP sends Web Push messages; nil is a default client.
	HTTP *http.Client

	st             *store.Store
	cfg            *config.Config
	compactForSend func(job, sendProgress)
}

// liveTTL bounds how stale the Paseo statuses behind node states may be.
const liveTTL = 10 * time.Second

// liveCache keeps the Paseo statuses read last, shared by every App (the
// server copies its App per request).
var liveCache struct {
	sync.Mutex
	home string
	at   time.Time
	m    map[string]string
}

func (a *App) paseoLive() map[string]string {
	liveCache.Lock()
	defer liveCache.Unlock()
	home, now := a.paseoHome(), time.Now()
	if liveCache.m == nil || liveCache.home != home || now.Sub(liveCache.at) > liveTTL {
		liveCache.m, liveCache.home, liveCache.at = ident.PaseoStatuses(home), home, now
	}
	return liveCache.m
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
                                      adds a node to those this session holds
  agent-desk takeover <paseo-id|session-id>
                                      take over every node that agent holds
  agent-desk leave <node>...          drop a node this session holds
      while a session holds several nodes, every write (ask, tell, set, clear,
      link, done, task add) needs --node PATH: a held node, full or relative
      to its project (run/role)
  agent-desk ask --blocked --title T                  <<'Q' ... Q
  agent-desk ask --default 'what you will do' --title T <<'Q' ... Q
      ask also takes --choices FILE: multiple-choice questions as JSON,
      {"questions": [{"text": "...", "options": ["...", "..."], "recommended": "B"}]}
      and --on-answer CMD: run CMD (answer on stdin) when the user answers;
      exit 0 means it handled the answer, anything else passes it on
  agent-desk tell [--notify] --title T                <<'R' ... R
  agent-desk tell --re <m-id|a-id> [--quiet] [--title T] <<'R' ... R
                                      respond to the user's message or answer
  agent-desk tell --re-chat 'what the user asked' [--quiet] [--title T] <<'R' ... R
                                      file your answer to a question asked in chat
  agent-desk whoami                   this session's nodes, one per line, or exit 1
  agent-desk instructions [<path-prefix>] [--since 2d] [--format json]
                                      the user's messages to this run, word for word
  agent-desk hook prompt --agent claude|codex   the UserPromptSubmit hook that records them
  agent-desk set <slot> [text]          text or stdin; status 2 lines, next 5, others 15
  agent-desk set --for agents <slot> [text]   a note only agents see (run state): never unread, 400 lines
  agent-desk get <slot> [--node PATH]   print a slot's current text (yours by default)
  agent-desk clear <slot>
  agent-desk task add --title T [--brief TEXT|-] [--priority 1-3] [--size S|M|L] [--node PATH]
  agent-desk task add --defer --title T [--size] [--priority] [--profile P] [--cwd DIR] <<'B' ... B
  agent-desk task start|done|drop|defer|reopen <id>... [--note TEXT]
  agent-desk task list [<prefix>] [--deferred] [--all] [--format json] | task prompt <id>
  agent-desk task launch <id>... | --deferred [<prefix>] [--max N] [--dry-run]
                                      start agents on tasks ([tasks] launch_command)
  agent-desk task nudge [--dry-run]   remind the user when quota would go unused (cron)
  agent-desk link <slot> <file>
  agent-desk screen take --reason R [--for 30m] [--wait [--timeout 1h]]
                                      before driving this machine's GUI: yours at once if the
                                      user has been idle 30 minutes, else asks them (exit 3
                                      until granted); a banner shows while you hold it
  agent-desk screen release | status [--host H]
  agent-desk answer <id> --from-chat 'text'   record an answer given in chat
  agent-desk done --title T [--report FILE]   <<'S' ... S   (agent-done calls this)
      ask, tell and done take [--attach FILE --caption 'what it shows']...: any
      file, up to 8; images, audio and video play in the dashboard

User side:
  agent-desk inbox [<path-prefix>] [--all]    interactive (needs fzf)
  agent-desk show [<path-prefix>|--mine] [--all] [--unfocused] [--host H] [--session S] [--format org|md|json]
  agent-desk screen grant|deny <q-id>   hand an agent the screen, or refuse it
  agent-desk answer <id> [--behavior B] [--notify-desktop V] [--notify-remote V] 'text'
                                      record, then type it into the asking session
  agent-desk reply <id|path> [--behavior B] [--notify-desktop V] [--notify-remote V] 'text'
                                      message the session behind an item or node; the
                                      --notify-* values route the notification of its response
                                      B: steer (joins the running turn), queue (waits for
                                      the turn to end) or interrupt (cancels it); the
                                      default is the deliver.behavior setting, else steer
      answer and reply take [--attach FILE [--caption C]]...: the session gets
      the files' paths
  agent-desk reply <id|path> --prompt velocity-report
                                      send a prepared message: an executive report on the
                                      node's velocity (internal/prompts; yours in
                                      ~/.config/agent-desk/prompts/ replaces it)
  agent-desk deliver                  retry answers and replies not yet delivered
  agent-desk deliver --force <id>      retry an unconfirmed send even if the agent already got it
  agent-desk serve [--listen ADDR|unix:PATH] [--allow-host H,...] [--allow-origin URL,...]
                   [--pid-file F] [--config F.json]
                                      the web dashboard (default 127.0.0.1:7740)
  agent-desk read <id|path-prefix>... [--host H]
  agent-desk unread <id>...
  agent-desk pin <id>... | unpin <id>...   keep items on top (a slot stays pinned as it changes)
  agent-desk search <text> [--host H]
  agent-desk pref set <key> <value> [--scope PATH] [--class C]
  agent-desk pref unset <key> [--scope PATH] [--class C]
  agent-desk pref show [<path>] [--class C] [--format json]
                                      settings: deliver.behavior, notify.desktop,
                                      notify.remote, notify.remote.dest, notify.push
  agent-desk sets [--format json]     project sets; * marks the focused one
  agent-desk sets define NAME PREFIX...   create or replace a set of path prefixes
  agent-desk sets delete NAME
  agent-desk focus [NAME|off] [--format json]
                                      show only a set's items, and notify only for them;
                                      no argument prints the focus
  agent-desk quota list [--format json]   Paseo agents stopped at a usage limit ([quota])
  agent-desk cache                     show this Paseo agent's cache state
  agent-desk cache warm|compact|off [--warm-hours N] [<paseo-id>]
  agent-desk standby [<paseo-id>]       end warming and switch to compact
  agent-desk quota auto-continue on|off <paseo-id>
  agent-desk quota dismiss|undismiss <paseo-id>   hide it from Out of quota until it runs again
  agent-desk quota ask-parent <paseo-id> [--dry-run]   ask its parent to hand its role on
  agent-desk quota replace <paseo-id> [--profile P] [--dry-run]   start an agent to take over
  agent-desk open <id|path>       what a notification click runs (open_command)
  agent-desk export | import [--notify] | snapshot <file> | render
  agent-desk merge <file>|ssh:<host> [--notify]
  agent-desk gc [--dry-run] [--days N]   delete attachments of old items read or answered
                                      (media_keep_days, default 30)

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
	var x exitError
	if errors.As(err, &x) {
		return x.code
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
	d, err := desk.Load(st, a.Host, a.Now())
	if err == nil {
		d.Live = a.paseoLive()
	}
	return d, err
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
	case "takeover":
		return a.cmdTakeover(rest)
	case "ask":
		return a.cmdAsk(rest)
	case "tell":
		return a.cmdTell(rest)
	case "set":
		return a.cmdSet(rest)
	case "pref":
		return a.cmdPref(rest)
	case "sets":
		return a.cmdSets(rest)
	case "focus":
		return a.cmdFocus(rest)
	case "whoami":
		return a.cmdWhoami(rest)
	case "leave":
		return a.cmdLeave(rest)
	case "hook":
		return a.cmdHook(rest)
	case "instructions":
		return a.cmdInstructions(rest)
	case "task":
		return a.cmdTask(rest)
	case "screen":
		return a.cmdScreen(rest)
	case "cache", "standby":
		return a.cmdCache(cmd, rest)
	case "quota":
		return a.cmdQuota(rest)
	case "get":
		return a.cmdGet(rest)
	case "pin":
		return a.cmdPin(rest, false)
	case "unpin":
		return a.cmdPin(rest, true)
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
	case "gc":
		return a.cmdGC(rest)
	case "media":
		return a.cmdMedia(rest)
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
	case "notify-run":
		return a.cmdNotifyRun(rest)
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
	// seq is every valued option in the order given, for options that may
	// repeat (--attach F --caption C ...).
	seq [][2]string
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
			o.seq = append(o.seq, [2]string{name, val})
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
	return nodeOwnerFrom(d.Events, node)
}

func nodeOwnerFrom(events []store.Event, node string) (store.Event, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if e := events[i]; e.Kind == "join" && e.Path == node {
			return e, true
		}
	}
	return store.Event{}, false
}

// commit appends events in one transaction, prints the display id of the
// event `show` ("ok" when show is empty, nothing when it is "-"), and re-renders.
func (a *App) commit(evs []store.Event, show string) error {
	st, err := a.store()
	if err != nil {
		return err
	}
	if err := st.Append(evs...); err != nil {
		return err
	}
	switch show {
	case "-": // the caller printed its own report
	case "":
		fmt.Fprintln(a.Stdout, "ok")
	default:
		d, err := a.load()
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Stdout, d.Display(show))
	}
	if err := a.render(); err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: warning: rendering failed: %v\n", err)
	}
	a.announce(evs, false, false)
	a.maybeGC()
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

// writer resolves the node this session writes to: the --node value (a
// node it holds), else the one node it holds, auto-joining
// <project>/adhoc/<slug> when a session with no parent holds none. With
// several held and no --node it refuses: a write must never land on a node
// the agent did not mean. The returned join event, if any, must be
// committed with the write.
func (a *App) writer(d *desk.Desk, project, nodeArg, cmd string) (ident.From, string, *store.Event, error) {
	from := ident.Resolve(a.Env)
	if from.Session == "" {
		return from, "", nil, errors.New("cannot tell which agent session this is: none of " +
			"CLAUDE_CODE_SESSION_ID, CODEX_THREAD_ID, CODEX_SESSION_ID or ANTIGRAVITY_CONVERSATION_ID is set. " +
			"Only agent sessions write to the desk")
	}
	node, err := a.myNode(d, from, nodeArg, cmd)
	if err != nil || node != "" {
		return from, node, nil, err
	}
	if err := a.takenOver(d.Events, from); err != nil {
		return from, "", nil, err
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
	node = project + "/adhoc/" + slug
	if owner, ok := nodeHolder(d, node); ok && sessionOf(owner) != from.Session {
		node = project + "/adhoc/" + shortID(from.Session)
	}
	j := a.event("join", node, from, "cli", map[string]any{"auto": true})
	return from, node, &j, nil
}

func shortID(s string) string {
	s = ident.Slug(s, 64)
	s = strings.ReplaceAll(s, "-", "")
	if len(s) > 8 {
		s = s[:8]
	}
	return s
}

// agentWrite writes events built for this session's node; nodeArg is the
// command's --node value and cmd its name, for the refusal.
func (a *App) agentWrite(project, nodeArg, cmd string, build func(from ident.From, node string, d *desk.Desk) ([]store.Event, error)) error {
	d, err := a.load()
	if err != nil {
		return err
	}
	from, node, join, err := a.writer(d, project, nodeArg, cmd)
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
	if owner, ok := nodeHolder(d, node); ok && sessionOf(owner) != from.Session && !o.bools["takeover"] {
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
	o, err := parse(args, []string{"blocked"}, append([]string{"default", "title", "project", "node", "choices", "on-answer"}, attachOpts...), 0, 0)
	if err != nil {
		return err
	}
	if o.set["on-answer"] && strings.TrimSpace(o.vals["on-answer"]) == "" {
		return usagef("--on-answer needs the command to run when the user answers")
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
	if f := o.vals["choices"]; o.set["choices"] {
		if !filepath.IsAbs(f) && a.Env.Cwd != "" {
			f = filepath.Join(a.Env.Cwd, f)
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		c, err := desk.ParseChoices(b)
		if err != nil {
			return usagef("%s: %v", o.vals["choices"], err)
		}
		extra["choices"] = c
	}
	if o.set["on-answer"] {
		extra["on_answer"] = o.vals["on-answer"]
	}
	if err := a.attach(o, extra); err != nil {
		return err
	}
	return a.agentWrite(o.vals["project"], o.vals["node"], "ask", func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
		e := a.event("ask", node, from, "cli", extra)
		e.Level, e.Title, e.Body = level, title, body
		return []store.Event{e}, nil
	})
}

func (a *App) cmdTell(args []string) error {
	o, err := parse(args, []string{"notify", "quiet"}, append([]string{"title", "project", "node", "re", "re-chat"}, attachOpts...), 0, 0)
	if err != nil {
		return err
	}
	re, asked := strings.TrimSpace(o.vals["re"]), strings.TrimSpace(o.vals["re-chat"])
	if o.set["re"] && re == "" {
		return usagef("--re needs the id of the message or answer you respond to")
	}
	if o.set["re-chat"] && asked == "" {
		return usagef("--re-chat needs what the user asked in chat, quoted or in a line")
	}
	if re != "" && asked != "" {
		return usagef("give --re (a message on the desk) or --re-chat (a question asked in chat), not both")
	}
	responding := re != "" || asked != ""
	if o.bools["quiet"] && !responding {
		return usagef("--quiet goes with --re or --re-chat: it marks a bare acknowledgement")
	}
	title := strings.TrimSpace(o.vals["title"])
	if title == "" && !responding {
		return usagef("tell needs --title")
	}
	if title == "" && asked != "" {
		title = "Re: " + clipLine(asked, 60)
	}
	body, err := a.stdinText()
	if err != nil {
		return err
	}
	if responding && strings.TrimSpace(body) == "" {
		return usagef("tell --re needs the response on stdin, even one line such as \"Done.\"")
	}
	level := "fyi"
	if o.bools["notify"] {
		level = "notify"
	}
	extra := map[string]any{}
	if err := a.attach(o, extra); err != nil {
		return err
	}
	nodeArg := o.vals["node"]
	if re != "" && nodeArg == "" {
		// The message or answer responded to names its node: when this
		// session holds it, that is the node, however many it holds.
		if node, err := a.reNode(re); err != nil {
			return err
		} else if node != "" {
			nodeArg = node
		}
	}
	return a.agentWrite(o.vals["project"], nodeArg, "tell", func(from ident.From, node string, d *desk.Desk) ([]store.Event, error) {
		if asked != "" {
			extra["asked"] = asked
		}
		if o.bools["quiet"] {
			extra["quiet"] = true
		}
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
		}
		e := a.event("tell", node, from, "cli", extra)
		e.Level, e.Title, e.Body, e.Ref = level, title, body, ref
		return []store.Event{e}, nil
	})
}

// cmdPin pins items to the top of every view, or unpins them. A pin on a
// slot holds the slot, whatever its current text.
func (a *App) cmdPin(args []string, unpin bool) error {
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
		switch e.Kind {
		case "read", "delivered", "undelivered", "pin", "pref", "notified", "retracted", "join", "leave", "projectset", "focus":
			return fmt.Errorf("%s is a %s mark; pin the item itself", d.Display(e.ID), e.Kind)
		}
		k := d.PinKey(e)
		if seen[k] {
			continue
		}
		seen[k] = true
		if d.Pinned(e) != unpin {
			state := "pinned"
			if unpin {
				state = "not pinned"
			}
			fmt.Fprintf(a.Stderr, "agent-desk: %s is already %s\n", d.Display(e.ID), state)
			continue
		}
		var extra map[string]any
		if unpin {
			extra = map[string]any{"cancel": true}
		}
		p := a.event("pin", e.Path, from, "cli", extra)
		p.Ref = e.ID
		evs = append(evs, p)
	}
	if len(evs) == 0 {
		fmt.Fprintln(a.Stdout, "ok")
		return nil
	}
	return a.commit(evs, "")
}

// clipLine is the first line of s, cut to n characters.
func clipLine(s string, n int) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
	if r := []rune(line); len(r) > n {
		line = string(r[:n-1]) + "…"
	}
	return line
}

// cmdWhoami prints the nodes the calling session holds, one per line, in
// the order it joined them.
func (a *App) cmdWhoami(args []string) error {
	if _, err := parse(args, nil, nil, 0, 0); err != nil {
		return err
	}
	st, err := a.store()
	if err != nil {
		return err
	}
	from := ident.Resolve(a.Env)
	if from.Session == "" {
		return errors.New("not an agent session")
	}
	events, err := st.Membership()
	if err != nil {
		return err
	}
	held := a.heldFrom(events, from)
	if len(held) == 0 {
		if err := a.takenOver(events, from); err != nil {
			return err
		}
		return errors.New("this session has not joined a node")
	}
	for _, j := range held {
		fmt.Fprintln(a.Stdout, j.Path)
	}
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

// Agent notes (set --for agents) are bigger: run state an agent keeps for
// itself, read back after compaction.
const (
	NoteCap   = 400
	NoteBytes = 64 << 10
)

func (a *App) cmdSet(args []string) error {
	o, err := parse(args, nil, []string{"project", "node", "for"}, 1, -1)
	if err != nil {
		return err
	}
	name := o.pos[0]
	if err := slotArg(name); err != nil {
		return err
	}
	agents := false
	switch o.vals["for"] {
	case "", "user":
	case "agents":
		agents = true
	default:
		return usagef("--for is agents (or user, the default)")
	}
	if Reserved[name] && !(agents && name == "state") {
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
	if agents {
		limit = NoteCap
		if len(text) > NoteBytes {
			return fmt.Errorf("an agent note takes at most %d KiB; this text has %d bytes. Nothing was written", NoteBytes>>10, len(text))
		}
	}
	if n := strings.Count(text, "\n") + 1; n > limit {
		return fmt.Errorf("slot %s takes at most %d lines; this text has %d. Nothing was written. "+
			"State only what is true now", name, limit, n)
	}
	var extra map[string]any
	if agents {
		extra = map[string]any{"audience": "agents"}
	}
	return a.agentWrite(o.vals["project"], o.vals["node"], "set", func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
		e := a.event("set", node+"/"+name, from, "cli", extra)
		e.Body = text
		return []store.Event{e}, nil
	})
}

func (a *App) cmdClear(args []string) error {
	o, err := parse(args, nil, []string{"project", "node"}, 1, 1)
	if err != nil {
		return err
	}
	if err := slotArg(o.pos[0]); err != nil {
		return err
	}
	return a.agentWrite(o.vals["project"], o.vals["node"], "clear", func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
		return []store.Event{a.event("clear", node+"/"+o.pos[0], from, "cli", nil)}, nil
	})
}

func (a *App) cmdLink(args []string) error {
	o, err := parse(args, nil, []string{"project", "node"}, 2, 2)
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
	return a.agentWrite(o.vals["project"], o.vals["node"], "link", func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
		return []store.Event{a.event("link", node+"/"+name, from, "cli", map[string]any{"file": file})}, nil
	})
}

func (a *App) cmdAnswer(args []string) error {
	o, err := parse(args, nil, append([]string{"from-chat", "behavior", "notify-desktop", "notify-remote"}, attachOpts...), 1, -1)
	if err != nil {
		return err
	}
	beh, err := checkBehavior(o.vals["behavior"])
	if err != nil {
		return err
	}
	if o.set["from-chat"] && (o.set["behavior"] || o.set["notify-desktop"] || o.set["notify-remote"]) {
		return usagef("--behavior and --notify-* are for delivering an answer; one given in chat is not delivered")
	}
	notify, err := checkNotify(o.vals["notify-desktop"], o.vals["notify-remote"])
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
	} else if len(o.pos) == 1 && !o.set["attach"] {
		if text, err = a.stdinText(); err != nil {
			return err
		}
	}
	if o.set["attach"] && source == "chat" {
		return usagef("--attach goes with an answer from you, not one given in chat")
	}
	refs, err := a.attachRefs(o, false)
	if err != nil {
		return err
	}
	return a.recordAnswer(d, q, text, source, beh, notify, refs...)
}

// answerEvents prepares an answer and any associated screen decision.
func (a *App) answerEvents(d *desk.Desk, q store.Event, text, source, behavior string, notify map[string]string, refs ...media.Ref) ([]store.Event, error) {
	if strings.TrimSpace(text) == "" && len(refs) > 0 {
		text = attachedOnly
	}
	if strings.TrimSpace(text) == "" {
		return nil, usagef("answer needs the answer's text")
	}
	for _, x := range d.Questions {
		if x.ID == q.ID && !x.Open() {
			fmt.Fprintf(a.Stderr, "agent-desk: %s was already answered (%s); this answer replaces it\n",
				d.Display(q.ID), d.Display(x.AnswerID))
		}
	}
	var extra map[string]any
	if source != "chat" {
		b, err := checkBehavior(behavior)
		if err != nil {
			return nil, err
		}
		if b == "" {
			if b, err = a.defaultBehavior(d, q.Path); err != nil {
				return nil, err
			}
		}
		extra = map[string]any{"behavior": b}
		if notify != nil {
			extra["notify"] = notify
		}
	}
	if len(refs) > 0 {
		if extra == nil {
			extra = map[string]any{}
		}
		extra["media"] = refs
	}
	e := a.event("answer", q.Path, ident.Resolve(a.Env), source, extra)
	e.Ref, e.Body = q.ID, text
	evs := []store.Event{e}
	if se, ok := a.screenDecision(q, text, source); ok {
		evs = append(evs, se) // an answer to a screen request grants or denies it
	}
	return evs, nil
}

// recordAnswer appends an answer and delivers it for the CLI and bridges.
func (a *App) recordAnswer(d *desk.Desk, q store.Event, text, source, behavior string, notify map[string]string, refs ...media.Ref) error {
	evs, err := a.answerEvents(d, q, text, source, behavior, notify, refs...)
	if err != nil {
		return err
	}
	e := evs[0]
	if err := a.commit(evs, e.ID); err != nil {
		return err
	}
	if source == "chat" {
		return nil
	}
	if q.Host != a.Host {
		a.pushTo(q.Host, "asked on "+q.Host)
		return nil
	}
	if desk.MetaString(q, "on_answer") != "" && !a.WaitHooks {
		// The command may take minutes: whoever recorded the answer (the
		// dashboard, Emacs, a chat bridge) does not wait for it.
		if err := a.spawn([]string{"deliver"}); err != nil {
			return err
		}
		fmt.Fprintf(a.Stderr, "agent-desk: %s goes to the command it was asked with; its output is in %s\n",
			d.Display(q.ID), filepath.Join(a.Home, "hooks.log"))
		return a.renderWarn()
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
	o, err := parse(args, []string{"all", "mine", "unfocused"}, []string{"format", "host", "session", "node"}, 0, 1)
	if err != nil {
		return err
	}
	if o.set["node"] && !o.bools["mine"] {
		return usagef("--node goes with --mine; to show any node, give its path")
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
		node, err := a.myNode(d, ident.Resolve(a.Env), o.vals["node"], "show --mine")
		if err != nil {
			return err
		}
		if node == "" {
			return errors.New("this session has not joined a node")
		}
		f.Prefix, f.Dismissed = node, true
	} else if !o.bools["unfocused"] {
		// The user's view follows the focus; an agent's own view (--mine)
		// never does.
		f.Focus = d.FocusPrefixes()
	}
	v := d.Build(f)
	if o.bools["mine"] {
		v.Awaiting = d.AwaitingGroups(f.Prefix)
	}
	if fi := focusInfo(d, f); fi != nil && len(f.Focus) > 0 {
		v.Focus = fi
		note := "agent-desk: focus " + fi.Name + " (" + strings.Join(fi.Prefixes, ", ") + ")"
		if fi.Hidden.Total() > 0 {
			note += "; " + hiddenText(fi.Hidden)
		}
		fmt.Fprintln(a.Stderr, note+"; --unfocused shows everything")
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
		if desk.IsMark(e.Kind) || e.Kind == "join" || e.Kind == "leave" || e.Kind == "delivered" {
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
	if a.st != nil && a.st.Skipped > 0 {
		fmt.Fprintf(a.Stderr, "agent-desk: left out %d event(s) of kinds this version does not know; "+
			"update agent-desk and merge again to take them in\n", a.st.Skipped)
	}
	if len(added) == 0 {
		return nil
	}
	err := a.renderWarn()
	a.announce(added, true, notify)
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
	a.mergeMediaDir(filepath.Dir(src))
	if err := a.imported(added, o.bools["notify"]); err != nil {
		return err
	}
	_, err = a.deliver("")
	return err
}

// cmdOpen opens an item or node for the user: what a notification click
// runs. With open_command configured it hands over the item; otherwise,
// with base_url, it opens the item's page in the browser; otherwise, in a
// terminal, it opens the inbox scoped to the item's node.
func (a *App) cmdOpen(args []string) (err error) {
	o, err := parse(args, nil, nil, 1, 1)
	if err != nil {
		return err
	}
	// A notification click runs this with no terminal to see it: each try,
	// what open_command printed to stderr, and how it ended go to hooks.log.
	if logf, lerr := os.OpenFile(filepath.Join(a.Home, "hooks.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); lerr == nil {
		defer logf.Close()
		fmt.Fprintf(logf, "%s open %s\n", a.Now().Format(time.RFC3339), o.pos[0])
		saved := a.Stderr
		a.Stderr = io.MultiWriter(saved, logf)
		defer func() {
			a.Stderr = saved
			if err != nil {
				fmt.Fprintf(logf, "%s open %s failed: %v\n", a.Now().Format(time.RFC3339), o.pos[0], err)
			}
		}()
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
	if p.URL == "" && c.BaseURL != "" {
		// A node: its view in the dashboard.
		p.URL = strings.TrimRight(c.BaseURL, "/") + "/#/p/" + p.Path
	}
	if p.URL != "" {
		opener := "xdg-open"
		if runtime.GOOS == "darwin" {
			opener = "open"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := a.runExt(ctx, nil, opener, p.URL)
		return err
	}
	if isTerminal(a.Stdout) {
		return a.cmdInbox([]string{p.Path})
	}
	return fmt.Errorf("no open_command in %s; run agent-desk inbox %s", config.Path(a.Env.Getenv, a.Env.Home), p.Path)
}

// cmdDone files a finished session's summary and report path, and sets its
// status slot to say so, in one transaction.
func (a *App) cmdDone(args []string) error {
	o, err := parse(args, nil, append([]string{"title", "report", "project", "node"}, attachOpts...), 0, 0)
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
	if err := a.attach(o, extra); err != nil {
		return err
	}
	return a.agentWrite(o.vals["project"], o.vals["node"], "done", func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
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
