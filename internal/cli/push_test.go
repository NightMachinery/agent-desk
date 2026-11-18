package cli

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/push"
)

// fakePushService is a push service that decrypts what it receives, as
// the subscribed browser would.
type fakePushService struct {
	mu     sync.Mutex
	ua     *ecdh.PrivateKey
	auth   []byte
	notes  []pushNote
	hdrs   []http.Header
	status int
}

func (f *fakePushService) got() ([]pushNote, []http.Header) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pushNote{}, f.notes...), append([]http.Header{}, f.hdrs...)
}

func newFakePush(t *testing.T) (*fakePushService, *httptest.Server) {
	f := &fakePushService{auth: make([]byte, 16), status: http.StatusCreated}
	f.ua, _ = ecdh.P256().GenerateKey(rand.Reader)
	rand.Read(f.auth)
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.status != http.StatusCreated {
			w.WriteHeader(f.status)
			return
		}
		body, _ := io.ReadAll(r.Body)
		plain, err := push.Decrypt(f.ua, f.auth, body)
		if err != nil {
			t.Errorf("decrypt: %v", err)
		}
		var n pushNote
		if err := json.Unmarshal(plain, &n); err != nil {
			t.Errorf("payload %q: %v", plain, err)
		}
		f.notes = append(f.notes, n)
		f.hdrs = append(f.hdrs, r.Header.Clone())
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(ts.Close)
	return f, ts
}

func TestPush(t *testing.T) {
	h := newHarness(t)
	f, ts := newFakePush(t)
	h.httpc = ts.Client()
	_, hd := testServer(t, h)

	// No device yet: nothing is pushed and no key pair is needed.
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	h.ok(a, "ctx", "ask", "--blocked", "--title", "Before any device")
	if _, err := os.Stat(filepath.Join(h.desk, "push", "subs.json")); err == nil {
		t.Fatal("subs.json exists before any device subscribed")
	}

	code, body, _ := do(t, hd, "GET", "/api/push", "", nil)
	var info struct {
		Key     string       `json:"key"`
		Devices []PushDevice `json:"devices"`
	}
	must(t, json.Unmarshal([]byte(body), &info))
	if code != 200 || len(info.Key) != 87 || len(info.Devices) != 0 {
		t.Fatalf("GET api/push: %d %s", code, body)
	}

	// The browser subscribes.
	sub := map[string]any{"endpoint": ts.URL + "/push/dev1", "keys": map[string]string{
		"p256dh": base64.RawURLEncoding.EncodeToString(f.ua.PublicKey().Bytes()),
		"auth":   base64.URLEncoding.EncodeToString(f.auth)}}
	req, _ := json.Marshal(map[string]any{"action": "subscribe", "subscription": sub, "label": "Chrome on Android"})
	if code, body, _ := do(t, hd, "POST", "/api/push", string(req), nil); code != 403 {
		t.Fatalf("a cross-site subscribe was not refused: %d %s", code, body)
	}
	code, body, _ = do(t, hd, "POST", "/api/push", string(req), dashboard)
	if code != 200 {
		t.Fatalf("subscribe: %d %s", code, body)
	}
	id := push.ID(ts.URL + "/push/dev1")
	_, body, _ = do(t, hd, "GET", "/api/push", "", nil)
	must(t, json.Unmarshal([]byte(body), &info))
	if len(info.Devices) != 1 || info.Devices[0].ID != id || info.Devices[0].Label != "Chrome on Android" {
		t.Fatalf("devices: %s", body)
	}
	bad, _ := json.Marshal(map[string]any{"action": "subscribe", "subscription": map[string]any{"endpoint": "http://x/1"}})
	if code, _, _ := do(t, hd, "POST", "/api/push", string(bad), dashboard); code != 400 {
		t.Fatalf("a bad subscription was accepted: %d", code)
	}

	// A blocked question is pushed; a question with a default is not.
	q := h.ok(a, "ctx", "ask", "--blocked", "--title", "Which split?")
	h.ok(a, "ctx", "ask", "--default", "keep going", "--title", "Rename it?")
	notes, hdrs := f.got()
	if len(notes) != 1 || notes[0].Type != "show" || notes[0].Tag != q || notes[0].Title != "Blocked: Which split?" ||
		!notes[0].Sticky || notes[0].URL != "#/p/example-proj/r1/coord?item="+q || !strings.Contains(notes[0].Body, "ctx") {
		t.Fatalf("pushed: %+v", notes)
	}
	if hdrs[0].Get("Urgency") != "high" || hdrs[0].Get("Topic") != q || !strings.Contains(hdrs[0].Get("Authorization"), "k="+info.Key) {
		t.Fatalf("headers: %v", hdrs[0])
	}
	ns := kinds(h, "notified")
	if len(ns) != 1 || desk.Meta(ns[0])["channel"] != "push" || desk.Meta(ns[0])["handle"] != q {
		t.Fatalf("notified: %+v", ns)
	}

	// Answering takes it back, once.
	h.ok(nil, "", "answer", q, "B")
	notes, _ = f.got()
	if len(notes) != 2 || notes[1].Type != "close" || notes[1].Tag != q {
		t.Fatalf("taken back: %+v", notes)
	}
	if xs := kinds(h, "retracted"); len(xs) != 1 || xs[0].Ref != ns[0].ID {
		t.Fatalf("retracted: %+v", xs)
	}
	h.ok(nil, "", "read", "example-proj")
	if notes, _ = f.got(); len(notes) != 2 {
		t.Fatalf("taken back again: %+v", notes)
	}

	// The setting turns pushes on for another class.
	h.ok(nil, "", "pref", "set", "notify.push", "always", "--class", "done")
	h.ok(a, "", "done", "--title", "Rerun finished")
	if notes, _ = f.got(); len(notes) != 3 || notes[2].Title != "Done: Rerun finished" || notes[2].Sticky {
		t.Fatalf("done push: %+v", notes)
	}

	// The test button.
	tr, _ := json.Marshal(map[string]string{"action": "test", "id": id})
	if code, body, _ := do(t, hd, "POST", "/api/push", string(tr), dashboard); code != 200 || !strings.Contains(body, `"sent":1`) {
		t.Fatalf("test: %d %s", code, body)
	}
	if notes, _ = f.got(); len(notes) != 4 || notes[3].Tag != "agent-desk-test" {
		t.Fatalf("test push: %+v", notes)
	}

	// A device the service calls gone is dropped.
	f.mu.Lock()
	f.status = http.StatusGone
	f.mu.Unlock()
	h.ok(a, "ctx", "ask", "--blocked", "--title", "Another")
	_, body, _ = do(t, hd, "GET", "/api/push", "", nil)
	must(t, json.Unmarshal([]byte(body), &info))
	if len(info.Devices) != 0 {
		t.Fatalf("a gone device was kept: %s", body)
	}

	// Remove by id.
	f.mu.Lock()
	f.status = http.StatusCreated
	f.mu.Unlock()
	do(t, hd, "POST", "/api/push", string(req), dashboard)
	rm, _ := json.Marshal(map[string]string{"action": "remove", "id": id})
	if code, _, _ := do(t, hd, "POST", "/api/push", string(rm), dashboard); code != 200 {
		t.Fatalf("remove: %d", code)
	}
	if subs, _ := push.Dir(filepath.Join(h.desk, "push")).Subs(); len(subs) != 0 {
		t.Fatalf("removed device kept: %+v", subs)
	}
	if fi, err := os.Stat(filepath.Join(h.desk, "push", "vapid.json")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("vapid.json: %v %v", fi, err)
	}
}
