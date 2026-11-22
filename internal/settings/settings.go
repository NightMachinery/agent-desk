// Package settings resolves the user's settings for one event: how a
// message reaches a busy agent, and where a notification goes; and for one
// agent, whether it resumes itself after a usage limit. A setting is
// a key with a value, set for a scope (the empty path, or a project, run or
// node path) and a class (the kind of event it applies to, or * for any).
//
// Overrides are pref events in the store, so they merge between machines and
// keep their history; defaults are the config file's [defaults] tables; below
// those are the built-in values.
package settings

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// Keys are the settings there are, in display order.
var Keys = []string{"deliver.behavior", "notify.desktop", "notify.remote", "notify.remote.dest", "notify.push",
	"auto-continue.main", "auto-continue.subagents"}

// Classes are the kinds of event a setting can be set for, besides Any.
var Classes = []string{"ask-blocked", "ask-default", "response", "deliverable", "tell-notify", "tell", "done"}

// Any is the class that applies to every event.
const Any = "*"

// Classless reports whether key ignores the class: deliver.behavior is about
// messages, which have no class, and auto-continue about agents.
func Classless(key string) bool {
	return key == "deliver.behavior" || strings.HasPrefix(key, "auto-continue.")
}

var hostName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Check validates a key and a value for it.
func Check(key, value string) error {
	switch key {
	case "deliver.behavior":
		switch value {
		case "steer", "queue":
			return nil
		case "interrupt":
			return fmt.Errorf("deliver.behavior cannot be interrupt: an interrupt cancels the agent's work in progress, " +
				"so it is only chosen for one message (reply --behavior interrupt)")
		}
		return fmt.Errorf("deliver.behavior is steer or queue, not %q", value)
	case "notify.desktop":
		switch value {
		case "none", "origin", "all":
			return nil
		}
		for _, h := range strings.Split(value, ",") {
			if !hostName.MatchString(strings.TrimSpace(h)) {
				return fmt.Errorf("notify.desktop is none, origin, all, or host names separated by commas, not %q", value)
			}
		}
		return nil
	case "notify.remote", "notify.push":
		switch value {
		case "off", "away", "always":
			return nil
		}
		return fmt.Errorf("%s is off, away or always, not %q", key, value)
	case "auto-continue.main", "auto-continue.subagents":
		if value == "on" || value == "off" {
			return nil
		}
		return fmt.Errorf("%s is on or off, not %q", key, value)
	case "notify.remote.dest":
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("notify.remote.dest needs a value")
		}
		return nil
	}
	return fmt.Errorf("no setting %q; the settings are %s", key, strings.Join(Keys, ", "))
}

// CheckClass validates a class for key; empty means Any.
func CheckClass(key, class string) (string, error) {
	if class == "" || class == Any {
		return Any, nil
	}
	if Classless(key) {
		return "", fmt.Errorf("%s applies to messages, which have no class; drop --class", key)
	}
	for _, c := range Classes {
		if c == class {
			return c, nil
		}
	}
	return "", fmt.Errorf("no class %q; the classes are %s, or * for any", class, strings.Join(Classes, ", "))
}

// BuiltIn is the value below every override and default.
func BuiltIn(key, class string) string {
	switch key {
	case "deliver.behavior":
		return "steer"
	case "auto-continue.main", "auto-continue.subagents":
		// An agent stopped at a usage limit resumes when its quota returns,
		// unless someone said otherwise for it or its role.
		return "on"
	case "notify.desktop":
		if class == "tell" {
			return "none"
		}
		return "origin"
	case "notify.remote":
		if class == "ask-blocked" || class == "response" || class == "deliverable" {
			return "away"
		}
		return "off"
	case "notify.push":
		// Pushed to the devices that subscribed: a phone hears at once
		// about what stops an agent.
		if class == "ask-blocked" {
			return "always"
		}
		return "off"
	}
	return ""
}

// Defaults are the config file's [defaults] (Any) and
// [defaults.class.<class>] tables.
type Defaults struct {
	Any   map[string]string
	Class map[string]map[string]string
}

// Pref is one standing override.
type Pref struct {
	ID    string `json:"id"`
	Scope string `json:"scope"`
	Key   string `json:"key"`
	Class string `json:"class"`
	Value string `json:"value"`
}

// PrefMeta is a pref event's meta. A null value removes the override.
type PrefMeta struct {
	Key   string  `json:"key"`
	Class string  `json:"class"`
	Value *string `json:"value"`
}

