package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/ids"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Instructions are the user's own messages to a coordinator, copied word for
// word by a prompt hook (agent-desk hook prompt), so a run keeps them through
// compaction without a hand-kept file.

// MaxInstruction caps one recorded message; a longer paste is cut.
const MaxInstruction = 256 << 10

// automated are prompts that reach a session without the user typing them:
// messages from other agents (Paseo's envelope, and the hand-back of a
// Claude Code subagent), Paseo's schedules and heartbeats, the
// auto-continue ping, bare slash commands such as /compact, harness
// notices, and the answers and replies agent-desk itself delivers (already
// on the desk as answer and reply events).
var automated = []*regexp.Regexp{
	regexp.MustCompile(`^<paseo-system\b`),
	regexp.MustCompile(`^<agent-message\b`),
	regexp.MustCompile(`^Schedule ".*" fired \(id=`),
	regexp.MustCompile(`^(?i:continue)\.?$`),
	regexp.MustCompile(`^/compact\b`),
	// agent-quotas' keeper: cache keepalive pings carry this marker.
	regexp.MustCompile(`^\[agent-quotas keepalive\]`),
	regexp.MustCompile(`^/[\w:.-]+$`),
	regexp.MustCompile(`^<(task-notification|system-reminder|command-name|local-command-stdout)>`),
	regexp.MustCompile(`^\[Request interrupted`),
	regexp.MustCompile(`^Answer to [a-z]-[0-9a-z]+ "`),
	// ReplyText; older versions typed no id.
	regexp.MustCompile(`^Message (?:[a-z]-[0-9a-z]+ )?from the user via agent-desk\b`),
}

