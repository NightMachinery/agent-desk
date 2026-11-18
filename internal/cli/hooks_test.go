package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readHookLog(t *testing.T, path string) []Payload {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	must(t, err)
	var out []Payload
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		var p Payload
		must(t, json.Unmarshal([]byte(l), &p))
		out = append(out, p)
	}
	return out
}

func TestNotifyAndDismiss(t *testing.T) {
	h := newHarness(t)
	log := filepath.Join(t.TempDir(), "hooks.jsonl")
	cfg := filepath.Join(h.home, ".config", "agent-desk", "config.toml")
	must(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
	must(t, os.WriteFile(cfg, []byte(`notify_command = "cat >> '`+log+`'"
dismiss_command = "cat >> '`+log+`'"
`), 0o644))

	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	q := h.ok(a, "ctx", "ask", "--blocked", "--title", "Which split?")
	h.ok(a, "", "tell", "--title", "fyi only")
	tn := h.ok(a, "", "tell", "--notify", "--title", "look at this")
	h.ok(a, "", "set", "findings", "x")

	got := readHookLog(t, log)
	if len(got) != 2 || got[0].Action != "notify" || got[0].DisplayID != q || got[1].DisplayID != tn {
		t.Fatalf("notifications: %+v", got)
	}
	if got[0].Path != "example-proj/r1/coord" || got[0].Event.Level != "blocked" || !got[0].Local {
		t.Fatalf("payload: %+v", got[0])
	}

	h.ok(nil, "", "answer", q, "B")
	h.ok(nil, "", "read", tn)
	got = readHookLog(t, log)
	if len(got) != 4 || got[2].Action != "dismiss" || got[2].DisplayID != q || got[2].Cause.Kind != "answer" ||
		got[3].Action != "dismiss" || got[3].DisplayID != tn {
		t.Fatalf("dismissals: %+v", got[2:])
	}

	// Imported events notify only with --notify; dismissals always run.
	h2 := newHarness(t)
	h2.ok(a, "", "join", "r2/coord")
	h2.ok(a, "ctx", "ask", "--blocked", "--title", "Elsewhere?")
	dump := h2.ok(nil, "", "export")
	h.ok(nil, dump, "import")
	if n := len(readHookLog(t, log)); n != 4 {
		t.Fatalf("import without --notify ran %d hooks", n-4)
	}
	h3 := newHarness(t)
	h3.ok(a, "", "join", "r3/coord")
	h3.ok(a, "ctx", "ask", "--blocked", "--title", "Third?")
	h.ok(nil, h3.ok(nil, "", "export"), "import", "--notify")
	got = readHookLog(t, log)
	if len(got) != 5 || got[4].Event.Title != "Third?" {
		t.Fatalf("import --notify: %+v", got[4:])
	}

	// open hands the item to open_command.
	must(t, os.WriteFile(cfg, []byte(`open_command = "cat >> '`+log+`'"`), 0o644))
	h.ok(nil, "", "open", q)
	got = readHookLog(t, log)
	if got[len(got)-1].Action != "open" || got[len(got)-1].DisplayID != q {
		t.Fatalf("open: %+v", got[len(got)-1])
	}
	// A click has no terminal: each open, the command's stderr and a
	// failure are kept in hooks.log.
	must(t, os.WriteFile(cfg, []byte(`open_command = "echo no browser >&2; exit 3"`), 0o644))
	h.run(nil, "", "open", q)
	hl, _ := os.ReadFile(filepath.Join(h.desk, "hooks.log"))
	if s := string(hl); !strings.Contains(s, " open "+q+"\n") || !strings.Contains(s, "no browser\n") ||
		!strings.Contains(s, " open "+q+" failed: exit status 3") {
		t.Fatalf("hooks.log:\n%s", hl)
	}
	must(t, os.WriteFile(cfg, []byte(`notify_comand = "typo"`), 0o644))
	_, errOut, _ := h.run(a, "", "tell", "--notify", "--title", "x")
	if !strings.Contains(errOut, "unknown key") {
		t.Fatalf("a config typo must be reported: %q", errOut)
	}
}
