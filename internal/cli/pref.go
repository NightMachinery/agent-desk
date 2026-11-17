package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/settings"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// settings resolves settings over d's pref events and the config defaults.
func (a *App) settings(d *desk.Desk) (*settings.Resolver, error) {
	c, err := a.config()
	if err != nil {
		return nil, err
	}
	return settings.New(d.Events, c.Defaults), nil
}

// defaultBehavior is the deliver.behavior setting for messages to node.
func (a *App) defaultBehavior(d *desk.Desk, node string) (string, error) {
	r, err := a.settings(d)
	if err != nil {
		return "", err
	}
	v, _ := r.Resolve("deliver.behavior", node, "")
	return v, nil
}

func (a *App) cmdPref(args []string) error {
	if len(args) == 0 {
		return usagef("pref set|unset|show; run agent-desk help")
	}
	switch args[0] {
	case "set":
		return a.cmdPrefSet(args[1:], false)
	case "unset":
		return a.cmdPrefSet(args[1:], true)
	case "show":
		return a.cmdPrefShow(args[1:])
	}
	return usagef("pref set|unset|show, not %q", args[0])
}

// cmdPrefSet appends a pref event; unset appends one with a null value.
// Settings are the user's: an agent session may not write them.
func (a *App) cmdPrefSet(args []string, unset bool) error {
	n := 2
	if unset {
		n = 1
	}
	o, err := parse(args, nil, []string{"scope", "class"}, n, n)
	if err != nil {
		return err
	}
	from := ident.Resolve(a.Env)
	if from.Agent != "" {
		return errors.New("settings are the user's: an agent session cannot change them; " +
			"ask the user, who can run agent-desk pref from a terminal")
	}
	key := o.pos[0]
	if err := settings.Check(key, "x"); err != nil && strings.HasPrefix(err.Error(), "no setting") {
		return usagef("%v", err)
	}
	class, err := settings.CheckClass(key, o.vals["class"])
	if err != nil {
		return usagef("%v", err)
	}
	var value *string
	if !unset {
		v := strings.TrimSpace(o.pos[1])
		if key == "notify.desktop" {
			hosts := strings.Split(v, ",")
			for i := range hosts {
				hosts[i] = strings.TrimSpace(hosts[i])
			}
			v = strings.Join(hosts, ",")
		}
		if err := settings.Check(key, v); err != nil {
			return usagef("%v", err)
		}
		value = &v
	}
	scope := strings.Trim(o.vals["scope"], "/")
	d, err := a.load()
	if err != nil {
		return err
	}
	r, err := a.settings(d)
	if err != nil {
		return err
	}
	if unset && !r.Has(scope, key, class) {
		fmt.Fprintf(a.Stderr, "agent-desk: no override of %s %s to remove\n", key, where(scope, class))
		return nil
	}
	if scope != "" && !knownPath(d, scope) {
		fmt.Fprintf(a.Stderr, "agent-desk: nothing on the desk is under %s yet; the setting applies once it is\n", scope)
	}
	e := a.event("pref", scope, from, "cli", nil)
	m := settings.PrefMeta{Key: key, Class: class, Value: value}
	if e.Meta, err = json.Marshal(m); err != nil {
		return err
	}
	return a.commit([]store.Event{e}, e.ID)
}

func where(scope, class string) string {
	s := "globally"
	if scope != "" {
		s = "at " + scope
	}
	if class != settings.Any {
		s += " for class " + class
	}
	return s
}

func knownPath(d *desk.Desk, path string) bool {
	for _, e := range d.Events {
		if e.Kind != "pref" && desk.Under(desk.NodeOf(e), path) {
			return true
		}
	}
	return false
}

// Resolved is one setting's value for one class, and where it comes from.
type Resolved struct {
	Key    string          `json:"key"`
	Class  string          `json:"class,omitempty"`
	Value  string          `json:"value"`
	Origin settings.Origin `json:"origin"`
	From   string          `json:"from"`
}

// SettingsView is what pref show reports for a path.
type SettingsView struct {
	Path      string          `json:"path"`
	Settings  []Resolved      `json:"settings"`
	Overrides []settings.Pref `json:"overrides"`
}

// settingsAt resolves every key at path, for one class or for each.
func settingsAt(r *settings.Resolver, path, class string) SettingsView {
	v := SettingsView{Path: path, Overrides: r.Prefs()}
	if v.Overrides == nil {
		v.Overrides = []settings.Pref{}
	}
	for _, k := range settings.Keys {
		classes := settings.Classes
		if class != "" {
			classes = []string{class}
		}
		if settings.Classless(k) {
			classes = []string{""}
		}
		for _, c := range classes {
			val, o := r.Resolve(k, path, c)
			if val == "" && k == "notify.remote.dest" {
				continue
			}
			v.Settings = append(v.Settings, Resolved{Key: k, Class: c, Value: val, Origin: o, From: o.String()})
		}
	}
	return v
}

func (a *App) cmdPrefShow(args []string) error {
	o, err := parse(args, nil, []string{"class", "format"}, 0, 1)
	if err != nil {
		return err
	}
	class := o.vals["class"]
	if class != "" {
		if class, err = settings.CheckClass("notify.desktop", class); err != nil {
			return usagef("%v", err)
		}
		if class == settings.Any {
			return usagef("--class takes one class: %s", strings.Join(settings.Classes, ", "))
		}
	}
	path := ""
	if len(o.pos) == 1 {
		path = strings.Trim(o.pos[0], "/")
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	r, err := a.settings(d)
	if err != nil {
		return err
	}
	v := settingsAt(r, path, class)
	switch o.vals["format"] {
	case "json":
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case "", "text":
	default:
		return usagef("--format is text or json")
	}
	at := "everywhere (global)"
	if path != "" {
		at = path
	}
	fmt.Fprintf(a.Stdout, "Settings for %s\n", at)
	last := ""
	for _, s := range v.Settings {
		if s.Class == "" {
			fmt.Fprintf(a.Stdout, "\n%s = %s  (%s)\n", s.Key, s.Value, s.From)
			last = s.Key
			continue
		}
		if s.Key != last {
			fmt.Fprintf(a.Stdout, "\n%s\n", s.Key)
			last = s.Key
		}
		fmt.Fprintf(a.Stdout, "  %-12s %-7s (%s)\n", s.Class, s.Value, s.From)
	}
	if len(v.Overrides) > 0 {
		fmt.Fprintln(a.Stdout, "\nOverrides on the desk")
		for _, p := range v.Overrides {
			fmt.Fprintf(a.Stdout, "  %s = %s  %s  %s\n", p.Key, p.Value, where(p.Scope, p.Class), d.Display(p.ID))
		}
	}
	return nil
}
