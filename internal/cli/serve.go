package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/web"
)

// DefaultListen is where `serve` listens without --listen.
const DefaultListen = "127.0.0.1:7740"

// server is the web dashboard: a JSON API over the store and the built page.
type server struct {
	a     *App
	mu    sync.Mutex // one action at a time, as from one terminal
	ui    fs.FS
	index []byte
	csp   string
	hosts map[string]bool // Host header names accepted; nil accepts any
	// origins are the dashboard's public origins (scheme://host[:port]),
	// where a proxy that rewrites Host serves it.
	origins map[string]bool
	// forwarded trusts X-Forwarded-Host: set on a unix socket, which only
	// its owner, the proxy in front, can reach.
	forwarded bool
	poll      time.Duration // how often the live stream looks for new events
	stamp     func() (string, error)
	logger    io.Writer
	// manifest replaces the built manifest.json when the supervisor names
	// the device: its name and colour tell copies on two devices apart.
	manifest []byte
}

// brandManifest is the built web app manifest with the device in its name
// and the device's colour as its theme.
func brandManifest(base []byte, title, device, color string) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	if title == "" {
		title, _ = m["name"].(string)
	}
	if device != "" {
		m["name"] = title + " · " + device
	} else {
		m["name"] = title
	}
	if hexColor.MatchString(color) {
		m["theme_color"] = color
	}
	return json.MarshalIndent(m, "", "  ")
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// agentVars are hidden from actions taken in the browser: they are the
// user's, even when the server was started from an agent's shell.
var agentVars = map[string]bool{
	"AI_AGENT": true, "CLAUDE_CODE_SESSION_ID": true, "CODEX_THREAD_ID": true, "CODEX_SESSION_ID": true,
	"ANTIGRAVITY_CONVERSATION_ID": true, "PASEO_AGENT_ID": true, "TMUX_PANE": true, "TMUX_SUBAGENT_NODE": true,
	"CODEX_SANDBOX": true,
}

func newServer(a *App, ui fs.FS) (*server, error) {
	s := &server{a: a, ui: ui, poll: time.Second, logger: a.Stderr}
	index, err := fs.ReadFile(ui, "index.html")
	if err != nil {
		return nil, fmt.Errorf("the dashboard is not built: %w", err)
	}
	s.index = index
	s.csp = contentSecurityPolicy(index)
	s.stamp = func() (string, error) {
		st, err := a.store()
		if err != nil {
			return "", err
		}
		var n, max int64
		err = st.DB.QueryRow("SELECT count(*), coalesce(max(rowid), 0) FROM events").Scan(&n, &max)
		return fmt.Sprintf("%d.%d", n, max), err
	}
	return s, nil
}

var inlineScript = regexp.MustCompile(`(?s)<script(?:\s[^>]*)?>(.+?)</script>`)

// contentSecurityPolicy allows the page's own files and, by hash, any
// script inlined in index.html, and nothing from anywhere else.
func contentSecurityPolicy(index []byte) string {
	scripts := "'self'"
	for _, m := range inlineScript.FindAllSubmatch(index, -1) {
		sum := sha256.Sum256(m[1])
		scripts += " 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	}
	return "default-src 'self'; script-src " + scripts + "; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /api/view", s.apiView)
	mux.HandleFunc("GET /api/item", s.apiItem)
	mux.HandleFunc("GET /api/target", s.apiTarget)
	mux.HandleFunc("GET /api/stream", s.apiStream)
	mux.HandleFunc("GET /api/prefs", s.apiPrefs)
	mux.HandleFunc("GET /api/media/{file}", s.apiMedia)
	mux.HandleFunc("POST /api/pref", s.write(s.apiPref))
	mux.HandleFunc("POST /api/answer", s.write(s.apiAnswer))
	mux.HandleFunc("POST /api/reply", s.write(s.apiReply))
	mux.HandleFunc("POST /api/read", s.write(s.apiRead))
	mux.HandleFunc("POST /api/pin", s.write(s.apiPin))
	mux.HandleFunc("POST /api/unpin", s.write(s.apiUnpin))
	mux.HandleFunc("POST /api/unread", s.write(s.apiUnread))
	mux.HandleFunc("POST /api/task", s.write(s.apiTask))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		jsonError(w, http.StatusNotFound, "no such API")
	})
	mux.HandleFunc("/", s.static)
	return s.guard(mux)
}

