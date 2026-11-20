package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/config"
	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/prompts"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Out of quota: the Paseo agents stopped at a usage limit ([quota]
// stopped_command lists them), with what the user can do about each: let it
// resume itself when its quota returns (auto-continue), ask its parent to
// hand its role to an agent with quota left, or start a new agent that
// takes over from it.

// QuotaAgent is one stopped agent.
type QuotaAgent struct {
	Paseo      string     `json:"paseo"`
	Title      string     `json:"title"`
	Provider   string     `json:"provider"`
	Model      string     `json:"model,omitempty"`    // as started, for Replace's defaults
	Thinking   string     `json:"thinking,omitempty"` // likewise
	Cwd        string     `json:"cwd"`
	LastActive string     `json:"last_active,omitempty"`
	Parent     *AgentRef  `json:"parent,omitempty"`
	Children   []AgentRef `json:"children"`
	// Nodes is the desk nodes its session holds, in the order it joined
	// them (a session may hold several).
	Nodes []string `json:"nodes"`
	// Scopes is the desk nodes it works under: its own, else those of its
	// nearest ancestor that holds any. The dashboard lists it in the view of
	// each of them and of every node above them.
	Scopes       []string `json:"scopes"`
	AutoContinue bool     `json:"auto_continue"`
	// Dismissed: the user hid it, and it has not run since.
	Dismissed bool `json:"dismissed,omitempty"`
	// Seat: the account it runs on and when that has quota again, from
	// the agent-quotas keeper; absent when the keeper has not said.
	Seat *QuotaSeat `json:"seat,omitempty"`
}

// QuotaSeat is a stopped agent's account: Verdict usable, blocked or
// unknown; AvailableAt (Unix seconds) when a blocked one resets.
type QuotaSeat struct {
	Account     string   `json:"account"`
	Verdict     string   `json:"verdict"`
	AvailableAt *float64 `json:"available_at,omitempty"`
}

// AgentRef names another Paseo agent.
type AgentRef struct {
	Paseo   string `json:"paseo"`
	Title   string `json:"title"`
	Stopped bool   `json:"stopped,omitempty"` // also out of quota
}

func (r AgentRef) String() string {
	if r.Title == "" {
		return r.Paseo
	}
	return fmt.Sprintf("%s (%s)", r.Title, r.Paseo)
}

func (a *App) paseoHome() string {
	if h := a.Env.Getenv("PASEO_HOME"); h != "" {
		return h
	}
	return filepath.Join(a.Env.Home, ".paseo")
}

// quotaAgents lists the stopped agents, newest activity first.
func (a *App) quotaAgents(ctx context.Context, d *desk.Desk, c config.Config) ([]QuotaAgent, error) {
	out := []QuotaAgent{}
	if c.Quota.StoppedCommand == "" {
		return out, nil
	}
	text, err := a.runExt(ctx, nil, "sh", "-c", c.Quota.StoppedCommand)
	if err != nil {
		return nil, fmt.Errorf("stopped_command: %v", err)
	}
	stopped := map[string]bool{}
	for _, l := range strings.Fields(text) {
		stopped[l] = true
	}
	auto, _ := a.autoContinueList(ctx, c)
	hidden := a.quotaDismissed()
	recs := ident.PaseoRecords(a.paseoHome())
	byID := map[string]ident.PaseoAgent{}
	for _, r := range recs {
		byID[r.ID] = r
	}
	ref := func(id string) AgentRef { return AgentRef{Paseo: id, Title: byID[id].Title, Stopped: stopped[id]} }
	nodes := a.paseoHeld(d, byID)
	for id := range stopped {
		r, ok := byID[id]
		if !ok {
			continue // archived since, or not this daemon's
		}
		q := QuotaAgent{Paseo: id, Title: r.Title, Provider: r.Provider, Model: r.Config.Model, Thinking: r.Config.Thinking, Cwd: r.Cwd, LastActive: r.LastActive,
			Children: []AgentRef{}, Nodes: nodes(id), Scopes: []string{}, AutoContinue: auto[id]}
		if at, ok := hidden[id]; ok && at == r.LastActive {
			q.Dismissed = true
		}
		for x, seen := id, 0; x != "" && seen < 32; x, seen = byID[x].Parent(), seen+1 {
			if n := nodes(x); len(n) > 0 {
				q.Scopes = n
				break
			}
		}
		if p := r.Parent(); p != "" {
			pr := ref(p)
			q.Parent = &pr
		}
		for _, x := range recs {
			if x.Parent() == id {
				q.Children = append(q.Children, ref(x.ID))
			}
		}
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActive > out[j].LastActive })
	return out, nil
}

