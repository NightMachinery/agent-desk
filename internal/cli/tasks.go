package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/config"
	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/media"
	"github.com/NightMachinery/agent-desk/internal/prompts"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Tasks: work items at a node. An agent keeps its own list (open, started,
// done); a deferred task waits for quota to spare, and launching it starts
// an agent on it through launch_command.

// LaunchTimeout bounds one launch_command run.
const LaunchTimeout = 2 * time.Minute

func (a *App) cmdTask(args []string) error {
	if len(args) == 0 {
		return usagef("task add|start|done|drop|defer|reopen|title|tag|untag|launch|prompt|list|nudge; see agent-desk help")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return a.taskAdd(rest)
	case "start", "done", "drop", "defer", "reopen":
		return a.taskMark(sub, rest)
	case "tag", "untag":
		return a.taskTag(sub == "tag", rest)
	case "title":
		return a.taskTitle(rest)
	case "launch":
		return a.taskLaunch(rest)
	case "list":
		return a.taskList(rest)
	case "prompt":
		if len(rest) != 1 {
			return usagef("task prompt <id>")
		}
		return a.taskList([]string{"--prompt", rest[0]})
	case "nudge":
		return a.taskNudge(rest)
	}
	return usagef("unknown task command %q", sub)
}

func (a *App) taskAdd(args []string) error {
	o, err := parse(args, []string{"defer", "quota", "steps"}, []string{"title", "brief", "size", "profile", "priority", "cwd", "node", "project", "type", "parent", "tag"}, 0, 0)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(o.vals["title"])
	if title == "" {
		return usagef("task add needs --title")
	}
	// --defer files it deferred; --quota (with or without --defer) deferred
	// for quota, for an agent to launch when there is quota to spare.
	m := desk.TaskMeta{Deferred: o.bools["quota"], Later: o.bools["defer"] && !o.bools["quota"], Profile: o.vals["profile"], Cwd: o.vals["cwd"]}
	if t := strings.ToLower(strings.TrimSpace(o.vals["type"])); t != "" && t != "task" {
		if !desk.ValidTaskType(t) {
			return usagef("--type is one of %s", strings.Join(desk.TaskTypes, ", "))
		}
		m.Type = t
	}
	var typed []string
	for _, kv := range o.seq {
		if kv[0] == "tag" {
			typed = append(typed, kv[1])
		}
	}
	if m.Tags, err = desk.NormTags(typed); err != nil {
		return usagef("--tag: %v", err)
	}
	if p := strings.TrimSpace(o.vals["parent"]); p != "" {
		d, err := a.load()
		if err != nil {
			return err
		}
		parent, err := a.task(d, p)
		if err != nil {
			return err
		}
		m.Parent = parent.Event.ID
	}
	switch s := strings.ToUpper(o.vals["size"]); s {
	case "", "S", "M", "L":
		m.Size = s
	default:
		return usagef("--size is S, M or L")
	}
	if p := o.vals["priority"]; p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 3 {
			return usagef("--priority is 1 (first), 2 or 3")
		}
		m.Priority = n
	}
	if m.Cwd != "" {
		if m.Cwd, err = filepath.Abs(m.Cwd); err != nil {
			return err
		}
	}
	// The brief comes from stdin only when asked for (--brief -) or needed
	// (--defer, --quota): an agent's shell may hold stdin open, and reading
	// it unasked would hang.
	brief := o.vals["brief"]
	fromStdin := brief == "-" || ((m.Deferred || m.Later) && !o.set["brief"])
	var steps []string
	if o.bools["steps"] {
		switch {
		case m.Deferred:
			return usagef("--steps does not go with --quota: the brief is the plan, and the agent that takes the task files its own steps")
		case fromStdin:
			return usagef("--steps reads the steps from stdin, so give the brief as --brief TEXT")
		}
		text, err := a.stdinText()
		if err != nil {
			return err
		}
		if steps = stepLines(text); len(steps) == 0 {
			return usagef("--steps found no steps on stdin: write one per line")
		}
		if len(steps) > MaxSteps {
			return usagef("--steps takes at most %d steps; this has %d", MaxSteps, len(steps))
		}
	}
	if fromStdin {
		if brief, err = a.stdinText(); err != nil {
			return err
		}
	}
	brief = strings.TrimSpace(brief)
	if m.Deferred && brief == "" {
		return usagef("a task deferred for quota needs a brief on stdin: the agent that picks it up knows only what it says")
	}
	if m.Later && isAgent(ident.Resolve(a.Env)) {
		// --defer meant "for quota" before --quota existed.
		fmt.Fprintln(a.Stderr, "agent-desk: filed deferred, not for quota. For work that waits for spare quota, file it with --quota, or run: agent-desk task defer --quota <id>")
	}
	if brief == "" && m.Parent == "" && isAgent(ident.Resolve(a.Env)) {
		return usagef("task add needs --brief: a short executive summary the user reads under the title (what and why, one to three sentences; --brief - reads it from stdin). A step under --parent may go without one")
	}
	// The task, then each step under it (a step is a task with a parent),
	// parked with it when it is filed deferred.
	build := func(from ident.From, node string) []store.Event {
		if m.Cwd == "" && m.Deferred {
			m.Cwd = from.Cwd
		}
		var extra map[string]any
		b, _ := json.Marshal(m)
		_ = json.Unmarshal(b, &extra)
		e := a.event("task", node, from, "cli", extra)
		e.Title, e.Body = title, brief
		evs := []store.Event{e}
		for _, st := range steps {
			sm := map[string]any{"parent": e.ID}
			if m.Later {
				sm["later"] = true
			}
			se := a.event("task", node, from, "cli", sm)
			se.Title = st
			evs = append(evs, se)
		}
		return evs
	}
	var filed []store.Event
	err = func() error {
		nodeArg := strings.Trim(o.vals["node"], "/")
		if nodeArg != "" {
			// A node this session holds (also relative to its project), else
			// filed by the user, or for another node, by its full path.
			from := ident.Resolve(a.Env)
			d, err := a.load()
			if err != nil {
				return err
			}
			if _, err := pickHeld(heldPaths(a.held(d, from)), nodeArg); err != nil {
				if strings.Count(nodeArg, "/") < 2 {
					return usagef("--node is a full node path: project/run/role, or a node this session holds")
				}
				filed = build(from, nodeArg)
				return a.commit(filed, filed[0].ID)
			}
		}
		return a.agentWrite(o.vals["project"], nodeArg, "task add", func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
			filed = build(from, node)
			return filed, nil
		})
	}()
	if err != nil || len(filed) < 2 {
		return err
	}
	// The task's id is printed; each step's follows, with its title.
	st, err := a.store()
	if err != nil {
		return err
	}
	for _, e := range filed[1:] {
		display, err := st.DisplayID(e)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "  %s %s\n", display, e.Title)
	}
	return nil
}

