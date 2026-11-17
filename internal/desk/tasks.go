package desk

import (
	"encoding/json"
	"sort"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// Task states. A deferred task waits for quota to spare; launching it starts
// an agent on it.
const (
	TaskOpen     = "open"
	TaskDeferred = "deferred"
	TaskStarted  = "started"
	TaskDone     = "done"
	TaskDropped  = "dropped"
)

// TaskStates are the states a taskstate event may set.
var TaskStates = []string{TaskOpen, TaskDeferred, TaskStarted, TaskDone, TaskDropped}

// TaskMeta is what a task event carries besides its title and brief.
type TaskMeta struct {
	Deferred bool   `json:"deferred,omitempty"`
	Size     string `json:"size,omitempty"`    // S, M or L
	Profile  string `json:"profile,omitempty"` // what to launch it with, for launch_command
	Priority int    `json:"priority,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
}

// Task is a task with its current state.
type Task struct {
	Event   store.Event `json:"event"`
	ID      string      `json:"display_id"`
	State   string      `json:"state"`
	Agent   string      `json:"agent,omitempty"` // what launch_command printed
	Note    string      `json:"note,omitempty"`
	Updated string      `json:"updated"`
	TaskMeta
}

// Active reports whether the task still needs doing.
func (t Task) Active() bool { return t.State != TaskDone && t.State != TaskDropped }

func taskRank(s string) int {
	switch s {
	case TaskStarted:
		return 0
	case TaskOpen:
		return 1
	case TaskDeferred:
		return 2
	case TaskDone:
		return 3
	}
	return 4
}

// Tasks are the tasks the filter keeps, in working order: started, open,
// deferred, then finished; by priority (1 first), then oldest first.
func (d *Desk) Tasks(f Filter) []Task {
	byID := map[string]*Task{}
	var order []string
	for _, e := range d.Events {
		switch e.Kind {
		case "task":
			if !f.Keep(e) {
				continue
			}
			t := &Task{Event: e, ID: d.Display(e.ID), State: TaskOpen, Updated: e.TS}
			_ = json.Unmarshal(e.Meta, &t.TaskMeta)
			if t.Deferred {
				t.State = TaskDeferred
			}
			byID[e.ID] = t
			order = append(order, e.ID)
		case "taskstate":
			t, ok := byID[e.Ref]
			if !ok {
				continue
			}
			if s := MetaString(e, "state"); s != "" {
				t.State = s
			}
			if a := MetaString(e, "agent"); a != "" {
				t.Agent = a
			}
			t.Note = e.Body
			t.Updated = e.TS
		}
	}
	out := make([]Task, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if taskRank(a.State) != taskRank(b.State) {
			return taskRank(a.State) < taskRank(b.State)
		}
		pa, pb := a.Priority, b.Priority
		if pa == 0 {
			pa = 2
		}
		if pb == 0 {
			pb = 2
		}
		if pa != pb {
			return pa < pb
		}
		return a.Event.ID < b.Event.ID
	})
	return out
}