// paseoHeld returns the desk nodes each Paseo agent's session holds (see
// held). A Paseo agent's sessions are the native session in its record and
// any session that joined a node as that agent.
func (a *App) paseoHeld(d *desk.Desk, byID map[string]ident.PaseoAgent) func(id string) []string {
	sessions := map[string][]string{}
	add := func(id, s string) {
		if s != "" && !slices.Contains(sessions[id], s) {
			sessions[id] = append(sessions[id], s)
		}
	}
	for id, r := range byID {
		add(id, r.Persistence.NativeHandle)
	}
	for _, e := range d.Events {
		if e.Kind == "join" {
			if f := fromOf(e); f.Paseo != "" {
				add(f.Paseo, f.Session)
			}
		}
	}
	cache := map[string][]string{}
	return func(id string) []string {
		if out, ok := cache[id]; ok {
			return out
		}
		var evs []store.Event
		for _, s := range sessions[id] {
			evs = append(evs, a.held(d, ident.From{Session: s})...)
		}
		sort.Slice(evs, func(i, j int) bool { return evs[i].ID < evs[j].ID })
		out := heldPaths(evs)
		cache[id] = out
		return out
	}
}

// quotaPromptData fills reassign-role.md and replace-agent.md. Node, Join
// and Project are its first node, for prompts written before an agent could
// hold several; Nodes has them all.
type quotaPromptData struct {
	ID, Title, Cwd, Parent, Children, Node, Join, Project string
	Nodes                                                 []promptNode
}

// promptNode is a desk node in a prompt: its path, and the join arguments
// that take it over (agent-desk join <Join> --project <Project>).
type promptNode struct {
	Path, Join, Project string
}

func promptData(q QuotaAgent) quotaPromptData {
	p := quotaPromptData{ID: q.Paseo, Title: q.Title, Cwd: q.Cwd, Nodes: []promptNode{}}
	if p.Title == "" {
		p.Title = "untitled"
	}
	if q.Parent != nil {
		p.Parent = q.Parent.String()
	}
	var kids []string
	for _, c := range q.Children {
		s := c.String()
		if c.Stopped {
			s += ", also out of quota"
		}
		kids = append(kids, s)
	}
	p.Children = strings.Join(kids, "; ")
	for _, n := range q.Nodes {
		project, run, rest := desk.Split(n)
		p.Nodes = append(p.Nodes, promptNode{Path: n, Join: run + "/" + rest, Project: project})
	}
	if len(p.Nodes) > 0 {
		p.Node, p.Join, p.Project = p.Nodes[0].Path, p.Nodes[0].Join, p.Nodes[0].Project
	}
	return p
}

