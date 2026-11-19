package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Id mentions link only when they are exactly a display id in the store
// (docs/spec-conversation.md, "Id links").
func TestIDLinks(t *testing.T) {
	h := newHarness(t)
	log := filepath.Join(t.TempDir(), "hooks.jsonl")
	notifyConfig(t, h, log, "away_command = \"exit 1\"\n")
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	q := h.ok(a, "ctx", "ask", "--blocked", "--title", "Which split?")
	body := "See " + q + ", then run a t-test. q-zzzz is not on the desk."
	tl := h.ok(a, body, "tell", "--notify", "--title", "Next steps after "+q)

	// 1. The item response resolves the real id only, with what a hover needs.
	_, hd := testServer(t, h)
	_, raw, _ := do(t, hd, "GET", "/api/item?id="+tl, "", nil)
	var item struct {
		Refs map[string]Ref `json:"refs"`
	}
	must(t, json.Unmarshal([]byte(raw), &item))
	r, ok := item.Refs[q]
	if !ok || len(item.Refs) != 1 || r.Title != "Which split?" || r.Kind != "question" || r.State != "open, blocked" ||
		r.URL != "#/p/example-proj/r1/coord?item="+q {
		t.Fatalf("item refs: %+v", item.Refs)
	}
	_, raw, _ = do(t, hd, "GET", "/api/view", "", nil)
	if !strings.Contains(raw, `"refs":{"`+q+`":`) || strings.Contains(raw, `"t-test"`) {
		t.Fatalf("view refs: %s", raw)
	}

	// 4. The notify payload maps the mention to its full item URL.
	var p *Payload
	for _, x := range readHookLog(t, log) {
		if x.DisplayID == tl {
			x := x
			p = &x
		}
	}
	if p == nil || p.Refs[q] != "https://desk.example.net/#/p/example-proj/r1/coord?item="+q || len(p.Refs) != 1 {
		t.Fatalf("payload refs: %+v", p)
	}

	// 5. The org render gives item headings a CUSTOM_ID and links mentions.
	inbox, err := os.ReadFile(filepath.Join(h.desk, "render", "inbox.org"))
	must(t, err)
	s := string(inbox)
	if !strings.Contains(s, ":CUSTOM_ID: "+q+"\n") || !strings.Contains(s, "See [["+"#"+q+"]["+q+"]], then run a t-test") {
		t.Fatalf("inbox.org:\n%s", s)
	}
}