// guard applies to every request: the Host check that defeats DNS
// rebinding on a TCP listener, and headers that keep the page to itself.
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.hosts != nil {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			if !s.hosts[strings.ToLower(strings.Trim(host, "[]"))] {
				http.Error(w, "agent-desk: unknown host "+r.Host+"; start serve with --allow-host", http.StatusMisdirectedRequest)
				return
			}
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether a write comes from the dashboard itself. A
// write must carry the X-Agent-Desk header and a JSON body, which a page on
// another origin cannot send without a CORS preflight this server never
// grants; a browser's Sec-Fetch-Site and Origin must agree as well.
func (s *server) sameOrigin(r *http.Request) error {
	if r.Header.Get("X-Agent-Desk") != "1" {
		return errors.New("missing X-Agent-Desk header")
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return errors.New("the body must be JSON")
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return fmt.Errorf("cross-site request (%s)", site)
	}
	if o := r.Header.Get("Origin"); o != "" && o != "null" {
		u, err := url.Parse(o)
		ok := err == nil && (strings.EqualFold(u.Host, r.Host) || s.origins[strings.ToLower(u.Scheme+"://"+u.Host)])
		if !ok && err == nil && s.forwarded {
			for _, fh := range strings.Split(r.Header.Get("X-Forwarded-Host"), ",") {
				if fh = strings.TrimSpace(fh); fh != "" && strings.EqualFold(u.Host, fh) {
					ok = true
				}
			}
		}
		if !ok {
			return fmt.Errorf("origin %s is not this dashboard", o)
		}
	}
	return nil
}

// request is the JSON body of a write.
type request struct {
	ID      string   `json:"id"`      // answer, unread: the item
	Text    string   `json:"text"`    // answer, reply
	To      string   `json:"to"`      // reply: an item or a node path
	Prompt  string   `json:"prompt"`  // reply: a prepared message instead of text (velocity-report)
	Target  string   `json:"target"`  // read: an item or a node path
	Targets []string `json:"targets"` // read, unread: several at once
	// Behavior: answer, reply: steer, queue or interrupt (empty: default).
	Behavior string `json:"behavior"`
	// Notify: answer, reply: {desktop, remote} for the response to this
	// message only.
	Notify map[string]string `json:"notify"`
	// pref: the override to set; a null value removes it.
	Scope string  `json:"scope"`
	Class string  `json:"class"`
	Key   string  `json:"key"`
	Value *string `json:"value"`
	// task: start, done, drop, defer, reopen or launch.
	Action string `json:"action"`
}

func (r request) notify() (map[string]string, error) {
	return checkNotify(r.Notify["desktop"], r.Notify["remote"])
}

type writeFunc func(app *App, body request) (any, error)

func (s *server) write(f writeFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.sameOrigin(r); err != nil {
			jsonError(w, http.StatusForbidden, err.Error())
			return
		}
		var body request
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			jsonError(w, http.StatusBadRequest, "bad JSON: "+err.Error())
			return
		}
		s.mu.Lock()
		app, out, errb := s.userApp()
		res, err := f(app, body)
		s.mu.Unlock()
		if err != nil {
			code := http.StatusUnprocessableEntity
			var u usageError
			if errors.As(err, &u) {
				code = http.StatusBadRequest
			}
			jsonError(w, code, err.Error())
			return
		}
		if res == nil {
			res = map[string]string{}
		}
		if m, ok := res.(map[string]string); ok {
			if o := strings.TrimSpace(out.String()); o != "" && o != "ok" && m["id"] == "" {
				m["id"] = o
			}
			if n := strings.TrimSpace(strings.ReplaceAll(errb.String(), "agent-desk: ", "")); n != "" {
				m["note"] = n
			}
		}
		writeJSON(w, res)
	}
}

// userApp is the App an action in the browser runs as.
func (s *server) userApp() (*App, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	app := *s.a
	get := s.a.Env.Getenv
	app.Env.Getenv = func(k string) string {
		if agentVars[k] || get == nil {
			return ""
		}
		return get(k)
	}
	app.Env.Tmux = nil
	app.Stdin, app.Stdout, app.Stderr = strings.NewReader(""), &out, &errb
	return &app, &out, &errb
}

