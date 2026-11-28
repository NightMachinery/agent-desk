package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Stopped by an error: a Paseo agent whose last turn failed keeps the error
// in its record (lastError) until its next turn starts, so the desk lists
// it (quotaAgents, the dashboard's Stopped by an error) and, once per failed
// turn, files a notice about it, which notifies like any item (the class
// "stopped": pushed and sent away by default, as a blocking question is).
// The notice is the agent's: it goes to the node its session holds (else
// to its nearest Paseo ancestor's), from that node's holder, so Reply on it
// reaches the agent (or the parent that runs it). Once the agent runs again,
// the desk marks the notice read, which takes the notification back.
//
// A usage-limit error is left to Out of quota and its auto-continue.

// LoginError matches the errors of an account that is logged out, or whose
// login expired or was refused: only the user can fix those.
var LoginError = regexp.MustCompile(`(?i)not logged in|please run /login|\blog ?in again|sign in again|(?:codex|claude) login\b|` +
	`authentication[_ ](?:error|failed)|\b401\b|unauthori[sz]ed|(?:invalid|expired|revoked) (?:api[ _-]?key|token|credentials|oauth)|` +
	`oauth token (?:has )?expired|refresh token`)

// usageLimit matches a usage-limit error (agent-quotas' QuotaError).
var usageLimit = regexp.MustCompile(`(?i)usage[_ -]limit|rate[_ -]limit|quota (?:exceeded|exhausted)|` +
	`(?:you(?:'ve| have)?|account) (?:hit|reached|exceeded) .*limit|` +
	`(?:hit|reached) your limit|out of .*usage|too many requests|quota reached`)

// stoppedNoticeEvery is how often the server looks for stopped agents;
// stoppedNoticeMaxAge, how long ago an agent may have stopped and still get
// a notice (an old one, found when the desk first looks, is only listed).
const (
	stoppedNoticeEvery  = time.Minute
	stoppedNoticeMaxAge = 24 * time.Hour
)

// stoppedMeta is a stopped notice's meta "stopped": the agent, and the
// failed turn (its prompt's time and the error) it is about.
type stoppedMeta struct {
	Paseo string `json:"paseo"`
	Turn  string `json:"turn"`
	Error string `json:"error"`
	Login bool   `json:"login,omitempty"`
}

func stoppedOf(e store.Event) (stoppedMeta, bool) {
	var m struct {
		Stopped *stoppedMeta `json:"stopped"`
	}
	if !desk.StoppedNotice(e) || json.Unmarshal(e.Meta, &m) != nil || m.Stopped == nil {
		return stoppedMeta{}, false
	}
	return *m.Stopped, true
}

// failedTurn is the failed turn a record shows, or ok false when its last
// turn did not fail (or failed at a usage limit).
func failedTurn(r ident.PaseoAgent) (stoppedMeta, bool) {
	text := strings.TrimSpace(r.LastError)
	if text == "" || usageLimit.MatchString(text) {
		return stoppedMeta{}, false
	}
	return stoppedMeta{Paseo: r.ID, Turn: r.LastUserMessageAt, Error: text, Login: LoginError.MatchString(text)}, true
}

// stoppedNotices files a notice for each agent newly stopped by an error,
// and marks read the notices of agents that have run since. It returns the
// display ids filed.
func (a *App) stoppedNotices() ([]string, error) {
	d, err := a.load()
	if err != nil {
		return nil, err
	}
	recs := ident.PaseoRecords(a.paseoHome())
	byID := map[string]ident.PaseoAgent{}
	for _, r := range recs {
		byID[r.ID] = r
	}
	// The latest notice about each agent.
	last := map[string]store.Event{}
	for _, e := range d.Events {
		if m, ok := stoppedOf(e); ok {
			last[m.Paseo] = e
		}
	}
	var evs []store.Event
	for id, e := range last {
		m, _ := stoppedOf(e)
		now, failed := failedTurn(byID[id])
		if d.Unread(e.ID) && (!failed || now.Turn != m.Turn || now.Error != m.Error) {
			evs = append(evs, a.readMark(e, ident.From{}, "stopped"))
		}
	}
	nodes := a.paseoHeld(d, byID)
	var filed []string
	for _, r := range recs {
		m, failed := failedTurn(r)
		if !failed {
			continue
		}
		if e, ok := last[r.ID]; ok {
			if was, _ := stoppedOf(e); was.Turn == m.Turn && was.Error == m.Error {
				continue // told already
			}
		}
		if at, err := time.Parse(time.RFC3339Nano, r.LastActive); err == nil && a.Now().Sub(at) > stoppedNoticeMaxAge {
			continue
		}
		path := ""
		for x, seen := r.ID, 0; x != "" && seen < 32 && path == ""; x, seen = byID[x].Parent(), seen+1 {
			if n := nodes(x); len(n) > 0 {
				path = n[0]
			}
		}
		if path == "" {
			continue // on no desk node: the dashboard still lists it
		}
		evs = append(evs, a.stoppedNotice(d, r, m, path))
		filed = append(filed, evs[len(evs)-1].ID)
	}
	if len(evs) == 0 {
		return nil, nil
	}
	if err := a.commit(evs, "-"); err != nil {
		return nil, err
	}
	return filed, nil
}

// stoppedNotice is the notice that agent r stopped with failed turn m, at
// path, from the session holding path.
func (a *App) stoppedNotice(d *desk.Desk, r ident.PaseoAgent, m stoppedMeta, path string) store.Event {
	var from ident.From
	if j, ok := nodeOwner(d, path); ok {
		from = fromOf(j)
	}
	name := r.Title
	if name == "" {
		name = short8(r.ID)
	}
	title := name + " stopped: " + clipLine(m.Error, 100)
	fix := "Resume it, replace it or archive it under Stopped by an error on the dashboard, or reply here."
	if m.Login {
		title = name + " is logged out: " + clipLine(m.Error, 100)
		fix = "Log its account in again (in Claude Code, /login in a session on that account; in Codex, codex login), then press Resume under Stopped by an error on the dashboard, or reply here."
	}
	e := a.event("tell", path, from, "stopped", map[string]any{"stopped": m})
	e.Title = title
	e.Body = fmt.Sprintf("The Paseo agent %s (%s) stopped with an error:\n\n%s\n\n%s", name, strings.Join(nonEmpty(r.ID, r.Provider), ", "), m.Error, fix)
	return e
}

// stoppedNoticeLoop runs stoppedNotices every stoppedNoticeEvery.
func (s *server) stoppedNoticeLoop(ctx context.Context) {
	t := time.NewTicker(stoppedNoticeEvery)
	defer t.Stop()
	for {
		app, _, _ := s.userApp()
		if filed, err := app.stoppedNotices(); err != nil {
			fmt.Fprintf(s.logger, "agent-desk: stopped agents: %v\n", err)
		} else if len(filed) > 0 {
			fmt.Fprintf(s.logger, "agent-desk: filed %d stopped-agent notice(s)\n", len(filed))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
