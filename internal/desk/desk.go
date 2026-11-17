// Package desk reads a store into the dashboard's information design: the
// "Needs you", "New" and "Runs" views, scoped by path prefix and host.
package desk

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/ids"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// QuietAfter is how long without events makes an active run quiet.
const QuietAfter = 72 * time.Hour

// States, most urgent first.
const (
	Waiting  = "waiting on you"
	Active   = "active"
	Quiet    = "quiet"
	Finished = "finished"
)

func stateRank(s string) int {
	switch s {
	case Waiting:
		return 0
	case Active:
		return 1
	case Quiet:
		return 2
	}
	return 3
}

// Desk is everything loaded from one store at one moment.
type Desk struct {
	Now       time.Time
	Host      string // this machine
	Events    []store.Event
	ByID      map[string]store.Event
	Table     *ids.Table
	Questions []store.Question
	unread    map[string]bool
	dismissed map[string]bool // questions marked read: open, but out of Needs you
	marked    map[string]bool // events whose latest read mark stands
	pins      []string        // pin keys, most recently pinned first
	slots     []store.Event   // slot_current minus clears
}

// Load reads the store.
func Load(s *store.Store, host string, now time.Time) (*Desk, error) {
	evs, err := s.All()
	if err != nil {
		return nil, err
	}
	d := &Desk{Now: now, Host: host, Events: evs, ByID: make(map[string]store.Event, len(evs))}
	all := make([]string, len(evs))
	for i := range evs {
		if evs[i].Kind == "set" {
			evs[i].Body = SlotText(evs[i].Body)
		}
	}
	for i, e := range evs {
		d.ByID[e.ID] = e
		all[i] = e.ID
	}
	d.Table = ids.NewTable(all)
	if d.Questions, err = s.Questions(); err != nil {
		return nil, err
	}
	un, err := s.Query("unread", "")
	if err != nil {
		return nil, err
	}
	d.unread = make(map[string]bool, len(un))
	for _, e := range un {
		d.unread[e.ID] = true
	}
	if d.slots, err = s.Query("slot_current", "kind != 'clear'"); err != nil {
		return nil, err
	}
	for i := range d.slots {
		if d.slots[i].Kind == "set" {
			d.slots[i].Body = SlotText(d.slots[i].Body)
		}
	}
	// The read marker that counts is the latest naming the event, as in the
	// read_current view; events arrive in id order.
	last := map[string]bool{}
	for _, e := range evs {
		if e.Kind == "read" {
			last[e.Ref] = Meta(e)["cancel"] != true
		}
	}
	// Pins: the latest pin event per key counts; a cancel unpins.
	pinAt := map[string]int{}
	for i, e := range evs {
		if e.Kind == "pin" {
			k := d.PinKey(d.ByID[e.Ref])
			if Meta(e)["cancel"] == true {
				delete(pinAt, k)
			} else {
				pinAt[k] = i
			}
		}
	}
	for k := range pinAt {
		d.pins = append(d.pins, k)
	}
	sort.Slice(d.pins, func(i, j int) bool { return pinAt[d.pins[i]] > pinAt[d.pins[j]] })
	d.dismissed = map[string]bool{}
	d.marked = map[string]bool{}
	for id, on := range last {
		if on {
			d.marked[id] = true
			if d.ByID[id].Kind == "ask" {
				d.dismissed[id] = true
			}
		}
	}
	return d, nil
}

// KindOf returns an event's kind, for ids.Table.Resolve.
func (d *Desk) KindOf(id string) string { return d.ByID[id].Kind }

// Display returns an event's display id.
func (d *Desk) Display(id string) string { return d.Table.Display(id, d.ByID[id].Kind) }

// Resolve finds the event a typed token names.
func (d *Desk) Resolve(token string) (store.Event, error) {
	id, err := d.Table.Resolve(token, d.KindOf)
	if err != nil {
		return store.Event{}, err
	}
	return d.ByID[id], nil
}