func (s *server) apiAnswer(app *App, body request) (any, error) {
	d, err := app.load()
	if err != nil {
		return nil, err
	}
	q, err := d.Resolve(body.ID)
	if err != nil {
		return nil, err
	}
	if q.Kind != "ask" {
		return nil, fmt.Errorf("%s is a %s, not a question", d.Display(q.ID), q.Kind)
	}
	notify, err := body.notify()
	if err != nil {
		return nil, err
	}
	return map[string]string{}, app.recordAnswer(d, q, body.Text, "web", body.Behavior, notify)
}

func (s *server) apiReply(app *App, body request) (any, error) {
	notify, err := body.notify()
	if err != nil {
		return nil, err
	}
	text := body.Text
	if body.Prompt != "" {
		if text != "" {
			return nil, usagef("give a text or a prompt, not both")
		}
		if text, err = app.replyPrompt(body.To, body.Prompt); err != nil {
			return nil, err
		}
	}
	e, err := app.recordReply(body.To, text, "web", body.Behavior, notify)
	if err != nil {
		return nil, err
	}
	d, err := app.load()
	if err != nil {
		return nil, err
	}
	return map[string]string{"id": d.Display(e.ID)}, nil
}

func (s *server) apiRead(app *App, body request) (any, error) {
	return map[string]string{}, app.run(append([]string{"read", "--"}, body.list(body.Target)...))
}

func (s *server) apiPin(app *App, body request) (any, error) {
	return map[string]string{}, app.run(append([]string{"pin", "--"}, body.list(body.ID)...))
}

func (s *server) apiUnpin(app *App, body request) (any, error) {
	return map[string]string{}, app.run(append([]string{"unpin", "--"}, body.list(body.ID)...))
}

func (s *server) apiUnread(app *App, body request) (any, error) {
	return map[string]string{}, app.run(append([]string{"unread", "--"}, body.list(body.ID)...))
}

// apiTask changes tasks' state, or launches them; the response's "out" is
// what launch printed.
func (s *server) apiTask(app *App, body request) (any, error) {
	switch body.Action {
	case "start", "done", "drop", "defer", "reopen", "launch":
	default:
		return nil, usagef("task action is start, done, drop, defer, reopen or launch")
	}
	ids := body.list(body.ID)
	if len(ids) == 0 {
		return nil, usagef("task needs an id")
	}
	err := app.run(append([]string{"task", body.Action, "--"}, ids...))
	return map[string]string{}, err
}

