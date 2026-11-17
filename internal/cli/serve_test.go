package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func testServer(t *testing.T, h *harness) (*server, http.Handler) {
	t.Helper()
	a, _, _ := h.app(nil, "")
	ui := fstest.MapFS{
		"index.html":         {Data: []byte(`<!doctype html><script>window.x=1</script><script type="module" src="./assets/app-1a2b.js"></script>`)},
		"assets/app-1a2b.js": {Data: []byte("console.log(1)")},
	}
	s, err := newServer(a, ui)
	must(t, err)
	return s, s.routes()
}

func do(t *testing.T, hd http.Handler, method, target, body string, hdr map[string]string) (int, string, http.Header) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	hd.ServeHTTP(w, req)
	return w.Code, w.Body.String(), w.Header()
}

var dashboard = map[string]string{"X-Agent-Desk": "1", "Content-Type": "application/json",
	"Sec-Fetch-Site": "same-origin", "Origin": "http://example.com"}

func TestServe(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-000000000003"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	q := h.ok(env, "A or B.\nI recommend B.", "ask", "--blocked", "--title", "Which split?")
	tl := h.ok(env, "Live.", "tell", "--title", "Deploy finished")
	h.respond = func(name string, args []string) (string, error) { return "queued", nil }
	_, hd := testServer(t, h)

	if code, body, _ := do(t, hd, "GET", "/api/health", "", nil); code != 200 || body != "ok\n" {
		t.Fatalf("health: %d %q", code, body)
	}

	// The global view, and filters.
	var v struct {
		Host     string   `json:"host"`
		Projects []string `json:"projects"`
		View     struct {
			NeedsYou []struct {
				Items []struct {
					ID string `json:"display_id"`
				} `json:"items"`
			} `json:"needs_you"`
			New []struct {
				Items []struct {
					ID string `json:"display_id"`
				} `json:"items"`
			} `json:"new"`
		} `json:"view"`
		Target  *Target  `json:"target"`
		Session *Session `json:"session"`
	}
	get := func(target string) {
		t.Helper()
		code, body, _ := do(t, hd, "GET", target, "", nil)
		if code != 200 {
			t.Fatalf("%s: %d %s", target, code, body)
		}
		v.Target, v.Session = nil, nil
		must(t, json.Unmarshal([]byte(body), &v))
	}
	get("/api/view")
	if len(v.View.NeedsYou) != 1 || v.View.NeedsYou[0].Items[0].ID != q || len(v.View.New) != 1 {
		t.Fatalf("global view: %+v", v.View)
	}
	if v.Host != "host-a" || len(v.Projects) != 1 || v.Projects[0] != "example-proj" {
		t.Fatalf("host and projects: %q %q", v.Host, v.Projects)
	}
	get("/api/view?path=example-proj/r1/coord")
	if v.Target == nil || v.Target.Via != "paseo" || v.Target.Paseo != uuid {
		t.Fatalf("node target: %+v", v.Target)
	}
	get("/api/view?session=" + uuid)
	if len(v.View.NeedsYou) != 1 || v.Session == nil || v.Session.Node != "example-proj/r1/coord" {
		t.Fatalf("session view: %+v %+v", v.View, v.Session)
	}
	get("/api/view?session=nobody")
	if len(v.View.NeedsYou) != 0 {
		t.Fatalf("another session's view: %+v", v.View)
	}
	get("/api/view?q=deploy")
	if len(v.View.NeedsYou) != 0 || len(v.View.New) != 1 {
		t.Fatalf("search: %+v", v.View)
	}

	// Writes from anywhere but the dashboard are refused.
	for name, hdr := range map[string]map[string]string{
		"no header":  {"Content-Type": "application/json"},
		"form post":  {"X-Agent-Desk": "1", "Content-Type": "application/x-www-form-urlencoded"},
		"cross-site": {"X-Agent-Desk": "1", "Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"},
		"origin":     {"X-Agent-Desk": "1", "Content-Type": "application/json", "Origin": "http://evil.example"},
	} {
		if code, body, _ := do(t, hd, "POST", "/api/answer", `{"id":"`+q+`","text":"A"}`, hdr); code != 403 {
			t.Fatalf("%s: %d %s", name, code, body)
		}
	}

	// Answer from the dashboard: recorded and typed into the session.
	code, body, _ := do(t, hd, "POST", "/api/answer", `{"id":"`+q+`","text":"B"}`, dashboard)
	if code != 200 || !strings.Contains(body, `"id":"a-`) {
		t.Fatalf("answer: %d %s", code, body)
	}
	if c := *h.calls; len(c) != 1 || !strings.Contains(c[0], `Answer to `+q+` "Which split?": B`) {
		t.Fatalf("answer delivery: %q", c)
	}
	// Reply, then the item's thread shows both.
	code, body, _ = do(t, hd, "POST", "/api/reply", `{"to":"`+q+`","text":"and log it"}`, dashboard)
	if code != 200 || !strings.Contains(body, `"id":"m-`) {
		t.Fatalf("reply: %d %s", code, body)
	}
	code, body, _ = do(t, hd, "GET", "/api/item?id="+q, "", nil)
	var it itemResponse
	must(t, json.Unmarshal([]byte(body), &it))
	if code != 200 || !it.Answered || len(it.Thread) != 2 || it.Thread[0].Source != "web" ||
		it.Thread[0].Delivered != "paseo" || it.Thread[1].Event.Kind != "reply" || it.Target.Via != "paseo" {
		t.Fatalf("item: %d %s", code, body)
	}
	// The answer came from the user, not from the agent whose shell runs the server.
	if strings.Contains(string(it.Thread[0].Event.Meta), sessA) {
		t.Fatalf("a web answer was attributed to an agent: %s", it.Thread[0].Event.Meta)
	}
	// Read marks.
	if code, body, _ := do(t, hd, "POST", "/api/read", `{"target":"`+tl+`"}`, dashboard); code != 200 {
		t.Fatalf("read: %d %s", code, body)
	}
	get("/api/view")
	if len(v.View.New) != 0 {
		t.Fatalf("read item still new: %+v", v.View.New)
	}
	if code, body, _ := do(t, hd, "POST", "/api/read", `{"target":"`+q+`"}`, dashboard); code != 200 || !strings.Contains(body, "already answered") {
		t.Fatalf("reading an answered question: %d %s", code, body)
	}
	if code, body, _ := do(t, hd, "POST", "/api/read", `{"targets":["zzzz"]}`, dashboard); code != 422 {
		t.Fatalf("reading an unknown target: %d %s", code, body)
	}
	if code, _, _ := do(t, hd, "GET", "/api/item?id=zzzz", "", nil); code != 404 {
		t.Fatalf("unknown item: %d", code)
	}

	// The page: inline scripts allowed by hash, assets cached for good.
	code, body, hdr := do(t, hd, "GET", "/", "", nil)
	if code != 200 || hdr.Get("Cache-Control") != "no-cache" ||
		!strings.Contains(hdr.Get("Content-Security-Policy"), "'sha256-") || !strings.Contains(body, "app-1a2b.js") {
		t.Fatalf("index: %d %v", code, hdr)
	}
	if _, _, hdr := do(t, hd, "GET", "/assets/app-1a2b.js", "", nil); !strings.Contains(hdr.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset caching: %v", hdr)
	}
	if code, _, _ := do(t, hd, "GET", "/nothing.js", "", nil); code != 404 {
		t.Fatalf("missing file: %d", code)
	}
}

