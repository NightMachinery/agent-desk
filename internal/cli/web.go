package cli

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/settings"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Entry is one event in a thread: a message, an answer or a response.
// Delivery marks are folded into the message they mark.
type Entry struct {
	ID     string      `json:"display_id"`
	Event  store.Event `json:"event"`
	Kind   string      `json:"kind"` // message, answer or response
	Age    string      `json:"age"`
	Source string      `json:"source,omitempty"`
	// By: for a response, the agent that wrote it; for a message or an
	// answer, the session it went to.
	By string `json:"by,omitempty"`
	// Behavior is the behaviour asked for (old messages: steer).
	Behavior string    `json:"behavior,omitempty"`
	Delivery *Delivery `json:"delivery,omitempty"`
	// Pending says why an undelivered message has not gone out yet.
	Pending string `json:"pending,omitempty"`
	// Failure is why the latest attempt to deliver it failed.
	Failure *Failure `json:"failure,omitempty"`
	// Status: responded, awaiting, no_response or cleared (read without a
	// response); empty before delivery and for answers with no response.
	Status    string            `json:"status,omitempty"`
	Responses []string          `json:"responses,omitempty"`
	Re        string            `json:"re,omitempty"` // a response: what it responds to
	Notify    map[string]string `json:"notify,omitempty"`
	Unread    bool              `json:"unread,omitempty"`
	Media     []MediaView       `json:"media,omitempty"`
}

// Failure is an undelivered mark: why the message did not reach its
// session, and how to resume that session by hand.
type Failure struct {
	Error     string `json:"error"`
	Resume    string `json:"resume,omitempty"`
	Age       string `json:"age"`
	Retrying  bool   `json:"retrying,omitempty"`
	Uncertain bool   `json:"uncertain,omitempty"`
}

// Delivery is a delivered mark's receipt.
type Delivery struct {
	Via           string `json:"via,omitempty"`
	Behavior      string `json:"behavior,omitempty"`
	Disposition   string `json:"disposition,omitempty"`
	QueuePosition int    `json:"queue_position,omitempty"`
	Revived       bool   `json:"revived,omitempty"`
	Fallback      string `json:"fallback,omitempty"`
	// OnAnswer is what the question's on-answer command did.
	OnAnswer *onAnswerRun `json:"on_answer,omitempty"`
	Age      string       `json:"age"`
}

func fromTitle(f ident.From) string {
	if f.Title != "" {
		return f.Title
	}
	if f.Agent == "" || f.Session == "" {
		return f.Agent
	}
	return f.Agent + " " + shortID(f.Session)
}

// entries turns a thread into entries, with each message's delivery and
// response state. pushed names the hosts in ssh_hosts.
func entries(d *desk.Desk, thread []store.Event, pushed map[string]string) []Entry {
	out := []Entry{}
	if len(thread) == 0 {
		return out
	}
	states := deliveryStates(d)
	answers := map[string]string{}
	for _, q := range d.Questions {
		answers[q.ID] = q.AnswerID
	}
	for _, e := range thread {
		if e.Kind == "delivered" {
			continue
		}
		en := Entry{ID: d.Display(e.ID), Event: e, Age: desk.Age(d.Now.Sub(desk.Time(e))),
			Source: desk.MetaString(e, "source"), Unread: d.Unread(e.ID)}
		if desk.IsResponse(e) {
			en.Kind, en.By = "response", agentTitle(e)
			if e.Ref != "" {
				en.Re = d.Display(e.Ref)
			}
			out = append(out, en)
			continue
		}
		en.Kind = "message"
		if e.Kind == "answer" {
			en.Kind = "answer"
		}
		var m struct {
			To       ident.From        `json:"to"`
			ToHost   string            `json:"to_host"`
			Behavior string            `json:"behavior"`
			Notify   map[string]string `json:"notify"`
		}
		_ = json.Unmarshal(e.Meta, &m)
		en.By, en.Notify = fromTitle(m.To), m.Notify
		host := m.ToHost
		if e.Kind == "answer" {
			q := d.ByID[e.Ref]
			en.By, host = agentTitle(q), q.Host
		}
		if en.Source != "chat" {
			en.Behavior = m.Behavior
			if en.Behavior == "" {
				en.Behavior = "steer"
			}
		}
		for _, r := range d.ResponsesTo(e.ID) {
			en.Responses = append(en.Responses, d.Display(r.ID))
		}
		del, delivered := d.Delivery(e.ID)
		if delivered {
			var r struct {
				Via, Behavior, Disposition, Fallback string
				QueuePosition                        int `json:"queue_position"`
				Revived                              bool
				OnAnswer                             *onAnswerRun `json:"on_answer"`
			}
			_ = json.Unmarshal(del.Meta, &r)
			en.Delivery = &Delivery{Via: r.Via, Behavior: r.Behavior, Disposition: r.Disposition,
				QueuePosition: r.QueuePosition, Revived: r.Revived, Fallback: r.Fallback, OnAnswer: r.OnAnswer,
				Age: desk.Age(d.Now.Sub(desk.Time(del)))}
		} else if en.Source != "chat" {
			switch {
			case host != "" && host != d.Host && pushed[host] != "":
				en.Pending = "sent to " + host + "; its delivery shows here after the next merge"
			case host != "" && host != d.Host:
				en.Pending = "waiting for a merge to " + host
			case e.Kind == "answer" && answers[e.Ref] != "" && answers[e.Ref] != e.ID:
				en.Pending = "superseded by a newer answer"
			case queuedByWeb(e) || states[e.ID].Status != "":
				en.Pending = "queued"
			default:
				en.Pending = "not delivered yet"
			}
			if state := states[e.ID]; state.Error != "" && en.Pending != "superseded by a newer answer" {
				f, _ := d.DeliveryFailure(e.ID)
				en.Pending = "not delivered"
				if state.RetryAt != "" && host == d.Host {
					en.Pending = "retrying"
				}
				en.Failure = &Failure{Error: state.Error, Resume: state.Resume,
					Age: desk.Age(d.Now.Sub(desk.Time(f))), Retrying: state.RetryAt != "", Uncertain: state.Uncertain}
			}
		}
		switch {
		case len(en.Responses) > 0:
			en.Status = "responded"
		case e.Kind != "reply" || !delivered:
		case d.Marked(e.ID):
			en.Status = "cleared"
		case d.NoResponse(e.ID):
			en.Status = "no_response"
		default:
			en.Status = "awaiting"
		}
		out = append(out, en)
	}
	return out
}

