package cli

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// GET api/keys serves the user's keys file from next to config.toml, and the
// stream says when it changes.
func TestKeysFile(t *testing.T) {
	h := newHarness(t)
	s, hd := testServer(t, h)
	get := func() keysResponse {
		t.Helper()
		code, body, hdr := do(t, hd, "GET", "/api/keys", "", nil)
		if code != 200 || hdr.Get("Cache-Control") != "no-store" {
			t.Fatalf("api/keys: %d %q %q", code, hdr.Get("Cache-Control"), body)
		}
		var k keysResponse
		must(t, json.Unmarshal([]byte(body), &k))
		return k
	}
	if k := get(); k.Exists || k.Path != "~/.config/agent-desk/keys.edn" || k.Text != "" || k.Stamp != "" {
		t.Fatalf("no file: %+v", k)
	}

	ts := httptest.NewServer(hd)
	defer ts.Close()
	s.poll = 20 * time.Millisecond
	resp, err := http.Get(ts.URL + "/api/stream")
	must(t, err)
	defer resp.Body.Close()
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	// next returns the data of the next keys event.
	next := func() string {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for keys := false; ; {
			select {
			case l := <-lines:
				if l == "event: keys" {
					keys = true
				} else if keys && strings.HasPrefix(l, "data:") {
					return strings.TrimSpace(strings.TrimPrefix(l, "data:"))
				}
			case <-deadline:
				t.Fatal("no keys event on the stream")
			}
		}
	}
	if d := next(); d != "" {
		t.Fatalf("first keys event names a file: %q", d)
	}

	p := filepath.Join(h.home, ".config", "agent-desk", "keys.edn")
	must(t, os.MkdirAll(filepath.Dir(p), 0o755))
	text := "{:normal {\"g c\" nil}}\n"
	must(t, os.WriteFile(p, []byte(text), 0o644))
	stamp := next()
	k := get()
	if !k.Exists || k.Text != text || k.Stamp != stamp || stamp == "" || k.Error != "" {
		t.Fatalf("file: %+v (stream said %q)", k, stamp)
	}

	must(t, os.WriteFile(p, []byte(strings.Repeat(";", maxKeysFile+1)), 0o644))
	next()
	if k := get(); !k.Exists || k.Text != "" || !strings.Contains(k.Error, "larger than 64 KiB") {
		t.Fatalf("too large: %+v", k)
	}
	must(t, os.Remove(p))
	if d := next(); d != "" {
		t.Fatalf("removed file still has a stamp: %q", d)
	}
}
