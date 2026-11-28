package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/prompts"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Reminders to agents: one line the desk prints into an agent's prompt or
// command output, at a moment it matters, from a prompt template the user
// can edit (internal/prompts, overridden in ~/.config/agent-desk/prompts/).
// They print text and file nothing.
//
//   - no-task (nag-no-task.md): the prompt hook prints it when a message
//     arrives and the agent has no started task at its node.
//   - velocity (nag-velocity.md): task done prints it to a coordinator whose
//     velocity slot is missing or old.
//
// Which reminders are on, and on what, is the shared dashboard setting
// "reminders", kept in the desk so the hook reads it without the server.

// Occasions: the kind of prompt that just reached an agent.
const (
	OnUser      = "user"      // typed by the user
	OnDesk      = "desk"      // an answer or a message the desk delivered
	OnHeartbeat = "heartbeat" // a Paseo heartbeat or schedule
	OnAgent     = "agent"     // a message from another agent
)

var (
	paseoEnvelope  = regexp.MustCompile(`^<paseo-system\b[^>]*>`)
	paseoScheduled = regexp.MustCompile(`\bkind="(?:heartbeat|schedule)[^"]*"`)
	scheduleFired  = regexp.MustCompile(`^Schedule ".*" fired \(id=`)
	deskDelivery   = regexp.MustCompile(`^(?:Answer to [a-z]-[0-9a-z]+ "|Message (?:[a-z]-[0-9a-z]+ )?from the user via agent-desk\b)`)
)

// promptOccasion says what kind of prompt this is, or "" for one that never
// brings a reminder: cache keepalive pings, /compact and other bare slash
// commands, harness notices, the auto-continue ping, and prompts matching
// the user's skip patterns. Those must stay cheap and must not make the
// agent act.
func promptOccasion(prompt string, extra []*regexp.Regexp) string {
	p := strings.TrimSpace(prompt)
	switch {
	case p == "":
		return ""
	case paseoEnvelope.MatchString(p):
		if paseoScheduled.MatchString(paseoEnvelope.FindString(p)) {
			return OnHeartbeat
		}
		return OnAgent
	case strings.HasPrefix(p, "<agent-message"):
		return OnAgent
	case scheduleFired.MatchString(p):
		return OnHeartbeat
	case deskDelivery.MatchString(p):
		return OnDesk
	case isAutomated(p, extra):
		return ""
	}
	return OnUser
}

// reminderPrefs is the shared "reminders" dashboard setting.
type reminderPrefs struct {
	NoTask struct {
		On bool `json:"on"`
		// The occasions it comes on.
		User      bool `json:"user"`
		Desk      bool `json:"desk"`
		Heartbeat bool `json:"heartbeat"`
		Agent     bool `json:"agent"`
	} `json:"no_task"`
	Velocity struct {
		On bool `json:"on"`
		// Hours: remind only when the velocity slot is older than this (or
		// missing); 0 reminds on every task done.
		Hours int `json:"hours"`
	} `json:"velocity"`
}

// MaxVelocityHours bounds the velocity reminder's hours.
const MaxVelocityHours = 168

func defaultReminderPrefs() reminderPrefs {
	var p reminderPrefs
	p.NoTask.On, p.NoTask.User, p.NoTask.Desk, p.NoTask.Heartbeat = true, true, true, true
	p.Velocity.On, p.Velocity.Hours = true, 3
	return p
}

func (p reminderPrefs) noTaskOn(occasion string) bool {
	if !p.NoTask.On {
		return false
	}
	switch occasion {
	case OnUser:
		return p.NoTask.User
	case OnDesk:
		return p.NoTask.Desk
	case OnHeartbeat:
		return p.NoTask.Heartbeat
	case OnAgent:
		return p.NoTask.Agent
	}
	return false
}

func checkReminderPrefs(data json.RawMessage) error {
	var p reminderPrefs
	if err := json.Unmarshal(data, &p); err != nil {
		return usagef("reminders is {no_task: {on, user, desk, heartbeat, agent}, velocity: {on, hours}}")
	}
	if p.Velocity.Hours < 0 || p.Velocity.Hours > MaxVelocityHours {
		return usagef("reminders velocity hours is 0 to %d", MaxVelocityHours)
	}
	return nil
}

// reminderPrefsFrom reads the setting from the store: the defaults when it
// is unset or unreadable.
func reminderPrefsFrom(st *store.Store) reminderPrefs {
	p := defaultReminderPrefs()
	prefs, err := st.DashboardPrefs()
	if err != nil || len(prefs["reminders"]) == 0 {
		return p
	}
	if json.Unmarshal(prefs["reminders"], &p) != nil || checkReminderPrefs(prefs["reminders"]) != nil {
		return defaultReminderPrefs()
	}
	return p
}

// noTaskData is what nag-no-task.md sees.
type noTaskData struct {
	Node     string
	Occasion string
	Open     []taskRef
}

type taskRef struct{ ID, Title string }

// velocityData is what nag-velocity.md sees.
type velocityData struct {
	Node string
	Task string
	Age  string
}

// reminderSamples fill each reminder's template when the user saves a copy,
// so a field that does not exist is refused then, not when it would print.
var reminderSamples = map[string]any{
	"nag-no-task.md":  noTaskData{Node: "proj/run/coord", Occasion: OnUser, Open: []taskRef{{"w-1a2b", "An open task"}}},
	"nag-velocity.md": velocityData{Node: "proj/run/coord", Task: "w-1a2b", Age: "5h"},
}

