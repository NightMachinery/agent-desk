package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/media"
	"github.com/NightMachinery/agent-desk/internal/store"
	"github.com/NightMachinery/agent-desk/web"
)

// DefaultListen is where `serve` listens without --listen.
const DefaultListen = "127.0.0.1:7740"

// server is the web dashboard: a JSON API over the store and the built page.
type server struct {
	// build names the dashboard this server serves (a hash of index.html,
	// which names every hashed asset): the stream sends it, so a page left
	// open across an upgrade offers to reload.
	build           string
	closing         chan struct{} // closed on shutdown: event streams end
	acNudge         chan struct{} // an auto-continue setting changed: apply the defaults now
	closeOnce       sync.Once
	voice           voiceHub
	a               *App
	mu              sync.Mutex // protects the shared App used by reads
	writeMu         sync.Mutex // serialises read-modify-write actions
	sendMu          sync.Mutex // protects queued post-commit effects
	sendEffects     [][]store.Event
	cacheMu         sync.Mutex
	cacheAt         time.Time
	cacheList       cacheResponse
	cacheRefreshing bool
	seatMu          sync.Mutex
	seatAt          time.Time
	seats           map[string]QuotaSeat
	seatRefreshing  bool
	compacting      map[string]bool // one keeper request in flight per agent
	sendWake        chan struct{}
	ui              fs.FS
	index           []byte
	csp             string
	hosts           map[string]bool // Host header names accepted; nil accepts any
	gzipAssets      sync.Map        // path to compressed bytes for immutable scripts and styles
	// origins are the dashboard's public origins (scheme://host[:port]),
	// where a proxy that rewrites Host serves it.
	origins map[string]bool
	// forwarded trusts X-Forwarded-Host: set on a unix socket, which only
	// its owner, the proxy in front, can reach.
	forwarded bool
	// quota caches the stopped agents for a little while: listing them
	// reads every agent's last turn.
	quotaMu   sync.Mutex
	quotaAt   time.Time
	quotaResp map[string]any
	// The provider choices for Replace (apiProviders).
	provMu         sync.Mutex
	provResp       []ProviderChoice
	provAt         time.Time
	provRefreshing bool
	// The cached desk contains only store-derived state. Each request gets
	// its own time and live statuses.
	readMu    sync.Mutex
	readDesk  *desk.Desk
	readStamp string
	poll      time.Duration // how often the live stream looks for new events
	org       orgState      // the org files' renders (orgfiles.go)
	taskRetry time.Time     // after a failed send, when to tell task changes again (taskchanges.go)
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
	s := &server{a: a, ui: ui, poll: time.Second, logger: a.Stderr, sendWake: make(chan struct{}, 1), closing: make(chan struct{}), acNudge: make(chan struct{}, 1)}
	index, err := fs.ReadFile(ui, "index.html")
	if err != nil {
		return nil, fmt.Errorf("the dashboard is not built: %w", err)
	}
	s.index = index
	sum := sha256.Sum256(index)
	s.build = hex.EncodeToString(sum[:6])
	s.csp = contentSecurityPolicy(index)
	st, err := a.store()
	if err != nil {
		return nil, err
	}
	s.stamp = func() (string, error) {
		var n, max int64
		// Separate subqueries let SQLite use its fast COUNT and MAX
		// paths, while keeping the two values in the same read snapshot.
		err := st.DB.QueryRow(`SELECT (SELECT count(*) FROM events),
			coalesce((SELECT max(rowid) FROM events), 0)`).Scan(&n, &max)
		return fmt.Sprintf("%d.%d", n, max), err
	}
	return s, nil
}

var inlineScript = regexp.MustCompile(`(?s)<script(?:\s[^>]*)?>(.+?)</script>`)