func TestServeHostCheck(t *testing.T) {
	h := newHarness(t)
	s, hd := testServer(t, h)
	s.hosts = map[string]bool{"localhost": true, "127.0.0.1": true}
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Host = "rebound.example:7740"
	w := httptest.NewRecorder()
	hd.ServeHTTP(w, req)
	if w.Code != http.StatusMisdirectedRequest {
		t.Fatalf("foreign host: %d", w.Code)
	}
	req.Host = "127.0.0.1:7740"
	w = httptest.NewRecorder()
	hd.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("loopback host: %d", w.Code)
	}
}

func TestServeStream(t *testing.T) {
	h := newHarness(t)
	env := h.as(sessA)
	h.ok(env, "", "join", "r1/coord")
	s, hd := testServer(t, h)
	s.poll = 20 * time.Millisecond
	ts := httptest.NewServer(hd)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/stream")
	must(t, err)
	defer resp.Body.Close()
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	wait := func(want string) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case l := <-lines:
				if l == want {
					return
				}
			case <-deadline:
				t.Fatalf("no %q on the stream", want)
			}
		}
	}
	wait("event: hello")
	h.ok(env, "", "set", "status", "Seeds 3/5 done.")
	wait("event: change")
}

// Behind a proxy that rewrites Host, a write from the dashboard's public
// origin is accepted when that origin is configured, or when the server is on
// a unix socket and the proxy names it in X-Forwarded-Host.
func TestServeBehindProxy(t *testing.T) {
	h := newHarness(t)
	h.ok(h.as(sessA), "", "join", "r1/coord")
	s, hd := testServer(t, h)
	post := func(hdr map[string]string) int {
		req := httptest.NewRequest("POST", "/api/read", strings.NewReader(`{"target":"example-proj/r1"}`))
		req.Host = "localhost"
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		hd.ServeHTTP(w, req)
		return w.Code
	}
	base := map[string]string{"X-Agent-Desk": "1", "Content-Type": "application/json", "Origin": "https://desk.example.net"}
	if c := post(base); c != 403 {
		t.Fatalf("unconfigured origin: %d", c)
	}
	s.origins = map[string]bool{"https://desk.example.net": true}
	if c := post(base); c != 200 {
		t.Fatalf("configured origin: %d", c)
	}
	s.origins = nil
	fwd := map[string]string{"X-Forwarded-Host": "desk.example.net"}
	for k, v := range base {
		fwd[k] = v
	}
	if c := post(fwd); c != 403 {
		t.Fatalf("X-Forwarded-Host trusted on TCP: %d", c)
	}
	s.forwarded = true
	if c := post(fwd); c != 200 {
		t.Fatalf("X-Forwarded-Host on a socket: %d", c)
	}
	fwd["Origin"] = "https://evil.example"
	if c := post(fwd); c != 403 {
		t.Fatalf("foreign origin behind the proxy: %d", c)
	}
}

