package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/keeper"
)

func fakeKeeper(t *testing.T, h *harness, handler http.HandlerFunc) {
	t.Helper()
	lockPaths.Clear()
	t.Cleanup(lockPaths.Clear)
	// macOS Unix socket addresses have a tight length limit.
	dir, err := os.MkdirTemp("/tmp", "desk-keeper-")
	must(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "socket")
	l, err := net.Listen("unix", socket)
	must(t, err)
	server := &httptest.Server{Listener: l, Config: &http.Server{Handler: handler}}
	server.Start()
	t.Cleanup(server.Close)
	writeConfig(t, h, fmt.Sprintf("[keeper]\nsocket = %q\n", socket))
}

func TestKeeperPolicyAndHistory(t *testing.T) {
	h, _, _ := queueFixture(t)
	var mu sync.Mutex
	var requests []keeper.PolicyRequest
	agent := keeper.Agent{ID: queuedAgent}
	fakeKeeper(t, h, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Query().Get("home") != h.paseo {
			t.Errorf("unexpected Paseo home: %q", r.URL.Query().Get("home"))
		}
		if r.Method == http.MethodPost {
			var p keeper.PolicyRequest
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			requests = append(requests, p)
			switch {
			case strings.HasSuffix(r.URL.Path, "/standby"):
				agent.Cache = &keeper.Cache{Policy: "compact", Phase: "standby"}
			case p.Cache == "warm":
				agent.Cache = &keeper.Cache{Policy: "warm", Phase: "warming", WarmHours: p.WarmHours}
			default:
				agent.Cache = &keeper.Cache{Policy: p.Cache, Phase: p.Cache}
			}
		}
		writeJSON(w, map[string]any{"agent": agent})
	})

	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": queuedAgent}
	if got := h.ok(env, "", "cache", "warm", "--warm-hours", "2.5"); !strings.Contains(got, "warming") {
		t.Fatalf("cache warm: %q", got)
	}
	if got := h.ok(env, "", "cache"); !strings.Contains(got, "warming") {
		t.Fatalf("cache status: %q", got)
	}
	_, hd := testServer(t, h)
	code, body, _ := do(t, hd, "POST", "/api/cache", fmt.Sprintf(`{"id":%q,"action":"off"}`, queuedAgent), dashboard)
	if code != http.StatusOK || !strings.Contains(body, `"policy":"off"`) {
		t.Fatalf("dashboard policy: %d %s", code, body)
	}
	h.ok(nil, "", "standby", queuedAgent)
	_, errb, code := h.run(env, "", "cache", "off", "22222222-0000-4000-8000-000000000039")
	if code == 0 || !strings.Contains(errb, "only for their own") {
		t.Fatalf("agent changed another agent's policy: %d %q", code, errb)
	}
	mu.Lock()
	defer mu.Unlock()
	events := kinds(h, "cache")
	if len(events) != 3 || len(requests) != 3 || requests[0].WarmHours != 2.5 {
		t.Fatalf("events=%v requests=%v", events, requests)
	}
	for i, actor := range []string{"agent", "desk", "cli"} {
		e := events[i]
		if desk.MetaString(e, "set_by") != actor || desk.MetaString(e, "paseo") != queuedAgent ||
			requests[i].SetBy != actor || e.TS == "" {
			t.Errorf("cache event %d: %+v request %+v", i, e, requests[i])
		}
	}
	h.ok(nil, "", "render") // writes no longer render; the server or this does
	ledger, err := os.ReadFile(filepath.Join(h.desk, "render", "ledger", "2026-03.org"))
	must(t, err)
	if !strings.Contains(string(ledger), "User via the dashboard set "+queuedAgent+" to off.") {
		t.Fatalf("cache history missing in ledger: %s", ledger)
	}
}

