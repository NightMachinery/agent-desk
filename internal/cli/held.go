package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// A session holds a node while it is the node's owner (its latest join,
// whether a first join, a re-join or a takeover, came from this session on
// this host) and it has not left the node since. A session may hold several
// nodes: join adds one, leave drops one, and a takeover by another session
// drops it too.

// leftAfter reports whether session left node after the event id.
func leftAfter(events []store.Event, node, session, id string) bool {
	for _, e := range events {
		if e.Kind == "leave" && e.Path == node && e.ID > id && sessionOf(e) == session {
			return true
		}
	}
	return false
}

// nodeHolder is the session holding node: its owner, unless the owner has
// left it. A node nobody holds is free to join without --takeover.
func nodeHolder(d *desk.Desk, node string) (store.Event, bool) {
	owner, ok := nodeOwner(d, node)
	if !ok || leftAfter(d.Events, node, sessionOf(owner), owner.ID) {
		return store.Event{}, false
	}
	return owner, true
}

// held is the nodes this session holds, in the order it joined them, with
// the join that holds each.
func (a *App) held(d *desk.Desk, from ident.From) []store.Event {
	return a.heldFrom(d.Events, from)
}

func (a *App) heldFrom(events []store.Event, from ident.From) []store.Event {
	if from.Session == "" {
		return nil
	}
	owner := map[string]store.Event{}
	left := map[string]string{} // node -> this session's latest leave
	for _, e := range events {
		switch e.Kind {
		case "join":
			owner[e.Path] = e
		case "leave":
			if sessionOf(e) == from.Session && e.ID > left[e.Path] {
				left[e.Path] = e.ID
			}
		}
	}
	var out []store.Event
	for node, j := range owner {
		if j.Host == a.Host && sessionOf(j) == from.Session && left[node] < j.ID {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func heldPaths(evs []store.Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Path
	}
	return out
}

func listNodes(paths []string) string {
	return "  " + strings.Join(paths, "\n  ")
}

// pickHeld resolves a --node value against the held nodes: a full path, or
// one relative to a held node's project, as join takes it (run/role).
func pickHeld(held []string, arg string) (string, error) {
	arg = strings.Trim(arg, "/")
	if arg == "" {
		return "", usagef("--node needs a node path")
	}
	var hits []string
	for _, h := range held {
		if h == arg {
			return h, nil
		}
		if p, _, _ := strings.Cut(h, "/"); p+"/"+arg == h {
			hits = append(hits, h)
		}
	}
	switch {
	case len(hits) == 1:
		return hits[0], nil
	case len(hits) > 1:
		return "", fmt.Errorf("--node %s matches more than one node this session holds; give the full path:\n%s",
			arg, listNodes(hits))
	case len(held) == 0:
		return "", fmt.Errorf("--node %s: this session holds no node; join it first: agent-desk join <run>/<role>", arg)
	}
	return "", fmt.Errorf("--node %s is not a node this session holds; it holds:\n%s\n"+
		"A session writes only to nodes it joined (workers report to their parent)", arg, listNodes(held))
}

// severalHeld is the refusal for a write, or a read of "my node", that does
// not say which of several held nodes it is for.
func severalHeld(paths []string, cmd string) error {
	return fmt.Errorf("this session holds %d nodes, so %s must say which one it is for: add --node PATH, one of:\n%s\n"+
		"(--node also takes a path relative to the node's project, such as %s. "+
		"A node this session no longer works on: agent-desk leave PATH)",
		len(paths), cmd, listNodes(paths), relPath(paths[0]))
}

func relPath(node string) string {
	_, rest, _ := strings.Cut(node, "/")
	return rest
}

// myNode resolves the node a command acts on when it means "this session's
// node": the --node value if given (it must be held), else the one node
// held. With no node held it returns "" and no error; with several and no
// --node it refuses.
func (a *App) myNode(d *desk.Desk, from ident.From, nodeArg, cmd string) (string, error) {
	paths := heldPaths(a.held(d, from))
	if nodeArg != "" {
		return pickHeld(paths, nodeArg)
	}
	switch len(paths) {
	case 0:
		return "", nil
	case 1:
		return paths[0], nil
	}
	return "", severalHeld(paths, cmd)
}

// takenOver explains why a session that joined nodes holds none of them,
// when another session took over the one it joined last.
func (a *App) takenOver(events []store.Event, from ident.From) error {
	var last store.Event
	found := false
	for _, e := range events {
		if e.Kind == "join" && e.Host == a.Host && sessionOf(e) == from.Session {
			last, found = e, true
		}
	}
	if !found || leftAfter(events, last.Path, from.Session, last.ID) {
		return nil
	}
	owner, _ := nodeOwnerFrom(events, last.Path)
	if sessionOf(owner) == from.Session {
		return nil
	}
	return fmt.Errorf("node %s was taken over by another session (%s %s, %s); "+
		"this session no longer writes there. Join another node, or report to whoever took it over",
		last.Path, agentOfEvent(owner), shortID(sessionOf(owner)), owner.TS)
}

// cmdLeave drops nodes this session holds. Leaving the last one makes the
// session nodeless again.
func (a *App) cmdLeave(args []string) error {
	o, err := parse(args, nil, nil, 1, -1)
	if err != nil {
		return err
	}
	from := ident.Resolve(a.Env)
	if from.Session == "" {
		return errors.New("cannot tell which agent session this is; only agent sessions leave nodes")
	}
	d, err := a.load()
	if err != nil {
		return err
	}
	paths := heldPaths(a.held(d, from))
	var evs []store.Event
	seen := map[string]bool{}
	for _, arg := range o.pos {
		node, err := pickHeld(paths, arg)
		if err != nil {
			return err
		}
		if !seen[node] {
			seen[node] = true
			evs = append(evs, a.event("leave", node, from, "cli", nil))
		}
	}
	if err := a.commit(evs, "-"); err != nil {
		return err
	}
	rest := []string{}
	for _, p := range paths {
		if !seen[p] {
			rest = append(rest, p)
		}
	}
	switch len(rest) {
	case 0:
		fmt.Fprintln(a.Stdout, "left; this session holds no node now")
	case 1:
		fmt.Fprintf(a.Stdout, "left; this session holds %s\n", rest[0])
	default:
		fmt.Fprintf(a.Stdout, "left; this session still holds:\n%s\n", listNodes(rest))
	}
	return nil
}

// reNode is the node of the message or answer a tell --re responds to, when
// this session holds it, else "".
func (a *App) reNode(re string) (string, error) {
	d, err := a.load()
	if err != nil {
		return "", err
	}
	e, err := d.Resolve(re)
	if err != nil {
		return "", err
	}
	node := e.Path
	if e.Kind == "answer" {
		node = desk.NodeOf(d.ByID[e.Ref])
	}
	for _, p := range heldPaths(a.held(d, ident.Resolve(a.Env))) {
		if p == node {
			return p, nil
		}
	}
	return "", nil
}
