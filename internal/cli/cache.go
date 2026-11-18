package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/keeper"
	"github.com/NightMachinery/agent-desk/internal/store"
)

const (
	cacheStatusTTL      = 15 * time.Second
	cacheRefreshTimeout = 30 * time.Second
)

// cacheFirstWait bounds how long the first read waits for the keeper.
var cacheFirstWait = 2 * time.Second

var cacheAgentID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (a *App) keeperClient() (keeper.Client, error) {
	cfg, err := a.config()
	if err != nil {
		return keeper.Client{}, err
	}
	socket := cfg.Keeper.Socket
	switch {
	case socket == "":
		socket = filepath.Join(a.Env.Home, ".local", "state", "agent-quotas", "agent-quotas.sock")
	case strings.HasPrefix(socket, "~/"):
		socket = filepath.Join(a.Env.Home, socket[2:])
	}
	home, err := filepath.Abs(a.paseoHome())
	return keeper.Client{Socket: socket, Home: home}, err
}

type cacheStatus struct {
	Policy    string   `json:"policy"`
	Phase     string   `json:"phase"`
	WarmUntil *float64 `json:"warm_until,omitempty"`
	WarmHours float64  `json:"warm_hours,omitempty"`
	ExpiresAt *float64 `json:"expires_at,omitempty"`
	Big       bool     `json:"big,omitempty"`
	Archived  bool     `json:"archived,omitempty"`
}

func cacheState(a keeper.Agent) cacheStatus {
	status := cacheStatus{Policy: "off", Phase: "off", Big: a.Big, Archived: a.Archived,
		WarmUntil: a.WarmUntil, ExpiresAt: a.CacheExpiresAt}
	if c := a.Cache; c != nil && c.Policy != "" {
		status.Policy, status.Phase, status.WarmHours = c.Policy, c.Phase, c.WarmHours
	}
	return status
}

type cacheResponse struct {
	Available bool                   `json:"available"`
	Enabled   bool                   `json:"enabled"`
	Message   string                 `json:"message,omitempty"`
	Agents    map[string]cacheStatus `json:"agents,omitempty"`
}

// cacheSnapshot never waits long on the keeper, whose reads can take
// seconds: it returns the last result at once and refreshes it in the
// background once it is older than cacheStatusTTL. Only the first call waits,
// and only briefly.
func (s *server) cacheSnapshot(ctx context.Context) cacheResponse {
	s.cacheMu.Lock()
	have, fresh := !s.cacheAt.IsZero(), time.Since(s.cacheAt) < cacheStatusTTL
	result, done := s.cacheList, s.cacheDone
	if !fresh && done == nil {
		done = make(chan struct{})
		s.cacheDone = done
		go s.refreshCache(done)
	}
	s.cacheMu.Unlock()
	if have || done == nil {
		return result
	}
	select {
	case <-done:
	case <-ctx.Done():
	case <-time.After(cacheFirstWait):
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cacheAt.IsZero() {
		return cacheResponse{Message: "Cache status loading"}
	}
	return s.cacheList
}

func (s *server) refreshCache(done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), cacheRefreshTimeout)
	defer cancel()
	result := cacheResponse{Message: "Cache status unavailable"}
	app, _, _ := s.userApp()
	if client, err := app.keeperClient(); err == nil {
		if doc, err := client.Agents(ctx, true); err == nil {
			result = cacheResponse{Available: true, Enabled: doc.Enabled, Agents: make(map[string]cacheStatus, len(doc.Agents))}
			for _, a := range doc.Agents {
				result.Agents[a.ID] = cacheState(a)
				rememberLockPath(a.ID, a.LockPath)
			}
			if !doc.Enabled {
				result.Message = "The agent-quotas keeper is disabled"
			}
		} else if errors.Is(err, keeper.ErrUnavailable) {
			result.Message = err.Error()
		}
	}
	s.cacheMu.Lock()
	s.cacheList, s.cacheAt, s.cacheDone = result, time.Now(), nil
	s.cacheMu.Unlock()
	close(done)
}

func (s *server) apiCache(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("target") == "" {
		writeJSON(w, s.cacheSnapshot(r.Context()))
		return
	}
	// Resolve again on send, especially for a question whose original
	// session may differ from the coordinator who would get a reply.
	result := struct {
		Cold    bool `json:"cold"`
		Enabled bool `json:"enabled"`
	}{}
	defer func() { writeJSON(w, result) }()
	a, _, _ := s.userApp()
	d, _, err := s.storedDesk()
	if err != nil {
		return
	}
	var id, host string
	switch q.Get("kind") {
	case "answer":
		e, err := d.Resolve(q.Get("target"))
		if err == nil && e.Kind == "ask" {
			id, host = fromOf(e).Paseo, e.Host
		}
	case "reply":
		to, targetHost, _, _, err := replyTarget(d, q.Get("target"))
		if err == nil {
			id, host = to.Paseo, targetHost
		}
	}
	if id == "" || host != a.Host {
		return
	}
	status := s.cacheSnapshot(r.Context())
	agent, ok := status.Agents[strings.ToLower(id)]
	if ok {
		result.Cold = (keeper.Agent{Big: agent.Big, CacheExpiresAt: agent.ExpiresAt}).Cold(a.Now())
		result.Enabled = status.Enabled && !agent.Archived
	}
}