// contentSecurityPolicy allows the page's own files and, by hash, any
// script inlined in index.html, and nothing from anywhere else. blob:
// images are the page's own previews of files about to be attached.
func contentSecurityPolicy(index []byte) string {
	scripts := "'self'"
	for _, m := range inlineScript.FindAllSubmatch(index, -1) {
		sum := sha256.Sum256(m[1])
		scripts += " 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	}
	return "default-src 'self'; script-src " + scripts + "; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
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
	mux.HandleFunc("GET /api/verbatim", s.apiVerbatim)
	mux.HandleFunc("GET /api/tasks", s.apiTasks)
	mux.HandleFunc("GET /api/stream", s.apiStream)
	mux.HandleFunc("GET /api/prefs", s.apiPrefs)
	mux.HandleFunc("GET /api/keys", s.apiKeys)
	mux.HandleFunc("GET /api/media/{file}", s.apiMedia)
	mux.HandleFunc("POST /api/pref", s.write(s.apiPref))
	mux.HandleFunc("POST /api/upload", s.apiUpload)
	mux.HandleFunc("POST /api/answer", s.action(s.apiAnswer))
	mux.HandleFunc("POST /api/reply", s.action(s.apiReply))
	mux.HandleFunc("POST /api/read", s.write(s.apiRead))
	mux.HandleFunc("POST /api/deliver", s.action(s.apiDeliver))
	mux.HandleFunc("POST /api/pin", s.write(s.apiPin))
	mux.HandleFunc("POST /api/unpin", s.write(s.apiUnpin))
	mux.HandleFunc("POST /api/unread", s.write(s.apiUnread))
	mux.HandleFunc("POST /api/task", s.write(s.apiTask))
	mux.HandleFunc("POST /api/screen", s.write(s.apiScreen))
	mux.HandleFunc("POST /api/sets", s.write(s.apiSets))
	mux.HandleFunc("GET /api/push", s.apiPush)
	mux.HandleFunc("POST /api/push", s.apiPushAct)
	mux.HandleFunc("GET /api/quota", s.apiQuota)
	mux.HandleFunc("POST /api/quota", s.action(s.apiQuotaAct))
	mux.HandleFunc("GET /api/providers", s.apiProviders)
	mux.HandleFunc("POST /api/agent", s.action(s.apiAgentAct))
	mux.HandleFunc("GET /api/draft", s.apiDraft)
	mux.HandleFunc("GET /api/drafts", s.apiDrafts)
	mux.HandleFunc("POST /api/draft", s.action(s.apiDraftPut))
	mux.HandleFunc("GET /api/dashboard-prefs", s.apiDashboardPrefs)
	mux.HandleFunc("POST /api/dashboard-prefs", s.action(s.apiDashboardPrefPut))
	mux.HandleFunc("GET /api/auto-continue", s.apiAutoContinue)
	mux.HandleFunc("GET /api/render", s.apiRender)
	mux.HandleFunc("POST /api/render", s.action(s.apiRenderNow))
	mux.HandleFunc("GET /api/task-changes", s.apiTaskChanges)
	mux.HandleFunc("GET /api/export", s.apiExport)
	s.voiceRoutes(mux)
	mux.HandleFunc("GET /api/cache", s.apiCache)
	mux.HandleFunc("POST /api/cache", s.action(s.apiCachePolicy))
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
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return errors.New("the body must be JSON")
	}
	return s.sameOriginAny(r)
}