// MetaOf reads a pref event's meta.
func MetaOf(e store.Event) PrefMeta {
	var m PrefMeta
	_ = json.Unmarshal(e.Meta, &m)
	if m.Class == "" {
		m.Class = Any
	}
	return m
}

type prefKey struct{ scope, key, class string }

// Resolver answers what a setting is for an event at a path.
type Resolver struct {
	prefs    map[prefKey]Pref
	order    []prefKey
	Defaults Defaults
}

// New reads the standing overrides from events in id order: the latest pref
// for a scope, key and class counts.
func New(evs []store.Event, defaults Defaults) *Resolver {
	r := &Resolver{prefs: map[prefKey]Pref{}, Defaults: defaults}
	for _, e := range evs {
		if e.Kind != "pref" {
			continue
		}
		m := MetaOf(e)
		k := prefKey{strings.Trim(e.Path, "/"), m.Key, m.Class}
		if _, ok := r.prefs[k]; !ok {
			r.order = append(r.order, k)
		}
		if m.Value == nil {
			delete(r.prefs, k)
			continue
		}
		r.prefs[k] = Pref{ID: e.ID, Scope: k.scope, Key: m.Key, Class: m.Class, Value: *m.Value}
	}
	return r
}

// Prefs lists the standing overrides, by scope, then key, then class.
func (r *Resolver) Prefs() []Pref {
	var out []Pref
	for _, k := range r.order {
		if p, ok := r.prefs[k]; ok {
			out = append(out, p)
		}
	}
	sortPrefs(out)
	return out
}

// Has reports whether an override stands for scope, key and class.
func (r *Resolver) Has(scope, key, class string) bool {
	_, ok := r.prefs[prefKey{strings.Trim(scope, "/"), key, class}]
	return ok
}

// Origin is where a resolved value comes from.
type Origin struct {
	Source string `json:"source"` // pref, config or built-in
	Scope  string `json:"scope,omitempty"`
	Class  string `json:"class,omitempty"`
	ID     string `json:"id,omitempty"` // the pref event
}

func (o Origin) String() string {
	class := "any class"
	if o.Class != "" && o.Class != Any {
		class = "class " + o.Class
	}
	switch o.Source {
	case "pref":
		where := "globally"
		if o.Scope != "" {
			where = "at " + o.Scope
		}
		return fmt.Sprintf("set %s, %s", where, class)
	case "config":
		if o.Class != "" && o.Class != Any {
			return "config [defaults.class." + o.Class + "]"
		}
		return "config [defaults]"
	}
	return "built in"
}

// Scopes are path and each of its parents, nearest first, ending with the
// global scope "".
func Scopes(path string) []string {
	path = strings.Trim(path, "/")
	var out []string
	for path != "" {
		out = append(out, path)
		i := strings.LastIndex(path, "/")
		if i < 0 {
			break
		}
		path = path[:i]
	}
	return append(out, "")
}

// Resolve finds key for an event of class at path: overrides from the path
// upwards (at one scope the class beats *, and a nearer scope beats a
// farther one whatever the class), then the config defaults (class before
// *), then the built-in value.
func (r *Resolver) Resolve(key, path, class string) (string, Origin) {
	classes := []string{class, Any}
	if Classless(key) || class == "" || class == Any {
		classes = []string{Any}
	}
	for _, s := range Scopes(path) {
		for _, c := range classes {
			if p, ok := r.prefs[prefKey{s, key, c}]; ok {
				return p.Value, Origin{Source: "pref", Scope: s, Class: c, ID: p.ID}
			}
		}
	}
	for _, c := range classes {
		var v string
		var ok bool
		if c == Any {
			v, ok = r.Defaults.Any[key]
		} else {
			v, ok = r.Defaults.Class[c][key]
		}
		if ok {
			return v, Origin{Source: "config", Class: c}
		}
	}
	return BuiltIn(key, class), Origin{Source: "built-in"}
}

func sortPrefs(ps []Pref) {
	rank := func(class string) int {
		if class == Any {
			return -1
		}
		for i, c := range Classes {
			if c == class {
				return i
			}
		}
		return len(Classes)
	}
	keyRank := func(key string) int {
		for i, k := range Keys {
			if k == key {
				return i
			}
		}
		return len(Keys)
	}
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		if a.Key != b.Key {
			return keyRank(a.Key) < keyRank(b.Key)
		}
		return rank(a.Class) < rank(b.Class)
	})
}