// list is the targets of a bulk write: one, or several.
func (r request) list(one string) []string {
	var out []string
	if one != "" {
		out = append(out, one)
	}
	return append(out, r.Targets...)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// load reads the desk for a GET.
func (s *server) load(w http.ResponseWriter) (*desk.Desk, bool) {
	s.mu.Lock()
	d, err := s.a.load()
	s.mu.Unlock()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return d, true
}

// Session describes one agent session, for the session filter's header.
type Session struct {
	ID      string `json:"id"`
	Agent   string `json:"agent,omitempty"`
	Paseo   string `json:"paseo,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Node    string `json:"node,omitempty"`
	Host    string `json:"host,omitempty"`
	Resume  string `json:"resume,omitempty"`
	LastAge string `json:"last_age,omitempty"`
}

type viewResponse struct {
	Host   string     `json:"host"`
	Stamp  string     `json:"stamp"`
	Path   string     `json:"path"`
	Filter viewFilter `json:"filter"`
	View   desk.View  `json:"view"`
	Node   *desk.Node `json:"node,omitempty"`
	Target *Target    `json:"target,omitempty"`
	// Conversation is the target node's own thread: messages to the node
	// and the responses to them, oldest first.
	Conversation []Entry  `json:"conversation,omitempty"`
	Session      *Session `json:"session,omitempty"`
	Projects     []string `json:"projects"`
	Hosts        []string `json:"hosts"`
	// Deferred are the tasks waiting for quota under the path; Launch says
	// whether launch_command is set and how many tasks one launch takes.
	Deferred []desk.Task `json:"deferred"`
	Launch   launchInfo  `json:"launch"`
}

type launchInfo struct {
	Ready bool `json:"ready"`
	Max   int  `json:"max"`
}

type viewFilter struct {
	Host    string `json:"host,omitempty"`
	Session string `json:"session,omitempty"`
	All     bool   `json:"all"`
	Query   string `json:"q,omitempty"`
}

func (s *server) apiView(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d, ok := s.load(w)
	if !ok {
		return
	}
	stamp, _ := s.stamp()
	f := desk.Filter{
		Prefix:  strings.Trim(q.Get("path"), "/"),
		Host:    q.Get("host"),
		Session: q.Get("session"),
		All:     q.Get("all") == "1" || q.Get("all") == "true",
	}
	if l := q.Get("linger"); l != "" {
		f.Linger = map[string]bool{}
		for i, id := range strings.Split(l, ",") {
			if i == 200 {
				break
			}
			f.Linger[strings.TrimSpace(id)] = true
		}
	}
	v := d.Build(f)
	query := strings.TrimSpace(q.Get("q"))
	if query != "" {
		v.NeedsYou = searchGroups(d, v.NeedsYou, query)
		v.Responses = searchGroups(d, v.Responses, query)
		v.New = searchGroups(d, v.New, query)
		pinned := []desk.Item{}
		for _, g := range searchGroups(d, []desk.Group{{Items: v.Pinned}}, query) {
			pinned = g.Items
		}
		v.Pinned = pinned
		v.Runs = searchTree(v.Runs, strings.ToLower(query), nodeSessions(d))
	}
	resp := viewResponse{
		Host: d.Host, Stamp: stamp, Path: f.Prefix, View: v,
		Filter: viewFilter{Host: f.Host, Session: f.Session, All: f.All, Query: query},
	}
	projects, hosts := map[string]bool{}, map[string]bool{}
	for _, e := range d.Events {
		p, _, _ := desk.Split(e.Path)
		projects[p] = true
		hosts[e.Host] = true
	}
	resp.Projects, resp.Hosts = sortedKeys(projects), sortedKeys(hosts)
	resp.Deferred = []desk.Task{}
	lq := strings.ToLower(query)
	for _, t := range d.Tasks(desk.Filter{Prefix: f.Prefix, Host: f.Host}) {
		if t.State == desk.TaskDeferred && (lq == "" || strings.Contains(strings.ToLower(t.Event.Title+"\n"+t.Event.Body), lq)) {
			resp.Deferred = append(resp.Deferred, t)
		}
	}
	s.mu.Lock()
	if c, err := s.a.config(); err == nil {
		resp.Launch = launchInfo{Ready: c.Tasks.LaunchCommand != "", Max: c.Tasks.Max()}
	}
	s.mu.Unlock()
	if f.Prefix != "" {
		all := f
		all.All = true
		resp.Node = desk.Find(d.Tree(all), f.Prefix)
		if t, err := targetOf(d, f.Prefix); err == nil {
			resp.Target = &t
		} else if resp.Node != nil && len(resp.Node.Children) == 1 {
			// A run's one coordinator speaks for the run.
			if t, err := targetOf(d, resp.Node.Children[0].Path); err == nil {
				resp.Target = &t
			}
		}
		if resp.Target != nil {
			s.behaviors(d, resp.Target)
			resp.Conversation = entries(d, d.Thread("", resp.Target.Node), s.sshHosts())
		}
	}
	if f.Session != "" {
		resp.Session = sessionInfo(d, f.Session)
	}
	writeJSON(w, resp)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func searchGroups(d *desk.Desk, gs []desk.Group, query string) []desk.Group {
	query = strings.ToLower(query)
	out := []desk.Group{}
	for _, g := range gs {
		var items []desk.Item
		for _, it := range g.Items {
			e := it.Event
			f := fromOf(e)
			hay := strings.ToLower(it.ID + "\n" + e.Path + "\n" + e.Title + "\n" + e.Body + "\n" + f.Session + "\n" + f.Paseo)
			if strings.Contains(hay, query) {
				items = append(items, it)
			}
		}
		if len(items) > 0 {
			g.Items = items
			out = append(out, g)
		}
	}
	return out
}

// nodeSessions maps each node to the ids of the sessions that joined it
// (native session ids and Paseo agent ids), lowercased, one per line.
func nodeSessions(d *desk.Desk) map[string]string {
	m := map[string]string{}
	for _, e := range d.Events {
		if e.Kind == "join" {
			f := fromOf(e)
			m[e.Path] += strings.ToLower(f.Session + "\n" + f.Paseo + "\n")
		}
	}
	return m
}

// searchTree keeps the nodes whose path, or the id of a session that joined
// them, matches, with their ancestors, so a search finds a project, run or
// agent in the tree, also by its Paseo or session id.
func searchTree(nodes []*desk.Node, query string, sessions map[string]string) []*desk.Node {
	out := []*desk.Node{}
	for _, n := range nodes {
		if strings.Contains(strings.ToLower(n.Path), query) || strings.Contains(sessions[n.Path], query) {
			out = append(out, n)
			continue
		}
		if kids := searchTree(n.Children, query, sessions); len(kids) > 0 {
			c := *n
			c.Children = kids
			out = append(out, &c)
		}
	}
	return out
}

// sessionInfo describes the session behind a session filter, from the
// latest event it wrote.
func sessionInfo(d *desk.Desk, id string) *Session {
	for i := len(d.Events) - 1; i >= 0; i-- {
		e := d.Events[i]
		if !desk.IsSession(e, id) {
			continue
		}
		f := fromOf(e)
		return &Session{ID: id, Agent: f.Agent, Paseo: f.Paseo, Cwd: f.Cwd, Node: desk.NodeOf(e), Host: e.Host,
			Resume: ResumeCommand(f), LastAge: desk.Age(d.Now.Sub(desk.Time(e)))}
	}
	return &Session{ID: id}
}

// Target says where a reply would go.
type Target struct {
	Agent   string `json:"agent,omitempty"`
	Session string `json:"session,omitempty"`
	Paseo   string `json:"paseo,omitempty"`
	Host    string `json:"host"`
	Node    string `json:"node"`
	Via     string `json:"via"` // paseo, tmux, command, merge (another machine) or none
	Resume  string `json:"resume,omitempty"`
	// Behaviors: steer, queue and interrupt, each supported or not, and
	// why; filled by withBehaviors.
	Behaviors       []Support `json:"behaviors,omitempty"`
	DefaultBehavior string    `json:"default_behavior,omitempty"`
	// Notify is where the response to a message would notify:
	// {desktop, remote}, resolved for class response at the node.
	Notify map[string]string `json:"notify,omitempty"`
	// Finished: done (the node has a done report since it joined) or
	// archived (its Paseo agent is); sending revives the session.
	Finished string `json:"finished,omitempty"`
	// Pushed: the host is in ssh_hosts, so a message is pushed there at once.
	Pushed bool `json:"pushed,omitempty"`
	// RemoteLabel is the config's name for the remote channel.
	RemoteLabel string `json:"remote_label,omitempty"`
	to          ident.From
}

// sshHosts is the config's ssh_hosts, under the server's lock.
func (s *server) sshHosts() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.a.config()
	if err != nil {
		return nil
	}
	return c.SSHHosts
}

// behaviors fills in a target's behaviours under the server's lock.
func (s *server) behaviors(d *desk.Desk, t *Target) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.a.withBehaviors(d, t)
}

// withBehaviors fills in what the target can do with a message.
// The default is the deliver.behavior setting for the target's node.
func (a *App) withBehaviors(d *desk.Desk, t *Target) {
	if c, err := a.config(); err == nil && t.Via == "none" && c.DeliverCommand != "" {
		t.Via = "command"
	}
	t.Behaviors = a.supports(t.Via, t.to)
	t.DefaultBehavior = "steer"
	if r, err := a.settings(d); err == nil {
		t.DefaultBehavior, _ = r.Resolve("deliver.behavior", t.Node, "")
		t.Notify = map[string]string{}
		t.Notify["desktop"], _ = r.Resolve("notify.desktop", t.Node, "response")
		t.Notify["remote"], _ = r.Resolve("notify.remote", t.Node, "response")
	}
	t.Finished = a.finished(d, t)
	if c, err := a.config(); err == nil {
		_, t.Pushed = c.SSHHosts[t.Host]
		t.RemoteLabel = c.RemoteLabel
	}
}

func targetOf(d *desk.Desk, token string) (Target, error) {
	to, host, node, _, err := replyTarget(d, token)
	if err != nil {
		return Target{}, err
	}
	t := Target{Agent: to.Agent, Session: to.Session, Paseo: to.Paseo, Host: host, Node: node, Resume: ResumeCommand(to), to: to}
	switch {
	case host != d.Host:
		t.Via = "merge"
	case to.Paseo != "":
		t.Via = "paseo"
	case to.TmuxPane != "":
		t.Via = "tmux"
	default:
		t.Via = "none"
	}
	return t, nil
}

func (s *server) apiTarget(w http.ResponseWriter, r *http.Request) {
	d, ok := s.load(w)
	if !ok {
		return
	}
	t, err := targetOf(d, r.URL.Query().Get("to"))
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	s.behaviors(d, &t)
	writeJSON(w, t)
}

type itemResponse struct {
	Item   desk.Item  `json:"item"`
	Node   string     `json:"node"`
	Slot   string     `json:"slot,omitempty"`
	From   ident.From `json:"from"`
	Resume string     `json:"resume,omitempty"`
	Thread []Entry    `json:"thread"`
	// ThreadOf: the thread shown is not this item's own but that of the
	// item (display id) or node (path) it belongs to; ThreadTitle names it.
	ThreadOf    string      `json:"thread_of,omitempty"`
	ThreadTitle string      `json:"thread_title,omitempty"`
	Target      *Target     `json:"target,omitempty"`
	File        string      `json:"file,omitempty"`
	FileText    string      `json:"file_text,omitempty"`
	Report      string      `json:"report,omitempty"`
	Default     string      `json:"default,omitempty"`
	Answered    bool        `json:"answered"`
	Superseded  string      `json:"superseded,omitempty"` // a newer text of this slot
	Media       []MediaView `json:"media,omitempty"`
	Task        *desk.Task  `json:"task,omitempty"`
	LaunchReady bool        `json:"launch_ready,omitempty"`
}

func (s *server) apiItem(w http.ResponseWriter, r *http.Request) {
	d, ok := s.load(w)
	if !ok {
		return
	}
	e, err := d.Resolve(r.URL.Query().Get("id"))
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	it := d.Item(e)
	it.Dismissed = d.Dismissed(e.ID) && isOpen(d, e.ID)
	for i := range d.Questions {
		if d.Questions[i].ID == e.ID {
			q := d.Questions[i]
			it.Question = &q
		}
	}
	f := fromOf(e)
	resp := itemResponse{Item: it, Node: desk.NodeOf(e), From: f, Resume: ResumeCommand(f), Thread: []Entry{},
		Report: desk.MetaString(e, "report"), Default: desk.MetaString(e, "default"),
		Answered: it.Question != nil && !it.Question.Open()}
	if e.Kind == "task" {
		for _, t := range d.Tasks(desk.Filter{}) {
			if t.Event.ID == e.ID {
				t := t
				resp.Task = &t
			}
		}
		s.mu.Lock()
		if c, err := s.a.config(); err == nil {
			resp.LaunchReady = c.Tasks.LaunchCommand != ""
		}
		s.mu.Unlock()
	}
	if desk.IsSlotKind(e.Kind) {
		resp.Slot = desk.SlotName(e)
		for _, x := range d.Events {
			if x.Path == e.Path && x.ID > e.ID && desk.IsSlotKind(x.Kind) {
				resp.Superseded = d.Display(x.ID)
			}
		}
	}
	if file := desk.MetaString(e, "file"); file != "" {
		resp.File = file
		resp.FileText = fileText(file)
	}
	thread := d.Thread(e.ID, "")
	if e.Kind == "reply" || e.Kind == "answer" || desk.IsResponse(e) {
		// A message or response is shown in the conversation it belongs to.
		if root, ok := d.Root(e); ok {
			thread, resp.ThreadOf, resp.ThreadTitle = d.Thread(root.ID, ""), d.Display(root.ID), root.Title
		} else if e.Ref != "" || e.Kind == "reply" {
			thread, resp.ThreadOf = d.Thread("", desk.NodeOf(e)), desk.NodeOf(e)
		}
	}
	resp.Media = s.a.mediaViews(d, e)
	resp.Thread = entries(d, thread, s.sshHosts())
	for i := range resp.Thread {
		resp.Thread[i].Media = s.a.mediaViews(d, resp.Thread[i].Event)
	}
	if t, err := targetOf(d, e.ID); err == nil {
		s.behaviors(d, &t)
		resp.Target = &t
	}
	writeJSON(w, resp)
}

// apiStream tells the page when the store changes, as server-sent events.
func (s *server) apiStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	last, _ := s.stamp()
	fmt.Fprintf(w, "retry: 3000\nevent: hello\ndata: %s\n\n", last)
	fl.Flush()
	tick := time.NewTicker(s.poll)
	defer tick.Stop()
	beat := time.NewTicker(25 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-beat.C:
			io.WriteString(w, ": keepalive\n\n")
			fl.Flush()
		case <-tick.C:
			cur, err := s.stamp()
			if err != nil || cur == last {
				continue
			}
			last = cur
			fmt.Fprintf(w, "event: change\ndata: %s\n\n", cur)
			fl.Flush()
		}
	}
}

// static serves the page and its assets. Vite names every asset by its
// content hash, so they are cached for good; index.html never is.
func (s *server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" || p == "index.html" {
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Security-Policy", s.csp)
		w.Write(s.index)
		return
	}
	if p == "manifest.json" && s.manifest != nil {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, p, time.Time{}, bytes.NewReader(s.manifest))
		return
	}
	b, err := fs.ReadFile(s.ui, p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(p, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, p, time.Time{}, bytes.NewReader(b))
}

func (a *App) cmdServe(args []string) error {
	o, err := parse(args, nil, []string{"listen", "allow-host", "pid-file", "config", "allow-origin"}, 0, 0)
	if err != nil {
		return err
	}
	// --config names a JSON file a process supervisor writes; its listen,
	// pid_file and allow_hosts apply unless the flag is given too; title,
	// device and color brand the web app manifest. Other keys are the
	// supervisor's and are ignored.
	var brand struct{ Title, Device, Color string }
	if file := o.vals["config"]; file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		var c struct {
			Listen     string   `json:"listen"`
			PidFile    string   `json:"pid_file"`
			AllowHosts []string `json:"allow_hosts"`
			Origin     string   `json:"origin"`
			Title      string   `json:"title"`
			Device     string   `json:"device"`
			Color      string   `json:"color"`
		}
		if err := json.Unmarshal(b, &c); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		brand.Title, brand.Device, brand.Color = c.Title, c.Device, c.Color
		for k, v := range map[string]string{"listen": c.Listen, "pid-file": c.PidFile,
			"allow-host": strings.Join(c.AllowHosts, ","), "allow-origin": c.Origin} {
			if _, set := o.vals[k]; !set && v != "" {
				o.vals[k] = v
			}
		}
	}
	if _, err := a.store(); err != nil {
		return err
	}
	s, err := newServer(a, web.Dist())
	if err != nil {
		return err
	}
	if brand.Title != "" || brand.Device != "" || brand.Color != "" {
		base, err := fs.ReadFile(s.ui, "manifest.json")
		if err != nil {
			return err
		}
		if s.manifest, err = brandManifest(base, brand.Title, brand.Device, brand.Color); err != nil {
			return fmt.Errorf("manifest.json: %w", err)
		}
	}
	s.origins = map[string]bool{}
	for _, or := range strings.Split(o.vals["allow-origin"], ",") {
		if or = strings.ToLower(strings.TrimRight(strings.TrimSpace(or), "/")); or != "" {
			if u, err := url.Parse(or); err == nil && u.Host != "" {
				s.origins[u.Scheme+"://"+u.Host] = true
			}
		}
	}
	listen := o.vals["listen"]
	if listen == "" {
		listen = DefaultListen
	}
	var ln net.Listener
	shown := ""
	if sock, ok := strings.CutPrefix(listen, "unix:"); ok {
		// A browser cannot reach a unix socket, so no Host check: the proxy
		// in front of it sends its own public name.
		if fi, err := os.Lstat(sock); err == nil && fi.Mode()&os.ModeSocket != 0 {
			os.Remove(sock)
		}
		if ln, err = net.Listen("unix", sock); err != nil {
			return err
		}
		os.Chmod(sock, 0o600)
		s.forwarded = true
		defer os.Remove(sock)
		shown = "unix:" + sock
	} else {
		listen = strings.TrimPrefix(listen, "tcp:")
		if ln, err = net.Listen("tcp", listen); err != nil {
			return err
		}
		s.hosts = map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
		if h, _, err := net.SplitHostPort(listen); err == nil && h != "" {
			s.hosts[strings.ToLower(h)] = true
		}
		for _, h := range strings.Split(o.vals["allow-host"], ",") {
			if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
				s.hosts[h] = true
			}
		}
		shown = "http://" + ln.Addr().String() + "/"
	}
	if pf := o.vals["pid-file"]; pf != "" {
		if err := os.WriteFile(pf, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
			return err
		}
		defer os.Remove(pf)
	}
	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	fmt.Fprintf(a.Stderr, "agent-desk: serving %s on %s\n", a.Home, shown)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
