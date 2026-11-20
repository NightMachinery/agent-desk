package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/NightMachinery/agent-desk/internal/config"
	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/push"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Push: the third notification channel, beside desktop and remote. A
// browser that turned on "Push to this device" in the dashboard's settings
// is a subscription, kept in <desk>/push on the machine serving the
// dashboard; that machine pushes to it, for events written there and those
// it imports, as the notify.push setting says (built in: blocked questions,
// always). The service worker shows the message as a notification, tagged
// with the item's display id; when the item stops needing the user, a
// second message closes it.

// pushNote is what the service worker receives.
type pushNote struct {
	Type   string `json:"type"` // show or close
	Tag    string `json:"tag"`
	Title  string `json:"title,omitempty"`
	Body   string `json:"body,omitempty"`
	URL    string `json:"url,omitempty"`    // relative to the dashboard
	Sticky bool   `json:"sticky,omitempty"` // stays until acted on
}

func (a *App) pushDir() push.Dir { return push.Dir(filepath.Join(a.Home, "push")) }

// pushOn reports whether this machine has devices to push to.
func (a *App) pushOn(c config.Config) bool {
	if c.Push.Off {
		return false
	}
	subs, err := a.pushDir().Subs()
	return err == nil && len(subs) > 0
}

func (a *App) pushSender(c config.Config) (push.Sender, error) {
	key, public, err := a.pushDir().Keys()
	if err != nil {
		return push.Sender{}, err
	}
	subject := c.Push.Subject
	if subject == "" {
		subject = push.DefaultSubject
	}
	return push.Sender{Key: key, Public: public, Subject: subject, Client: a.HTTP, Now: a.Now}, nil
}

// pushSend sends n to every subscription (or the one whose id is only),
// records how each went, and drops those the push service says are gone.
// It returns how many took it.
func (a *App) pushSend(c config.Config, n pushNote, m push.Message, only string) (int, error) {
	x, err := a.pushSender(c)
	if err != nil {
		return 0, err
	}
	m.Payload, _ = json.Marshal(n)
	subs, err := a.pushDir().Subs()
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	results := map[string]error{}
	var errs []string
	for _, s := range subs {
		if only != "" && push.ID(s.Endpoint) != only {
			continue
		}
		err := x.Send(ctx, s, m)
		results[s.Endpoint] = err
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", labelOf(s), err))
		}
	}
	if len(results) == 0 {
		return 0, errors.New("no device is subscribed to push")
	}
	now := a.Now().UTC().Format(time.RFC3339)
	sent := 0
	err = a.pushDir().Update(func(subs []push.Sub) []push.Sub {
		out := subs[:0]
		for _, s := range subs {
			r, tried := results[s.Endpoint]
			switch {
			case !tried:
			case r == nil:
				s.LastOK, s.LastErr = now, ""
				sent++
			case errors.Is(r, push.ErrGone):
				a.pushLog("gone", s, "push service says the subscription no longer exists; dropped")
				continue
			default:
				s.LastErr = now + " " + clipLine(r.Error(), 200)
				a.pushLog("error", s, clipLine(r.Error(), 200))
			}
			out = append(out, s)
		}
		return out
	})
	if err == nil && len(errs) > 0 {
		err = errors.New(strings.Join(errs, "; "))
	}
	return sent, err
}

// pushLog appends one line to <desk>/push/log: time, what happened, the
// device id and label, and detail. Subscriptions come and go silently
// otherwise, which left a phone that stopped getting pushes undiagnosable.
func (a *App) pushLog(action string, s push.Sub, detail string) {
	a.pushLogLine(action, push.ID(s.Endpoint), labelOf(s), detail)
}

func (a *App) pushLogID(action, id, detail string) {
	a.pushLogLine(action, id, "", detail)
}

func (a *App) pushLogLine(action, id, label, detail string) {
	dir := string(a.pushDir())
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	line := a.Now().UTC().Format(time.RFC3339) + " " + action + " " + id
	if label != "" {
		line += " (" + label + ")"
	}
	if detail != "" {
		line += ": " + detail
	}
	fmt.Fprintln(f, line)
}

func labelOf(s push.Sub) string {
	if s.Label != "" {
		return s.Label
	}
	return "device " + push.ID(s.Endpoint)
}

// pushTitles leads a notification's title, by class.
var pushTitles = map[string]string{
	"ask-blocked": "Blocked: ", "ask-default": "Question: ", "response": "Reply: ",
	"tell-notify": "", "tell": "", "done": "Done: ", "deliverable": "Ready: ",
}

// pushRaise pushes e's notification and returns the notified event.
func (a *App) pushRaise(d *desk.Desk, c config.Config, e store.Event, rt route) (store.Event, bool) {
	tag := d.Display(e.ID)
	title := e.Title
	if title == "" {
		title = e.Kind
	}
	var lines []string
	if who := strings.TrimSpace(strings.Join(nonEmpty(agentTitle(e), desk.NodeOf(e)), " · ")); who != "" {
		lines = append(lines, who)
	}
	if b := strings.TrimSpace(e.Body); b != "" {
		lines = append(lines, b)
	}
	n := pushNote{Type: "show", Tag: tag, Title: clipRunes(pushTitles[rt.class]+title, 120),
		Body: clipRunes(strings.Join(lines, "\n"), 700), URL: itemHash(d, e), Sticky: rt.class == "ask-blocked"}
	m := push.Message{TTL: 24 * time.Hour, Urgency: "normal", Topic: pushTopic(tag)}
	if rt.class == "ask-blocked" {
		m.TTL, m.Urgency = 72*time.Hour, "high"
	}
	sent, err := a.pushSend(c, n, m, "")
	if err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: warning: push %s: %v\n", tag, err)
	}
	if sent == 0 {
		return store.Event{}, false
	}
	x := a.event("notified", e.Path, ident.From{}, "notify",
		map[string]any{"channel": "push", "handle": tag, "host": a.Host})
	x.Ref = e.ID
	return x, true
}