// isAutomated reports whether a prompt is one the user did not type.
func isAutomated(prompt string, extra []*regexp.Regexp) bool {
	p := strings.TrimSpace(prompt)
	if p == "" {
		return true
	}
	for _, re := range append(automated[:len(automated):len(automated)], extra...) {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

// hookInput is what Claude Code and Codex hand a UserPromptSubmit hook.
type hookInput struct {
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
	Cwd       string `json:"cwd"`
}

// cmdHook is the entry point for agent hooks. It never fails the hook and
// prints nothing: a UserPromptSubmit hook's output is added to the prompt.
// Errors go to hook-errors.log in the store directory.
func (a *App) cmdHook(args []string) error {
	o, err := parse(args, nil, []string{"agent"}, 1, 1)
	if err != nil {
		return err
	}
	if o.pos[0] != "prompt" {
		return usagef("unknown hook %q; the one hook is: agent-desk hook prompt --agent claude|codex", o.pos[0])
	}
	if err := a.hookPrompt(o.vals["agent"]); err != nil {
		a.hookLog(err)
	}
	return nil
}

func (a *App) hookLog(err error) {
	line := fmt.Sprintf("%s hook prompt: %v\n", time.Now().UTC().Format(time.RFC3339), err)
	if f, e := os.OpenFile(filepath.Join(a.Home, "hook-errors.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); e == nil {
		f.WriteString(line)
		f.Close()
	}
}

func (a *App) hookPrompt(agent string) error {
	b, err := io.ReadAll(io.LimitReader(a.Stdin, 16<<20))
	if err != nil {
		return err
	}
	var in hookInput
	if err := json.Unmarshal(b, &in); err != nil {
		return fmt.Errorf("hook input is not JSON: %w", err)
	}
	c, err := a.config()
	if err != nil {
		return err
	}
	if c.Instructions.Off() {
		return nil
	}
	extra, err := c.Instructions.SkipPatterns()
	if err != nil {
		return err
	}
	if isAutomated(in.Prompt, extra) {
		return nil
	}
	env := a.hookEnv(agent, in)
	from := ident.Resolve(env)
	if from.Session == "" {
		return nil
	}
	// A tmux worker's prompts are typed by its parent, not the user. A Paseo
	// worker's come wrapped as agent messages, skipped above.
	if strings.HasPrefix(from.Parent, "tmux:") {
		return nil
	}
	st, err := a.store()
	if err != nil {
		return err
	}
	events, err := st.Membership()
	if err != nil {
		return err
	}
	held := a.heldFrom(events, from)
	if len(held) == 0 {
		return nil // only sessions on the desk keep their instructions
	}
	text := strings.TrimSpace(in.Prompt)
	extraMeta := map[string]any{}
	if len(text) > MaxInstruction {
		text = strings.ToValidUTF8(text[:MaxInstruction], "") + "\n[cut: longer than 256 KiB]"
		extraMeta["cut"] = true
	}
	// The prompt may concern any node the session holds, so each keeps a
	// copy; the copies share one id under meta.prompt, and listings show it
	// once.
	if len(held) > 1 {
		extraMeta["prompt"] = ids.New(a.Now())
	}
	var evs []store.Event
	for _, j := range held {
		e := a.event("instruction", j.Path, from, "hook", extraMeta)
		e.Body = text
		e.Title = clipLine(text, 80)
		evs = append(evs, e)
	}
	// No render and no announcement: instructions appear in neither.
	return st.Append(evs...)
}

// hookEnv is the environment as the hook sees it, with the session named by
// the hook's input: hooks need not inherit the session variables.
func (a *App) hookEnv(agent string, in hookInput) ident.Env {
	env := a.Env
	base := env.Getenv
	set := map[string]string{}
	switch strings.ToLower(agent) {
	case "claude":
		set["AI_AGENT"], set["CLAUDE_CODE_SESSION_ID"] = "claude", in.SessionID
	case "codex":
		set["AI_AGENT"], set["CODEX_THREAD_ID"] = "codex", in.SessionID
	}
	if in.SessionID == "" {
		set = nil
	}
	env.Getenv = func(k string) string {
		if v, ok := set[k]; ok {
			return v
		}
		return base(k)
	}
	if in.Cwd != "" {
		env.Cwd = in.Cwd
	}
	return env
}

// cmdInstructions prints the user's recorded messages, oldest first: by
// default those to this session's run.
func (a *App) cmdInstructions(args []string) error {
	o, err := parse(args, nil, []string{"since", "format", "host", "node"}, 0, 1)
	if err != nil {
		return err
	}
	if len(o.pos) == 1 && o.set["node"] {
		return usagef("give a path prefix or --node, not both")
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	prefix := ""
	if len(o.pos) == 1 {
		prefix = strings.Trim(o.pos[0], "/")
	} else {
		node, err := a.myNode(d, ident.Resolve(a.Env), o.vals["node"], "instructions")
		if err != nil {
			return err
		}
		if node == "" {
			return usagef("give a path prefix (project/run): this session has not joined a node")
		}
		p, r, _ := desk.Split(node)
		prefix = p + "/" + r
	}
	var since time.Time
	if s := o.vals["since"]; s != "" {
		if since, err = parseSince(s, d.Now); err != nil {
			return err
		}
	}
	out := a.instructionList(d, instructionFilter{prefix: prefix, host: o.vals["host"], since: since})
	switch o.vals["format"] {
	case "", "text", "md":
	case "json":
		enc := json.NewEncoder(a.Stdout)
		for _, e := range out {
			if err := enc.Encode(map[string]any{"id": d.Display(e.ID), "ts": e.TS, "path": e.Path, "host": e.Host, "text": e.Body}); err != nil {
				return err
			}
		}
		return nil
	default:
		return usagef("--format is text or json")
	}
	if len(out) == 0 {
		fmt.Fprintf(a.Stdout, "(no instructions recorded under %s)\n", prefix)
		return nil
	}
	for i, e := range out {
		if i > 0 {
			fmt.Fprintln(a.Stdout)
		}
		fmt.Fprintf(a.Stdout, "## %s  %s  %s\n\n%s\n", d.Display(e.ID), desk.Time(e).Local().Format("2006-01-02 15:04"), e.Path, e.Body)
	}
	return nil
}

// parseSince takes an RFC 3339 time, a date, or a duration back from now (2h, 3d).
func parseSince(s string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	if strings.HasSuffix(s, "d") {
		if dur, err := time.ParseDuration(strings.TrimSuffix(s, "d") + "h"); err == nil {
			return now.Add(-24 * dur), nil
		}
	}
	if dur, err := time.ParseDuration(s); err == nil {
		return now.Add(-dur), nil
	}
	return time.Time{}, errors.New("--since takes a time (RFC 3339), a date (2006-01-02) or a duration (2h, 3d)")
}

// cmdGet prints a slot's current text: this session's by default.
func (a *App) cmdGet(args []string) error {
	o, err := parse(args, nil, []string{"node"}, 1, 1)
	if err != nil {
		return err
	}
	name := o.pos[0]
	if err := slotArg(name); err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	// Any node by its full path; this session's own by default, or one it
	// holds named relative to its project.
	node := strings.Trim(o.vals["node"], "/")
	mine, err := a.myNode(d, ident.Resolve(a.Env), node, "get")
	switch {
	case err == nil && mine != "":
		node = mine
	case node != "" && strings.Count(node, "/") >= 2:
	case err != nil:
		return err
	default:
		return usagef("give --node PATH: this session has not joined a node")
	}
	for _, s := range d.Slots() {
		if s.Path != node+"/"+name {
			continue
		}
		switch s.Kind {
		case "set":
			fmt.Fprintln(a.Stdout, s.Body)
			return nil
		case "link":
			f := desk.MetaString(s, "file")
			b, err := os.ReadFile(f)
			if err != nil {
				return fmt.Errorf("slot %s links %s: %w", name, f, err)
			}
			_, err = a.Stdout.Write(b)
			return err
		}
	}
	return fmt.Errorf("no slot %s at %s", name, node)
}

// instructionFilter selects recorded instructions: under a path prefix, from
// one host, since a time, and holding every word of a query.
type instructionFilter struct {
	prefix, host, query string
	since               time.Time
}

// instructionList is the user's recorded instructions matching f, oldest
// first: one per prompt (a session holding several nodes records it under
// each), without prompts recorded before a pattern caught them as automated
// (events are immutable, so they are dropped here).
func (a *App) instructionList(d *desk.Desk, f instructionFilter) []store.Event {
	var extra []*regexp.Regexp
	if c, err := a.config(); err == nil {
		extra, _ = c.Instructions.SkipPatterns()
	}
	words := strings.Fields(strings.ToLower(f.query))
	var out []store.Event
	copies := map[string]bool{}
	for _, e := range d.Events {
		if e.Kind != "instruction" || !desk.Under(e.Path, f.prefix) || isAutomated(e.Body, extra) {
			continue
		}
		if p := desk.MetaString(e, "prompt"); p != "" {
			if copies[p] {
				continue // the same prompt, kept for another node the session held
			}
			copies[p] = true
		}
		if f.host != "" && e.Host != f.host {
			continue
		}
		if !f.since.IsZero() && desk.Time(e).Before(f.since) {
			continue
		}
		if len(words) > 0 {
			low := strings.ToLower(e.Body + "\n" + e.Path)
			all := true
			for _, w := range words {
				all = all && strings.Contains(low, w)
			}
			if !all {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}