func (a *App) cmdQuota(args []string) error {
	if len(args) == 0 {
		return usagef("quota list | auto-continue on|off <paseo-id> | dismiss|undismiss <paseo-id> | ask-parent <paseo-id> | replace <paseo-id> [--profile P]")
	}
	switch args[0] {
	case "list":
		o, err := parse(args[1:], nil, []string{"format"}, 0, 0)
		if err != nil {
			return err
		}
		qs, err := a.quotaLoad()
		if err != nil {
			return err
		}
		if o.vals["format"] == "json" {
			return json.NewEncoder(a.Stdout).Encode(qs)
		}
		if len(qs) == 0 {
			fmt.Fprintln(a.Stdout, "(no agent is stopped at a usage limit)")
		}
		for _, q := range qs {
			extra := ""
			if q.Parent != nil {
				extra += ", parent " + q.Parent.String()
			}
			if len(q.Children) > 0 {
				extra += fmt.Sprintf(", %d subagents", len(q.Children))
			}
			switch len(q.Nodes) {
			case 0:
			case 1:
				extra += ", node " + q.Nodes[0]
			default:
				extra += ", nodes " + strings.Join(q.Nodes, " ")
			}
			if q.AutoContinue {
				extra += ", auto-continue on"
			}
			if q.Dismissed {
				extra += ", dismissed"
			}
			fmt.Fprintf(a.Stdout, "%s  %s  (%s%s)\n", q.Paseo, q.Title, q.Provider, extra)
		}
		return nil
	case "auto-continue":
		o, err := parse(args[1:], nil, nil, 2, 2)
		if err != nil {
			return err
		}
		if o.pos[0] != "on" && o.pos[0] != "off" {
			return usagef("quota auto-continue on|off <paseo-id>")
		}
		return a.quotaAutoContinue(o.pos[1], o.pos[0] == "on")
	case "dismiss", "undismiss":
		o, err := parse(args[1:], nil, nil, 1, 1)
		if err != nil {
			return err
		}
		return a.quotaDismiss(o.pos[0], args[0] == "dismiss")
	case "ask-parent", "replace":
		o, err := parse(args[1:], []string{"dry-run"}, []string{"profile"}, 1, 1)
		if err != nil {
			return err
		}
		out, err := a.quotaAct(args[0], o.pos[0], o.vals["profile"], o.bools["dry-run"])
		if out != "" {
			fmt.Fprintln(a.Stdout, out)
		}
		return err
	}
	return usagef("unknown quota command %q", args[0])
}

func (a *App) quotaLoad() ([]QuotaAgent, error) {
	c, err := a.config()
	if err != nil {
		return nil, err
	}
	d, err := a.load()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return a.quotaAgents(ctx, d, c)
}

// autoContinueList is the ids auto_continue_command lists: those that
// resume themselves when their quota returns.
func (a *App) autoContinueList(ctx context.Context, c config.Config) (map[string]bool, error) {
	auto := map[string]bool{}
	if c.Quota.AutoContinueCommand == "" {
		return auto, nil
	}
	text, err := a.runExt(ctx, nil, "sh", "-c", c.Quota.AutoContinueCommand, "--", "list")
	if err != nil {
		return auto, fmt.Errorf("auto_continue_command list: %v", err)
	}
	for _, l := range strings.Split(text, "\n") {
		if f := strings.Fields(l); len(f) > 0 {
			auto[f[0]] = true
		}
	}
	return auto, nil
}

// The dismissed list: <desk>/quota-dismissed.json maps an agent's id to
// its last activity when the user hid it. It shows again once it has run
// since (and stopped again).
func (a *App) quotaDismissedPath() string { return filepath.Join(a.Home, "quota-dismissed.json") }

func (a *App) quotaDismissed() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(a.quotaDismissedPath()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// quotaDismiss hides stopped agent id (or, with on false, shows it again).
func (a *App) quotaDismiss(id string, on bool) error {
	if !on {
		return a.quotaSetDismissed([]string{id}, false, nil, nil)
	}
	qs, err := a.quotaLoad()
	if err != nil {
		return err
	}
	stopped := map[string]string{}
	for _, q := range qs {
		stopped[q.Paseo] = q.LastActive
	}
	return a.quotaSetDismissed([]string{id}, true, stopped, nil)
}

// quotaSetDismissed writes the whole selection atomically. The dashboard
// already displayed the stopped agents, so it only needs their own records.
func (a *App) quotaSetDismissed(ids []string, on bool, stopped, displayed map[string]string) error {
	if len(ids) == 0 || slices.Contains(ids, "") {
		return usagef("choose an agent to dismiss or show again")
	}
	unlock, err := a.deskLock("quota-dismissed.lock")
	if err != nil {
		return err
	}
	defer unlock()
	hidden := a.quotaDismissed()
	for _, id := range ids {
		if !on {
			delete(hidden, id)
			continue
		}
		at, found := stopped[id]
		if stopped == nil {
			rec, ok := ident.PaseoRecord(a.paseoHome(), id)
			at, found = rec.LastActive, ok && rec.ID == id && rec.ArchivedAt == nil
		}
		if !found {
			return fmt.Errorf("%s is not an agent stopped at a usage limit (agent-desk quota list)", id)
		}
		if snapshot, ok := displayed[id]; ok {
			at = snapshot
		}
		hidden[id] = at
	}
	b, _ := json.MarshalIndent(hidden, "", " ")
	tmp := a.quotaDismissedPath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.quotaDismissedPath())
}