// MaxSteps bounds the steps task add --steps files at once.
const MaxSteps = 50

var stepMark = regexp.MustCompile(`^(?:[-*+]|\d+[.)]|\[[ xX]?\])\s+`)

// stepLines are the steps in text, one per line: blank lines skipped, and a
// list mark (-, *, 1., [ ]) taken off, so a pasted checklist works too.
func stepLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		for {
			t := stepMark.ReplaceAllString(l, "")
			if t == l {
				break
			}
			l = strings.TrimSpace(t)
		}
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

var markState = map[string]string{
	"start": desk.TaskStarted, "done": desk.TaskDone, "drop": desk.TaskDropped,
	"defer": desk.TaskLater, "reopen": desk.TaskOpen,
}

func (a *App) taskMark(sub string, args []string) error {
	o, err := parse(args, []string{"quota"}, []string{"note", "start"}, 1, -1)
	if err != nil {
		return err
	}
	state := markState[sub]
	if o.set["start"] && sub != "done" && sub != "drop" {
		return usagef("--start goes with task done or task drop: finish one task and start the next")
	}
	if o.bools["quota"] {
		if sub != "defer" {
			return usagef("--quota goes with task defer")
		}
		state = desk.TaskDeferred
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	from := ident.Resolve(a.Env)
	note := o.vals["note"]
	if note == "-" {
		if note, err = a.stdinText(); err != nil {
			return err
		}
	}
	note = strings.TrimSpace(note)
	if note == "" && isAgent(from) {
		switch sub {
		case "done":
			return usagef("task done needs --note: what was done and how it was checked, in a line or two the user reads on the task (--note - reads it from stdin)")
		case "drop":
			return usagef("task drop needs --note: why it was dropped, in a line the user reads on the task (--note - reads it from stdin)")
		}
	}
	var evs []store.Event
	for _, tok := range o.pos {
		t, err := a.task(d, tok)
		if err != nil {
			return err
		}
		extra := map[string]any{"state": state}
		if sub == "start" && from.Session != "" {
			extra["agent"] = agentLabel(from)
		}
		e := a.event("taskstate", t.Event.Path, from, "cli", extra)
		e.Ref, e.Body = t.Event.ID, note
		evs = append(evs, e)
	}
	if tok := strings.TrimSpace(o.vals["start"]); o.set["start"] {
		if tok == "" {
			return usagef("--start needs the id of the task to start next")
		}
		next, err := a.task(d, tok)
		if err != nil {
			return err
		}
		for _, e := range evs {
			if e.Ref == next.Event.ID {
				return usagef("--start names %s, which this command finishes", next.ID)
			}
		}
		if !next.Active() {
			return usagef("%s is %s: reopen it first (agent-desk task reopen %s)", next.ID, next.State, next.ID)
		}
		extra := map[string]any{"state": desk.TaskStarted}
		if from.Session != "" {
			extra["agent"] = agentLabel(from)
		}
		e := a.event("taskstate", next.Event.Path, from, "cli", extra)
		e.Ref = next.Event.ID
		evs = append(evs, e)
	}
	return a.commit(evs, "")
}

// isAgent reports whether from is an agent session, not the user at a shell
// (or the dashboard, which runs the CLI as the user): an agent must say why
// it files a task and how it finished one, for the user to read.
func isAgent(from ident.From) bool {
	return from.Session != "" || from.Paseo != ""
}

// taskTag adds tags to a task (task tag ID TAG...) or removes them
// (task untag ID TAG...), keeping its state and note.
func (a *App) taskTag(add bool, args []string) error {
	o, err := parse(args, nil, nil, 2, -1)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	t, err := a.task(d, o.pos[0])
	if err != nil {
		return err
	}
	tags, err := desk.NormTags(o.pos[1:])
	if err != nil {
		return usagef("%v", err)
	}
	next := []string{}
	if add {
		next = append(next, t.Tags...)
		next = append(next, tags...)
	} else {
		for _, x := range t.Tags {
			if !slices.Contains(tags, x) {
				next = append(next, x)
			}
		}
	}
	return a.setTaskTags(t, next, ident.Resolve(a.Env), "cli")
}

// taskTitle renames a task (task title ID TITLE...), keeping its state and
// note: how an agent names a task the user filed without a title.
func (a *App) taskTitle(args []string) error {
	o, err := parse(args, nil, nil, 2, -1)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(strings.Join(o.pos[1:], " "))
	if title == "" || strings.Contains(title, "\n") {
		return usagef("task title needs a title of one line")
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	t, err := a.task(d, o.pos[0])
	if err != nil {
		return err
	}
	if title == t.Event.Title && !t.Untitled {
		return nil
	}
	e := a.event("taskstate", t.Event.Path, ident.Resolve(a.Env), "cli", map[string]any{"title": title})
	e.Ref = t.Event.ID
	return a.commit([]store.Event{e}, "-")
}

// untitledLen is how much of the brief's first line titles a task filed
// without a title, until an agent names it.
const untitledLen = 60

// setTaskTags records a task's whole new tag list.
func (a *App) setTaskTags(t desk.Task, tags []string, from ident.From, source string) error {
	tags, err := desk.NormTags(tags)
	if err != nil {
		return usagef("%v", err)
	}
	if slices.Equal(tags, t.Tags) || (len(tags) == 0 && len(t.Tags) == 0) {
		return nil
	}
	e := a.event("taskstate", t.Event.Path, from, source, map[string]any{"tags": tags})
	e.Ref = t.Event.ID
	return a.commit([]store.Event{e}, "-")
}

func agentLabel(f ident.From) string {
	if f.Paseo != "" {
		return "paseo:" + f.Paseo
	}
	return f.Agent + ":" + f.Session
}

// task resolves a token to a task.
func (a *App) task(d *desk.Desk, tok string) (desk.Task, error) {
	e, err := d.Resolve(tok)
	if err != nil {
		return desk.Task{}, err
	}
	if e.Kind != "task" {
		return desk.Task{}, fmt.Errorf("%s is not a task", d.Display(e.ID))
	}
	for _, t := range d.Tasks(desk.Filter{}) {
		if t.Event.ID == e.ID {
			return t, nil
		}
	}
	return desk.Task{}, fmt.Errorf("task %s not found", tok)
}

// LaunchPayload is what launch_command reads on stdin.
type LaunchPayload struct {
	ID       string `json:"id"`
	Display  string `json:"display_id"`
	Title    string `json:"title"`
	Brief    string `json:"brief"`
	Path     string `json:"path"`
	Project  string `json:"project"`
	Run      string `json:"run"`
	Role     string `json:"role"`
	Cwd      string `json:"cwd"`
	Size     string `json:"size"`
	Profile  string `json:"profile"`
	Priority int    `json:"priority"`
	Prompt   string `json:"prompt"`
}

func (a *App) promptsDir() string {
	return filepath.Join(filepath.Dir(config.Path(a.Env.Getenv, a.Env.Home)), "prompts")
}

func (a *App) launchPayload(d *desk.Desk, t desk.Task) (LaunchPayload, error) {
	project, run, _ := desk.Split(t.Event.Path)
	p := LaunchPayload{
		ID: t.Event.ID, Display: t.ID, Title: t.Event.Title, Brief: t.Event.Body, Path: t.Event.Path,
		Project: project, Run: run, Role: "task-" + strings.TrimPrefix(t.ID, "w-"),
		Cwd: t.Cwd, Size: t.Size, Profile: t.Profile, Priority: t.Priority,
	}
	var err error
	p.Prompt, err = prompts.Render(a.promptsDir(), "start-task.md", p)
	return p, err
}

// taskLaunch starts agents on tasks: the ones named, or with --deferred the
// first deferred ones under a prefix, up to max_launch.
func (a *App) taskLaunch(args []string) error {
	o, err := parse(args, []string{"deferred", "dry-run"}, []string{"max"}, 0, -1)
	if err != nil {
		return err
	}
	c, err := a.config()
	if err != nil {
		return err
	}
	max := c.Tasks.Max()
	if s := o.vals["max"]; s != "" {
		if max, err = strconv.Atoi(s); err != nil || max < 1 {
			return usagef("--max is a positive number")
		}
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	var picked []desk.Task
	if o.bools["deferred"] {
		prefix := ""
		if len(o.pos) > 1 {
			return usagef("task launch --deferred takes at most one path prefix")
		}
		if len(o.pos) == 1 {
			prefix = o.pos[0]
		}
		for _, t := range d.Tasks(desk.Filter{Prefix: prefix}) {
			if t.State == desk.TaskDeferred && len(picked) < max {
				picked = append(picked, t)
			}
		}
		if len(picked) == 0 {
			return fmt.Errorf("no tasks deferred for quota under %q", prefix)
		}
	} else {
		if len(o.pos) == 0 {
			return usagef("task launch <id>... or task launch --deferred [<prefix>]")
		}
		if len(o.pos) > max {
			return usagef("that is %d tasks; max_launch is %d (pass --max to launch more at once)", len(o.pos), max)
		}
		for _, tok := range o.pos {
			t, err := a.task(d, tok)
			if err != nil {
				return err
			}
			if t.State == desk.TaskStarted || !t.Active() {
				return fmt.Errorf("task %s is %s", t.ID, t.State)
			}
			picked = append(picked, t)
		}
	}
	if o.bools["dry-run"] {
		for _, t := range picked {
			fmt.Fprintf(a.Stdout, "%s  %s\n", t.ID, t.Event.Title)
		}
		return nil
	}
	if c.Tasks.LaunchCommand == "" {
		return fmt.Errorf("no launch_command in [tasks] of %s: it starts an agent on a task (see docs/usage.md). "+
			"Until then, start one by hand with the prompt from: agent-desk task prompt %s",
			config.Path(a.Env.Getenv, a.Env.Home), picked[0].ID)
	}
	from := ident.Resolve(a.Env)
	var evs []store.Event
	var failed []string
	for _, t := range picked {
		p, err := a.launchPayload(d, t)
		if err != nil {
			return err
		}
		in, _ := json.Marshal(p)
		ctx, cancel := context.WithTimeout(context.Background(), LaunchTimeout)
		out, err := a.runExt(ctx, in, "sh", "-c", c.Tasks.LaunchCommand)
		cancel()
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v: %s", t.ID, err, clipLine(out, 200)))
			continue
		}
		e := a.event("taskstate", t.Event.Path, from, "launch", map[string]any{"state": desk.TaskStarted, "agent": lastLine(out)})
		e.Ref = t.Event.ID
		evs = append(evs, e)
		fmt.Fprintf(a.Stdout, "%s  launched  %s\n", t.ID, lastLine(out))
	}
	if len(evs) > 0 {
		if err := a.commit(evs, "-"); err != nil {
			return err
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("launch failed:\n%s", strings.Join(failed, "\n"))
	}
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	l := strings.TrimSpace(lines[len(lines)-1])
	if r := []rune(l); len(r) > 120 {
		l = string(r[:120])
	}
	return l
}

func (a *App) taskList(args []string) error {
	o, err := parse(args, []string{"all", "deferred", "org"}, []string{"format", "prompt", "type", "tag"}, 0, 1)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	if tok := o.vals["prompt"]; tok != "" {
		t, err := a.task(d, tok)
		if err != nil {
			return err
		}
		p, err := a.launchPayload(d, t)
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Stdout, p.Prompt)
		return nil
	}
	f := desk.Filter{}
	if len(o.pos) == 1 {
		f.Prefix = o.pos[0]
	}
	typ := strings.ToLower(o.vals["type"])
	if typ != "" && !desk.ValidTaskType(typ) {
		return usagef("--type is one of %s", strings.Join(desk.TaskTypes, ", "))
	}
	tag := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(o.vals["tag"]), "#"))
	all := d.Tasks(f)
	if tag != "" {
		kept := all[:0:0]
		for _, t := range all {
			if slices.Contains(t.Tags, tag) {
				kept = append(kept, t)
			}
		}
		all = kept
	}
	if o.bools["org"] {
		// The org checklist shows finished steps too, as a TASKS.org does.
		return writeTasksOrg(a.Stdout, d, all, typ, o.bools["all"])
	}
	var ts []desk.Task
	for _, t := range all {
		if o.bools["deferred"] && t.State != desk.TaskDeferred {
			continue
		}
		if typ != "" && t.Kind() != typ {
			continue
		}
		if !o.bools["all"] && !t.Active() {
			continue
		}
		ts = append(ts, t)
	}
	if o.vals["format"] == "json" {
		enc := json.NewEncoder(a.Stdout)
		for _, t := range ts {
			if err := enc.Encode(t); err != nil {
				return err
			}
		}
		return nil
	}
	for _, t := range ts {
		extra := []string{}
		if t.Size != "" {
			extra = append(extra, t.Size)
		}
		if t.Priority != 0 {
			extra = append(extra, fmt.Sprintf("p%d", t.Priority))
		}
		if t.Profile != "" {
			extra = append(extra, t.Profile)
		}
		if t.Agent != "" {
			extra = append(extra, t.Agent)
		}
		if t.ParentID != "" {
			extra = append(extra, "under "+t.ParentID)
		}
		for _, g := range t.Tags {
			extra = append(extra, "#"+g)
		}
		x := ""
		if len(extra) > 0 {
			x = "  [" + strings.Join(extra, " ") + "]"
		}
		kind := ""
		if t.Type != "" {
			kind = t.Type + ": "
		}
		fmt.Fprintf(a.Stdout, "%s  %-8s %s%s%s  %s\n", t.ID, t.State, kind, t.Event.Title, x, t.Event.Path)
	}
	return nil
}

