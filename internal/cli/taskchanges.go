package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/prompts"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Task changes: when the user changes a task's state (Done, Drop, Reopen,
// Start, Defer; on the dashboard or with `agent-desk task` outside an
// agent), the dashboard server tells the agent working on it (taskRecipient).
// It waits until the user has made no change for the debounce, so a run of
// changes goes as one message per agent, and a change undone inside it
// (Done, then Undo) sends nothing: what is told is each task's net change.
// The setting ("task-changes", in Settings) is shared by every device.
// A server tells only changes made on its own host, so desks merged across
// hosts tell each change once.

// taskChangePrefs is the shared "task-changes" dashboard setting.
type taskChangePrefs struct {
	Enabled bool `json:"enabled"`
	// Debounce is how many seconds after the user's last change to wait.
	Debounce int `json:"debounce"`
}

var defaultTaskChangePrefs = taskChangePrefs{Enabled: true, Debounce: 10}

const (
	minTaskChangeDebounce = 1
	maxTaskChangeDebounce = 3600
	// taskChangeWindow is how long past the debounce a change not yet told
	// is still told: one made while no server ran for longer is left.
	taskChangeWindow = 10 * time.Minute
	// taskChangeRetry is the wait after a failed send before trying again.
	taskChangeRetry = 30 * time.Second
)

// taskChangePrefs reads the shared setting, the defaults when unset or
// unreadable.
func (s *server) taskChangePrefs() taskChangePrefs {
	p := defaultTaskChangePrefs
	st, err := s.a.store()
	if err != nil {
		return p
	}
	prefs, err := st.DashboardPrefs()
	if err != nil || len(prefs["task-changes"]) == 0 {
		return p
	}
	if json.Unmarshal(prefs["task-changes"], &p) != nil || p.Debounce < minTaskChangeDebounce || p.Debounce > maxTaskChangeDebounce {
		return defaultTaskChangePrefs
	}
	return p
}

// checkTaskChangePrefs validates a "task-changes" setting from the dashboard.
func checkTaskChangePrefs(data json.RawMessage) error {
	var p taskChangePrefs
	if err := json.Unmarshal(data, &p); err != nil {
		return usagef("task-changes is {enabled, debounce}")
	}
	if p.Debounce < minTaskChangeDebounce || p.Debounce > maxTaskChangeDebounce {
		return usagef("task-changes debounce is %d to %d seconds", minTaskChangeDebounce, maxTaskChangeDebounce)
	}
	return nil
}

// taskChange is one task's net change by the user, not yet told; the
// exported fields are what prompts/task-changes.md sees.
type taskChange struct {
	Task, Title string
	Was, Now    string // the states before and after, desk.TaskStates
	Note        string // the note on the user's last change, if any
	task        store.Event
	marks       []string // the ids of the user's taskstate events it covers
}

// userTaskMark reports whether e is the user's own change of a task's state
// on host: `task done` and the like run outside any agent (the dashboard's
// buttons run them so), not a launch, not a tag edit.
func userTaskMark(e store.Event, host string) bool {
	if e.Kind != "taskstate" || e.Host != host || desk.MetaString(e, "source") != "cli" || desk.MetaString(e, "state") == "" {
		return false
	}
	f := fromOf(e)
	return f.Session == "" && f.Paseo == "" && f.TmuxPane == ""
}

// taskStateAt is the task's state from the events before index end.
func taskStateAt(d *desk.Desk, task store.Event, end int) string {
	var tm desk.TaskMeta
	_ = json.Unmarshal(task.Meta, &tm)
	state := desk.TaskOpen
	if tm.Deferred {
		state = desk.TaskDeferred
	}
	for _, e := range d.Events[:end] {
		if e.Kind == "taskstate" && e.Ref == task.ID {
			if s := desk.MetaString(e, "state"); s != "" {
				state = s
			}
		}
	}
	return state
}

