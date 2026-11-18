package cli

import (
	"errors"
	"fmt"

	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// cmdTakeover takes over, in one write, every node another agent holds on
// this machine: what a replacement does for an agent that stopped (out of
// quota, crashed, archived). The other agent is named by its Paseo id (all
// its sessions count, as in Out of quota) or by its session id.
func (a *App) cmdTakeover(args []string) error {
	o, err := parse(args, nil, nil, 1, 1)
	if err != nil {
		return err
	}
	from := ident.Resolve(a.Env)
	if from.Session == "" {
		return errors.New("cannot tell which agent session this is; only agent sessions take over nodes")
	}
	id := o.pos[0]
	if id == from.Session || (from.Paseo != "" && id == from.Paseo) {
		return usagef("%s is this session; name the agent you replace", id)
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	byID := map[string]ident.PaseoAgent{}
	for _, r := range ident.PaseoRecords(a.paseoHome()) {
		byID[r.ID] = r
	}
	var nodes []string
	if _, ok := byID[id]; ok {
		nodes = a.paseoHeld(d, byID)(id)
	} else {
		nodes = heldPaths(a.held(d, ident.From{Session: id}))
	}
	if len(nodes) == 0 {
		return fmt.Errorf("%s holds no desk node on this machine (give its Paseo id or its session id)", id)
	}
	evs := make([]store.Event, 0, len(nodes))
	for _, n := range nodes {
		evs = append(evs, a.event("join", n, from, "cli", map[string]any{"takeover": true, "replaces": id}))
	}
	if err := a.commit(evs, "-"); err != nil {
		return err
	}
	for _, n := range nodes {
		fmt.Fprintf(a.Stdout, "took over %s\n", n)
	}
	if len(nodes) > 1 {
		fmt.Fprintf(a.Stdout, "This session now holds several nodes: name one with --node PATH on writes (agent-desk whoami lists them).\n")
	}
	return nil
}