// sameOriginAny is sameOrigin for a body of any type (an upload). The
// custom header alone already forces a cross-site page into a preflight.
func (s *server) sameOriginAny(r *http.Request) error {
	if r.Header.Get("X-Agent-Desk") != "1" {
		return errors.New("missing X-Agent-Desk header")
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
	Force           bool     `json:"force"`      // deliver: retry even if the agent may already have it
	FailureID       string   `json:"failure_id"` // deliver: the failure shown when the user confirmed
	SendKey         string   `json:"send_key"`   // idempotency key for answer and reply
	ID              string   `json:"id"`         // answer, unread: the item
	Text            string   `json:"text"`       // answer, reply
	To              string   `json:"to"`         // reply: an item or a node path
	Prompt          string   `json:"prompt"`     // reply: a prepared message instead of text (velocity-report)
	Auto            string   `json:"auto"`       // reply: a generated message (task-filed), see markAuto
	Target          string   `json:"target"`     // read: an item or a node path
	Targets         []string `json:"targets"`    // read, unread: several at once
	AfterCompaction bool     `json:"after_compaction,omitempty"`
	WarmHours       float64  `json:"warm_hours,omitempty"`
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
	// task: start, done, drop, defer, reopen or launch; screen and quota
	// have their own.
	Action  string `json:"action"`
	Profile string `json:"profile"` // quota replace: the new agent's provider[/model][@thinking]
	// task add: Title, Type and Parent (a task id); To is the node, Text
	// the brief.
	Title         string            `json:"title"`
	Type          string            `json:"type"`
	Parent        string            `json:"parent"`
	Tags          []string          `json:"tags"`           // task add, task tags: free tags
	QuotaActivity map[string]string `json:"quota_activity"` // last activity of each selected quota agent
	// sets: define, delete or focus a project set by Name (focus with an
	// empty Name turns it off); define takes Prefixes.
	Name     string   `json:"name"`
	Prefixes []string `json:"prefixes"`
	// Media: answer, reply: files sent with it, as /api/upload returned them.
	Media           []media.Ref `json:"media"`
	VoiceRecordings []string    `json:"voice_recordings,omitempty"`
	// draft: Key names the box, Draft is its JSON value (null drops it),
	// Device the browser that typed it.
	Draft  json.RawMessage `json:"draft"`
	Device string          `json:"device"`
	// dashboard-prefs: Key names the shared setting, Data is its JSON.
	Data json.RawMessage `json:"data"`
}

func (r request) notify() (map[string]string, error) {
	return checkNotify(r.Notify["desktop"], r.Notify["remote"])
}

type writeFunc func(app *App, body request) (any, error)

func (s *server) write(f writeFunc) http.HandlerFunc {
	return s.action(func(app *App, body request) (any, error) {
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
		return f(app, body)
	})
}

func (s *server) action(f writeFunc) http.HandlerFunc {
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
		app, out, errb := s.userApp()
		res, err := f(app, body)
		if err != nil {
			code := http.StatusUnprocessableEntity
			var u usageError
			var unavailable sendUnavailable
			switch {
			case errors.As(err, &unavailable):
				code = http.StatusServiceUnavailable
			case errors.As(err, &u):
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
	base := s.a
	app := App{Env: base.Env, Now: base.Now, Host: base.Host, Home: base.Home,
		WaitHooks: base.WaitHooks, Exec: base.Exec, HTTP: base.HTTP, st: base.st, storedDesk: s.storedDesk}
	get := base.Env.Getenv
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
	return s.enqueue(app, "answer", body, func() ([]store.Event, error) {
		return answerFromRequest(app, body)
	})
}

func answerFromRequest(app *App, body request) ([]store.Event, error) {
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
	refs, err := app.uploaded(body.Media)
	if err != nil {
		return nil, err
	}
	return app.answerEvents(d, q, body.Text, "web", body.Behavior, notify, refs...)
}

func (s *server) apiReply(app *App, body request) (any, error) {
	return s.enqueue(app, "reply", body, func() ([]store.Event, error) {
		return replyFromRequest(app, body)
	})
}

func replyFromRequest(app *App, body request) ([]store.Event, error) {
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
	refs, err := app.uploaded(body.Media)
	if err != nil {
		return nil, err
	}
	auto := body.Prompt
	if body.Auto != "" {
		if !autoKinds[body.Auto] || auto != "" {
			return nil, usagef("auto is task-filed, without a prompt")
		}
		auto = body.Auto
	}
	e, err := app.replyEvent(body.To, text, "web", body.Behavior, notify, refs...)
	if err != nil {
		return nil, err
	}
	markAuto(&e, auto)
	return []store.Event{e}, nil
}

func (s *server) apiRead(app *App, body request) (any, error) {
	return map[string]string{}, app.run(append([]string{"read", "--"}, body.list(body.Target)...))
}

// apiDeliver retries delivering one answer (named by itself or its
// question) or reply: the Retry button beside "not delivered".
func (s *server) apiDeliver(app *App, body request) (any, error) {
	d, err := app.load()
	if err != nil {
		return nil, err
	}
	e, err := deliveryTarget(d, body.ID)
	if err != nil {
		return nil, err
	}
	var status string
	if body.Action == "omit-recording" {
		status, err = app.queueWithoutVoice(d, e, body.FailureID)
	} else {
		status, err = app.queueRetry(d, e, body.Force, body.FailureID)
	}
	if err != nil {
		return nil, err
	}
	if status == "queued" {
		s.wakeSendQueue()
	}
	return map[string]string{"status": status}, nil
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

// apiScreen decides a request for the screen (grant, deny), or takes the
// screen back from the agent that holds it (release, on this machine only).
func (s *server) apiScreen(app *App, body request) (any, error) {
	switch body.Action {
	case "grant", "deny":
		return map[string]string{}, app.screenDecide(body.ID, body.Action == "grant", "web")
	case "release":
		d, err := app.load()
		if err != nil {
			return nil, err
		}
		st := d.Screen(app.Host)
		if st.Hold == nil {
			return nil, usagef("no agent holds the screen on %s", app.Host)
		}
		return map[string]string{}, app.screenLetGo(ident.Resolve(app.Env), st, "user")
	}
	return nil, usagef("screen action is grant, deny or release")
}

// apiTask changes tasks' state, or launches them; the response's "out" is
// what launch printed.
func (s *server) apiSets(app *App, body request) (any, error) {
	return map[string]string{}, app.setsRequest(body.Action, body.Name, body.Prefixes)
}

func (s *server) apiTask(app *App, body request) (any, error) {
	switch body.Action {
	case "add":
		id, err := app.taskAddWeb(body.To, body.Title, body.Type, body.Text, body.Parent, body.Tags, body.Media)
		return map[string]string{"id": id}, err
	case "tags":
		d, err := app.load()
		if err != nil {
			return nil, err
		}
		t, err := app.task(d, body.ID)
		if err != nil {
			return nil, err
		}
		return map[string]string{}, app.setTaskTags(t, body.Tags, ident.Resolve(app.Env), "web")
	case "start", "done", "drop", "defer", "defer-quota", "reopen", "launch":
	default:
		return nil, usagef("task action is add, tags, start, done, drop, defer, defer-quota, reopen or launch")
	}
	ids := body.list(body.ID)
	if len(ids) == 0 {
		return nil, usagef("task needs an id")
	}
	args := []string{"task", body.Action}
	if body.Action == "defer-quota" {
		args = []string{"task", "defer", "--quota"}
	}
	if note := strings.TrimSpace(body.Text); note != "" && body.Action != "launch" {
		args = append(args, "--note", note) // Reopen's "what is still wrong"
	}
	if err := app.run(append(append(args, "--"), ids...)); err != nil {
		return nil, err
	}
	// Who hears of it (taskchanges.go), for the toast.
	if len(ids) == 1 && body.Action != "launch" {
		if told := s.taskTold(app, ids[0]); told != nil {
			return told, nil
		}
	}
	return map[string]string{}, nil
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

// storedDesk returns the immutable event data for the current store stamp.
func (s *server) storedDesk() (*desk.Desk, string, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	stamp, stampErr := s.stamp()
	if stampErr == nil && s.readDesk != nil && s.readStamp == stamp {
		return s.readDesk, stamp, nil
	}

	// Sample the stamp before reading. If a writer commits during Load,
	// the next request sees a new stamp and reloads.
	d, err := desk.Load(s.a.st, s.a.Host, time.Time{})
	if stampErr != nil {
		return d, "", err
	}
	if err == nil {
		s.readDesk, s.readStamp = d, stamp
	}
	return d, stamp, err
}

// load reads the desk for a GET, with time and live status for this request.
func (s *server) load(w http.ResponseWriter) (*desk.Desk, string) {
	stored, stamp, err := s.storedDesk()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return nil, ""
	}
	d := *stored
	d.Now, d.Live = s.a.Now(), s.a.paseoLive()
	return &d, stamp
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
	// Conversations are the threads of the messages sent to the target
	// node about no item, each message with the responses to it, newest
	// first.
	Conversations []Convo  `json:"conversations,omitempty"`
	Session       *Session `json:"session,omitempty"`
	Projects      []string `json:"projects"`
	Hosts         []string `json:"hosts"`
	// Deferred are the tasks waiting for quota under the path; Launch says
	// whether launch_command is set and how many tasks one launch takes.
	Deferred []desk.Task `json:"deferred"`
	Launch   launchInfo  `json:"launch"`
	// Screens are the machines whose screen an agent holds or asks for.
	Screens []screenInfo `json:"screens"`
	// Sets are the project sets; Focus is the focused one and what it
	// hides, or null. Unfocused is set when ?unfocused=1 ignored the focus.
	Sets  []SetInfo       `json:"sets"`
	Focus *desk.FocusView `json:"focus"`
	// Refs resolves the id mentions in the text above (see Ref).
	Refs      map[string]Ref `json:"refs,omitempty"`
	Unfocused bool           `json:"unfocused,omitempty"`
	// Paths are what a set can be made of: every project, each followed by
	// its runs.
	Paths []string `json:"paths"`
}

type screenInfo struct {
	Host     string   `json:"host"`
	Held     bool     `json:"held"`
	Agent    string   `json:"agent,omitempty"` // who holds it
	Paseo    string   `json:"paseo,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Until    string   `json:"until,omitempty"`
	By       string   `json:"by,omitempty"`
	Requests []string `json:"requests"` // display ids of open requests
}

// screens describes the held and asked-for screens.
func screens(d *desk.Desk) []screenInfo {
	out := []screenInfo{}
	for _, st := range d.Screens() {
		si := screenInfo{Host: st.Host, Requests: []string{}}
		for _, q := range st.Requests {
			si.Requests = append(si.Requests, d.Display(q.ID))
		}
		if h := st.Hold; h != nil {
			si.Held, si.Reason, si.By = true, h.Reason, h.By
			si.Until = h.Until.Format(time.RFC3339)
			for _, e := range d.Events { // the holder's latest word names it
				if sessionOf(e) == h.Session {
					si.Agent, si.Paseo = agentTitle(e), fromOf(e).Paseo
				}
			}
		}
		out = append(out, si)
	}
	return out
}

type launchInfo struct {
	Ready bool `json:"ready"`
	Max   int  `json:"max"`
}

// readRecently is how far back show=hour keeps read items listed.
const readRecently = time.Hour

type viewFilter struct {
	Host    string `json:"host,omitempty"`
	Session string `json:"session,omitempty"`
	All     bool   `json:"all"`
	Query   string `json:"q,omitempty"`
}

func (s *server) apiView(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d, stamp := s.load(w)
	if d == nil {
		return
	}
	f := desk.Filter{
		Prefix:  strings.Trim(q.Get("path"), "/"),
		Host:    q.Get("host"),
		Session: q.Get("session"),
		All:     q.Get("all") == "1" || q.Get("all") == "true" || q.Get("show") == "all",
	}
	if m := q.Get("deliverables"); desk.ValidDeliverables(m) {
		f.Deliverables = m
	}
	// show=hour: the unread items, and those read in the last hour, kept
	// listed the way an item you are still on lingers.
	if q.Get("show") == "hour" && !f.All {
		recent := d.ReadSince(d.Now.Add(-readRecently))
		if len(recent) > 0 && f.Linger == nil {
			f.Linger = map[string]bool{}
		}
		for _, id := range recent {
			f.Linger[id] = true
		}
	}
	// An item with a draft stays listed, read or not, until the draft is
	// sent or discarded.
	drafted := s.draftedItems(d)
	if len(drafted) > 0 && f.Linger == nil {
		f.Linger = map[string]bool{}
	}
	for id := range drafted {
		f.Linger[id] = true
	}
	unfocused := q.Get("unfocused") == "1"
	if !unfocused {
		f.Focus = d.FocusPrefixes()
	}
	if l := q.Get("linger"); l != "" {
		if f.Linger == nil {
			f.Linger = map[string]bool{}
		}
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
		v.Deliverables = searchGroups(d, v.Deliverables, query)
		v.New = searchGroups(d, v.New, query)
		pinned := []desk.Item{}
		for _, g := range searchGroups(d, []desk.Group{{Items: v.Pinned}}, query) {
			pinned = g.Items
		}
		v.Pinned = pinned
		v.Runs = searchTree(v.Runs, strings.ToLower(query), nodeSessions(d))
	}
	markDrafted(&v, drafted)
	resp := viewResponse{
		Host: d.Host, Stamp: stamp, Path: f.Prefix, View: v,
		Filter: viewFilter{Host: f.Host, Session: f.Session, All: f.All, Query: query},
		Sets:   setInfos(d), Focus: focusInfo(d, f), Unfocused: unfocused && len(d.FocusPrefixes()) > 0,
		Paths: setPaths(d),
	}
	projects, hosts := map[string]bool{}, map[string]bool{}
	for _, e := range d.Events {
		p, _, _ := desk.Split(e.Path)
		projects[p] = true
		hosts[e.Host] = true
	}
	resp.Projects, resp.Hosts = sortedKeys(projects), sortedKeys(hosts)
	resp.Deferred = []desk.Task{}
	resp.Screens = screens(d)
	lq := strings.ToLower(query)
	for _, t := range d.Tasks(desk.Filter{Prefix: f.Prefix, Host: f.Host, Focus: f.Focus}) {
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
		// A project or a run speaks through the coordinator working under it.
		if t, err := targetOf(d, f.Prefix); err == nil {
			resp.Target = &t
		}
		if resp.Target != nil {
			s.behaviors(d, resp.Target)
			resp.Conversations = conversations(d, resp.Target.Node, s.sshHosts())
		}
	}
	if f.Session != "" {
		resp.Session = sessionInfo(d, f.Session)
	}
	resp.Refs = refsOf(d, textsOf(resp))
	writeJSON(w, resp)
}

// setPaths are every node on the desk, each followed by the nodes under it,
// siblings in name order: what the set editor offers (a project, a run or
// any role can be a prefix). The desk's own paths are left out.
func setPaths(d *desk.Desk) []string {
	kids := map[string]map[string]bool{} // parent path -> child paths
	for _, e := range d.Events {
		if desk.IsMark(e.Kind) {
			continue
		}
		node := desk.NodeOf(e)
		if p, _, _ := desk.Split(node); p == "" || p == desk.SystemPrefix {
			continue
		}
		for node != "" {
			parent := ""
			if i := strings.LastIndex(node, "/"); i >= 0 {
				parent = node[:i]
			}
			if kids[parent] == nil {
				kids[parent] = map[string]bool{}
			}
			if kids[parent][node] {
				break // its ancestors are in already
			}
			kids[parent][node] = true
			node = parent
		}
	}
	out := []string{}
	var walk func(string)
	walk = func(parent string) {
		for _, c := range sortedKeys(kids[parent]) {
			out = append(out, c)
			walk(c)
		}
	}
	walk("")
	return out
}

func mapKeys[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
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
	// Finished: archived (its Paseo agent is), or done (a session Paseo
	// does not know has filed a done report since it joined); sending
	// revives the session.
	Finished string `json:"finished,omitempty"`
	// Pushed: the host is in ssh_hosts, so a message is pushed there at once.
	Pushed bool `json:"pushed,omitempty"`
	// RemoteLabel is the config's name for the remote channel.
	RemoteLabel string `json:"remote_label,omitempty"`
	// For is the node asked for when Node is the coordinator working under
	// it (a project or a run, which no agent holds).
	For string `json:"for,omitempty"`
	to  ident.From
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
	if p := strings.Trim(token, "/"); p != node {
		if _, rerr := d.Resolve(p); rerr != nil {
			t.For = p // a node no agent holds: the coordinator under it (replyTarget)
		}
	}
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

// apiQuota lists the agents stopped at a usage limit (cached 30 s;
// ?fresh=1 reads again).
func (s *server) apiQuota(w http.ResponseWriter, r *http.Request) {
	// Under a focus, only the agents working inside it are listed. The desk
	// is read before taking quotaMu.
	var d *desk.Desk
	var focus []string
	if r.URL.Query().Get("unfocused") != "1" {
		d, _ = s.load(w)
		if d == nil {
			return
		}
		focus = d.FocusPrefixes()
	}
	s.mu.Lock()
	c, configErr := s.a.config()
	s.mu.Unlock()
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	if r.URL.Query().Get("fresh") != "" || time.Since(s.quotaAt) >= 30*time.Second || s.quotaResp == nil {
		if d == nil {
			d, _ = s.load(w)
			if d == nil {
				return
			}
		}
		if configErr != nil {
			jsonError(w, http.StatusInternalServerError, configErr.Error())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		qs, err := s.a.quotaAgents(ctx, d, c)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.quotaAt = time.Now()
		profiles := c.Quota.Profiles
		if profiles == nil {
			profiles = []string{}
		}
		s.quotaResp = map[string]any{"agents": qs, "auto_continue": c.Quota.AutoContinueCommand != "",
			"launch_ready": c.Tasks.LaunchCommand != "", "profiles": profiles}
	}
	resp := maps.Clone(s.quotaResp)
	hidden := s.a.quotaDismissed()
	kept := []QuotaAgent{}
	for _, q := range resp["agents"].([]QuotaAgent) {
		inFocus := len(focus) == 0
		for _, sc := range q.Scopes {
			inFocus = inFocus || desk.InFocus(sc, focus)
		}
		if inFocus {
			at, ok := hidden[q.Paseo]
			q.Dismissed = ok && at == q.LastActive
			kept = append(kept, q)
		}
	}
	seats := s.seatSnapshot()
	for i := range kept {
		if seat, ok := seats[kept[i].Paseo]; ok {
			kept[i].Seat = &seat
		}
	}
	resp["agents"] = kept
	writeJSON(w, resp)
}

// seatTTL is how long the agents' seats are served before a background
// refresh; a request never waits for the keeper.
const seatTTL = time.Minute

// seatSnapshot is each Paseo agent's seat from the last refresh (empty
// before the first), and starts a refresh when that is older than seatTTL.
func (s *server) seatSnapshot() map[string]QuotaSeat {
	s.seatMu.Lock()
	defer s.seatMu.Unlock()
	if time.Since(s.seatAt) >= seatTTL && !s.seatRefreshing {
		s.seatRefreshing = true
		go s.refreshSeats()
	}
	return s.seats
}

func (s *server) refreshSeats() {
	ctx, cancel := context.WithTimeout(context.Background(), cacheRefreshTimeout)
	defer cancel()
	var seats map[string]QuotaSeat
	app, _, _ := s.userApp()
	if client, err := app.keeperClient(); err == nil {
		list, err1 := client.Agents(ctx, true)
		accounts, err2 := client.Accounts(ctx)
		if err1 == nil && err2 == nil {
			until := map[string]*float64{}
			for _, acc := range accounts {
				until[acc.ID] = acc.BlockedUntil
			}
			seats = map[string]QuotaSeat{}
			for _, a := range list.Agents {
				if a.Seat == nil || a.Seat.Account == "" {
					continue
				}
				seat := QuotaSeat{Account: a.Seat.Account, Verdict: a.Seat.Verdict}
				if seat.Verdict == "blocked" {
					seat.AvailableAt = a.Seat.AvailableAt
					if seat.AvailableAt == nil {
						seat.AvailableAt = until[seat.Account]
					}
				}
				seats[a.ID] = seat
			}
		}
	}
	s.seatMu.Lock()
	if seats != nil {
		s.seats = seats
	}
	s.seatAt, s.seatRefreshing = time.Now(), false
	s.seatMu.Unlock()
}

// apiAutoContinue says whether Paseo agent ?paseo= resumes itself when its
// quota returns: {available, on}. available is false without
// auto_continue_command. Where agent-quotas runs, set_by says who decided
// and default is what it inherits (acInfo).
func (s *server) apiAutoContinue(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("paseo")
	s.mu.Lock()
	c, err := s.a.config()
	s.mu.Unlock()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c.Quota.AutoContinueCommand == "" || id == "" {
		writeJSON(w, map[string]bool{"available": false, "on": false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	auto, err := s.a.autoContinueList(ctx, c)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := map[string]any{"available": true, "on": auto[id]}
	if d, _, err := s.storedDesk(); err == nil {
		if info, err := s.a.autoContinueInfo(ctx, d, id); err == nil && info != nil {
			out["set_by"], out["default"] = info.SetBy, info.Default
		}
	}
	writeJSON(w, out)
}

// apiQuotaAct acts on a stopped agent: auto-continue-on, auto-continue-off,
// ask-parent or replace (with an optional profile).
func (s *server) apiQuotaAct(app *App, body request) (any, error) {
	switch body.Action {
	case "auto-continue-on", "auto-continue-off":
		defer s.expireQuota()
		return map[string]string{}, app.quotaAutoContinue(body.ID, body.Action == "auto-continue-on")
	case "auto-continue-default":
		defer s.expireQuota()
		d, err := app.load()
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		def, err := app.followAutoContinueDefault(ctx, d, body.ID)
		state := "off"
		if def.On {
			state = "on"
		}
		return map[string]string{"note": "It follows its default again: " + state + " (" + def.From() + ")."}, err
	case "dismiss", "undismiss":
		return map[string]string{}, app.quotaSetDismissed(body.list(body.ID), body.Action == "dismiss", nil, body.QuotaActivity)
	case "ask-parent", "replace":
		defer s.expireQuota()
		out, err := app.quotaAct(body.Action, body.ID, body.Profile, false)
		return map[string]string{"note": out}, err
	}
	return nil, usagef("quota action is auto-continue-on, auto-continue-off, auto-continue-default, dismiss, undismiss, ask-parent or replace")
}

func (s *server) expireQuota() {
	s.quotaMu.Lock()
	s.quotaAt = time.Time{}
	s.quotaMu.Unlock()
}

func (s *server) apiTarget(w http.ResponseWriter, r *http.Request) {
	d, _ := s.load(w)
	if d == nil {
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
	// Subtasks are the task's steps (task add --parent), finished ones too.
	Subtasks    []desk.Task `json:"subtasks,omitempty"`
	LaunchReady bool        `json:"launch_ready,omitempty"`
	// Refs resolves the id mentions in the item and its thread.
	Refs map[string]Ref `json:"refs,omitempty"`
}

func (s *server) apiItem(w http.ResponseWriter, r *http.Request) {
	d, _ := s.load(w)
	if d == nil {
		return
	}
	e, err := d.Resolve(r.URL.Query().Get("id"))
	if err != nil {
		jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	it := d.Item(e)
	it.Dismissed = d.Dismissed(e.ID) && isOpen(d, e.ID)
	it.Draft = s.draftedItems(d)[e.ID]
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
		for _, t := range d.Tasks(desk.Filter{All: true}) {
			if t.Event.ID == e.ID {
				t := t
				resp.Task = &t
			}
		}
		if resp.Task != nil {
			for _, t := range d.Tasks(desk.Filter{All: true}) {
				if t.ParentID == resp.Task.ID {
					resp.Subtasks = append(resp.Subtasks, t)
				}
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
			thread, resp.ThreadOf, resp.ThreadTitle = d.Thread(root.ID, ""), d.Display(root.ID), desk.ThreadTitle(root)
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
	resp.Refs = refsOf(d, textsOf(resp))
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
	// keys names the version of the user's keys file: sent now, and again
	// whenever it changes, so a saved edit applies without a reload.
	keys := s.a.keysStamp()
	fmt.Fprintf(w, "retry: 3000\nevent: hello\ndata: %s\n\nevent: build\ndata: %s\n\nevent: keys\ndata: %s\n\n", last, s.build, keys)
	fl.Flush()
	tick := time.NewTicker(s.poll)
	defer tick.Stop()
	beat := time.NewTicker(25 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.closing:
			return
		case <-beat.C:
			io.WriteString(w, ": keepalive\n\n")
			fl.Flush()
		case <-tick.C:
			if k := s.a.keysStamp(); k != keys {
				keys = k
				fmt.Fprintf(w, "event: keys\ndata: %s\n\n", k)
				fl.Flush()
			}
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

// gzipAccepted chooses an explicitly accepted coding for a static asset.
// Clients commonly send a list of bare tokens. Honor a zero quality or an
// explicit preference for the identity representation as well.
func gzipAccepted(values []string) bool {
	var compressed, identity float64
	for _, value := range values {
		for token := range strings.SplitSeq(value, ",") {
			coding, params, err := mime.ParseMediaType(strings.TrimSpace(token))
			if err != nil {
				continue
			}
			q := 1.0
			if weight, ok := params["q"]; ok {
				q, err = strconv.ParseFloat(weight, 64)
				if err != nil || !(q >= 0 && q <= 1) {
					continue
				}
			}
			switch coding {
			case "gzip":
				compressed = q
			case "identity":
				identity = q
			}
		}
	}
	return compressed > 0 && compressed >= identity
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
		if ext := path.Ext(p); ext == ".js" || ext == ".css" {
			w.Header().Add("Vary", "Accept-Encoding")
			if r.Header.Get("Range") == "" && gzipAccepted(r.Header.Values("Accept-Encoding")) {
				compressed, ok := s.gzipAssets.Load(p)
				if !ok {
					var buf bytes.Buffer
					zw := gzip.NewWriter(&buf)
					_, _ = zw.Write(b)
					_ = zw.Close()
					compressed, _ = s.gzipAssets.LoadOrStore(p, buf.Bytes())
				}
				b = compressed.([]byte)
				w.Header().Set("Content-Encoding", "gzip")
			}
		}
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
	// The user's keys file, made where missing so there is one to edit. A
	// failure here only costs the template.
	if p, err := a.ensureKeysFile(); err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: could not create the keys file: %v\n", err)
	} else if p != "" {
		fmt.Fprintf(a.Stderr, "agent-desk: created %s, your keys over the built-in ones\n", a.tilde(p))
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
	if err := s.recoverVoice(); err != nil {
		ln.Close()
		return fmt.Errorf("recovering interrupted recordings: %w", err)
	}
	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	// Open event streams end at once on shutdown (the page reconnects), so
	// they do not hold the drain below until its timeout.
	srv.RegisterOnShutdown(s.closeStreams)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go s.sweepScreen(ctx, time.Minute)
	s.cacheSnapshot() // start the first keeper read before a browser opens the desk
	go s.runSendQueue(ctx)
	go s.renderLoop(ctx)
	go s.taskChangeLoop(ctx)
	go s.autoContinueLoop(ctx)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), shutdownDrain)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	fmt.Fprintf(a.Stderr, "agent-desk: serving %s on %s\n", a.Home, shown)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	// Serve returns as soon as Shutdown starts; requests still in flight
	// would be cut off (the proxy answers 502) if the process exited now.
	<-drained
	return nil
}

// shutdownDrain bounds how long a stopping server waits for requests in
// flight.
const shutdownDrain = 10 * time.Second

// closeStreams ends every open /api/stream response.
func (s *server) closeStreams() {
	s.closeOnce.Do(func() { close(s.closing) })
}

// sweepScreen grants a waiting request for the screen once the user has
// been away long enough (screenSweep), every interval until ctx ends.
func (s *server) sweepScreen(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := s.sweepScreenOnce(); err != nil {
			fmt.Fprintf(s.logger, "agent-desk: screen sweep: %v\n", err)
		}
	}
}

func (s *server) sweepScreenOnce() error {
	app, _, errb := s.userApp()
	defer func() {
		if n := strings.TrimSpace(errb.String()); n != "" {
			fmt.Fprintln(s.logger, n)
		}
	}()
	events, count, err := app.screenSweep()
	if err != nil || len(events) == 0 {
		return err
	}
	events[0] = markQueued(events[0], "", "")
	recorded, err := app.st.AppendScreenSweep(events, count)
	if err == nil && recorded {
		s.sendCommitted(events)
	}
	return err
}

// verbatimView is one entry of the verbatim log: a message the user typed
// to an agent, word for word.
type verbatimView struct {
	ID      string `json:"id"`
	TS      string `json:"ts"`
	Path    string `json:"path"`
	Host    string `json:"host"`
	Text    string `json:"text"`
	Agent   string `json:"agent,omitempty"` // the session's title, else its agent
	Session string `json:"session,omitempty"`
	Paseo   string `json:"paseo,omitempty"`
}

// apiVerbatim is the verbatim log: the messages the user typed to agents under ?prefix=,
// newest first, matching every word of ?q=, ?limit= at a time (default 100)
// from ?offset=. Refs resolves the ids mentioned in them.
func (s *server) apiVerbatim(w http.ResponseWriter, r *http.Request) {
	d, _ := s.load(w)
	if d == nil {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	s.mu.Lock()
	all := s.a.instructionList(d, instructionFilter{prefix: strings.Trim(q.Get("prefix"), "/"), host: q.Get("host"), query: q.Get("q")})
	s.mu.Unlock()
	resp := struct {
		Items []verbatimView `json:"items"`
		Total int            `json:"total"`
		Refs  map[string]Ref `json:"refs,omitempty"`
	}{Items: []verbatimView{}, Total: len(all)}
	var texts []string
	for i := len(all) - 1 - offset; i >= 0 && len(resp.Items) < limit; i-- {
		e := all[i]
		var m struct {
			From struct {
				Agent, Session, Paseo, Title string
			} `json:"from"`
		}
		_ = json.Unmarshal(e.Meta, &m)
		agent := m.From.Title
		if agent == "" {
			agent = m.From.Agent
		}
		resp.Items = append(resp.Items, verbatimView{ID: d.Display(e.ID), TS: e.TS, Path: e.Path, Host: e.Host,
			Text: e.Body, Agent: agent, Session: m.From.Session, Paseo: m.From.Paseo})
		texts = append(texts, e.Body)
	}
	resp.Refs = refsOf(d, texts)
	writeJSON(w, resp)
}

// Convo is one thread at a node: a message sent about no item and what
// followed it.
type Convo struct {
	Root    string  `json:"root"` // the message's display id
	Title   string  `json:"title"`
	Age     string  `json:"age"` // since its newest entry
	Entries []Entry `json:"entries"`
}

// maxConvos is how many of a node's threads its page shows.
const maxConvos = 30

// conversations are node's message threads, newest activity first.
func conversations(d *desk.Desk, node string, hosts map[string]string) []Convo {
	roots := d.MessageThreads(node)
	out := make([]Convo, 0, len(roots))
	for _, r := range roots {
		th := d.Thread(r.ID, "")
		if len(th) == 0 {
			continue
		}
		out = append(out, Convo{Root: d.Display(r.ID), Title: desk.ThreadTitle(r), Age: desk.Age(d.Now.Sub(desk.Time(th[len(th)-1]))), Entries: entries(d, th, hosts)})
	}
	last := func(c Convo) string { return c.Entries[len(c.Entries)-1].Event.ID }
	sort.SliceStable(out, func(i, j int) bool { return last(out[i]) > last(out[j]) })
	if len(out) > maxConvos {
		out = out[:maxConvos]
	}
	return out
}