// SlotText turns the two characters \n into line breaks in a slot text that
// has no real line break: an agent that wrote set next 'a\nb' in a shell
// meant two lines. Text with real line breaks is left as it is.
func SlotText(s string) string {
	if strings.Contains(s, "\n") || !strings.Contains(s, `\n`) {
		return s
	}
	return strings.ReplaceAll(s, `\n`, "\n")
}

// Unread reports whether an event is unread.
func (d *Desk) Unread(id string) bool { return d.unread[id] }

// PinKey is what a pin on e holds on to: a slot, whatever its current text,
// or else the event itself.
func (d *Desk) PinKey(e store.Event) string {
	if IsSlotKind(e.Kind) {
		return "slot:" + e.Path
	}
	return "id:" + e.ID
}

// Pinned reports whether e (or its slot) is pinned.
func (d *Desk) Pinned(e store.Event) bool {
	k := d.PinKey(e)
	for _, p := range d.pins {
		if p == k {
			return true
		}
	}
	return false
}

// PinnedItems are the pinned items in scope, most recently pinned first. A
// pinned slot shows its current text, and drops out while cleared.
func (d *Desk) PinnedItems(f Filter) []Item {
	cur := map[string]store.Event{}
	for _, s := range d.slots {
		cur[s.Path] = s
	}
	out := []Item{}
	for _, k := range d.pins {
		var e store.Event
		if path, ok := strings.CutPrefix(k, "slot:"); ok {
			e = cur[path]
		} else {
			e = d.ByID[strings.TrimPrefix(k, "id:")]
		}
		if e.ID == "" || !f.Keep(e) {
			continue
		}
		out = append(out, d.item(e))
	}
	return out
}

// Dismissed reports whether a question has been marked read. It stays open,
// and its agent stays blocked, but it leaves Needs you until marked unread.
func (d *Desk) Dismissed(id string) bool { return d.dismissed[id] }

// IsSlotKind reports whether events of kind k carry a slot name in their path.
func IsSlotKind(k string) bool { return k == "set" || k == "clear" || k == "link" }

// NodeOf is the node an event belongs to: its path, minus the slot name for
// slot events.
func NodeOf(e store.Event) string {
	if IsSlotKind(e.Kind) {
		if i := strings.LastIndex(e.Path, "/"); i >= 0 {
			return e.Path[:i]
		}
	}
	return e.Path
}

// SlotName is the slot of a slot event.
func SlotName(e store.Event) string {
	return e.Path[strings.LastIndex(e.Path, "/")+1:]
}

// Under reports whether path is prefix or below it.
func Under(path, prefix string) bool {
	prefix = strings.Trim(prefix, "/")
	return prefix == "" || path == prefix || strings.HasPrefix(path, prefix+"/")
}

// Filter scopes a view.
type Filter struct {
	Prefix  string
	Host    string
	Session string // a native session id or a Paseo agent id
	All     bool   // show read items and hidden runs
	// Dismissed keeps dismissed questions in Needs you: an agent's own view
	// of what it is still waiting for.
	Dismissed bool
}

func (f Filter) Keep(e store.Event) bool {
	return Under(e.Path, f.Prefix) && (f.Host == "" || e.Host == f.Host) &&
		(f.Session == "" || IsSession(e, f.Session))
}

// IsSession reports whether event e was written by session id, given as its
// native session id or its Paseo agent id.
func IsSession(e store.Event, id string) bool {
	var m struct {
		From struct {
			Session string `json:"session"`
			Paseo   string `json:"paseo"`
		} `json:"from"`
	}
	if json.Unmarshal(e.Meta, &m) != nil {
		return false
	}
	return id != "" && (m.From.Session == id || m.From.Paseo == id)
}

// Meta decodes an event's meta into a map.
func Meta(e store.Event) map[string]any {
	m := map[string]any{}
	_ = json.Unmarshal(e.Meta, &m)
	return m
}

// MetaString returns one string key of meta.
func MetaString(e store.Event, key string) string {
	s, _ := Meta(e)[key].(string)
	return s
}

// Time parses an event's timestamp.
func Time(e store.Event) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, e.TS)
	return t
}