// renderReminder fills a reminder's template; when the user's copy fails,
// it logs why and falls back to the built-in one.
func (a *App) renderReminder(name string, data any) string {
	text, err := prompts.Render(a.promptsDir(), name, data)
	if err != nil {
		a.hookLog(fmt.Errorf("reminder %s: %w; used the built-in one", name, err))
		text, _ = prompts.Render("", name, data)
	}
	return text
}

// noTaskReminder is the no-task reminder for a session holding the nodes in
// held, or "" when one of them has a started task.
func (a *App) noTaskReminder(st *store.Store, held []store.Event, occasion string) (string, error) {
	paths := heldPaths(held)
	args := make([]any, len(paths))
	for i, p := range paths {
		args[i] = p
	}
	evs, err := st.Query("events", "kind IN ('task', 'taskstate') AND path IN (?"+strings.Repeat(", ?", len(paths)-1)+")", args...)
	if err != nil {
		return "", err
	}
	state := map[string]string{}
	var order []store.Event
	for _, e := range evs {
		switch e.Kind {
		case "task":
			state[e.ID] = desk.FiledState(e)
			order = append(order, e)
		case "taskstate":
			if s := desk.MetaString(e, "state"); s != "" {
				if _, ok := state[e.Ref]; ok {
					state[e.Ref] = s
				}
			}
		}
	}
	var open []taskRef
	for _, e := range order {
		switch state[e.ID] {
		case desk.TaskStarted:
			return "", nil
		case desk.TaskOpen:
			if len(open) < 3 {
				id, err := st.DisplayID(e)
				if err != nil {
					return "", err
				}
				open = append(open, taskRef{id, e.Title})
			}
		}
	}
	return a.renderReminder("nag-no-task.md", noTaskData{Node: strings.Join(paths, ", "), Occasion: occasion, Open: open}), nil
}

// coordinates reports whether node is a coordinator's: a run's top node
// (project/run/role, outside the adhoc run of sessions working alone), or a
// node with nodes under it.
func coordinates(d *desk.Desk, node string) bool {
	if segs := strings.Split(node, "/"); len(segs) == 3 && segs[1] != "adhoc" {
		return true
	}
	for _, e := range d.Events {
		if e.Kind == "join" && strings.HasPrefix(e.Path, node+"/") {
			return true
		}
	}
	return false
}

// velocityReminder is the velocity reminder after the session finished task
// (a display id), or "": it goes to a coordinator whose velocity slot is
// missing or older than the setting's hours.
func (a *App) velocityReminder(d *desk.Desk, from ident.From, task string) string {
	st, err := a.store()
	if err != nil {
		return ""
	}
	p := reminderPrefsFrom(st)
	if !p.Velocity.On {
		return ""
	}
	for _, j := range a.held(d, from) {
		if !coordinates(d, j.Path) {
			continue
		}
		var set *store.Event
		for i := len(d.Events) - 1; i >= 0; i-- {
			if e := d.Events[i]; (e.Kind == "set" || e.Kind == "clear") && e.Path == j.Path+"/velocity" {
				if e.Kind == "set" {
					set = &d.Events[i]
				}
				break
			}
		}
		data := velocityData{Node: j.Path, Task: task}
		if set != nil {
			age := d.Now.Sub(desk.Time(*set))
			if age < time.Duration(p.Velocity.Hours)*time.Hour {
				continue
			}
			data.Age = desk.Age(age)
		}
		return a.renderReminder("nag-velocity.md", data)
	}
	return ""
}

// apiReminders returns the "reminders" setting as it stands, the defaults
// filled in (GET api/reminders).
func (s *server) apiReminders(w http.ResponseWriter, r *http.Request) {
	st, err := s.a.store()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, reminderPrefsFrom(st))
}

// The reminders' templates, in the order Settings shows them.
var reminderTemplates = []string{"nag-no-task.md", "nag-velocity.md"}

// apiPrompts lists every prompt the desk sends, with the user's copy (GET
// api/prompts): {dir, prompts: [...]}, the reminders' first.
func (s *server) apiPrompts(w http.ResponseWriter, r *http.Request) {
	dir := s.a.promptsDir()
	names := slices.Clone(reminderTemplates)
	for _, n := range prompts.Names() {
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	out := []prompts.Source{}
	for _, n := range names {
		src, err := prompts.Read(dir, n)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, src)
	}
	writeJSON(w, map[string]any{"dir": dir, "prompts": out})
}

// apiPromptPut saves the user's copy of a prompt (POST api/prompt {name,
// text}) or removes it ({name, reset: true}), and returns the prompt.
func (s *server) apiPromptPut(_ *App, body request) (any, error) {
	if !prompts.Known(body.Name) {
		return nil, usagef("no prompt %q", body.Name)
	}
	dir := s.a.promptsDir()
	var err error
	if body.Reset {
		err = prompts.Reset(dir, body.Name)
	} else {
		err = prompts.Save(dir, body.Name, body.Text, reminderSamples[body.Name])
	}
	if err != nil {
		return nil, usagef("%v", err)
	}
	return prompts.Read(dir, body.Name)
}

// hookContext writes text as a UserPromptSubmit hook's additional context,
// in the JSON both Claude Code and Codex read, if there is any.
func hookContext(w io.Writer, text string) {
	if text = strings.TrimSpace(text); text == "" {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName": "UserPromptSubmit", "additionalContext": text}})
}

// printReminder writes a reminder to w, if there is one.
func printReminder(w io.Writer, text string) {
	if text = strings.TrimSpace(text); text != "" {
		fmt.Fprintln(w, text)
	}
}
