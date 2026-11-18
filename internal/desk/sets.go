package desk

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// Project sets and the focus. A project set is a name and a list of path
// prefixes (a project such as agent-desk, or something deeper such as
// private/linear-finetune), matched on whole path segments. The focus is
// one set, or none: while a set is focused, the views show only what is
// under its prefixes, and notifications are raised only for it.
//
// Both are events, so they merge between machines like everything else:
// a projectset event at SetPath(name) defines a set (meta.prefixes) or
// deletes it (meta.deleted), and a focus event at FocusPath names the
// focused set (meta.set, empty for none). The latest event (by id) counts.

// SystemPrefix is where the desk keeps its own state, outside every project.
const SystemPrefix = "_desk"

// FocusPath is where focus events live.
const FocusPath = SystemPrefix + "/focus"

// SetPath is where the events of the set called name live.
func SetPath(name string) string { return SystemPrefix + "/sets/" + name }

var setName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,31}$`)

// ValidSetName reports whether name can name a set: letters, digits, dot,
// underscore and dash, at most 32, and not "off", which turns the focus off.
func ValidSetName(name string) bool {
	return setName.MatchString(name) && !strings.EqualFold(name, "off")
}

// ProjectSet is one defined set.
type ProjectSet struct {
	Name     string      `json:"name"`
	Prefixes []string    `json:"prefixes"`
	Event    store.Event `json:"-"`
}

// SetMeta is a projectset event's meta.
type SetMeta struct {
	Prefixes []string `json:"prefixes,omitempty"`
	Deleted  bool     `json:"deleted,omitempty"`
}

// CleanPrefixes trims slashes and drops empty and repeated prefixes, keeping
// the order given.
func CleanPrefixes(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, p := range in {
		p = strings.Trim(strings.TrimSpace(p), "/")
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// Sets are the defined sets, by name.
func (d *Desk) Sets() []ProjectSet {
	latest := map[string]store.Event{}
	for _, e := range d.Events {
		if e.Kind != "projectset" {
			continue
		}
		if cur, ok := latest[e.Path]; !ok || e.ID > cur.ID {
			latest[e.Path] = e
		}
	}
	out := []ProjectSet{}
	for path, e := range latest {
		var m SetMeta
		_ = json.Unmarshal(e.Meta, &m)
		if m.Deleted {
			continue
		}
		out = append(out, ProjectSet{Name: strings.TrimPrefix(path, SetPath("")), Prefixes: CleanPrefixes(m.Prefixes), Event: e})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Set finds a defined set by name.
func (d *Desk) Set(name string) (ProjectSet, bool) {
	for _, s := range d.Sets() {
		if s.Name == name {
			return s, true
		}
	}
	return ProjectSet{}, false
}

// FocusName is the name the latest focus event gives, empty for none. The
// set may since have been deleted; Focus resolves it.
func (d *Desk) FocusName() string {
	var last store.Event
	for _, e := range d.Events {
		if e.Kind == "focus" && e.Path == FocusPath && e.ID > last.ID {
			last = e
		}
	}
	return MetaString(last, "set")
}

// Focus is the focused set, if a set is focused and still defined.
func (d *Desk) Focus() (ProjectSet, bool) {
	name := d.FocusName()
	if name == "" {
		return ProjectSet{}, false
	}
	return d.Set(name)
}

// FocusPrefixes are the focused set's prefixes, or nil when nothing is
// focused. A focused set with no prefixes focuses on nothing but the desk's
// own items.
func (d *Desk) FocusPrefixes() []string {
	s, ok := d.Focus()
	if !ok {
		return nil
	}
	if len(s.Prefixes) == 0 {
		return []string{SystemPrefix}
	}
	return s.Prefixes
}

// InFocus reports whether path shows under the focus prefixes: nothing is
// focused, the path is under one of them, or it is the desk's own (a screen
// request concerns the user whatever they focus on).
func InFocus(path string, focus []string) bool {
	if len(focus) == 0 || Under(path, SystemPrefix) {
		return true
	}
	for _, p := range focus {
		if Under(path, p) {
			return true
		}
	}
	return false
}

// Hidden counts what the focus hides that needs the user.
type Hidden struct {
	// NeedsYou: open questions outside the focus, not dismissed.
	NeedsYou int `json:"needs_you"`
	// Unread: unread notices, done reports, slots and responses outside it.
	Unread int `json:"unread"`
}

// Total is everything hidden that needs the user.
func (h Hidden) Total() int { return h.NeedsYou + h.Unread }

// HiddenBy counts what f's focus hides from f.
func (d *Desk) HiddenBy(f Filter) Hidden {
	if len(f.Focus) == 0 {
		return Hidden{}
	}
	f.All, f.Linger, f.Dismissed = false, nil, false
	all := f
	all.Focus = nil
	return Hidden{
		NeedsYou: len(d.OpenQuestions(all)) - len(d.OpenQuestions(f)),
		Unread: count(d.New(all)) + count(d.ResponseGroups(all)) -
			count(d.New(f)) - count(d.ResponseGroups(f)),
	}
}