// pushClose takes back a pushed notification.
func (a *App) pushClose(c config.Config, n Notice) error {
	_, err := a.pushSend(c, pushNote{Type: "close", Tag: n.Handle},
		push.Message{TTL: 72 * time.Hour, Urgency: "normal", Topic: pushTopic(n.Handle)}, "")
	return err
}

// pushTopic is a Topic header for tag: base64url characters, at most 32.
func pushTopic(tag string) string {
	var b strings.Builder
	for _, r := range tag {
		if b.Len() == 32 {
			break
		}
		if r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func nonEmpty(xs ...string) []string {
	var out []string
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

// PushDevice is one subscription, as the dashboard lists it.
type PushDevice struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Created string `json:"created,omitempty"`
	LastOK  string `json:"last_ok,omitempty"`
	LastErr string `json:"last_error,omitempty"`
}

// apiPush (GET) is the key a browser subscribes with and the devices that
// did; a browser tells whether it is one of them by hashing its endpoint.
func (s *server) apiPush(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	c, err := s.a.config()
	s.mu.Unlock()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, public, err := s.a.pushDir().Keys()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	subs, err := s.a.pushDir().Subs()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	devs := []PushDevice{}
	for _, x := range subs {
		devs = append(devs, PushDevice{ID: push.ID(x.Endpoint), Label: labelOf(x), Created: x.Created, LastOK: x.LastOK, LastErr: x.LastErr})
	}
	writeJSON(w, map[string]any{"key": public, "off": c.Push.Off, "host": s.a.Host, "devices": devs})
}

type pushRequest struct {
	Action       string   `json:"action"` // subscribe, unsubscribe, remove, test
	Subscription push.Sub `json:"subscription"`
	Label        string   `json:"label"`
	ID           string   `json:"id"`
	// Replaces is the endpoint a re-subscription replaces (the service
	// worker's pushsubscriptionchange): dropped, its label kept.
	Replaces string `json:"replaces"`
}

// apiPushAct subscribes this browser, unsubscribes it, removes another
// device, or sends a test.
func (s *server) apiPushAct(w http.ResponseWriter, r *http.Request) {
	if err := s.sameOrigin(r); err != nil {
		jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	var q pushRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&q); err != nil {
		jsonError(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return
	}
	dir := s.a.pushDir()
	drop := func(id string) error {
		return dir.Update(func(subs []push.Sub) []push.Sub {
			out := subs[:0]
			for _, x := range subs {
				if push.ID(x.Endpoint) != id {
					out = append(out, x)
				}
			}
			return out
		})
	}
	switch q.Action {
	case "subscribe":
		sub := q.Subscription
		if err := sub.Check(); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		sub.Label = clipRunes(strings.TrimSpace(q.Label), 80)
		sub.Created = s.a.Now().UTC().Format(time.RFC3339)
		sub.LastOK, sub.LastErr = "", ""
		known := false
		err := dir.Update(func(subs []push.Sub) []push.Sub {
			out := subs[:0]
			for _, x := range subs {
				if x.Endpoint == sub.Endpoint {
					known = true
				}
				if x.Endpoint == sub.Endpoint || (q.Replaces != "" && x.Endpoint == q.Replaces) {
					if sub.Label == "" {
						sub.Label = x.Label
					}
					continue
				}
				out = append(out, x)
			}
			return append(out, sub)
		})
		if err != nil {
			s.a.pushLog("subscribe-failed", sub, err.Error())
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		detail := "new"
		if known {
			detail = "again"
		}
		if q.Replaces != "" {
			detail = "replaces " + push.ID(q.Replaces)
		}
		s.a.pushLog("subscribe", sub, detail)
		writeJSON(w, map[string]string{"id": push.ID(sub.Endpoint)})
	case "unsubscribe", "remove":
		id := q.ID
		if q.Action == "unsubscribe" {
			id = push.ID(q.Subscription.Endpoint)
		}
		if id == "" {
			jsonError(w, http.StatusBadRequest, "name the device: id, or the subscription's endpoint")
			return
		}
		if err := drop(id); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.a.pushLogID(q.Action, id, "")
		writeJSON(w, map[string]string{})
	case "test":
		if q.ID == "" {
			jsonError(w, http.StatusBadRequest, "name the device to test: id")
			return
		}
		s.mu.Lock()
		c, err := s.a.config()
		s.mu.Unlock()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		n := pushNote{Type: "show", Tag: "agent-desk-test", Title: "Agent desk",
			Body: "Push works: this device hears about blocked questions even while the app is closed."}
		sent, err := s.a.pushSend(c, n, push.Message{TTL: 10 * time.Minute, Urgency: "high"}, q.ID)
		if sent == 0 {
			msg := "the push did not go through"
			if err != nil {
				msg = err.Error()
			}
			jsonError(w, http.StatusBadGateway, msg)
			return
		}
		writeJSON(w, map[string]int{"sent": sent})
	default:
		jsonError(w, http.StatusBadRequest, "action is subscribe, unsubscribe, remove or test")
	}
}
