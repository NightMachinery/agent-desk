package cli

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const voiceTestConfig = `
media_keep_days = 0
[voice]
stt_backend = "command"
keep_days = 1
command = """
dd bs=2 count=1 of=/dev/null 2>/dev/null
printf '%s\\n' '{"type":"partial","text":"one"}'
cat >/dev/null
printf '%s\\n' '{"type":"final","text":"one two"}'
"""
`

func testVoiceRequest(t *testing.T, hd http.Handler, method, path, body string) recording {
	t.Helper()
	code, raw, _ := do(t, hd, method, "/api/recordings"+path, body, dashboard)
	if code != 200 {
		t.Fatalf("%s %s = %d: %s", method, path, code, raw)
	}
	var rec recording
	must(t, json.Unmarshal([]byte(raw), &rec))
	return rec
}

func testVoiceWait(t *testing.T, hd http.Handler, id string, expect func(recording) bool) recording {
	t.Helper()
	last := recording{}
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		last = testVoiceRequest(t, hd, "GET", "/"+id, "")
		if expect(last) {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("unexpected recording state: %+v", last)
	return recording{}
}

func TestVoiceCaptureResolveAndExpire(t *testing.T) {
	h := newHarness(t)
	h.ok(h.as(sessA), "", "join", "r1/coord")
	writeConfig(t, h, voiceTestConfig)
	s, hd := testServer(t, h)
	node := "example-proj/r1/coord"
	req := `{"node":"` + node + `"}`
	if code, _, _ := do(t, hd, "POST", "/api/recordings", req, nil); code != 403 {
		t.Fatalf("recording without origin header: %d", code)
	}
	rec := testVoiceRequest(t, hd, "POST", "", req)
	match := "before [voice " + rec.ID + " 0:00, transcribing] after"
	st := s.voice.store
	if _, _, err := resolveVoiceTokens(st, node, match, nil); err == nil || !strings.Contains(err.Error(), "waiting") {
		t.Fatalf("unfinished voice should block delivery: %v", err)
	}
	if code, body, _ := do(t, hd, "POST", "/api/recordings/"+rec.ID+"/chunk?offset=0", "\x01\x02\x03\x04", dashboard); code != 200 {
		t.Fatalf("audio upload: %d %s", code, body)
	}
	testVoiceWait(t, hd, rec.ID, func(r recording) bool { return r.Partial == "one" && r.Status == voiceRecording })
	testVoiceRequest(t, hd, "POST", "/"+rec.ID, `{"action":"finish"}`)
	final := testVoiceWait(t, hd, rec.ID, func(r recording) bool { return r.Status == voiceReady })
	if final.Text != "one two" || final.Bytes != 4 {
		t.Fatalf("unexpected final transcript: %+v", final)
	}
	resolved, _, err := resolveVoiceTokens(st, node, match, nil)
	must(t, err)
	if resolved != "before \n\n```speech-to-text\none two\n```\n\n after" {
		t.Fatalf("resolved message = %q", resolved)
	}
	code, audio, headers := do(t, hd, "GET", "/api/recordings/"+rec.ID+"/audio", "", nil)
	if code != 200 || headers.Get("Content-Type") != "audio/wav" || len(audio) != 48 || binary.LittleEndian.Uint32([]byte(audio[40:])) != 4 {
		t.Fatalf("invalid saved WAV: %d %s %x", code, headers.Get("Content-Type"), audio)
	}
	originalNow := s.a.Now
	s.a.Now = func() time.Time { return originalNow().Add(30 * time.Hour) }
	for _, dry := range []bool{true, false} {
		count, bytes, err := s.a.gcVoice(1, dry)
		must(t, err)
		if count != 1 || bytes != 48 {
			t.Fatalf("gc dry=%v: count=%d bytes=%d", dry, count, bytes)
		}
	}
	if _, err := os.Stat(voiceFile(s.a.Home, rec.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("audio after gc: %v", err)
	}
	if _, _, err := resolveVoiceTokens(st, node, match, nil); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expired audio should block unresolved delivery: %v", err)
	}
}

func TestVoiceRetryAfterFailure(t *testing.T) {
	h := newHarness(t)
	h.ok(h.as(sessA), "", "join", "r1/coord")
	sentinel := filepath.Join(t.TempDir(), "fail-stt")
	must(t, os.WriteFile(sentinel, nil, 0600))
	cfg := strings.Replace(voiceTestConfig, "dd bs=2", "if [ -f '"+sentinel+"' ]; then exit 3; fi\ndd bs=2", 1)
	writeConfig(t, h, cfg)
	_, hd := testServer(t, h)
	rec := testVoiceRequest(t, hd, "POST", "", `{"node":"example-proj/r1/coord"}`)
	if code, body, _ := do(t, hd, "POST", "/api/recordings/"+rec.ID+"/chunk?offset=0", "\x01\x02", dashboard); code != 200 {
		t.Fatalf("audio survives backend failure: %d %s", code, body)
	}
	testVoiceRequest(t, hd, "POST", "/"+rec.ID, `{"action":"finish"}`)
	failed := testVoiceWait(t, hd, rec.ID, func(r recording) bool { return r.Status == voiceFailed })
	if failed.Bytes != 2 || failed.Error == "" {
		t.Fatalf("failed recording = %+v", failed)
	}
	must(t, os.Remove(sentinel))
	testVoiceRequest(t, hd, "POST", "/"+rec.ID, `{"action":"retry","backend":"command"}`)
	final := testVoiceWait(t, hd, rec.ID, func(r recording) bool { return r.Status == voiceReady })
	if final.Text != "one two" || final.Error != "" {
		t.Fatalf("retried transcript = %+v", final)
	}
}

func TestVoiceFenceWithLiteralBackticks(t *testing.T) {
	if block := voiceBlock("literal ``` fence"); block != "````speech-to-text\nliteral ``` fence\n````" {
		t.Fatalf("voice fence = %q", block)
	}
}