// writeTasksOrg prints tasks as an org checklist (desk.TasksOrg).
func writeTasksOrg(w io.Writer, d *desk.Desk, ts []desk.Task, typ string, all bool) error {
	desk.TasksOrg(w, ts, typ, all, 1)
	return nil
}

// NudgePath is where the desk files its own quota notices.
const NudgePath = "_desk/quota/nudge"

type gauge struct {
	Pct   float64  `json:"pct"`
	Reset *float64 `json:"reset"`
}

// taskNudge files a notice when a weekly quota window is about to reset with
// much of it unused while deferred tasks wait: quota spent before the reset
// is free, quota left over is lost. Run it from cron every few hours.
func (a *App) taskNudge(args []string) error {
	o, err := parse(args, []string{"dry-run"}, nil, 0, 0)
	if err != nil {
		return err
	}
	c, err := a.config()
	if err != nil {
		return err
	}
	if c.Tasks.QuotaCommand == "" {
		return fmt.Errorf("no quota_command in [tasks] of %s; it prints {\"gauges\": {\"<name>\": {\"pct\": N, \"reset\": <unix seconds>}}}",
			config.Path(a.Env.Getenv, a.Env.Home))
	}
	match, err := c.Tasks.Gauges()
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	var waiting []desk.Task
	for _, t := range d.Tasks(desk.Filter{}) {
		if t.State == desk.TaskDeferred {
			waiting = append(waiting, t)
		}
	}
	if len(waiting) == 0 {
		fmt.Fprintln(a.Stdout, "no deferred tasks")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	out, err := a.runExt(ctx, nil, "sh", "-c", c.Tasks.QuotaCommand)
	cancel()
	var q struct {
		Gauges map[string]gauge `json:"gauges"`
	}
	// Readers may print a partial result and fail; take what parses.
	if jerr := json.Unmarshal([]byte(jsonPart(out)), &q); jerr != nil {
		if err != nil {
			return fmt.Errorf("quota_command: %v: %s", err, clipLine(out, 200))
		}
		return fmt.Errorf("quota_command printed no gauges JSON: %v", jerr)
	}
	seen := map[string]bool{}
	for _, e := range d.Events {
		if e.Kind == "tell" && e.Path == NudgePath {
			seen[desk.MetaString(e, "key")] = true
		}
	}
	now := a.Now()
	var evs []store.Event
	for name, g := range q.Gauges {
		if !match.MatchString(name) || g.Reset == nil || g.Pct > float64(c.Tasks.Below()) {
			continue
		}
		reset := time.Unix(int64(*g.Reset), 0)
		left := reset.Sub(now)
		if left <= 0 || left > time.Duration(c.Tasks.Hours())*time.Hour {
			continue
		}
		key := name + "@" + strconv.FormatInt(reset.Unix(), 10)
		if seen[key] {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s is %.0f%% used and resets in %s (%s). Whatever is unused then is lost, and %d deferred task(s) are waiting:\n\n",
			name, g.Pct, desk.Age(left), reset.Local().Format("Mon 15:04"), len(waiting))
		for i, t := range waiting {
			if i == 10 {
				fmt.Fprintf(&b, "- and %d more\n", len(waiting)-10)
				break
			}
			fmt.Fprintf(&b, "- %s %s (%s)\n", t.ID, t.Event.Title, t.Event.Path)
		}
		fmt.Fprintf(&b, "\nLaunch them from the dashboard's Deferred section, or: agent-desk task launch --deferred")
		e := a.event("tell", NudgePath, ident.From{}, "nudge", map[string]any{"key": key})
		e.Level = "notify"
		e.Title = fmt.Sprintf("Quota to spare: %s resets in %s at %.0f%% used", name, desk.Age(left), g.Pct)
		e.Body = b.String()
		evs = append(evs, e)
	}
	if len(evs) == 0 {
		fmt.Fprintln(a.Stdout, "no nudge due")
		return nil
	}
	if o.bools["dry-run"] {
		for _, e := range evs {
			fmt.Fprintf(a.Stdout, "%s\n\n%s\n", e.Title, e.Body)
		}
		return nil
	}
	return a.commit(evs, evs[0].ID)
}

// jsonPart is the JSON object in a command's output, which may carry
// warnings before or after it.
func jsonPart(s string) string {
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j < i {
		return s
	}
	return s[i : j+1]
}

// taskAddWeb files a task the user wrote in the dashboard at node path,
// which must be a project, run or agent on the desk. It returns the task's
// display id.
func (a *App) taskAddWeb(path, title, typ, brief, parent string, tags []string, refs []media.Ref) (string, error) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	title, brief = strings.TrimSpace(title), strings.TrimSpace(brief)
	if title == "" && brief == "" && len(refs) == 0 {
		return "", usagef("a task needs a title, details or a file")
	}
	refs, err := a.uploaded(refs) // pasted, dropped or attached in the form
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", usagef("a task needs a node: a project, run or agent")
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ == "task" {
		typ = ""
	}
	if typ != "" && !desk.ValidTaskType(typ) {
		return "", usagef("the type is one of %s", strings.Join(desk.TaskTypes, ", "))
	}
	d, err := a.load()
	if err != nil {
		return "", err
	}
	known := false
	for _, e := range d.Events {
		if e.Path == path || strings.HasPrefix(e.Path, path+"/") {
			known = true
			break
		}
	}
	if !known || strings.HasPrefix(path, "_desk") {
		return "", fmt.Errorf("%s is not a project, run or agent on the desk", path)
	}
	m := desk.TaskMeta{Type: typ}
	if title == "" {
		// Titled by its brief (or its files) until the agent names it (task title).
		title, m.Untitled = clipLine(brief, untitledLen), true
		if title == "" {
			names := make([]string, len(refs))
			for i, r := range refs {
				names[i] = r.Name
			}
			title = clipLine("Files: "+strings.Join(names, ", "), untitledLen)
		}
	}
	if m.Tags, err = desk.NormTags(tags); err != nil {
		return "", usagef("%v", err)
	}
	if parent = strings.TrimSpace(parent); parent != "" {
		p, err := a.task(d, parent)
		if err != nil {
			return "", err
		}
		m.Parent = p.Event.ID
	}
	var extra map[string]any
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &extra)
	if len(refs) > 0 {
		extra["media"] = refs
	}
	e := a.event("task", path, ident.Resolve(a.Env), "web", extra)
	e.Title, e.Body = title, brief
	if err := a.commit([]store.Event{e}, "-"); err != nil {
		return "", err
	}
	d, err = a.load()
	if err != nil {
		return "", err
	}
	return d.Display(e.ID), nil
}

// apiTasks lists the tasks under ?path= (every state; the page filters),
// with the types there are.
func (s *server) apiTasks(w http.ResponseWriter, r *http.Request) {
	d, _ := s.load(w)
	if d == nil {
		return
	}
	f := desk.Filter{Prefix: strings.Trim(r.URL.Query().Get("path"), "/")}
	if r.URL.Query().Get("unfocused") != "1" {
		f.Focus = d.FocusPrefixes()
	}
	tasks := d.Tasks(f)
	out := make([]taskView, len(tasks))
	for i, t := range tasks {
		out[i] = taskView{Task: t, Media: s.a.mediaViews(d, t.Event)}
	}
	writeJSON(w, map[string]any{"tasks": out, "types": desk.TaskTypes})
}

// taskView is a task as the Tasks page lists it: with its files, which it
// shows as thumbnails and download links.
type taskView struct {
	desk.Task
	Media []MediaView `json:"media,omitempty"`
}