// Age formats a duration as 45s, 10m, 5h or 3d.
func Age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", max(0, int(d.Seconds())))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// Split breaks a path into project, run and the rest.
func Split(path string) (project, run, rest string) {
	parts := strings.SplitN(path, "/", 3)
	project = parts[0]
	if len(parts) > 1 {
		run = parts[1]
	}
	if len(parts) > 2 {
		rest = parts[2]
	}
	return
}

// Item is one row of "Needs you" or "New".
type Item struct {
	Event    store.Event     `json:"event"`
	ID       string          `json:"display_id"`
	Label    string          `json:"label"` // the path below project/run
	Age      string          `json:"age"`
	Question *store.Question `json:"question,omitempty"`
	Unread   bool            `json:"unread"`
	// Dismissed: an open question marked read, shown only with Filter.All.
	Dismissed bool `json:"dismissed,omitempty"`
	// NoResponse: a message delivered NoResponseAfter ago with no response.
	NoResponse bool `json:"no_response,omitempty"`
	Pinned     bool `json:"pinned,omitempty"`
	// Response: an agent's response to the user; Asked is what the user
	// asked (or answered), AskedVia is message, answer or chat.
	Response bool   `json:"response,omitempty"`
	Asked    string `json:"asked,omitempty"`
	AskedVia string `json:"asked_via,omitempty"`
}

// Group is the items of one project and run.
type Group struct {
	Project string `json:"project"`
	Run     string `json:"run"`
	Items   []Item `json:"items"`
}

func (d *Desk) item(e store.Event) Item {
	_, _, rest := Split(e.Path)
	it := Item{Event: e, ID: d.Display(e.ID), Label: rest, Age: Age(d.Now.Sub(Time(e))), Unread: d.unread[e.ID],
		NoResponse: e.Kind == "reply" && d.NoResponse(e.ID), Pinned: d.Pinned(e), Response: IsResponse(e)}
	it.Asked, it.AskedVia = d.Asked(e)
	return it
}

// group keeps items in order and groups them by project, then run, with
// groups in order of their first item.
func group(items []Item) []Group {
	var out []Group
	idx := map[string]int{}
	for _, it := range items {
		p, r, _ := Split(it.Event.Path)
		k := p + "/" + r
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, Group{Project: p, Run: r})
		}
		out[i].Items = append(out[i].Items, it)
	}
	// Projects stay together: order groups by their project's first group.
	first := map[string]int{}
	for i, g := range out {
		if _, ok := first[g.Project]; !ok {
			first[g.Project] = i
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return first[out[a].Project] < first[out[b].Project] })
	return out
}

// OpenQuestions are the unanswered questions in scope: blocked first, then
// default, oldest first within each. Dismissed ones come only with f.All or
// f.Dismissed.
func (d *Desk) OpenQuestions(f Filter) []Item {
	var out []Item
	for i := range d.Questions {
		q := d.Questions[i]
		if !q.Open() || !f.Keep(q.Event) || (d.dismissed[q.ID] && !f.All && !f.Dismissed) {
			continue
		}
		it := d.item(q.Event)
		it.Question = &q
		it.Dismissed = d.dismissed[q.ID]
		out = append(out, it)
	}
	sort.SliceStable(out, func(a, b int) bool {
		ba, bb := out[a].Event.Level == "blocked", out[b].Event.Level == "blocked"
		if ba != bb {
			return ba
		}
		return out[a].Event.ID < out[b].Event.ID
	})
	return out
}

// NeedsYou groups the open questions by project and run.
func (d *Desk) NeedsYou(f Filter) []Group { return group(d.OpenQuestions(f)) }

// New is the unread notices, done reports and changed slots, grouped by
// project then run, oldest first. With f.All it includes read ones.
func (d *Desk) New(f Filter) []Group {
	var items []Item
	cur := map[string]bool{}
	for _, s := range d.slots {
		cur[s.ID] = true
	}
	for _, e := range d.Events {
		if !f.Keep(e) {
			continue
		}
		switch {
		case IsResponse(e):
			continue // in Responses
		case e.Kind == "tell" || e.Kind == "done":
		case e.Kind == "set" && cur[e.ID] && SlotName(e) != "status":
		default:
			continue
		}
		if !d.unread[e.ID] && !f.All {
			continue
		}
		items = append(items, d.item(e))
	}
	return group(items)
}

