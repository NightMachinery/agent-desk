package settings

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/store"
)

func pref(n int, scope, key, class string, value any) store.Event {
	m, _ := json.Marshal(map[string]any{"key": key, "class": class, "value": value})
	return store.Event{ID: fmt.Sprintf("01P%023d", n), Kind: "pref", Path: scope, Meta: m}
}

// Spec acceptance 6: a node pref beats a run pref beats a config default; at
// one scope a class beats *; a nearer * beats a farther class; unset
// restores inheritance.
func TestResolve(t *testing.T) {
	node := "example-proj/rerun-nov/coord"
	defs := Defaults{
		Any:   map[string]string{"notify.remote": "off"},
		Class: map[string]map[string]string{"ask-blocked": {"notify.remote": "always"}},
	}
	check := func(r *Resolver, class, want, from string) {
		t.Helper()
		v, o := r.Resolve("notify.remote", node, class)
		if v != want || o.String() != from {
			t.Fatalf("%s: got %s (%s), want %s (%s)", class, v, o, want, from)
		}
	}
	r := New(nil, Defaults{})
	check(r, "ask-blocked", "away", "built in")
	check(r, "done", "off", "built in")
	r = New(nil, defs)
	check(r, "ask-blocked", "always", "config [defaults.class.ask-blocked]")
	check(r, "response", "off", "config [defaults]")

	evs := []store.Event{pref(1, "example-proj/rerun-nov", "notify.remote", "*", "away")}
	check(New(evs, defs), "ask-blocked", "away", "set at example-proj/rerun-nov, any class")
	evs = append(evs, pref(2, node, "notify.remote", "*", "off"))
	check(New(evs, defs), "ask-blocked", "off", "set at "+node+", any class")
	// A farther class does not beat a nearer *.
	evs = append(evs, pref(3, "", "notify.remote", "ask-blocked", "always"))
	check(New(evs, defs), "ask-blocked", "off", "set at "+node+", any class")
	// At one scope the class beats *.
	evs = append(evs, pref(4, node, "notify.remote", "ask-blocked", "always"))
	check(New(evs, defs), "ask-blocked", "always", "set at "+node+", class ask-blocked")
	check(New(evs, defs), "done", "off", "set at "+node+", any class")
	// Unset restores inheritance, step by step.
	evs = append(evs, pref(5, node, "notify.remote", "ask-blocked", nil), pref(6, node, "notify.remote", "*", nil))
	r = New(evs, defs)
	check(r, "ask-blocked", "away", "set at example-proj/rerun-nov, any class")
	if len(r.Prefs()) != 2 || r.Has(node, "notify.remote", Any) {
		t.Fatalf("prefs: %+v", r.Prefs())
	}
}

func TestCheck(t *testing.T) {
	for _, ok := range [][2]string{{"deliver.behavior", "queue"}, {"notify.desktop", "host-a,host-b"},
		{"notify.desktop", "all"}, {"notify.remote", "away"}, {"notify.remote.dest", "example-chat"}} {
		if err := Check(ok[0], ok[1]); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
	for _, bad := range [][2]string{{"deliver.behavior", "interrupt"}, {"notify.desktop", "a b"},
		{"notify.remote", "sometimes"}, {"notify.remote.dest", " "}, {"colour", "red"}} {
		if err := Check(bad[0], bad[1]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if _, err := CheckClass("deliver.behavior", "done"); err == nil {
		t.Error("deliver.behavior took a class")
	}
	if c, err := CheckClass("notify.remote", ""); err != nil || c != Any {
		t.Errorf("empty class: %q %v", c, err)
	}
	if v, _ := New(nil, Defaults{}).Resolve("deliver.behavior", "x/y", "done"); v != "steer" {
		t.Errorf("deliver.behavior: %s", v)
	}
}