// pendingTaskChanges are the user's changes of task states made on host
// since `since` and not yet told, as each task's net change (none for a task
// back where it was), and when the newest of those changes was made.
func pendingTaskChanges(d *desk.Desk, host string, since time.Time) ([]taskChange, time.Time) {
	told := map[string]bool{}
	var marks []int // indexes of untold changes, newest first
	var newest time.Time
	for i := len(d.Events) - 1; i >= 0; i-- {
		e := d.Events[i]
		if e.Host != host {
			continue // merged from another host, whose server tells its own
		}
		t, err := time.Parse(time.RFC3339, e.TS)
		if err == nil && t.Before(since) {
			break
		}
		if e.Kind == "reply" {
			var m struct {
				Marks []string `json:"task_changes"`
			}
			_ = json.Unmarshal(e.Meta, &m)
			for _, id := range m.Marks {
				told[id] = true
			}
			continue
		}
		if userTaskMark(e, host) && !told[e.ID] && d.ByID[e.Ref].Kind == "task" {
			marks = append(marks, i)
			if t.After(newest) {
				newest = t
			}
		}
	}
	byTask := map[string]*taskChange{}
	var order []string
	for j := len(marks) - 1; j >= 0; j-- { // oldest first
		i := marks[j]
		e := d.Events[i]
		c := byTask[e.Ref]
		if c == nil {
			task := d.ByID[e.Ref]
			c = &taskChange{Task: d.Display(task.ID), Title: task.Title, Was: taskStateAt(d, task, i), task: task}
			byTask[e.Ref] = c
			order = append(order, e.Ref)
		}
		c.marks = append(c.marks, e.ID)
		c.Note = e.Body
	}
	var out []taskChange
	for _, id := range order {
		c := byTask[id]
		c.Now = taskStateAt(d, c.task, len(d.Events))
		if c.Now != c.Was {
			out = append(out, *c)
		}
	}
	return out, newest
}

// tellTaskChanges sends the pending task changes, one message per agent,
// once the user's newest change is the debounce old. It returns how many
// messages it queued.
func (s *server) tellTaskChanges(now time.Time) (int, error) {
	p := s.taskChangePrefs()
	if !p.Enabled || now.Before(s.taskRetry) {
		return 0, nil
	}
	d, _, err := s.storedDesk()
	if err != nil {
		return 0, err
	}
	debounce := time.Duration(p.Debounce) * time.Second
	changes, newest := pendingTaskChanges(d, s.a.Host, now.Add(-debounce-taskChangeWindow))
	if len(changes) == 0 || now.Sub(newest) < debounce {
		return 0, nil
	}
	type group struct {
		to         ident.From
		host, node string
		changes    []taskChange
	}
	groups := map[string]*group{}
	var order []string
	for _, c := range changes {
		to, host, node, _, err := replyTarget(d, c.task.ID)
		if err != nil {
			continue // nobody works on it: nobody to tell
		}
		key := to.Session + "|" + to.Paseo + "|" + to.TmuxPane + "|" + host
		if groups[key] == nil {
			groups[key] = &group{to: to, host: host, node: node}
			order = append(order, key)
		}
		groups[key].changes = append(groups[key].changes, c)
	}
	app, _, _ := s.userApp()
	sent := 0
	for _, key := range order {
		g := groups[key]
		text, err := prompts.Render(s.a.promptsDir(), "task-changes.md", struct{ Changes []taskChange }{g.changes})
		if err != nil {
			return sent, s.taskChangeFailed(now, err)
		}
		var about store.Event
		if len(g.changes) == 1 {
			about = g.changes[0].task // threads under the task
		}
		var marks []string
		for _, c := range g.changes {
			marks = append(marks, c.marks...)
		}
		_, err = s.enqueue(app, "reply", request{}, func() ([]store.Event, error) {
			e, err := app.replyTo(d, g.to, g.host, g.node, about, text, "web", "", nil)
			if err != nil {
				return nil, err
			}
			m := desk.Meta(e)
			m["task_changes"] = marks
			e.Meta = meta(m)
			return []store.Event{e}, nil
		})
		if err != nil {
			return sent, s.taskChangeFailed(now, err)
		}
		sent++
	}
	return sent, nil
}

func (s *server) taskChangeFailed(now time.Time, err error) error {
	s.taskRetry = now.Add(taskChangeRetry)
	fmt.Fprintf(s.logger, "agent-desk: telling agents about task changes failed (retrying in %v): %v\n", taskChangeRetry, err)
	return err
}

// taskChangeLoop tells task changes as they come due, until ctx ends.
func (s *server) taskChangeLoop(ctx context.Context) {
	tick := time.NewTicker(s.poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.tellTaskChanges(s.a.Now())
	}
}

// apiTaskChanges is the setting for Settings (GET api/task-changes).
func (s *server) apiTaskChanges(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.taskChangePrefs())
}

// taskTold is what a task action's answer says about telling: who hears of
// the change, and after how many seconds; nil when nobody will.
func (s *server) taskTold(app *App, id string) map[string]any {
	p := s.taskChangePrefs()
	if !p.Enabled {
		return nil
	}
	d, err := app.load()
	if err != nil {
		return nil
	}
	_, _, node, _, err := replyTarget(d, id)
	if err != nil {
		return nil
	}
	return map[string]any{"tell": node, "after": p.Debounce}
}