func (s *server) apiCachePolicy(a *App, body request) (any, error) {
	agent, err := a.setCache(body.ID, body.Action, body.WarmHours, "desk", "web")
	if err != nil {
		return nil, err
	}
	// Show the change at once and let the next read refresh the rest.
	s.cacheMu.Lock()
	if s.cacheList.Agents != nil && agent.ID != "" {
		// Copy: earlier snapshots share the map and may still be encoding.
		agents := make(map[string]cacheStatus, len(s.cacheList.Agents)+1)
		for id, status := range s.cacheList.Agents {
			agents[id] = status
		}
		agents[agent.ID] = cacheState(agent)
		s.cacheList.Agents = agents
	}
	if !s.cacheAt.IsZero() {
		s.cacheAt = time.Now().Add(-cacheStatusTTL)
	}
	s.cacheMu.Unlock()
	return cacheState(agent), nil
}

func (a *App) cmdCache(cmd string, args []string) error {
	var action, id string
	var hours float64
	if cmd == "standby" {
		o, err := parse(args, nil, nil, 0, 1)
		if err != nil {
			return err
		}
		action = cmd
		if len(o.pos) == 1 {
			id = o.pos[0]
		}
	} else if len(args) != 0 {
		o, err := parse(args, nil, []string{"warm-hours"}, 1, 2)
		if err != nil {
			return err
		}
		action = o.pos[0]
		if len(o.pos) == 2 {
			id = o.pos[1]
		}
		if v, ok := o.vals["warm-hours"]; ok {
			hours, err = strconv.ParseFloat(v, 64)
			if err != nil || hours <= 0 || math.IsNaN(hours) || math.IsInf(hours, 0) {
				return usagef("--warm-hours must be a positive number")
			}
		}
		if action == "standby" {
			return usagef("use agent-desk standby [<paseo-id>]")
		}
	}
	own := a.Env.Getenv("PASEO_AGENT_ID")
	if id == "" {
		id = own
	}
	if !cacheAgentID.MatchString(id) {
		return usagef("give a Paseo agent id or run within a Paseo agent")
	}
	actor := "cli"
	if own != "" || ident.Resolve(a.Env).Agent != "" {
		if !strings.EqualFold(id, own) {
			return usagef("agents can set cache policy only for their own Paseo agent")
		}
		actor = "agent"
	}
	var agent keeper.Agent
	var err error
	if action == "" {
		client, clientErr := a.keeperClient()
		if clientErr != nil {
			return clientErr
		}
		agent, err = client.Agent(context.Background(), id)
	} else {
		agent, err = a.setCache(id, action, hours, actor, "cli")
	}
	if err != nil {
		return err
	}
	state := cacheState(agent)
	label := state.Phase
	if label == "" {
		label = state.Policy
	}
	fmt.Fprintf(a.Stdout, "%s: %s", agent.ID, label)
	if state.Phase == "warming" && state.WarmUntil != nil {
		fmt.Fprintf(a.Stdout, " until %s", time.Unix(int64(*state.WarmUntil), 0).Local().Format(time.RFC3339))
	}
	if agent.Cold(a.Now()) && !agent.Archived {
		fmt.Fprint(a.Stdout, " (cold)")
	}
	fmt.Fprintln(a.Stdout)
	return nil
}

func (a *App) setCache(id, action string, hours float64, actor, source string) (keeper.Agent, error) {
	if !cacheAgentID.MatchString(id) {
		return keeper.Agent{}, usagef("invalid Paseo agent id")
	}
	switch action {
	case "off", "compact", "warm", "standby":
	default:
		return keeper.Agent{}, usagef("cache policy must be off, compact or warm")
	}
	if hours < 0 || math.IsNaN(hours) || math.IsInf(hours, 0) || (hours != 0 && action != "warm") {
		return keeper.Agent{}, usagef("warm hours can only be set with the warm policy")
	}
	client, err := a.keeperClient()
	if err != nil {
		return keeper.Agent{}, err
	}
	st, err := a.store()
	if err != nil {
		return keeper.Agent{}, err
	}
	endpoint := "policy"
	value := keeper.PolicyRequest{Cache: action, WarmHours: hours, SetBy: actor}
	if action == "standby" {
		endpoint, value.Cache = "standby", ""
	}
	agent, err := client.Policy(context.Background(), id, endpoint, value)
	if err != nil {
		return keeper.Agent{}, err
	}
	state := cacheState(agent)
	from := ident.Resolve(a.Env)
	e := a.event("cache", "cache/"+agent.ID, from, source, map[string]any{
		"paseo": agent.ID, "policy": state.Policy, "phase": state.Phase, "warm_hours": state.WarmHours, "set_by": actor,
	})
	e.Title = "Cache policy set to " + action
	who := "User via the CLI"
	switch actor {
	case "desk":
		who = "User via the dashboard"
	case "agent":
		who = fromTitle(from)
		if who == "" {
			who = "Agent"
		}
	}
	e.Body = fmt.Sprintf("%s set %s to %s.", who, agent.ID, action)
	if action == "warm" && state.WarmHours > 0 {
		e.Body += fmt.Sprintf(" Warming lasts up to %g idle hours.", state.WarmHours)
	}
	if err := st.Append(e); err != nil {
		return agent, fmt.Errorf("cache policy changed but its desk event could not be recorded: %w", err)
	}
	_ = a.renderWarn()
	return agent, nil
}

func localPaseoSend(st *store.Store, host string, msg store.Event) bool {
	if msg.Kind == "answer" {
		q, err := st.Get(msg.Ref)
		return err == nil && q.Host == host && fromOf(q).Paseo != ""
	}
	to, toHost := toOf(msg)
	return toHost == host && to.Paseo != ""
}

func needsCompaction(j job) bool {
	return j.to.Paseo != "" && desk.Meta(j.msg)["after_compaction"] == true
}
