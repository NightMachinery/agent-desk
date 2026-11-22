package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/keeper"
	"github.com/NightMachinery/agent-desk/internal/settings"
)

// Auto-continue defaults. The settings auto-continue.main and
// auto-continue.subagents say whether an agent resumes itself when its quota
// returns. Like any setting they are set globally or at a project, run or
// role and inherited below it; an agent reads them at its desk node (its
// own, else its nearest Paseo ancestor's), and a subagent whose parent is
// archived gets off. The server writes the result into each agent's
// agent-quotas registration as a default: agent-quotas keeps a decision
// made for the agent itself (its checkbox, /auto-continue) over any
// default, so only agents nobody decided for follow the setting.

// autoContinueRecent bounds which undecided agents a default turns on: an
// agent idle longer is left alone, so old stopped agents do not wake up.
// Turning a default off, or following a changed setting for an agent the
// default already decided, has no such bound.
const autoContinueRecent = 24 * time.Hour

// autoContinueEvery is how often the server applies the defaults.
const autoContinueEvery = 3 * time.Minute

// acDefault is the default an agent inherits, and from where.
type acDefault struct {
	Key    string          `json:"key"`   // auto-continue.main or auto-continue.subagents
	Scope  string          `json:"scope"` // the node it was read at; "" for an agent with none
	On     bool            `json:"on"`
	Origin settings.Origin `json:"origin"`
	// Orphan: a subagent whose parent is archived, which gets off.
	Orphan bool `json:"orphan,omitempty"`
}

// From says where the default comes from, for a tooltip.
func (d acDefault) From() string {
	if d.Orphan {
		return "its parent is archived"
	}
	return d.Origin.String()
}

// autoContinueDefault resolves the default for agent id.
func autoContinueDefault(id string, byID map[string]ident.PaseoAgent, nodes func(string) []string, r *settings.Resolver) acDefault {
	d := acDefault{Key: "auto-continue.main"}
	parent := byID[id].Parent()
	if parent != "" {
		d.Key = "auto-continue.subagents"
	}
	for x, seen := id, 0; x != "" && seen < 32; x, seen = byID[x].Parent(), seen+1 {
		if n := nodes(x); len(n) > 0 {
			d.Scope = n[0]
			break
		}
	}
	v, o := r.Resolve(d.Key, d.Scope, settings.Any)
	d.On, d.Origin = v == "on", o
	if _, ok := byID[parent]; parent != "" && !ok {
		d.On, d.Orphan = false, true
	}
	return d
}

// acChange is one default to write.
type acChange struct {
	Paseo string    `json:"paseo"`
	Title string    `json:"title"`
	On    bool      `json:"on"`
	Was   string    `json:"was"` // "undecided" or "default"
	Why   acDefault `json:"why"`
}

// autoContinuePlan lists the defaults to write: for each live agent nobody
// decided for, its inherited default where that differs from its
// registration. refused holds agents agent-quotas declined before (an old
// registration with no recorded source counts as decided), skipped.
func autoContinuePlan(agents []keeper.Agent, byID map[string]ident.PaseoAgent, nodes func(string) []string,
	r *settings.Resolver, now time.Time, refused map[string]bool) []acChange {
	var out []acChange
	for _, ag := range agents {
		rec, ok := byID[ag.ID]
		if ag.Archived || !ok || refused[ag.ID] {
			continue // archived, or not this machine's Paseo
		}
		cur, src := false, ""
		if ac := ag.AutoContinue; ac != nil {
			cur, src = ac.Enabled, ac.SetBy
		}
		if src != "" && src != "default" {
			continue // decided for this agent: it stands
		}
		d := autoContinueDefault(ag.ID, byID, nodes, r)
		if d.On == cur {
			continue
		}
		was := "default"
		if src == "" {
			was = "undecided"
			recent := ag.LastActivityAt != nil && now.Sub(time.Unix(int64(*ag.LastActivityAt), 0)) <= autoContinueRecent
			if d.On && !recent {
				continue
			}
		}
		out = append(out, acChange{Paseo: ag.ID, Title: rec.Title, On: d.On, Was: was, Why: d})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Paseo < out[j].Paseo })
	return out
}

// applyAutoContinueDefaults plans and, unless dryRun, writes the defaults.
// It does nothing where agent-quotas is not running.
func (a *App) applyAutoContinueDefaults(ctx context.Context, d *desk.Desk, refused map[string]bool, dryRun bool) ([]acChange, error) {
	client, err := a.keeperClient()
	if err != nil {
		return nil, err
	}
	list, err := client.Agents(ctx, true)
	if err != nil {
		return nil, err
	}
	r, err := a.settings(d)
	if err != nil {
		return nil, err
	}
	byID := map[string]ident.PaseoAgent{}
	for _, rec := range ident.PaseoRecords(a.paseoHome()) {
		byID[rec.ID] = rec
	}
	plan := autoContinuePlan(list.Agents, byID, a.paseoHeld(d, byID), r, a.Now(), refused)
	if dryRun {
		return plan, nil
	}
	var done []acChange
	var errs []error
	for _, c := range plan {
		ag, err := client.SetAutoContinue(ctx, c.Paseo, c.On, "default", false)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", c.Paseo, err))
			continue
		}
		if ag.AutoContinue == nil || ag.AutoContinue.Enabled != c.On {
			if refused != nil {
				refused[c.Paseo] = true // decided after all: leave it
			}
			continue
		}
		done = append(done, c)
	}
	return done, errors.Join(errs...)
}

