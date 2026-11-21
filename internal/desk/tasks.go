package desk

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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

// TaskTypes are the kinds of task, the first the default: what the
// checklist item is, so the Tasks page can filter bugs from features.
var TaskTypes = []string{"task", "bug", "feature", "idea", "chore"}

// ValidTaskType reports whether t is one of TaskTypes.
func ValidTaskType(t string) bool {
	for _, x := range TaskTypes {
		if x == t {
			return true
		}
	}
	return false
}

// TaskMeta is what a task event carries besides its title and brief.
type TaskMeta struct {
	// Type is one of TaskTypes; empty is a plain task.
	Type string `json:"type,omitempty"`
	// Parent is the full id of the task this one is a step of.
	Parent   string `json:"parent,omitempty"`
	Deferred bool   `json:"deferred,omitempty"`
	Size     string `json:"size,omitempty"`    // S, M or L
	Profile  string `json:"profile,omitempty"` // what to launch it with, for launch_command
	Priority int    `json:"priority,omitempty"`
	Cwd      string `json:"cwd,omitempty"`
	// Tags are free labels beside the fixed Type ("ui", "phone"), from
	// NormTags; a taskstate event with meta.tags replaces them.
	Tags []string `json:"tags,omitempty"`
	// Untitled marks a task the user filed without a title: its title is
	// the brief's first line until an agent names it (task title), which a
	// taskstate event with meta.title does, clearing this.
	Untitled bool `json:"untitled,omitempty"`
}

// MaxTags is how many tags a task may carry.
const MaxTags = 12

// NormTags cleans tags as typed ("#UI", "phone, sync"): split on commas
// and spaces, lowercased, without a leading #, each once, in order. A tag
// is a letter or digit, then up to 31 of letters, digits and . _ / -.
func NormTags(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, chunk := range in {
		for _, t := range strings.FieldsFunc(chunk, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
			t = strings.ToLower(strings.TrimPrefix(t, "#"))
			if t == "" || seen[t] {
				continue
			}
			if !validTag(t) {
				return nil, fmt.Errorf("tag %q: a letter or digit, then letters, digits, . _ / or - (at most 32)", t)
			}
			seen[t] = true
			out = append(out, t)
		}
	}
	if len(out) > MaxTags {
		return nil, fmt.Errorf("at most %d tags", MaxTags)
	}
	return out, nil
}

func validTag(t string) bool {
	if len(t) > 32 {
		return false
	}
	for i, r := range t {
		alnum := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if !alnum && (i == 0 || !strings.ContainsRune("._/-", r)) {
			return false
		}
	}
	return true
}

// Task is a task with its current state.
type Task struct {
	Event   store.Event `json:"event"`
	ID      string      `json:"display_id"`
	State   string      `json:"state"`
	Agent   string      `json:"agent,omitempty"` // what launch_command printed
	Note    string      `json:"note,omitempty"`
	Updated string      `json:"updated"`
	// ParentID is the parent task's display id, when it has one.
	ParentID string `json:"parent_display,omitempty"`
	TaskMeta
}

// Kind is the task's type, task when none was given.
func (t Task) Kind() string {
	if t.Type == "" {
		return "task"
	}
	return t.Type
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
			var tagged struct {
				Tags *[]string `json:"tags"`
			}
			_ = json.Unmarshal(e.Meta, &tagged)
			titled := MetaString(e, "title") != "" // Load put the title on the task
			if tagged.Tags != nil {
				t.Tags = *tagged.Tags
			}
			if titled {
				t.Untitled = false
			}
			if (tagged.Tags != nil || titled) && MetaString(e, "state") == "" && e.Body == "" {
				t.Updated = e.TS // only the tags or the title changed: the note stays
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
		t := byID[id]
		if t.Parent != "" {
			t.ParentID = d.Display(t.Parent)
		}
		out = append(out, *t)
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
