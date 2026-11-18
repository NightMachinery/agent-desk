package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Any file can be attached: images, audio and video play in the page, the
// rest are downloads. The user's answers and messages from the dashboard
// carry uploaded files, and their session gets the files' paths.
func TestAttachAnyFileAndUserUploads(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-0000000000f1"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "Attach coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "r1/coord")
	must(t, os.WriteFile(filepath.Join(h.cwd, "report.pdf"), []byte("%PDF-1.4 tiny"), 0o644))
	must(t, os.WriteFile(filepath.Join(h.cwd, "blob.xyz"), []byte{0, 1, 2, 3}, 0o644))
	tl := h.ok(env, "Two files.", "tell", "--title", "Files", "--attach", "report.pdf", "--caption", "the report",
		"--attach", "blob.xyz", "--caption", "raw data")

	_, hd := testServer(t, h)
	code, body, _ := do(t, hd, "GET", "/api/item?id="+tl, "", nil)
	if code != 200 {
		t.Fatalf("item: %d %s", code, body)
	}
	var item struct {
		Media []MediaView `json:"media"`
	}
	must(t, json.Unmarshal([]byte(body), &item))
	if len(item.Media) != 2 || item.Media[0].Kind != "file" || item.Media[0].MIME != "application/pdf" ||
		item.Media[1].MIME != "application/octet-stream" || !strings.HasSuffix(item.Media[1].File(), ".bin") {
		t.Fatalf("media: %+v", item.Media)
	}
	code, _, hdr := do(t, hd, "GET", "/"+item.Media[1].URL, "", nil)
	if code != 200 || hdr.Get("Content-Disposition") != `attachment; filename=blob.xyz` {
		t.Fatalf("download: %d %q", code, hdr.Get("Content-Disposition"))
	}

	// Uploads need the dashboard's header.
	if code, _, _ := do(t, hd, "POST", "/api/upload?name=x.txt", "hi", nil); code != 403 {
		t.Fatalf("upload without the header: %d", code)
	}
	up := map[string]string{"X-Agent-Desk": "1", "Content-Type": "application/octet-stream", "Origin": "http://example.com"}
	code, body, _ = do(t, hd, "POST", "/api/upload?name="+url.QueryEscape("../shot.png")+"&caption=the+bug", string(tinyPNG), up)
	if code != 200 {
		t.Fatalf("upload: %d %s", code, body)
	}
	var res struct {
		Ref  json.RawMessage `json:"ref"`
		View MediaView       `json:"view"`
	}
	must(t, json.Unmarshal([]byte(body), &res))
	if res.View.Name != "shot.png" || res.View.Kind != "image" || res.View.URL == "" {
		t.Fatalf("upload view: %+v", res.View)
	}

	// A message that names a file not uploaded is refused.
	fake := `{"to":"` + tl + `","text":"x","media":[{"sha256":"` + strings.Repeat("a", 64) + `","name":"n.png","mime":"image/png"}]}`
	if code, body, _ := do(t, hd, "POST", "/api/reply", fake, dashboard); code != 400 || !strings.Contains(body, "upload it again") {
		t.Fatalf("fake ref: %d %s", code, body)
	}

	// A message of files only reaches the session with their paths.
	h.respond = func(name string, args []string) (string, error) { return `{"disposition":"steered"}`, nil }
	code, body, _ = do(t, hd, "POST", "/api/reply", fmt.Sprintf(`{"to":%q,"text":"","media":[%s]}`, tl, res.Ref), dashboard)
	if code != 200 {
		t.Fatalf("reply: %d %s", code, body)
	}
	var sent string
	for _, c := range *h.calls {
		if strings.HasPrefix(c, "paseo send") {
			sent = c
		}
	}
	path := filepath.Join(h.desk, "media", res.View.File())
	if !strings.Contains(sent, attachedOnly) || !strings.Contains(sent, "- shot.png (image/png, ") ||
		!strings.Contains(sent, path) || !strings.Contains(sent, "Caption: the bug") {
		t.Fatalf("sent: %q", sent)
	}

	// The thread shows the file under the user's message.
	code, body, _ = do(t, hd, "GET", "/api/item?id="+tl, "", nil)
	if code != 200 || !strings.Contains(body, `"url":"api/media/`+res.View.File()+`"`) {
		t.Fatalf("thread: %d %s", code, body)
	}

	// An answer from another machine waits for its files to arrive.
	a, _, _ := h.app(nil, "")
	d, err := a.load()
	must(t, err)
	var reply = d.Events[len(d.Events)-1]
	for _, e := range d.Events {
		if e.Kind == "reply" {
			reply = e
		}
	}
	must(t, os.Remove(path))
	if _, missing := a.attachmentNote(reply); missing != "shot.png" {
		t.Fatalf("missing: %q", missing)
	}

	// From the CLI, a caption is optional for the user.
	q := h.ok(env, "ctx", "ask", "--blocked", "--title", "Which plot?")
	*h.calls = nil
	h.ok(nil, "", "answer", q, "--attach", "report.pdf", "this one")
	if c := strings.Join(*h.calls, "\n"); !strings.Contains(c, ": this one") || !strings.Contains(c, "- report.pdf (application/pdf, 13 B): ") ||
		strings.Contains(c, "Caption:") {
		t.Fatalf("answer --attach sent: %q", c)
	}
	h.fails(nil, "", "not one given in chat", "answer", q, "--from-chat", "x", "--attach", "report.pdf")
}