func TestKeeperAbsent(t *testing.T) {
	h, _, note := queueFixture(t)
	dir, err := os.MkdirTemp("/tmp", "desk-keeper-absent-")
	must(t, err)
	t.Cleanup(func() { _ = os.Remove(dir) })
	writeConfig(t, h, fmt.Sprintf("[keeper]\nsocket = %q\n", filepath.Join(dir, "socket")))
	s, hd := testServer(t, h)
	s.refreshCache()
	code, body, _ := do(t, hd, "GET", "/api/cache", "", nil)
	if code != http.StatusOK || !strings.Contains(body, `"available":false`) || !strings.Contains(body, "agent-quotas is not running") {
		t.Fatalf("cache status: %d %s", code, body)
	}
	_, errb, code := h.run(nil, "", "cache", "warm", queuedAgent)
	if code == 0 || !strings.Contains(errb, "agent-quotas is not running") || len(kinds(h, "cache")) != 0 {
		t.Fatalf("missing keeper changed the desk: code %d, %q", code, errb)
	}
	s.a.Exec = func(_ context.Context, _ []byte, _ string, _ ...string) (string, error) { return "", nil }
	code, body, _ = do(t, hd, "POST", "/api/reply", fmt.Sprintf(`{"to":%q,"text":"fixture"}`, note), dashboard)
	s.sendQueueStep()
	if code != http.StatusOK || len(kinds(h, "delivered")) != 1 {
		t.Fatalf("ordinary delivery without keeper: %d %s", code, body)
	}
}

func TestColdCacheDialogData(t *testing.T) {
	h, q, note := queueFixture(t)
	expiry := float64(h.now.Unix()) - 0.5
	var lists atomic.Int32
	fakeKeeper(t, h, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/agents" || r.URL.Query().Has("all") {
			t.Errorf("unexpected keeper request: %s %s", r.Method, r.URL)
		}
		lists.Add(1)
		writeJSON(w, keeper.List{Enabled: true, Agents: []keeper.Agent{{ID: queuedAgent, Big: true, CacheExpiresAt: &expiry}}})
	})
	s, hd := testServer(t, h)
	s.refreshCache()
	for _, target := range []struct{ kind, id string }{{"answer", q}, {"reply", note}, {"reply", "example-proj/r1/coord"}} {
		path := "/api/cache?" + url.Values{"kind": {target.kind}, "target": {target.id}}.Encode()
		code, body, _ := do(t, hd, "GET", path, "", nil)
		if code != http.StatusOK || body != "{\"cold\":true,\"enabled\":true}\n" {
			t.Errorf("cold preflight for %s: %d %s", target.id, code, body)
		}
	}
	if n := lists.Load(); n != 1 {
		t.Fatalf("cache list fetched %d times", n)
	}
	future := time.Unix(int64(expiry)-5, 0)
	if (keeper.Agent{Big: true, CacheExpiresAt: &expiry}).Cold(future) ||
		(keeper.Agent{CacheExpiresAt: &expiry}).Cold(h.now) ||
		(keeper.Agent{Big: true}).Cold(h.now) {
		t.Fatal("a live cache, small context or unknown expiry should not warn")
	}
	// A foreign recipient can reuse the same UUID without triggering a local warning.
	s.a.Host = "other-host"
	_, body, _ := do(t, hd, "GET", "/api/cache?kind=answer&target="+q, "", nil)
	if !strings.Contains(body, `"cold":false`) {
		t.Fatalf("remote preflight: %s", body)
	}
}

