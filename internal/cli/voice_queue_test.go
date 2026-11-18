package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NightMachinery/agent-desk/internal/stt"
)

func TestServeQueuesVoiceBeforeTranscriptionFinishes(t *testing.T) {
	h, _, note := queueFixture(t)
	sentinel := filepath.Join(t.TempDir(), "fail-stt")
	cfg := strings.Replace(voiceTestConfig, "dd bs=2", "if [ -f '"+sentinel+"' ]; then exit 3; fi\ndd bs=2", 1)
	writeConfig(t, h, cfg)
	s, hd := testServer(t, h)
	s.logger = io.Discard
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	sends := make(chan string, 8)
	s.a.Exec = func(_ context.Context, _ []byte, name string, args ...string) (string, error) {
		if name == "paseo" && len(args) > 0 && args[0] == "send" && args[len(args)-1] != "--help" {
			sends <- args[len(args)-1]
		}
		return `{"disposition":"steered"}`, nil
	}
	go func() { s.runSendQueue(ctx); close(done) }()
	defer func() { stop(); <-done }()

	start := func() recording {
		t.Helper()
		rec := testVoiceRequest(t, hd, "POST", "", `{"node":"example-proj/r1/coord"}`)
		if code, body, _ := do(t, hd, "POST", "/api/recordings/"+rec.ID+"/chunk?offset=0", "\x01\x02", dashboard); code != 200 {
			t.Fatalf("upload: %d %s", code, body)
		}
		return rec
	}
	queue := func(rec recording, key string) string {
		t.Helper()
		text := fmt.Sprintf("before [voice %s 0:00, transcribing] after", rec.ID)
		req := fmt.Sprintf(`{"to":%q,"text":%q,"send_key":%q}`, note, text, key)
		code, body, _ := do(t, hd, "POST", "/api/reply", req, dashboard)
		var ack map[string]string
		must(t, json.Unmarshal([]byte(body), &ack))
		if code != 200 || ack["status"] != "queued" || ack["event_id"] == "" {
			t.Fatalf("reply: %d %s", code, body)
		}
		return ack["event_id"]
	}
	wait := func(id string, match func(Entry) bool) Entry {
		t.Helper()
		return testVoiceEntry(t, hd, note, id, match)
	}
	checkSent := func(id, expect string) Entry {
		t.Helper()
		select {
		case text := <-sends:
			if !strings.Contains(text, expect) || strings.Contains(text, "[voice ") {
				t.Fatalf("transport sent %q, want %q without token", text, expect)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("transcription result did not wake the delivery worker")
		}
		return wait(id, func(e Entry) bool { return e.Delivery != nil })
	}
	queued := func(rec recording, key string) string {
		t.Helper()
		msg := queue(rec, key)
		wait(msg, func(e Entry) bool { return e.Pending == "waiting for transcription" })
		if duplicate := queue(rec, key); duplicate != msg {
			t.Fatalf("lost acknowledgement created a second message: %s", duplicate)
		}
		events, err := s.a.st.Query("events", "ref = ? AND kind = 'delivery-state'", msg)
		must(t, err)
		if len(events) != 1 {
			t.Fatalf("voice waiting created extra delivery markers: %+v", events)
		}
		return msg
	}
	fail := func(key string) (recording, string, Entry) {
		t.Helper()
		must(t, os.WriteFile(sentinel, nil, 0600))
		rec := start()
		msg := queued(rec, key)
		testVoiceRequest(t, hd, "POST", "/"+rec.ID, `{"action":"finish"}`)
		e := wait(msg, func(e Entry) bool { return e.VoiceFailure != nil })
		if e.Pending != "transcription failed" || e.VoiceFailure.Recording != rec.ID ||
			!e.VoiceFailure.Retryable || !strings.Contains(e.VoiceFailure.Error, "exit status 3") || e.Failure != nil {
			t.Fatalf("failed voice state: %+v", e)
		}
		must(t, os.Remove(sentinel))
		return rec, msg, e
	}

	rec := start()
	msg := queued(rec, "voice-request-001")
	testVoiceRequest(t, hd, "POST", "/"+rec.ID, `{"action":"finish"}`)
	block := voiceBlock("one two")
	resolved := checkSent(msg, block)
	if !strings.Contains(resolved.Event.Body, block) || strings.Contains(resolved.Event.Body, "[voice ") {
		t.Fatalf("delivered thread shows %q", resolved.Event.Body)
	}

	retry, failed, _ := fail("voice-request-002")
	testVoiceRequest(t, hd, "POST", "/"+retry.ID, `{"action":"retry"}`)
	checkSent(failed, block)

	omit, stuck, failure := fail("voice-request-003")
	omitRequest := fmt.Sprintf(`{"id":%q,"action":"omit-recording","failure_id":%q}`, stuck, failure.VoiceFailure.ID)
	if code, body, _ := do(t, hd, "POST", "/api/deliver", omitRequest, dashboard); code != 200 || !strings.Contains(body, "queued") {
		t.Fatalf("omit: %d %s", code, body)
	}
	checkSent(stuck, "(Voice recording omitted.)")
	if code, body, _ := do(t, hd, "POST", "/api/deliver", omitRequest, dashboard); code != 200 || !strings.Contains(body, "delivered") {
		t.Fatalf("second omit: %d %s", code, body)
	}
	testVoiceRequest(t, hd, "POST", "/"+omit.ID, `{"action":"retry"}`)
	testVoiceWait(t, hd, omit.ID, func(r recording) bool { return r.Status == voiceReady })

	stop()
	<-done
	s.sendQueueStep()
	select {
	case extra := <-sends:
		t.Fatalf("a send was delivered twice: %q", extra)
	default:
	}
	code, body, _ := do(t, hd, "GET", "/api/recordings", "", nil)
	var list struct{ Recordings []recording }
	must(t, json.Unmarshal([]byte(body), &list))
	if code != 200 || len(list.Recordings) != 3 {
		t.Fatalf("recordings: %d %s", code, body)
	}
	for _, r := range list.Recordings {
		if (len(r.Messages) == 1) != (r.ID != omit.ID) {
			t.Fatalf("recording %s links to %v", r.ID, r.Messages)
		}
	}
}

func testVoiceEntry(t *testing.T, hd http.Handler, note, id string, match func(Entry) bool) Entry {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	var latest Entry
	for time.Now().Before(until) {
		code, body, _ := do(t, hd, "GET", "/api/item?id="+note, "", nil)
		if code != 200 {
			t.Fatalf("item: %d %s", code, body)
		}
		var item itemResponse
		must(t, json.Unmarshal([]byte(body), &item))
		for _, e := range item.Thread {
			if e.Event.ID == id {
				latest = e
				if match(e) {
					return e
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("message %s is in state %+v", id, latest)
	return Entry{}
}

func TestVoiceRecoveryMakesInterruptedAudioRetryable(t *testing.T) {
	h, _, _ := queueFixture(t)
	s, _ := testServer(t, h)
	rec := recording{ID: "v-001122334455", Node: "example-proj/r1/coord", Created: s.a.Now(),
		Backend: "command", Status: voiceRecording}
	must(t, os.MkdirAll(voiceDir(s.a.Home), 0700))
	must(t, os.WriteFile(voiceFile(s.a.Home, rec.ID), append(stt.WAVHeader(2), 1, 2), 0600))
	must(t, s.a.appendRecording(s.a.st, rec))
	must(t, s.recoverVoice())
	_, _, err := resolveVoiceTokens(s.a.st, rec.Node, "[voice "+rec.ID+" 0:00, recording]", nil)
	blocked, ok := err.(*voiceBlocked)
	if !ok || !blocked.failed || !blocked.retryable || !strings.Contains(blocked.reason, "interrupted") {
		t.Fatalf("recovered voice state: %v", err)
	}
}

func TestVoiceQueueResolvesBeforeOnAnswer(t *testing.T) {
	h, _, _ := queueFixture(t)
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": queuedAgent}
	question := h.ok(env, "context", "ask", "--blocked", "--title", "Voice question", "--on-answer", "printf fixture")
	writeConfig(t, h, voiceTestConfig)
	s, hd := testServer(t, h)
	s.logger = io.Discard
	var hooks []string
	s.a.Exec = func(_ context.Context, stdin []byte, name string, args ...string) (string, error) {
		if name == "env" && len(args) != 0 && strings.HasPrefix(args[0], "AGENT_DESK_") {
			hooks = append(hooks, string(stdin))
		}
		return "fixture", nil
	}
	rec := testVoiceRequest(t, hd, "POST", "", `{"node":"example-proj/r1/coord"}`)
	answer := fmt.Sprintf(`{"id":%q,"text":"[voice %s 0:00, recording]","send_key":"voice-answer-001"}`, question, rec.ID)
	code, body, _ := do(t, hd, "POST", "/api/answer", answer, dashboard)
	if code != 200 {
		t.Fatalf("answer: %d %s", code, body)
	}
	s.sendQueueStep()
	if len(hooks) != 0 {
		t.Fatalf("on-answer ran while waiting: %v", hooks)
	}
	if code, body, _ := do(t, hd, "POST", "/api/recordings/"+rec.ID+"/chunk?offset=0", "\x01\x02", dashboard); code != 200 {
		t.Fatalf("upload: %d %s", code, body)
	}
	testVoiceRequest(t, hd, "POST", "/"+rec.ID, `{"action":"finish"}`)
	testVoiceWait(t, hd, rec.ID, func(r recording) bool { return r.Status == voiceReady })
	s.sendQueueStep()
	s.sendQueueStep()
	if len(hooks) != 1 || strings.TrimSpace(hooks[0]) != voiceBlock("one two") {
		t.Fatalf("on-answer stdin: %q", hooks)
	}
}