// The files that make the page an installable app are served under stable
// names and revalidated, never cached for good.
func TestServeAppFiles(t *testing.T) {
	h := newHarness(t)
	a, _, _ := h.app(nil, "")
	ui := fstest.MapFS{
		"index.html":    {Data: []byte(`<!doctype html><link rel="manifest" href="./manifest.json">`)},
		"manifest.json": {Data: []byte(`{"name":"agent-desk"}`)},
		"sw.js":         {Data: []byte("self.addEventListener('fetch', () => {})")},
	}
	s, err := newServer(a, ui)
	must(t, err)
	hd := s.routes()
	for file, ctype := range map[string]string{"/manifest.json": "application/json", "/sw.js": "javascript"} {
		code, _, hdr := do(t, hd, "GET", file, "", nil)
		if code != 200 || !strings.Contains(hdr.Get("Content-Type"), ctype) || hdr.Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: %d %v", file, code, hdr)
		}
	}
}

// Behind a supervisor that names the device, the manifest says which device
// the app is on and takes its colour; a bad colour is ignored.
func TestServeBrandedManifest(t *testing.T) {
	base := []byte(`{"name":"agent-desk","theme_color":"#c2410c","start_url":"./"}`)
	b, err := brandManifest(base, "Agent desk", "host-a", "#2f6fd6")
	must(t, err)
	var m map[string]any
	must(t, json.Unmarshal(b, &m))
	if m["name"] != "Agent desk · host-a" || m["theme_color"] != "#2f6fd6" || m["start_url"] != "./" || m["id"] != nil {
		t.Fatalf("branded: %s", b)
	}
	b, err = brandManifest(base, "", "", "red; x")
	must(t, err)
	must(t, json.Unmarshal(b, &m))
	if m["name"] != "agent-desk" || m["theme_color"] != "#c2410c" {
		t.Fatalf("unbranded: %s", b)
	}
	h := newHarness(t)
	s, hd := testServer(t, h)
	s.manifest = []byte(`{"name":"Agent desk · host-a"}`)
	code, body, hdr := do(t, hd, "GET", "/manifest.json", "", nil)
	if code != 200 || !strings.Contains(body, "host-a") || hdr.Get("Cache-Control") != "no-cache" ||
		hdr.Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("served: %d %v %s", code, hdr, body)
	}
}

func TestSearchRunsBySession(t *testing.T) {
	h := newHarness(t)
	const uuid = "b155c02a-0000-4000-8000-000000000009"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Rerun coordinator")
	h.ok(map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}, "", "join", "r1/coord")
	h.ok(h.as(sessB), "", "join", "r2/coord")
	_, hd := testServer(t, h)
	for q, want := range map[string]string{uuid: "example-proj/r1/coord", "b155c02a": "example-proj/r1/coord", "r2": "example-proj/r2/coord", sessB[:13]: "example-proj/r2/coord"} {
		_, body, _ := do(t, hd, "GET", "/api/view?all=1&q="+q, "", nil)
		if !strings.Contains(body, `"path":"`+want+`"`) {
			t.Fatalf("q=%s: %s not found in %s", q, want, body)
		}
		other := "example-proj/r2/coord"
		if want == other {
			other = "example-proj/r1/coord"
		}
		if strings.Contains(body, `"path":"`+other+`"`) {
			t.Fatalf("q=%s also kept %s", q, other)
		}
	}
}
