package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
)

const queuedAgent = "22222222-0000-4000-8000-000000000031"

func queueFixture(t *testing.T) (*harness, string, string) {
	t.Helper()
	h := newHarness(t)
	writePaseoRecord(t, h.paseo, queuedAgent, sessA, "", "Queue fixture")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": queuedAgent}
	h.ok(env, "", "join", "r1/coord")
	q := h.ok(env, "context", "ask", "--blocked", "--title", "Fixture question")
	return h, q, h.ok(env, "notice", "tell", "--title", "Fixture notice")
}

func TestServeSendQueueDoesNotBlockWrites(t *testing.T) {
	h, q, note := queueFixture(t)
	writeConfig(t, h, "[quota]\nstopped_command = 'printf scan'\n")
	s, hd := testServer(t, h)
	s.logger = io.Discard
	ctx, stop := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int32
	s.a.Exec = func(_ context.Context, _ []byte, name string, args ...string) (string, error) {
		if name == "sh" && strings.Contains(strings.Join(args, " "), "scan") {
			return queuedAgent, nil
		}
		if name != "paseo" || args[0] != "send" || args[len(args)-1] == "--help" {
			return "", nil
		}
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
		}
		return `{"disposition":"steered"}`, nil
	}
	go func() { s.runSendQueue(ctx); close(done) }()
	defer func() { stop(); <-done }()
	answer := fmt.Sprintf(`{"id":%q,"text":"B","send_key":"answer-request-001"}`, q)
	code, body, _ := do(t, hd, "POST", "/api/answer", answer, dashboard)
	if code != 200 || !strings.Contains(body, `"status":"queued"`) {
		t.Fatalf("answer: %d %s", code, body)
	}
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("worker was not woken by the queued send")
	}

	finished := make(chan error, 1)
	go func() {
		for _, tc := range []struct{ method, path, payload, want string }{
			{"POST", "/api/read", fmt.Sprintf(`{"target":%q}`, note), ""},
			{"POST", "/api/quota", fmt.Sprintf(`{"action":"dismiss","id":%q}`, queuedAgent), ""},
			{"GET", "/api/quota", "", `"dismissed":true`},
			{"POST", "/api/answer", answer, body},
			{"POST", "/api/reply", fmt.Sprintf(`{"to":%q,"text":"follow-up"}`, q), `"status":"queued"`},
		} {
			code, body, _ := do(t, hd, tc.method, tc.path, tc.payload, dashboard)
			if code != 200 || !strings.Contains(body, tc.want) {
				finished <- fmt.Errorf("%s: %d %s", tc.path, code, body)
				return
			}
		}
		finished <- nil
	}()
	select {
	case err := <-finished:
		must(t, err)
	case <-time.After(4 * time.Second):
		t.Fatal("writes waited for the blocked send")
	}
	if calls.Load() != 1 || len(kinds(h, "answer")) != 1 || len(kinds(h, "reply")) != 1 {
		t.Fatalf("unexpected delivery count: %d", calls.Load())
	}
}

