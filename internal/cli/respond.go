package cli

import (
	"strings"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// responding is how an agent item responds to the user, whatever its kind
// (desk.AgentItem): to a message or an answer on the desk (--re), or to
// something asked in chat (--re-chat). Either puts the item in that
// thread. tell, done and deliverable share it; ask has its own because a
// question response also needs a level.
type responding struct {
	re, asked string
}

func respondingOf(o opts) (responding, error) {
	r := responding{re: strings.TrimSpace(o.vals["re"]), asked: strings.TrimSpace(o.vals["re-chat"])}
	if o.set["re"] && r.re == "" {
		return r, usagef("--re needs the id of the message or answer you respond to")
	}
	if o.set["re-chat"] && r.asked == "" {
		return r, usagef("--re-chat needs what the user asked in chat, quoted or in a line")
	}
	if r.re != "" && r.asked != "" {
		return r, usagef("give --re (a message on the desk) or --re-chat (a question asked in chat), not both")
	}
	return r, nil
}

func (r responding) on() bool { return r.re != "" || r.asked != "" }

// node is the node to write at: nodeArg when given, else the node of the
// message or answer responded to when this session holds it.
func (a *App) respondingNode(r responding, nodeArg string) (string, error) {
	if r.re == "" || nodeArg != "" {
		return nodeArg, nil
	}
	node, err := a.reNode(r.re)
	if err != nil {
		return "", err
	}
	if node != "" {
		return node, nil
	}
	return nodeArg, nil
}

// apply records the response on extra and returns the ref and a default
// title ("Re: <what it responds to>"). It refuses a --re that is not a
// message or answer sent to node.
func (r responding) apply(d *desk.Desk, node string, extra map[string]any) (ref, title string, err error) {
	if r.asked != "" {
		extra["asked"] = r.asked
		return "", "Re: " + clipLine(r.asked, 60), nil
	}
	if r.re == "" {
		return "", "", nil
	}
	return linkResponse(d, r.re, node, extra)
}

// cmdDeliverable files a deliverable: something the user can open or
// download now (a deployment, an Android package, a release), with how to
// try it on stdin. It is listed in Deliverables, above Responses, and may
// respond to a message like any agent item.
func (a *App) cmdDeliverable(args []string) error {
	o, err := parse(args, []string{"quiet"}, append([]string{"title", "url", "key", "project", "node", "re", "re-chat"}, attachOpts...), 0, 0)
	if err != nil {
		return err
	}
	r, err := respondingOf(o)
	if err != nil {
		return err
	}
	if o.bools["quiet"] && !r.on() {
		return usagef("--quiet goes with --re or --re-chat")
	}
	title := strings.TrimSpace(o.vals["title"])
	if title == "" {
		return usagef("deliverable needs --title: what it is, such as 'Android APK 1.4.2' or 'Site deployed to staging'")
	}
	link := strings.TrimSpace(o.vals["url"])
	if link != "" && !desk.OpenableURL(link) {
		return usagef("--url must be an http or https address, or an app link such as myapp://open?x=1 (not javascript:, vbscript:, file: or data:), not %q", link)
	}
	hasFile := false
	for _, kv := range o.seq {
		hasFile = hasFile || kv[0] == "attach"
	}
	if link == "" && !hasFile {
		return usagef("a deliverable is something to open or download: give --url, --attach FILE --caption '...', or both (anything else is a tell)")
	}
	body, err := a.stdinText()
	if err != nil {
		return err
	}
	extra := map[string]any{}
	if link != "" {
		extra["url"] = link
	}
	if k := strings.TrimSpace(o.vals["key"]); k != "" {
		extra["key"] = k
	}
	if o.bools["quiet"] {
		extra["quiet"] = true
	}
	if err := a.attach(o, extra); err != nil {
		return err
	}
	nodeArg, err := a.respondingNode(r, o.vals["node"])
	if err != nil {
		return err
	}
	return a.agentWrite(o.vals["project"], nodeArg, "deliverable", func(from ident.From, node string, d *desk.Desk) ([]store.Event, error) {
		ref, _, err := r.apply(d, node, extra)
		if err != nil {
			return nil, err
		}
		e := a.event("deliverable", node, from, "cli", extra)
		e.Title, e.Body, e.Ref = title, body, ref
		return []store.Event{e}, nil
	})
}