// autoContinueLoop applies the defaults every autoContinueEvery, and soon
// after an auto-continue setting changes (s.acNudge).
func (s *server) autoContinueLoop(ctx context.Context) {
	refused := map[string]bool{}
	wait := time.NewTimer(30 * time.Second)
	defer wait.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-wait.C:
		case <-s.acNudge:
			if !wait.Stop() {
				select {
				case <-wait.C:
				default:
				}
			}
		}
		s.autoContinueOnce(ctx, refused)
		wait.Reset(autoContinueEvery)
	}
}

func (s *server) autoContinueOnce(ctx context.Context, refused map[string]bool) {
	app, _, _ := s.userApp()
	d, err := app.load()
	if err != nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	done, err := app.applyAutoContinueDefaults(cctx, d, refused, false)
	for _, c := range done {
		state := "off"
		if c.On {
			state = "on"
		}
		fmt.Fprintf(s.logger, "agent-desk: auto-continue %s by default for %s (%s, %s)\n", state, c.Paseo, c.Why.Key, c.Why.From())
	}
	if err != nil && !errors.Is(err, keeper.ErrUnavailable) {
		fmt.Fprintf(s.logger, "agent-desk: auto-continue defaults: %v\n", err)
	}
}

// nudgeAutoContinue asks the loop to apply the defaults now.
func (s *server) nudgeAutoContinue() {
	select {
	case s.acNudge <- struct{}{}:
	default:
	}
}

// acInfo is what the dashboard shows beside an agent's Auto-continue box:
// who decided (SetBy, see keeper.AutoContinue) and the default it inherits.
type acInfo struct {
	SetBy   string     `json:"set_by"`
	Default *acDefault `json:"default,omitempty"`
}

// autoContinueInfo reads agent id's registration and default; nil where
// agent-quotas is not running or does not know the agent.
func (a *App) autoContinueInfo(ctx context.Context, d *desk.Desk, id string) (*acInfo, error) {
	client, err := a.keeperClient()
	if err != nil {
		return nil, err
	}
	ag, err := client.Agent(ctx, id)
	if err != nil {
		return nil, err
	}
	r, err := a.settings(d)
	if err != nil {
		return nil, err
	}
	byID := map[string]ident.PaseoAgent{}
	for _, rec := range ident.PaseoRecords(a.paseoHome()) {
		byID[rec.ID] = rec
	}
	if _, ok := byID[id]; !ok {
		return nil, nil
	}
	info := &acInfo{}
	if ag.AutoContinue != nil {
		info.SetBy = ag.AutoContinue.SetBy
	}
	def := autoContinueDefault(id, byID, a.paseoHeld(d, byID), r)
	info.Default = &def
	return info, nil
}

// followAutoContinueDefault drops the decision made for agent id: it
// follows its inherited default again.
func (a *App) followAutoContinueDefault(ctx context.Context, d *desk.Desk, id string) (acDefault, error) {
	info, err := a.autoContinueInfo(ctx, d, id)
	if errors.Is(err, keeper.ErrUnavailable) {
		return acDefault{}, errors.New("agent-quotas is not running: following the default needs it")
	}
	if err != nil {
		return acDefault{}, err
	}
	if info == nil {
		return acDefault{}, fmt.Errorf("no Paseo agent %s on this machine", id)
	}
	client, err := a.keeperClient()
	if err != nil {
		return acDefault{}, err
	}
	_, err = client.SetAutoContinue(ctx, id, info.Default.On, "default", true)
	return *info.Default, err
}

// cmdQuotaDefaults is `agent-desk quota defaults [--dry-run] [--quiet]
// [--format json]`. --quiet prints only the changes, for a cron job on a
// machine that runs no desk server.
func (a *App) cmdQuotaDefaults(args []string) error {
	o, err := parse(args, []string{"dry-run", "quiet"}, []string{"format"}, 0, 0)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	changes, err := a.applyAutoContinueDefaults(ctx, d, nil, o.bools["dry-run"])
	if errors.Is(err, keeper.ErrUnavailable) {
		return errors.New("agent-quotas is not running: auto-continue defaults need it")
	}
	if o.vals["format"] == "json" {
		if changes == nil {
			changes = []acChange{}
		}
		if jerr := json.NewEncoder(a.Stdout).Encode(changes); jerr != nil {
			return jerr
		}
		return err
	}
	verb := "Set"
	if o.bools["quiet"] {
		verb = a.Now().Format("2006-01-02 15:04") + " set"
	}
	if o.bools["dry-run"] {
		verb = "Would set"
	}
	if len(changes) == 0 && !o.bools["quiet"] {
		fmt.Fprintln(a.Stdout, "(every agent already follows its auto-continue default)")
	}
	for _, c := range changes {
		state := "off"
		if c.On {
			state = "on"
		}
		where := c.Why.Scope
		if where == "" {
			where = "no node"
		}
		fmt.Fprintf(a.Stdout, "%s auto-continue %s for %s  %s  (%s, was %s; %s at %s)\n",
			verb, state, c.Paseo, c.Title, c.Why.Key, c.Was, c.Why.From(), where)
	}
	return err
}
