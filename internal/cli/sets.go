package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Project sets and the focus (internal/desk/sets.go): sets lists, defines
// and deletes them; focus shows or changes which one the desk shows.

// SetInfo is one set as sets --format json and the dashboard list it.
type SetInfo struct {
	Name     string   `json:"name"`
	Prefixes []string `json:"prefixes"`
	Focused  bool     `json:"focused"`
}

func setInfos(d *desk.Desk) []SetInfo {
	focus := ""
	if s, ok := d.Focus(); ok {
		focus = s.Name
	}
	out := []SetInfo{}
	for _, s := range d.Sets() {
		out = append(out, SetInfo{Name: s.Name, Prefixes: s.Prefixes, Focused: s.Name == focus})
	}
	return out
}

// focusInfo is the focus and what it hides from f, or nil when nothing is
// focused. f carries the focus prefixes.
func focusInfo(d *desk.Desk, f desk.Filter) *desk.FocusView {
	s, ok := d.Focus()
	if !ok {
		return nil
	}
	return &desk.FocusView{Name: s.Name, Prefixes: s.Prefixes, Hidden: d.HiddenBy(f)}
}

func (a *App) cmdSets(args []string) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "define":
			return a.cmdSetsDefine(args[1:])
		case "delete":
			return a.cmdSetsDelete(args[1:])
		case "list":
			args = args[1:]
		default:
			return usagef("sets [list] | sets define NAME PREFIX... | sets delete NAME, not %q", args[0])
		}
	}
	o, err := parse(args, nil, []string{"format"}, 0, 0)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	sets := setInfos(d)
	switch o.vals["format"] {
	case "json":
		return desk.JSON(a.Stdout, sets)
	case "", "text":
	default:
		return usagef("--format is text or json")
	}
	if len(sets) == 0 {
		fmt.Fprintln(a.Stdout, "No project sets. Define one with: agent-desk sets define NAME PREFIX...")
		return nil
	}
	for _, s := range sets {
		mark := "  "
		if s.Focused {
			mark = "* "
		}
		fmt.Fprintf(a.Stdout, "%s%s: %s\n", mark, s.Name, strings.Join(s.Prefixes, ", "))
	}
	return nil
}

func (a *App) cmdSetsDefine(args []string) error {
	o, err := parse(args, nil, nil, 2, -1)
	if err != nil {
		return err
	}
	name := o.pos[0]
	if !desk.ValidSetName(name) {
		return usagef("a set name is letters, digits, '.', '_' and '-', at most 32, and not off; not %q", name)
	}
	prefixes := desk.CleanPrefixes(o.pos[1:])
	if len(prefixes) == 0 {
		return usagef("give at least one path prefix, such as a project")
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	for _, p := range prefixes {
		if !knownPath(d, p) {
			fmt.Fprintf(a.Stderr, "agent-desk: nothing on the desk is under %s yet; the set takes it in once something is\n", p)
		}
	}
	e := a.event("projectset", desk.SetPath(name), ident.Resolve(a.Env), "cli", map[string]any{"prefixes": prefixes})
	if err := a.commit([]store.Event{e}, "-"); err != nil {
		return err
	}
	verb := "defined"
	if _, ok := d.Set(name); ok {
		verb = "redefined"
	}
	fmt.Fprintf(a.Stdout, "%s %s: %s\n", name, verb, strings.Join(prefixes, ", "))
	return nil
}

func (a *App) cmdSetsDelete(args []string) error {
	o, err := parse(args, nil, nil, 1, 1)
	if err != nil {
		return err
	}
	name := o.pos[0]
	d, err := a.load()
	if err != nil {
		return err
	}
	if _, ok := d.Set(name); !ok {
		return fmt.Errorf("no project set %q%s", name, setsHint(d))
	}
	from := ident.Resolve(a.Env)
	evs := []store.Event{a.event("projectset", desk.SetPath(name), from, "cli", map[string]any{"deleted": true})}
	// Deleting the focused set ends the focus, so defining the name again
	// later does not bring it back unasked.
	focused := d.FocusName() == name
	if focused {
		evs = append(evs, a.event("focus", desk.FocusPath, from, "cli", map[string]any{"set": ""}))
	}
	if err := a.commit(evs, "-"); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "%s deleted\n", name)
	if focused {
		fmt.Fprintln(a.Stdout, "Focus: off (everything shows)")
	}
	return nil
}

func setsHint(d *desk.Desk) string {
	var names []string
	for _, s := range d.Sets() {
		names = append(names, s.Name)
	}
	if len(names) == 0 {
		return "; there are none yet (agent-desk sets define NAME PREFIX...)"
	}
	return "; the sets are " + strings.Join(names, ", ")
}

// cmdFocus prints the focus, or sets it to a set or off. The focus governs
// the user's notifications, so, like settings, an agent session may not
// change it.
func (a *App) cmdFocus(args []string) error {
	o, err := parse(args, nil, []string{"format"}, 0, 1)
	if err != nil {
		return err
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	if len(o.pos) == 0 {
		info := focusInfo(d, desk.Filter{Focus: d.FocusPrefixes()})
		switch o.vals["format"] {
		case "json":
			if info == nil {
				_, err := fmt.Fprintln(a.Stdout, "null")
				return err
			}
			return desk.JSON(a.Stdout, info)
		case "", "text":
		default:
			return usagef("--format is text or json")
		}
		if info == nil {
			fmt.Fprintln(a.Stdout, "Focus: off (everything shows)")
			return nil
		}
		fmt.Fprintf(a.Stdout, "Focus: %s (%s)\n", info.Name, strings.Join(info.Prefixes, ", "))
		if h := info.Hidden; h.Total() > 0 {
			fmt.Fprintf(a.Stdout, "Hidden: %s\n", hiddenText(h))
		}
		return nil
	}
	if ident.Resolve(a.Env).Agent != "" {
		return errors.New("the focus is the user's: an agent session cannot change it; " +
			"ask the user, who can run agent-desk focus from a terminal or pick it in the dashboard")
	}
	name := o.pos[0]
	if strings.EqualFold(name, "off") || name == "" {
		name = ""
	} else if _, ok := d.Set(name); !ok {
		return fmt.Errorf("no project set %q%s", name, setsHint(d))
	}
	e := a.event("focus", desk.FocusPath, ident.From{}, "cli", map[string]any{"set": name})
	if err := a.commit([]store.Event{e}, "-"); err != nil {
		return err
	}
	if name == "" {
		fmt.Fprintln(a.Stdout, "Focus: off (everything shows)")
		return nil
	}
	s, _ := d.Set(name)
	fmt.Fprintf(a.Stdout, "Focus: %s (%s)\n", name, strings.Join(s.Prefixes, ", "))
	return nil
}

// hiddenText says what a focus hides: "3 hidden: 1 waiting on you".
func hiddenText(h desk.Hidden) string {
	s := fmt.Sprintf("%d hidden", h.Total())
	if h.NeedsYou > 0 {
		s += fmt.Sprintf(": %d waiting on you", h.NeedsYou)
	}
	return s
}

// setsRequest runs a dashboard sets or focus action as the CLI would.
func (a *App) setsRequest(action, name string, prefixes []string) error {
	switch action {
	case "define":
		return a.run(append([]string{"sets", "define", "--", name}, prefixes...))
	case "delete":
		return a.run([]string{"sets", "delete", "--", name})
	case "focus":
		if name == "" {
			name = "off"
		}
		return a.run([]string{"focus", "--", name})
	}
	return usagef("the action is define, delete or focus, not %q", action)
}