func TestCompactThenSend(t *testing.T) {
	for _, terminal := range []string{"completed", "failed", "canceled"} {
		t.Run(terminal, func(t *testing.T) {
			h, _, note := queueFixture(t)
			waiting := make(chan struct{}, 1)
			release := make(chan struct{})
			delivered := make(chan string, 1)
			const compactID = "c-test"
			var compactions atomic.Int32
			fakeKeeper(t, h, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/compact"):
					compactions.Add(1)
					writeJSON(w, map[string]string{"compaction_id": compactID, "state": "queued"})
				case r.URL.Query().Get("wait_compaction") == compactID && r.URL.Query().Get("timeout") == "60":
					waiting <- struct{}{}
					select {
					case <-r.Context().Done():
						return
					case <-release:
					}
					writeJSON(w, map[string]any{"agent": keeper.Agent{ID: queuedAgent, Cache: &keeper.Cache{
						Compaction: &keeper.Compaction{ID: compactID, State: terminal},
					}}})
				default:
					writeJSON(w, map[string]any{"agent": keeper.Agent{ID: queuedAgent}})
				}
			})
			if terminal == "completed" {
				cfg, err := os.ReadFile(filepath.Join(h.home, ".config", "agent-desk", "config.toml"))
				must(t, err)
				writeConfig(t, h, voiceTestConfig+string(cfg))
			}
			s, hd := testServer(t, h)
			exec := func(_ context.Context, _ []byte, name string, args ...string) (string, error) {
				if name == "paseo" && args[0] == "send" && args[len(args)-1] != "--help" {
					delivered <- args[len(args)-1]
				}
				return "", nil
			}
			startQueue := func(s *server) func() {
				s.logger, s.a.Exec = io.Discard, exec
				ctx, stop := context.WithCancel(context.Background())
				done := make(chan struct{})
				go func() { s.runSendQueue(ctx); close(done) }()
				return func() { stop(); <-done }
			}
			stop := startQueue(s)
			defer stop()
			message := "fixture"
			var rec recording
			if terminal == "completed" {
				rec = testVoiceRequest(t, hd, "POST", "", `{"node":"example-proj/r1/coord"}`)
				if code, body, _ := do(t, hd, "POST", "/api/recordings/"+rec.ID+"/chunk?offset=0", "\x01\x02", dashboard); code != http.StatusOK {
					t.Fatalf("upload: %d %s", code, body)
				}
				message = fmt.Sprintf("before [voice %s 0:00, transcribing] after", rec.ID)
			}
			code, body, _ := do(t, hd, "POST", "/api/reply", fmt.Sprintf(`{"to":%q,"text":%q,"after_compaction":true}`, note, message), dashboard)
			if code != http.StatusOK {
				t.Fatalf("queue reply: %d %s", code, body)
			}
			if rec.ID != "" {
				var ack map[string]string
				must(t, json.Unmarshal([]byte(body), &ack))
				testVoiceEntry(t, hd, note, ack["event_id"], func(e Entry) bool { return e.Pending == "waiting for transcription" })
				if count := compactions.Load(); count != 0 {
					t.Fatalf("started %d compactions before transcription", count)
				}
				testVoiceRequest(t, hd, "POST", "/"+rec.ID, `{"action":"finish"}`)
			}
			select {
			case <-waiting:
			case <-time.After(4 * time.Second):
				t.Fatal("did not long-poll the requested compaction")
			}
			_, body, _ = do(t, hd, "GET", "/api/item?id="+note, "", nil)
			if !strings.Contains(body, `"pending":"waiting for compaction"`) {
				t.Fatalf("thread lost waiting state: %s", body)
			}
			// A separate CLI/merge delivery pass must honor the same hold.
			_, err := s.a.deliver("")
			must(t, err)
			select {
			case <-delivered:
				t.Fatal("message reached Paseo before compaction ended")
			default:
			}
			if terminal == "completed" {
				stop()
				s, hd = testServer(t, h)
				stop = startQueue(s)
				defer stop()
				select {
				case <-waiting:
				case <-time.After(4 * time.Second):
					t.Fatal("restarted queue did not resume waiting for the recorded ID")
				}
			}
			close(release)
			select {
			case text := <-delivered:
				if rec.ID != "" && !strings.Contains(text, voiceBlock("one two")) {
					t.Fatalf("delivery lost the resolved transcript: %q", text)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("message was not sent after compaction ended")
			}
			// Stop waits for the active delivery pass to commit the receipt.
			stop()
			if calls := compactions.Load(); calls != 1 {
				t.Fatalf("expected one compaction request, got %d", calls)
			}
			_, body, _ = do(t, hd, "GET", "/api/item?id="+note, "", nil)
			if !strings.Contains(body, `"delivery":`) || strings.Contains(body, "waiting for compaction") ||
				(terminal != "completed" && !strings.Contains(body, "Compaction "+terminal)) {
				t.Fatalf("thread lost delivery result for %s: %s", terminal, body)
			}
		})
	}
}