func TestServeQueueRetriesUntilDeliveredOrStopped(t *testing.T) {
	h, _, note := queueFixture(t)
	s, hd := testServer(t, h)
	s.logger = io.Discard
	now := s.a.Now()
	s.a.Now = func() time.Time { return now }

	queue := func(key string) string {
		t.Helper()
		body := fmt.Sprintf(`{"to":%q,"text":"message","send_key":%q}`, note, key)
		code, response, _ := do(t, hd, "POST", "/api/reply", body, dashboard)
		var res map[string]string
		must(t, json.Unmarshal([]byte(response), &res))
		if code != 200 || res["event_id"] == "" {
			t.Fatalf("reply: %d %s", code, response)
		}
		return res["event_id"]
	}
	entry := func(id string) Entry {
		t.Helper()
		_, body, _ := do(t, hd, "GET", "/api/item?id="+note, "", nil)
		var item itemResponse
		must(t, json.Unmarshal([]byte(body), &item))
		for _, e := range item.Thread {
			if e.Event.ID == id {
				return e
			}
		}
		t.Fatalf("missing %s: %s", id, body)
		return Entry{}
	}
	calls := 0
	var sendErr error = errors.New("fixture unavailable")
	s.a.Exec = func(_ context.Context, _ []byte, _ string, args ...string) (string, error) {
		if args[len(args)-1] == "--help" {
			return "--behavior (choices: steer, queue, interrupt)", nil
		}
		calls++
		return `{"disposition":"steered"}`, sendErr
	}
	msg := queue("reply-request-001")
	if duplicate := queue("reply-request-001"); duplicate != msg {
		t.Fatalf("duplicate %s, first %s", duplicate, msg)
	}
	if code, _, _ := do(t, hd, "POST", "/api/reply", fmt.Sprintf(`{"to":%q,"text":"different","send_key":"reply-request-001"}`, note), dashboard); code != 400 {
		t.Fatalf("reused key status = %d", code)
	}
	if e := entry(msg); e.Pending != "queued" || e.Delivery != nil {
		t.Fatalf("before first attempt: %+v", e)
	}
	if delay := s.sendQueueStep(); delay != 15*time.Second {
		t.Fatalf("first retry in %s", delay)
	}
	if e := entry(msg); e.Failure == nil || !e.Failure.Retrying || !strings.Contains(e.Failure.Error, "fixture unavailable") {
		t.Fatalf("failed attempt: %+v", e)
	}
	s.sendQueueStep()
	if calls != 1 {
		t.Fatalf("retry before due time: calls %d", calls)
	}
	now = now.Add(15 * time.Second)
	sendErr = nil
	s.sendQueueStep()
	s.sendQueueStep()
	if e := entry(msg); e.Delivery == nil || e.Delivery.Disposition != "steered" || e.Failure != nil || calls != 2 {
		t.Fatalf("successful retry: %+v, calls %d", e, calls)
	}

	giveUp := queue("reply-request-002")
	sendErr = errors.New("still down")
	s.sendQueueStep()
	now = now.Add(sendRetryWindow)
	s.sendQueueStep()
	if e := entry(giveUp); e.Failure == nil || e.Failure.Retrying || e.Pending != "not delivered" || calls != 3 {
		t.Fatalf("give up: %+v, calls %d", e, calls)
	}
	if code, body, _ := do(t, hd, "POST", "/api/deliver", fmt.Sprintf(`{"id":%q}`, giveUp), dashboard); code != 200 || !strings.Contains(body, "queued") {
		t.Fatalf("manual retry: %d %s", code, body)
	}
	sendErr = nil
	s.sendQueueStep()
	if e := entry(giveUp); e.Delivery == nil || calls != 4 {
		t.Fatalf("manual delivery: %+v, calls %d", e, calls)
	}

	sendErr = context.DeadlineExceeded
	payload := fmt.Sprintf(`{"to":%q,"text":"ambiguous","behavior":"queue","send_key":"reply-request-003"}`, note)
	_, body, _ := do(t, hd, "POST", "/api/reply", payload, dashboard)
	var res map[string]string
	must(t, json.Unmarshal([]byte(body), &res))
	timedOut := res["event_id"]
	s.sendQueueStep()
	now = now.Add(time.Minute)
	s.sendQueueStep()
	if e := entry(timedOut); e.Failure == nil || !e.Failure.Uncertain || e.Failure.Retrying || calls != 5 {
		t.Fatalf("timed-out send: %+v, calls %d", e, calls)
	}
	if code, body, _ := do(t, hd, "POST", "/api/deliver", fmt.Sprintf(`{"id":%q}`, timedOut), dashboard); code != 400 || !strings.Contains(body, "could not be confirmed") {
		t.Fatalf("ambiguous retry: %d %s", code, body)
	}

	interrupted := queue("reply-request-004")
	st, err := s.a.store()
	must(t, err)
	event, err := st.Get(interrupted)
	must(t, err)
	must(t, st.Append(s.a.deliveryState(event, "delivery-state", sendProgress{Status: "attempting"})))
	s.sendQueueStep()
	if e := entry(interrupted); e.Failure == nil || !e.Failure.Uncertain || calls != 5 {
		t.Fatalf("durable in-flight marker: %+v, calls %d", e, calls)
	}
	d, err := desk.Load(st, s.a.Host, now)
	must(t, err)
	if state := deliveryProgress(d, interrupted); state.Status != "" || !state.Uncertain {
		t.Fatalf("interrupted marker did not become terminal: %+v", state)
	}

	// Desks written before the queue recorded killed sends without a
	// structured flag. A manual retry must not resend one of those either.
	legacy := queue("reply-request-005")
	event, err = st.Get(legacy)
	must(t, err)
	must(t, st.Append(s.a.deliveryState(event, "undelivered", sendProgress{Error: "paseo send:  (signal: killed)"})))
	if e := entry(legacy); e.Failure == nil || !e.Failure.Uncertain {
		t.Fatalf("old timeout: %+v", e)
	}
	if code, _, _ := do(t, hd, "POST", "/api/deliver", fmt.Sprintf(`{"id":%q}`, legacy), dashboard); code != 400 {
		t.Fatalf("old timeout retry status: %d", code)
	}
	s.sendQueueStep()
	if calls != 5 {
		t.Fatalf("sent an unconfirmed message again: %d attempts", calls)
	}
}

func TestServeQuotaDismissUsesCachedListWithoutRescan(t *testing.T) {
	h, _, _ := queueFixture(t)
	const second = "22222222-0000-4000-8000-000000000032"
	writePaseoRecord(t, h.paseo, second, sessB, "", "Second fixture")
	writeConfig(t, h, "[quota]\nstopped_command = 'printf scan'\n")
	scans := 0
	h.respond = func(_ string, _ []string) (string, error) {
		scans++
		return queuedAgent + " " + second, nil
	}
	_, hd := testServer(t, h)
	_, _, _ = do(t, hd, "GET", "/api/quota", "", nil)
	for _, action := range []string{"dismiss", "undismiss"} {
		body := fmt.Sprintf(`{"action":%q,"targets":[%q,%q]}`, action, queuedAgent, second)
		if code, res, _ := do(t, hd, "POST", "/api/quota", body, dashboard); code != 200 {
			t.Fatalf("%s: %d %s", action, code, res)
		}
		_, res, _ := do(t, hd, "GET", "/api/quota", "", nil)
		got := strings.Count(res, `"dismissed":true`)
		if (action == "dismiss" && got != 2) || (action == "undismiss" && got != 0) || scans != 1 {
			t.Fatalf("%s: %d scans, %s", action, scans, res)
		}
	}
}