func (a *App) quotaAutoContinue(id string, on bool) error {
	c, err := a.config()
	if err != nil {
		return err
	}
	if c.Quota.AutoContinueCommand == "" {
		return errors.New("no auto_continue_command in [quota] of " + config.Path(a.Env.Getenv, a.Env.Home))
	}
	verb := "off"
	if on {
		verb = "on"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := a.runExt(ctx, nil, "sh", "-c", c.Quota.AutoContinueCommand, "--", verb, id)
	if err != nil {
		return fmt.Errorf("auto-continue %s: %v: %s", verb, err, clipLine(out, 300))
	}
	fmt.Fprintf(a.Stdout, "Auto-continue %s for %s.\n", verb, id)
	return nil
}

// quotaAct asks the parent of stopped agent id to reassign its role
// (ask-parent), or starts a new agent to take over from it (replace). It
// returns what it did; with dryRun, the prompt it would send.
func (a *App) quotaAct(action, id, profile string, dryRun bool) (string, error) {
	qs, err := a.quotaLoad()
	if err != nil {
		return "", err
	}
	var q *QuotaAgent
	for i := range qs {
		if qs[i].Paseo == id {
			q = &qs[i]
		}
	}
	if q == nil {
		return "", fmt.Errorf("%s is not an agent stopped at a usage limit (agent-desk quota list)", id)
	}
	data := promptData(*q)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	switch action {
	case "ask-parent":
		if q.Parent == nil {
			return "", fmt.Errorf("%s has no parent agent: replace it instead", id)
		}
		text, err := prompts.Render(a.promptsDir(), "reassign-role.md", data)
		if err != nil || dryRun {
			return text, err
		}
		if q.Parent.Stopped {
			return "", fmt.Errorf("its parent %s is out of quota too", q.Parent)
		}
		out, err := a.runExt(ctx, nil, "paseo", "send", "--no-wait", "--json", q.Parent.Paseo, text)
		if err != nil {
			return "", fmt.Errorf("paseo send to %s: %v: %s", q.Parent.Paseo, err, clipLine(out, 300))
		}
		return "Asked " + q.Parent.String() + " to hand its role to an agent with quota.", nil
	case "replace":
		text, err := prompts.Render(a.promptsDir(), "replace-agent.md", data)
		if err != nil || dryRun {
			return text, err
		}
		c, err := a.config()
		if err != nil {
			return "", err
		}
		if c.Tasks.LaunchCommand == "" {
			return "", errors.New("no launch_command in [tasks]: it starts the replacement (see docs/usage.md)")
		}
		p := LaunchPayload{ID: "replace-" + short8(id), Display: "replace", Title: q.Title, Cwd: q.Cwd, Profile: profile, Prompt: text}
		in, _ := json.Marshal(p)
		out, err := a.runExt(ctx, in, "sh", "-c", c.Tasks.LaunchCommand)
		if err != nil {
			return "", fmt.Errorf("launch_command: %v: %s", err, clipLine(out, 300))
		}
		return "Started " + lastLine(out) + " to take over from " + id + ".", nil
	}
	return "", usagef("quota action is ask-parent or replace")
}
