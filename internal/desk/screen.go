package desk

import (
	"encoding/json"
	"time"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// The screen lock: one per machine, taken by an agent before it drives the
// GUI, so the user knows to keep off the keyboard. Its events live at
// ScreenPath(host), outside every project, so any agent session can take
// it, a worker as well as a coordinator.
//
// A `screen` event (meta.action) grants it (meta.until, meta.reason,
// meta.by "auto" or "user", meta.request: the question that asked for it),
// releases it, or denies a request. An agent that cannot have it at once
// files a question there with meta.screen; the question is the request.

// ScreenPath is where host's screen events and requests live.
func ScreenPath(host string) string { return "_desk/screen/" + host }

// ScreenHold is a standing grant.
type ScreenHold struct {
	Event   store.Event `json:"event"` // the grant
	Session string      `json:"session"`
	Reason  string      `json:"reason"`
	Until   time.Time   `json:"until"`
	By      string      `json:"by"` // auto or user
}

// ScreenState is a machine's screen: who holds it, and who asks for it.
type ScreenState struct {
	Host     string        `json:"host"`
	Hold     *ScreenHold   `json:"hold,omitempty"`
	Requests []store.Event `json:"requests,omitempty"` // open requests, oldest first
	granted  map[string]bool
}

type screenMeta struct {
	Action  string `json:"action"`
	Until   string `json:"until"`
	Reason  string `json:"reason"`
	By      string `json:"by"`
	Request string `json:"request"`
	Holder  string `json:"holder"` // the session it is granted to
}

// Screen is host's screen now. A grant past its time no longer holds.
func (d *Desk) Screen(host string) ScreenState {
	st := ScreenState{Host: host, granted: map[string]bool{}}
	path := ScreenPath(host)
	for _, e := range d.Events {
		if e.Kind != "screen" || e.Path != path {
			continue
		}
		var m screenMeta
		_ = json.Unmarshal(e.Meta, &m)
		if m.Request != "" {
			st.granted[m.Request] = true // granted or denied: no longer asking
		}
		switch m.Action {
		case "grant":
			until, _ := time.Parse(time.RFC3339, m.Until)
			st.Hold = &ScreenHold{Event: e, Session: m.Holder, Reason: m.Reason, Until: until, By: m.By}
		case "release":
			st.Hold = nil
		}
	}
	if st.Hold != nil && !d.Now.Before(st.Hold.Until) {
		st.Hold = nil
	}
	for _, q := range d.Questions {
		if q.Path == path && q.Open() && !st.granted[q.ID] && !d.dismissed[q.ID] {
			st.Requests = append(st.Requests, q.Event)
		}
	}
	return st
}

// Request is the open request of session, if any.
func (s ScreenState) Request(session string) (store.Event, bool) {
	for _, q := range s.Requests {
		if sessionOfEvent(q) == session {
			return q, true
		}
	}
	return store.Event{}, false
}

// Holder reports whether session holds the screen.
func (s ScreenState) Holder(session string) bool {
	return s.Hold != nil && session != "" && s.Hold.Session == session
}

// Screens is every machine's screen that is held or asked for.
func (d *Desk) Screens() []ScreenState {
	seen := map[string]bool{}
	var out []ScreenState
	for _, e := range d.Events {
		if (e.Kind != "screen" && e.Kind != "ask") || len(e.Path) <= len("_desk/screen/") || e.Path[:len("_desk/screen/")] != "_desk/screen/" {
			continue
		}
		host := e.Path[len("_desk/screen/"):]
		if seen[host] {
			continue
		}
		seen[host] = true
		if s := d.Screen(host); s.Hold != nil || len(s.Requests) > 0 {
			out = append(out, s)
		}
	}
	return out
}

func sessionOfEvent(e store.Event) string {
	var m struct {
		From struct {
			Session string `json:"session"`
		} `json:"from"`
	}
	_ = json.Unmarshal(e.Meta, &m)
	return m.From.Session
}

// ScreenGrace is how long after an agent's hold runs out the machine's
// input is still taken to be the agent's: it may still be clicking as its
// time ends. After the agent releases the screen, input is the user's at
// once.
const ScreenGrace = 2 * time.Minute

// ScreenUserLast is for input seen at t on host's screen: if t falls while
// an agent held the screen (from a grant to its release or expiry, plus
// ScreenGrace when it ran out; back-to-back holds count as one), the input was the agent's,
// and userLast is when the user last touched the machine before that hold.
// An automatic grant records it (meta.away_since); an older one without it
// was given after at least idle of absence; a grant by the user is taken as
// the user's last touch.
func (d *Desk) ScreenUserLast(host string, t time.Time, idle time.Duration) (userLast time.Time, ok bool) {
	type window struct {
		start, end, user time.Time
		released         bool
	}
	var ws []window
	path := ScreenPath(host)
	grace := func(w window) time.Duration {
		if w.released {
			return 0
		}
		return ScreenGrace
	}
	for _, e := range d.Events {
		if e.Kind != "screen" || e.Path != path {
			continue
		}
		var m struct {
			screenMeta
			AwaySince string `json:"away_since"`
		}
		_ = json.Unmarshal(e.Meta, &m)
		at := Time(e)
		switch m.Action {
		case "grant":
			until, err := time.Parse(time.RFC3339, m.Until)
			if err != nil {
				continue
			}
			if n := len(ws); n > 0 && !at.After(ws[n-1].end.Add(grace(ws[n-1]))) {
				ws[n-1].end, ws[n-1].released = until, false // renewed, or handed on: one stretch of agent input
				continue
			}
			user := at
			if m.By == "auto" {
				user = at.Add(-idle)
				if s, err := time.Parse(time.RFC3339, m.AwaySince); err == nil {
					user = s
				}
			}
			ws = append(ws, window{start: at, end: until, user: user})
		case "release":
			if n := len(ws); n > 0 && at.Before(ws[n-1].end) {
				ws[n-1].end, ws[n-1].released = at, true
			}
		}
	}
	for i := len(ws) - 1; i >= 0; i-- {
		w := ws[i]
		if !t.Before(w.start) && (t.Before(w.end) || (!w.released && !t.After(w.end.Add(ScreenGrace)))) {
			return w.user, true
		}
	}
	return time.Time{}, false
}
