package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A 1x1 PNG.
var tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89" +
	"\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x35\x81\x84\x00\x00\x00\x00IEND\xaeB`\x82")

func TestMediaAttachServeAndGC(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	must(t, os.WriteFile(filepath.Join(h.cwd, "plot.png"), tinyPNG, 0o644))
	must(t, os.WriteFile(filepath.Join(h.cwd, "notes.txt"), []byte("text"), 0o644))
	sum := sha256.Sum256(tinyPNG)
	file := hex.EncodeToString(sum[:]) + ".png"

	h.fails(a, "x", "needs a caption", "tell", "--title", "T", "--attach", "plot.png")
	h.fails(a, "x", "describes the --attach before it", "tell", "--title", "T", "--caption", "c")
	h.fails(a, "x", "only images and audio", "tell", "--title", "T", "--attach", "notes.txt", "--caption", "c")
	many := []string{"tell", "--title", "T"}
	for i := 0; i < 9; i++ {
		many = append(many, "--attach", "plot.png", "--caption", "c")
	}
	h.fails(a, "x", "at most 8", many...)

	tl := h.ok(a, "The loss curve.", "tell", "--title", "Loss", "--attach", "plot.png", "--caption", "Loss falls after step 200")
	keep := h.ok(a, "Unread one.", "tell", "--title", "Kept", "--attach", "plot.png", "--caption", "same file")
	if _, err := os.Stat(filepath.Join(h.desk, "media", file)); err != nil {
		t.Fatalf("not stored: %v", err)
	}

	if out := h.ok(nil, "", "show", "--format", "md"); !strings.Contains(out, "- image [plot.png](<"+filepath.Join(h.desk, "media", file)+">): Loss falls after step 200") {
		t.Fatalf("show lacks the attachment:\n%s", out)
	}

	_, hd := testServer(t, h)
	code, body, _ := do(t, hd, "GET", "/api/item?id="+tl, "", nil)
	var resp struct {
		Media []MediaView `json:"media"`
	}
	must(t, json.Unmarshal([]byte(body), &resp))
	if code != 200 || len(resp.Media) != 1 || resp.Media[0].URL != "api/media/"+file ||
		resp.Media[0].Caption != "Loss falls after step 200" || resp.Media[0].Kind != "image" {
		t.Fatalf("item: %d %s", code, body)
	}
	code, body, hdr := do(t, hd, "GET", "/api/media/"+file, "", nil)
	if code != 200 || body != string(tinyPNG) || hdr.Get("Content-Type") != "image/png" ||
		!strings.Contains(hdr.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("media: %d %v", code, hdr)
	}
	if code, _, _ := do(t, hd, "GET", "/api/media/..%2fdesk.db", "", nil); code != 404 {
		t.Fatalf("bad name: %d", code)
	}

	// Read and a month old: collected, but the same file is still named by
	// an unread item, so it stays until that one is read too.
	h.ok(nil, "", "read", tl)
	h.now = h.now.Add(31 * 24 * time.Hour)
	if out := h.ok(nil, "", "gc", "--dry-run"); out != "would delete 0 file(s), 0 B" {
		t.Fatalf("gc with an unread holder: %q", out)
	}
	h.ok(nil, "", "pin", keep)
	h.ok(nil, "", "read", keep)
	if out := h.ok(nil, "", "gc"); out != "would delete 0 file(s), 0 B" && out != "deleted 0 file(s), 0 B" {
		t.Fatalf("gc with a pinned holder: %q", out)
	}
	h.ok(nil, "", "unpin", keep)
	if out := h.ok(nil, "", "gc", "--dry-run"); !strings.HasPrefix(out, "would delete 1 file(s)") {
		t.Fatalf("dry run: %q", out)
	}
	if out := h.ok(nil, "", "gc"); !strings.HasPrefix(out, "deleted 1 file(s)") {
		t.Fatalf("gc: %q", out)
	}
	if _, err := os.Stat(filepath.Join(h.desk, "media", file)); !os.IsNotExist(err) {
		t.Fatalf("still there: %v", err)
	}
	_, hd = testServer(t, h) // its clock is a month on now
	_, body, _ = do(t, hd, "GET", "/api/item?id="+tl, "", nil)
	resp.Media = nil
	must(t, json.Unmarshal([]byte(body), &resp))
	if len(resp.Media) != 1 || resp.Media[0].URL != "" || !strings.Contains(resp.Media[0].Gone, "deleted") {
		t.Fatalf("after gc: %s", body)
	}
}

