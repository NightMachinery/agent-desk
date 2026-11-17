package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/config"
	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
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
		return usagef("task add|start|done|drop|defer|reopen|launch|prompt|list|nudge; see agent-desk help")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return a.taskAdd(rest)
	case "start", "done", "drop", "defer", "reopen":
		return a.taskMark(sub, rest)
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
	o, err := parse(args, []string{"defer"}, []string{"title", "brief", "size", "profile", "priority", "cwd", "node", "project"}, 0, 0)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(o.vals["title"])
	if title == "" {
		return usagef("task add needs --title")
	}
	m := desk.TaskMeta{Deferred: o.bools["defer"], Profile: o.vals["profile"], Cwd: o.vals["cwd"]}
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
	// (--defer): an agent's shell may hold stdin open, and reading it
	// unasked would hang.
	brief := o.vals["brief"]
	if brief == "-" || (m.Deferred && !o.set["brief"]) {
		if brief, err = a.stdinText(); err != nil {
			return err
		}
	}
	brief = strings.TrimSpace(brief)
	if m.Deferred && brief == "" {
		return usagef("a deferred task needs a brief on stdin: the agent that picks it up knows only what it says")
	}
	build := func(from ident.From, node string) store.Event {
		if m.Cwd == "" && m.Deferred {
			m.Cwd = from.Cwd
		}
		var extra map[string]any
		b, _ := json.Marshal(m)
		_ = json.Unmarshal(b, &extra)
		e := a.event("task", node, from, "cli", extra)
		e.Title, e.Body = title, brief
		return e
	}
	if node := strings.Trim(o.vals["node"], "/"); node != "" {
		// Filed by the user, or for another node.
		if strings.Count(node, "/") < 2 {
			return usagef("--node is a full node path: project/run/role")
		}
		from := ident.Resolve(a.Env)
		return a.commit1(build(from, node))
	}
	return a.agentWrite(o.vals["project"], func(from ident.From, node string, _ *desk.Desk) ([]store.Event, error) {
		return []store.Event{build(from, node)}, nil
	})
}

var markState = map[string]string{
	"start": desk.TaskStarted, "done": desk.TaskDone, "drop": desk.TaskDropped,
	"defer": desk.TaskDeferred, "reopen": desk.TaskOpen,
}

func (a *App) taskMark(sub string, args []string) error {
	o, err := parse(args, nil, []string{"note"}, 1, -1)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	from := ident.Resolve(a.Env)
	var evs []store.Event
	for _, tok := range o.pos {
		t, err := a.task(d, tok)
		if err != nil {
			return err
		}
		extra := map[string]any{"state": markState[sub]}
		if sub == "start" && from.Session != "" {
			extra["agent"] = agentLabel(from)
		}
		e := a.event("taskstate", t.Event.Path, from, "cli", extra)
		e.Ref, e.Body = t.Event.ID, strings.TrimSpace(o.vals["note"])
		evs = append(evs, e)
	}
	return a.commit(evs, "")
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
			return fmt.Errorf("no deferred tasks under %q", prefix)
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
	o, err := parse(args, []string{"all", "deferred"}, []string{"format", "prompt"}, 0, 1)
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
	var ts []desk.Task
	for _, t := range d.Tasks(f) {
		if o.bools["deferred"] && t.State != desk.TaskDeferred {
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
		x := ""
		if len(extra) > 0 {
			x = "  [" + strings.Join(extra, " ") + "]"
		}
		fmt.Fprintf(a.Stdout, "%s  %-8s %s%s  %s\n", t.ID, t.State, t.Event.Title, x, t.Event.Path)
	}
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