// ResponseGroups are the agents' unread responses to the user (all of them
// with f.All), grouped like New.
func (d *Desk) ResponseGroups(f Filter) []Group {
	var items []Item
	for _, e := range d.Events {
		if IsResponse(e) && f.Keep(e) && (d.unread[e.ID] || f.All) {
			items = append(items, d.item(e))
		}
	}
	return group(items)
}

// Slot is the current content of one slot.
type Slot struct {
	Name   string      `json:"name"`
	Event  store.Event `json:"event"`
	ID     string      `json:"display_id"`
	Unread bool        `json:"unread"`
	File   string      `json:"file,omitempty"` // for link slots
}

// Node is a project, run or agent in the tree.
type Node struct {
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Depth    int     `json:"depth"` // 1 project, 2 run, 3+ agent
	State    string  `json:"state"`
	Age      string  `json:"age"` // since the last event beneath
	Open     int     `json:"open_questions"`
	Blocked  int     `json:"blocked"`
	UnreadN  int     `json:"unread"`
	Host     string  `json:"host,omitempty"` // when not this machine
	Slots    []Slot  `json:"slots,omitempty"`
	Children []*Node `json:"children,omitempty"`
	Hidden   bool    `json:"-"`

	last     time.Time
	finished bool // this agent node has a done after its latest join
	joined   bool
}

// Status is the node's status slot text, if any.
func (n *Node) Status() string {
	for _, s := range n.Slots {
		if s.Name == "status" {
			return s.Event.Body
		}
	}
	return ""
}

// Tree builds the Runs view for the filter.
func (d *Desk) Tree(f Filter) []*Node {
	root := &Node{}
	nodes := map[string]*Node{"": root}
	var get func(path string) *Node
	get = func(path string) *Node {
		if n, ok := nodes[path]; ok {
			return n
		}
		parent := ""
		name := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			parent, name = path[:i], path[i+1:]
		}
		p := get(parent)
		n := &Node{Name: name, Path: path, Depth: p.Depth + 1}
		p.Children = append(p.Children, n)
		nodes[path] = n
		return n
	}

	scope := Filter{Prefix: f.Prefix, Host: f.Host}
	lastJoin := map[string]string{}
	hosts := map[string]string{}
	for _, e := range d.Events {
		if !scope.Keep(e) || e.Kind == "read" || e.Kind == "pin" {
			continue
		}
		node := NodeOf(e)
		n := get(node)
		t := Time(e)
		for m := n; m != nil && m != root; m = nodes[parentOf(m.Path)] {
			if t.After(m.last) {
				m.last = t
			}
		}
		hosts[node] = e.Host
		switch e.Kind {
		case "join":
			lastJoin[node] = e.ID
			n.joined = true
			n.finished = false
		case "done":
			if e.ID > lastJoin[node] {
				n.finished = true
			}
		}
	}
	for _, s := range d.slots {
		if !scope.Keep(s) {
			continue
		}
		n := get(NodeOf(s))
		sl := Slot{Name: SlotName(s), Event: s, ID: d.Display(s.ID), Unread: d.unread[s.ID]}
		if s.Kind == "link" {
			sl.File = MetaString(s, "file")
		}
		n.Slots = append(n.Slots, sl)
	}
	// A dismissed question no longer asks for attention, but its agent is
	// still blocked on it.
	for _, q := range d.Questions {
		if !q.Open() || !scope.Keep(q.Event) {
			continue
		}
		for m := get(q.Event.Path); m != nil && m != root; m = nodes[parentOf(m.Path)] {
			if !d.dismissed[q.ID] {
				m.Open++
			}
			if q.Event.Level == "blocked" {
				m.Blocked++
			}
		}
	}
	for id := range d.unread {
		e := d.ByID[id]
		if !scope.Keep(e) {
			continue
		}
		for m := get(NodeOf(e)); m != nil && m != root; m = nodes[parentOf(m.Path)] {
			m.UnreadN++
		}
	}
	for path, h := range hosts {
		if h != d.Host {
			nodes[path].Host = h
		}
	}

	var finish func(n *Node)
	finish = func(n *Node) {
		for _, c := range n.Children {
			finish(c)
		}
		if n != root {
			d.settle(n, f)
		}
		sortSlots(n.Slots)
		sortNodes(n.Children)
	}
	finish(root)
	return root.Children
}

func parentOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}

// settle computes a node's state and visibility once its children are done.
func (d *Desk) settle(n *Node, f Filter) {
	age := d.Now.Sub(n.last)
	n.Age = Age(age)
	live := Active
	if age > QuietAfter {
		live = Quiet
	}
	switch {
	case n.Blocked > 0:
		n.State = Waiting
	case n.Depth == 2:
		// A run is finished when its root coordinators are.
		roots, done := 0, 0
		for _, c := range n.Children {
			if c.joined || c.finished {
				roots++
				if c.finished {
					done++
				}
			}
		}
		if roots > 0 && roots == done {
			n.State = Finished
		} else {
			n.State = live
		}
	case n.Depth >= 3 && n.finished:
		n.State = Finished
	case n.Depth == 1 && len(n.Children) > 0:
		// The most urgent state of its runs, with that run's age; among
		// runs in the same state, the most recent.
		var pick *Node
		for _, c := range n.Children {
			if pick == nil || stateRank(c.State) < stateRank(pick.State) ||
				(stateRank(c.State) == stateRank(pick.State) && c.last.After(pick.last)) {
				pick = c
			}
		}
		n.State, n.Age = pick.State, pick.Age
	default:
		n.State = live
	}

	// Read slots are hidden; status shows while the node is live.
	if !f.All {
		var keep []Slot
		for _, s := range n.Slots {
			if s.Unread || (s.Name == "status" && n.State != Finished) {
				keep = append(keep, s)
			}
		}
		n.Slots = keep
	}
	if n.Depth == 2 && !f.All && n.State == Finished && n.Open == 0 && n.UnreadN == 0 {
		n.Hidden = true
	}
	if n.Depth == 1 && !f.All {
		n.Hidden = true
		for _, c := range n.Children {
			if !c.Hidden {
				n.Hidden = false
			}
		}
	}
}

func sortSlots(s []Slot) {
	order := map[string]int{"status": 0, "next": 1, "done": 2, "velocity": 3, "findings": 4, "artifacts": 5}
	sort.SliceStable(s, func(a, b int) bool {
		oa, ok := order[s[a].Name]
		if !ok {
			oa = 10
		}
		ob, ok := order[s[b].Name]
		if !ok {
			ob = 10
		}
		if oa != ob {
			return oa < ob
		}
		return s[a].Name < s[b].Name
	})
}

// sortNodes orders siblings: waiting on you, then unread, then active by
// recency, then quiet, then finished.
func sortNodes(ns []*Node) {
	rank := func(n *Node) int {
		switch {
		case n.State == Waiting:
			return 0
		case n.UnreadN > 0 || n.Open > 0:
			return 1
		}
		return 1 + stateRank(n.State)
	}
	sort.SliceStable(ns, func(a, b int) bool {
		ra, rb := rank(ns[a]), rank(ns[b])
		if ra != rb {
			return ra < rb
		}
		return ns[a].last.After(ns[b].last)
	})
}

// Visible drops hidden nodes, recursively.
func Visible(ns []*Node) []*Node {
	var out []*Node
	for _, n := range ns {
		if n.Hidden {
			continue
		}
		n.Children = Visible(n.Children)
		out = append(out, n)
	}
	return out
}

// Find returns the node at path in a tree, or nil.
func Find(ns []*Node, path string) *Node {
	for _, n := range ns {
		if n.Path == path {
			return n
		}
		if Under(path, n.Path) {
			return Find(n.Children, path)
		}
	}
	return nil
}

// Attention reports whether anything beneath the node needs the user.
func (n *Node) Attention() bool { return n.Open > 0 || n.UnreadN > 0 }