func TestMediaMergeAndPut(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	must(t, os.WriteFile(filepath.Join(h.cwd, "plot.png"), tinyPNG, 0o644))
	h.ok(a, "x", "ask", "--blocked", "--title", "Which plot?", "--attach", "plot.png", "--caption", "option A")
	sum := sha256.Sum256(tinyPNG)
	file := hex.EncodeToString(sum[:]) + ".png"

	hb := *h
	hb.desk = filepath.Join(t.TempDir(), "desk-b")
	hb.host = "host-b"
	hb.ok(nil, "", "merge", filepath.Join(h.desk, "desk.db"))
	if _, err := os.Stat(filepath.Join(hb.desk, "media", file)); err != nil {
		t.Fatalf("merge did not copy the attachment: %v", err)
	}
	if out := hb.ok(nil, "", "media", "list"); out != file {
		t.Fatalf("media list: %q", out)
	}

	// put checks the content against the name.
	other := strings.Repeat("0", 64) + ".png"
	_, errOut, code := hb.run(nil, string(tinyPNG), "media", "put", other)
	if code == 0 || !strings.Contains(errOut, "hashes to") {
		t.Fatalf("put with a wrong name: %d %q", code, errOut)
	}
	hb.fails(nil, "", "not a media file name", "media", "cat", "../desk.db")
}

// merge ssh: sends the files the other side lacks and fetches the ones this
// side lacks.
func TestMediaMergeSSH(t *testing.T) {
	h := newHarness(t)
	self, err := os.Executable()
	must(t, err)
	remoteHome := filepath.Join(t.TempDir(), "remote-desk")
	fake := filepath.Join(t.TempDir(), "fake-ssh")
	must(t, os.WriteFile(fake, []byte("#!/bin/sh\nshift 3\nexec sh -c \"$1\"\n"), 0o755))
	sshEnv := map[string]string{
		"AGENT_DESK_SSH": fake,
		"AGENT_DESK_REMOTE_COMMAND": "AGENT_DESK_AS_CLI=1 AGENT_DESK_HOME='" + remoteHome +
			"' AGENT_DESK_HOST=host-b AGENT_DESK_CONFIG=/nonexistent '" + self + "'",
	}
	wav := append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), make([]byte, 32)...)
	must(t, os.WriteFile(filepath.Join(h.cwd, "plot.png"), tinyPNG, 0o644))
	must(t, os.WriteFile(filepath.Join(h.cwd, "note.wav"), wav, 0o644))
	name := func(b []byte, ext string) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) + ext }

	hb := *h
	hb.desk, hb.host = remoteHome, "host-b"
	hb.ok(h.as(sessB), "", "join", "far/coord")
	hb.ok(h.as(sessB), "x", "tell", "--title", "Far", "--attach", "note.wav", "--caption", "a voice note")
	h.ok(h.as(sessA), "", "join", "near/coord")
	h.ok(h.as(sessA), "x", "tell", "--title", "Near", "--attach", "plot.png", "--caption", "a plot")

	if _, errOut, code := h.run(sshEnv, "", "merge", "ssh:other"); code != 0 {
		t.Fatalf("merge ssh: %s", errOut)
	}
	for _, f := range []string{filepath.Join(h.desk, "media", name(wav, ".wav")),
		filepath.Join(remoteHome, "media", name(tinyPNG, ".png"))} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("not exchanged: %v", err)
		}
	}
}
