package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/ident"
)

// A behaviour is what a message does to a busy agent: steer joins the running
// turn at its next step, queue waits for the turn to end, interrupt cancels
// the turn. Steer is the default. Interrupt destroys in-flight work, so it is
// only ever chosen per message, never stored as a default.
var Behaviors = []string{"steer", "queue", "interrupt"}

// checkBehavior validates a requested behaviour; empty means the default.
func checkBehavior(b string) (string, error) {
	switch b {
	case "":
		return "steer", nil
	case "steer", "queue", "interrupt":
		return b, nil
	}
	return "", usagef("--behavior is steer, queue or interrupt, not %q", b)
}

// Support says whether a target can take a behaviour, and why not.
type Support struct {
	Name      string `json:"name"`
	Supported bool   `json:"supported"`
	Why       string `json:"why,omitempty"`
}

// tmuxInterrupts are the agents whose turn Escape cancels, leaving the
// prompt ready for a paste.
var tmuxInterrupts = map[string]bool{"claude": true, "codex": true}

func tmuxInterruptible(agent string) bool { return tmuxInterrupts[agent] }

// supports lists the three behaviours for a target reached via via.
func (a *App) supports(via string, to ident.From) []Support {
	out := make([]Support, 0, len(Behaviors))
	for _, b := range Behaviors {
		s := Support{Name: b, Supported: true}
		switch {
		case b == "queue" && via == "paseo" && !a.paseoCanQueue():
			s.Supported, s.Why = false, "this Paseo cannot queue a message yet; it is sent as steer"
		case b == "queue" && via == "tmux":
			s.Supported, s.Why = false, "a tmux pane cannot hold a message until the turn ends; it is sent as steer"
		case b == "interrupt" && via == "tmux" && !tmuxInterruptible(to.Agent):
			s.Supported, s.Why = false, "interrupting is only known to work for Claude Code and Codex in tmux"
		case via == "none":
			s.Supported, s.Why = false, "this session cannot be reached now"
		}
		out = append(out, s)
	}
	return out
}

// paseoQueueCache remembers, per paseo binary, whether it can queue.
type paseoQueueCache struct {
	Path  string `json:"path"`
	MTime string `json:"mtime"`
	Queue bool   `json:"queue"`
}

// paseoCanQueue reports whether paseo send accepts --behavior queue, read
// from its help without sending anything. The answer is cached per binary
// path and modification time, so an upgrade is noticed.
func (a *App) paseoCanQueue() bool {
	key := paseoQueueCache{}
	cache := filepath.Join(a.Home, "paseo-features.json")
	if a.Exec == nil {
		p, err := exec.LookPath("paseo")
		if err != nil {
			return false
		}
		st, err := os.Stat(p)
		if err != nil {
			return false
		}
		key.Path, key.MTime = p, st.ModTime().UTC().Format(time.RFC3339Nano)
		var old paseoQueueCache
		if b, err := os.ReadFile(cache); err == nil && json.Unmarshal(b, &old) == nil &&
			old.Path == key.Path && old.MTime == key.MTime {
			return old.Queue
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := a.runExt(ctx, nil, "paseo", "send", "--help")
	key.Queue = err == nil && helpListsQueue(out)
	if a.Exec == nil {
		if b, err := json.Marshal(key); err == nil {
			_ = os.WriteFile(cache, b, 0o600)
		}
	}
	return key.Queue
}

// helpListsQueue finds queue among the --behavior choices in paseo send's
// help, which may wrap the option's text over several lines.
func helpListsQueue(help string) bool {
	lines := strings.Split(help, "\n")
	for i, l := range lines {
		if !strings.Contains(l, "--behavior") {
			continue
		}
		text := l
		for _, next := range lines[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(next), "-") {
				break
			}
			text += " " + next
		}
		return strings.Contains(text, `"queue"`) || strings.Contains(text, "queue")
	}
	return false
}

// slashRefusal is why a message whose typed text starts with "/" cannot be
// steered: Paseo treats it as a command and replaces the running turn.
// The desk's own texts start with "Message" or "Answer", so this guards
// only against a future text format.
func slashRefusal(text string) error {
	if strings.HasPrefix(strings.TrimSpace(text), "/") {
		return fmt.Errorf("its text starts with /, which a busy agent would take as a command that replaces its turn; send it with --behavior interrupt")
	}
	return nil
}