// finished says why a target's session may have stopped: its node has a
// done report since it joined, or its Paseo agent is archived.
func (a *App) finished(d *desk.Desk, t *Target) string {
	if t.Paseo != "" {
		home := a.Env.Getenv("PASEO_HOME")
		if home == "" {
			home = a.Env.Home + "/.paseo"
		}
		if rec, ok := ident.PaseoRecord(home, t.Paseo); ok {
			if rec.ArchivedAt != nil {
				return "archived"
			}
			// Paseo knows the agent and it is not archived: it is running or
			// waits for a message. A done report from it marks a milestone
			// (coordinators file one per milestone), not the end of the session.
			return ""
		}
	}
	owner, _ := nodeOwner(d, t.Node)
	for _, e := range d.Events {
		if e.Kind == "done" && e.Path == t.Node && e.ID > owner.ID {
			return "done"
		}
	}
	return ""
}

// ClassValue is a setting's value for one class.
type ClassValue struct {
	Class string `json:"class"`
	Value string `json:"value"`
}

// PrefKey is one setting as the settings controls show it.
type PrefKey struct {
	Key       string          `json:"key"`
	Classless bool            `json:"classless,omitempty"`
	Value     string          `json:"value"`
	Origin    settings.Origin `json:"origin"`
	// Here: an override stands at this very scope and class, so it can be
	// reset to the inherited value.
	Here bool `json:"here"`
	// Differs lists the classes that resolve to another value than the one
	// shown for all kinds.
	Differs []ClassValue `json:"differs,omitempty"`
	Options []string     `json:"options,omitempty"` // empty: free text
}

// PrefsView is GET api/prefs.
type PrefsView struct {
	Path    string    `json:"path"`
	Class   string    `json:"class"`
	Classes []string  `json:"classes"`
	Hosts   []string  `json:"hosts"`
	Keys    []PrefKey `json:"keys"`
	// RemoteLabel is the config's name for the remote channel.
	RemoteLabel string          `json:"remote_label,omitempty"`
	Overrides   []settings.Pref `json:"overrides"`
}

func prefsView(d *desk.Desk, r *settings.Resolver, path, class string) PrefsView {
	v := PrefsView{Path: path, Class: class, Classes: settings.Classes, Hosts: []string{}, Overrides: r.Prefs()}
	if v.Overrides == nil {
		v.Overrides = []settings.Pref{}
	}
	hosts := map[string]bool{}
	for _, e := range d.Events {
		hosts[e.Host] = true
	}
	v.Hosts = sortedKeys(hosts)
	options := map[string][]string{
		"deliver.behavior": {"steer", "queue"},
		"notify.desktop":   {"none", "origin", "all"},
		"notify.remote":    {"off", "away", "always"},
		"notify.push":      {"off", "away", "always"},
	}
	for _, k := range settings.Keys {
		c := class
		if settings.Classless(k) {
			c = settings.Any
		}
		val, o := r.Resolve(k, path, c)
		pk := PrefKey{Key: k, Classless: settings.Classless(k), Value: val, Origin: o,
			Here: r.Has(path, k, c), Options: options[k]}
		if c == settings.Any && !pk.Classless {
			for _, cl := range settings.Classes {
				if cv, _ := r.Resolve(k, path, cl); cv != val {
					pk.Differs = append(pk.Differs, ClassValue{Class: cl, Value: cv})
				}
			}
		}
		v.Keys = append(v.Keys, pk)
	}
	return v
}

func (s *server) apiPrefs(w http.ResponseWriter, r *http.Request) {
	d, _ := s.load(w)
	if d == nil {
		return
	}
	q := r.URL.Query()
	class, err := settings.CheckClass("notify.desktop", q.Get("class"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	res, err := s.a.settings(d)
	s.mu.Unlock()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v := prefsView(d, res, strings.Trim(q.Get("path"), "/"), class)
	s.mu.Lock()
	if c, err := s.a.config(); err == nil {
		v.RemoteLabel = c.RemoteLabel
	}
	s.mu.Unlock()
	writeJSON(w, v)
}

// apiPref sets an override, or removes it when value is null.
func (s *server) apiPref(app *App, body request) (any, error) {
	if body.Key == "" {
		return nil, usagef("pref needs a key")
	}
	args := []string{"pref", "unset"}
	if body.Value != nil {
		args[1] = "set"
	}
	if sc := strings.Trim(body.Scope, "/"); sc != "" {
		args = append(args, "--scope", sc)
	}
	if body.Class != "" {
		args = append(args, "--class", body.Class)
	}
	args = append(args, "--", body.Key)
	if body.Value != nil {
		args = append(args, *body.Value)
	}
	return map[string]string{}, app.run(args)
}
