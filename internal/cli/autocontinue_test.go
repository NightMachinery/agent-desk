package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/keeper"
	"github.com/NightMachinery/agent-desk/internal/settings"
	"github.com/NightMachinery/agent-desk/internal/store"
)

func acPref(n int, scope, key, value string) store.Event {
	m, _ := json.Marshal(map[string]any{"key": key, "class": "*", "value": value})
	return store.Event{ID: fmt.Sprintf("01P%023d", n), Kind: "pref", Path: scope, Meta: m}
}

// The plan follows the settings inherited at each agent's node: main and
// subagents apart, a role's setting over its run's and the global one, a
// subagent reading its parent's node, an orphan off; a decision made for an
// agent stands, and an undecided agent idle for a day is not woken.
func TestAutoContinuePlan(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	recent, old := float64(now.Add(-time.Hour).Unix()), float64(now.Add(-48*time.Hour).Unix())
	rec := func(id, parent string) ident.PaseoAgent {
		r := ident.PaseoAgent{ID: id, Title: "t-" + id}
		if parent != "" {
			r.Labels = map[string]string{"paseo.parent-agent-id": parent}
		}
		return r
	}
	byID := map[string]ident.PaseoAgent{}
	for _, r := range []ident.PaseoAgent{
		rec("main1", ""), rec("main2", ""), rec("sub1", "main1"), rec("sub2", "main2"),
		rec("orphan", "gone"), rec("mine", ""), rec("oldie", ""), rec("old-default", ""), rec("noded", ""),
	} {
		byID[r.ID] = r
	}
	nodes := func(id string) []string {
		switch id {
		case "main1":
			return []string{"proj/run/coord"}
		case "main2":
			return []string{"proj/other/coord"}
		case "noded":
			return []string{"proj/run/worker"}
		}
		return nil
	}
	ac := func(on bool, by string) *keeper.AutoContinue { return &keeper.AutoContinue{Enabled: on, SetBy: by} }
	agents := []keeper.Agent{
		{ID: "main1", LastActivityAt: &recent},
		{ID: "main2", LastActivityAt: &recent},
		{ID: "sub1", LastActivityAt: &recent},
		{ID: "sub2", LastActivityAt: &recent, AutoContinue: ac(true, "default")},
		{ID: "orphan", LastActivityAt: &recent, AutoContinue: ac(true, "default")},
		{ID: "mine", LastActivityAt: &recent, AutoContinue: ac(false, "desk")},
		{ID: "oldie", LastActivityAt: &old},
		{ID: "old-default", LastActivityAt: &old, AutoContinue: ac(true, "default")},
		{ID: "noded", LastActivityAt: &recent},
		{ID: "archived", Archived: true, LastActivityAt: &recent},
		{ID: "elsewhere", LastActivityAt: &recent}, // not this machine's Paseo
	}
	evs := []store.Event{
		acPref(1, "", "auto-continue.subagents", "off"),
		acPref(2, "proj/run", "auto-continue.subagents", "on"),
		acPref(3, "proj", "auto-continue.main", "off"),
		acPref(4, "proj/run/coord", "auto-continue.main", "on"),
	}
	// And globally, main agents off.
	evs = append(evs, acPref(5, "", "auto-continue.main", "off"))
	want := map[string]string{
		// proj/run/coord says main on, over proj's off.
		"main1": "true undecided auto-continue.main@proj/run/coord",
		// sub1 reads main1's node, under proj/run: subagents on there.
		"sub1": "true undecided auto-continue.subagents@proj/run/coord",
		// sub2 reads main2's node, where the global subagents off holds.
		"sub2": "false default auto-continue.subagents@proj/other/coord",
		// Its parent is archived: off, whatever the setting.
		"orphan": "false default auto-continue.subagents@",
		// A default already decided follows a changed setting, however old.
		"old-default": "false default auto-continue.main@",
		// proj says main off; an undecided agent that is off stays as it is.
	}
	// main2 (proj: off, already off), mine (its own choice), oldie (idle for
	// two days), noded (off at proj), archived and elsewhere: no change.
	r := settings.New(evs, settings.Defaults{})
	got := map[string]string{}
	for _, c := range autoContinuePlan(agents, byID, nodes, r, now, map[string]bool{}) {
		got[c.Paseo] = fmt.Sprintf("%v %s %s@%s", c.On, c.Was, c.Why.Key, c.Why.Scope)
	}
	if len(got) != len(want) {
		t.Fatalf("plan: got %v\nwant %v", got, want)
	}
	for id, w := range want {
		if got[id] != w {
			t.Fatalf("%s: got %q, want %q (all: %v)", id, got[id], w, got)
		}
	}

	// With the built-in values (both on), the undecided recent agents turn
	// on, oldie stays asleep, and a refused agent is skipped.
	r = settings.New(nil, settings.Defaults{})
	var on []string
	for _, c := range autoContinuePlan(agents, byID, nodes, r, now, map[string]bool{"noded": true}) {
		on = append(on, fmt.Sprintf("%s=%v", c.Paseo, c.On))
	}
	if s := strings.Join(on, " "); s != "main1=true main2=true orphan=false sub1=true" {
		t.Fatalf("built-in plan: %s", s)
	}
	if d := autoContinueDefault("orphan", byID, nodes, r); d.On || d.From() != "its parent is archived" {
		t.Fatalf("orphan default: %+v", d)
	}
	if d := autoContinueDefault("sub1", byID, nodes, r); !d.On || d.From() != "built in" {
		t.Fatalf("sub1 default: %+v", d)
	}
}
