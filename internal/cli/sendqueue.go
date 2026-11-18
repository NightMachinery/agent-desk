package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/ids"
	"github.com/NightMachinery/agent-desk/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	sendRetryWindow = 24 * time.Hour
	sendQueuePoll   = 30 * time.Second
)

var sendKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,80}$`)

type sendUnavailable struct{ error }

// enqueue commits the message before asking the worker to render, notify
// and deliver it. The key handles a retry after a lost HTTP acknowledgement.
func (s *server) enqueue(app *App, kind string, body request, prepare func() ([]store.Event, error)) (any, error) {
	st, err := app.store()
	if err != nil {
		return nil, sendUnavailable{err}
	}
	key, fingerprint := body.SendKey, ""
	result := func(e store.Event) (any, error) {
		if key != "" && desk.MetaString(e, "send_request") != fingerprint {
			return nil, usagef("this send key already belongs to a different message")
		}
		return map[string]string{"id": sendDisplayID(st, e), "event_id": e.ID, "status": "queued"}, nil
	}
	if key != "" {
		if !sendKeyPattern.MatchString(key) {
			return nil, usagef("invalid send key")
		}
		data, _ := json.Marshal(body)
		fingerprint = fmt.Sprintf("%x", sha256.Sum256(append([]byte(kind), data...)))
		if e, err := st.WebSend(app.Host, key); err != nil {
			return nil, sendUnavailable{err}
		} else if e.ID != "" {
			s.wakeSendQueue()
			return result(e)
		}
	}
	evs, err := prepare()
	if err != nil {
		return nil, err
	}
	msg := markQueued(evs[0], key, fingerprint)
	evs[0] = msg
	inserted := true
	if key != "" {
		msg, inserted, err = st.AppendWebSend(key, evs)
	} else {
		err = st.Append(evs...)
	}
	if err != nil {
		return nil, sendUnavailable{err}
	}
	if inserted {
		s.sendCommitted(evs)
	} else {
		s.wakeSendQueue()
	}
	return result(msg)
}

func markQueued(msg store.Event, key, fingerprint string) store.Event {
	m := desk.Meta(msg)
	m["send_queue"] = true
	if key != "" {
		m["send_key"], m["send_request"] = key, fingerprint
	}
	msg.Meta = meta(m)
	return msg
}

// sendCommitted gives committed sends their post-commit effects, then wakes delivery.
func (s *server) sendCommitted(events []store.Event) {
	s.sendMu.Lock()
	s.sendEffects = append(s.sendEffects, events)
	s.sendMu.Unlock()
	s.wakeSendQueue()
}

// Only events sharing the shortest suffix affect the display token.
func sendDisplayID(st *store.Store, e store.Event) string {
	suffix := e.ID[len(e.ID)-ids.MinSuffix:]
	rows, err := st.DB.Query("SELECT id FROM events WHERE substr(id, -?) = ?", ids.MinSuffix, suffix)
	if err != nil {
		return e.ID
	}
	defer rows.Close()
	var peers []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return e.ID
		}
		peers = append(peers, id)
	}
	if rows.Err() != nil {
		return e.ID
	}
	return ids.NewTable(peers).Display(e.ID, e.Kind)
}

func (s *server) wakeSendQueue() {
	select {
	case s.sendWake <- struct{}{}:
	default:
	}
}

func (s *server) runSendQueue(ctx context.Context) {
	delay := time.Duration(0)
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.sendWake:
			timer.Stop()
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		delay = s.sendQueueStep()
	}
}

// sendQueueStep also scans on startup and while idle, picking up pending
// deliveries in a merged store even when there was no local HTTP request.
func (s *server) sendQueueStep() time.Duration {
	app, _, errb := s.userApp()
	s.sendMu.Lock()
	effects := s.sendEffects
	s.sendEffects = nil
	s.sendMu.Unlock()
	for _, evs := range effects {
		msg := evs[0]
		host := msg.Host
		if msg.Kind == "answer" {
			if q, err := app.st.Get(msg.Ref); err == nil {
				host = q.Host
			}
		} else {
			_, host = toOf(msg)
		}
		if host != "" && host != app.Host {
			app.pushTo(host, "its session is on "+host)
		}
		app.announce(evs, false, false)
	}
	if len(effects) > 0 {
		app.maybeGC()
	}
	_, changed, next, err := app.deliverPending("", true)
	if changed || len(effects) > 0 {
		_ = app.renderWarn()
	}
	if err != nil {
		fmt.Fprintf(s.logger, "agent-desk: send queue: %v\n", err)
	}
	if n := strings.TrimSpace(errb.String()); n != "" {
		fmt.Fprintln(s.logger, n)
	}
	if next.IsZero() {
		return sendQueuePoll
	}
	return max(0, min(sendQueuePoll, next.Sub(app.Now())))
}

type sendProgress struct {
	ID        string `json:"-"`
	Status    string `json:"status,omitempty"`
	Error     string `json:"error,omitempty"`
	Resume    string `json:"resume,omitempty"`
	RetryAt   string `json:"retry_at,omitempty"`
	Deadline  string `json:"deadline,omitempty"`
	Attempts  int    `json:"attempts,omitempty"`
	Uncertain bool   `json:"uncertain,omitempty"`
	Forced    bool   `json:"forced,omitempty"`
}

func queuedForDelivery(e store.Event) bool { return desk.Meta(e)["send_queue"] == true }

func deliveryProgress(d *desk.Desk, id string) sendProgress {
	e, ok := d.DeliveryProgress(id)
	if !ok {
		return sendProgress{}
	}
	var state sendProgress
	_ = json.Unmarshal(e.Meta, &state)
	// Older desks recorded timeouts as ordinary failures.
	if message, ok := strings.CutPrefix(state.Error, "paseo send: "); ok {
		state.Uncertain = state.Uncertain || paseoAcknowledgementLost(message)
	}
	state.ID = e.ID
	return state
}

func (a *App) deliveryState(msg store.Event, kind string, progress sendProgress) store.Event {
	e := a.event(kind, msg.Path, ident.Resolve(a.Env), "cli", nil)
	// Two processes can generate ULIDs in the same millisecond. A delivery
	// action must sort after the specific action that preceded it.
	if e.ID <= progress.ID {
		if previous, err := ulid.Parse(progress.ID); err == nil {
			e.ID = ids.New(time.UnixMilli(int64(previous.Time()) + 1))
		}
	}
	e.Meta = meta(map[string]any{"status": progress.Status, "error": progress.Error, "resume": progress.Resume,
		"retry_at": progress.RetryAt, "deadline": progress.Deadline, "attempts": progress.Attempts,
		"uncertain": progress.Uncertain, "forced": progress.Forced})
	e.Ref = msg.ID
	return e
}

func parseSendTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

func earlierSend(a, b time.Time) time.Time {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}

func sendRetryDelay(attempts int) time.Duration {
	return min(10*time.Minute, 15*time.Second<<min(max(0, attempts-1), 6))
}

func deliveryTarget(d *desk.Desk, token string) (store.Event, error) {
	e, err := d.Resolve(token)
	if err != nil {
		return e, err
	}
	if e.Kind == "ask" {
		e = d.ByID[d.AnswerID(e.ID)]
	}
	if e.Kind != "answer" && e.Kind != "reply" {
		return e, usagef("nothing to deliver for %s", token)
	}
	return e, nil
}

func deliveryJobID(e store.Event) string {
	if e.Kind == "answer" {
		return e.Ref
	}
	return e.ID
}

func (a *App) queueRetry(d *desk.Desk, e store.Event, force bool, failureID string) (string, error) {
	if _, ok := d.Delivery(e.ID); ok {
		return "delivered", nil
	}
	state := deliveryProgress(d, e.ID)
	if state.Status != "" {
		return "queued", nil
	}
	if state.Uncertain && !force {
		return "", usagef("delivery could not be confirmed. The agent may already have the message")
	}
	if force && !state.Uncertain {
		return "", usagef("force is for unconfirmed delivery. Use Retry for a confirmed failure")
	}
	if failureID != "" && state.ID != failureID {
		return "", usagef("the delivery state changed. Check the latest attempt before sending again")
	}
	for _, j := range pending(d, a.Host, deliveryJobID(e)) {
		if j.msg.ID == e.ID {
			retry := a.deliveryState(e, "delivery-state", sendProgress{Status: "queued", Forced: force, ID: state.ID})
			if _, err := a.st.AppendDeliveryIfCurrent(retry, state.ID); err != nil {
				return "", sendUnavailable{err}
			}
			return "queued", nil
		}
	}
	return "", usagef("%s is not pending on this desk", d.Display(e.ID))
}