func TestPaseoSendTakesKeeperLock(t *testing.T) {
	h, _, note := queueFixture(t)
	lockPath := filepath.Join(t.TempDir(), "keeper.lock")
	f, err := os.Create(lockPath)
	must(t, err)
	defer f.Close()
	lookedUp := make(chan struct{}, 1)
	fakeKeeper(t, h, func(w http.ResponseWriter, _ *http.Request) {
		lookedUp <- struct{}{}
		writeJSON(w, map[string]any{"agent": keeper.Agent{ID: queuedAgent, LockPath: lockPath}})
	})
	must(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX))
	s, hd := testServer(t, h)
	s.logger = io.Discard
	result := make(chan error, 1)
	s.a.Exec = func(_ context.Context, _ []byte, name string, args ...string) (string, error) {
		if name == "paseo" && args[0] == "send" && args[len(args)-1] != "--help" {
			err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if !errors.Is(err, syscall.EWOULDBLOCK) {
				result <- fmt.Errorf("expected the send to hold the keeper lock, got %v", err)
			} else {
				result <- nil
			}
		}
		return "", nil
	}
	code, body, _ := do(t, hd, "POST", "/api/reply", fmt.Sprintf(`{"to":%q,"text":"fixture"}`, note), dashboard)
	if code != http.StatusOK {
		t.Fatalf("queue reply: %d %s", code, body)
	}
	done := make(chan struct{})
	go func() { s.sendQueueStep(); close(done) }()
	select {
	case <-lookedUp:
	case <-time.After(4 * time.Second):
		t.Fatal("the send did not look up the keeper lock")
	}
	select {
	case <-result:
		t.Fatal("sent while the keeper held the lock")
	case <-time.After(100 * time.Millisecond):
	}
	must(t, syscall.Flock(int(f.Fd()), syscall.LOCK_UN))
	select {
	case err := <-result:
		must(t, err)
	case <-time.After(4 * time.Second):
		t.Fatal("did not send after the keeper released the lock")
	}
	<-done
	must(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	must(t, syscall.Flock(int(f.Fd()), syscall.LOCK_UN))
}

func TestSlowKeeperNeverBlocksReads(t *testing.T) {
	h, _, _ := queueFixture(t)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var lists atomic.Int32
	fakeKeeper(t, h, func(w http.ResponseWriter, r *http.Request) {
		lists.Add(1)
		<-release
		writeJSON(w, keeper.List{Enabled: true, Agents: []keeper.Agent{{ID: queuedAgent, LockPath: "/nonexistent/kw.lock"}}})
	})
	s, hd := testServer(t, h)
	start := time.Now()
	_, body, _ := do(t, hd, "GET", "/api/cache", "", nil)
	if !strings.Contains(body, "loading") || time.Since(start) > time.Second {
		t.Fatalf("first read with a slow keeper: %s after %v", body, time.Since(start))
	}
	// A second read while the refresh runs starts no second keeper request.
	do(t, hd, "GET", "/api/cache", "", nil)
	unblock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body, _ = do(t, hd, "GET", "/api/cache", "", nil)
		if strings.Contains(body, queuedAgent) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(body, queuedAgent) {
		t.Fatalf("refreshed read: %s", body)
	}
	if n := lists.Load(); n != 1 {
		t.Fatalf("keeper list fetched %d times, want 1", n)
	}
	if path, ok := lockPaths.Load(queuedAgent); !ok || path != "/nonexistent/kw.lock" {
		t.Fatalf("lock path not remembered: %v", path)
	}
	lockPaths.Delete(queuedAgent)
	// A stale snapshot is served at once while a new refresh runs.
	s.cacheMu.Lock()
	s.cacheAt = time.Now().Add(-2 * cacheStatusTTL)
	s.cacheMu.Unlock()
	start = time.Now()
	_, body, _ = do(t, hd, "GET", "/api/cache", "", nil)
	if !strings.Contains(body, queuedAgent) || time.Since(start) > time.Second {
		t.Fatalf("stale read: %s after %v", body, time.Since(start))
	}
}
