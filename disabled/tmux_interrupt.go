//go:build ignore

// Disabled 2026-11-17: interrupting a busy agent in a tmux pane. The user
// decided not to support it, so a tmux target treats interrupt as
// unsupported and sends the message as steer. This is the code that did it,
// kept for reference; the ignore tag keeps it out of every build.
//
// To bring it back: restore tmuxInterrupts and tmuxInterruptible in
// internal/cli/behavior.go (supports() offered interrupt in tmux only for
// these agents), interruptPane and interruptSettle in
// internal/cli/deliver.go, and in deliver()'s tmux branch:
//
//	case use == "interrupt" && !tmuxInterruptible(f.Agent):
//		use, fallback = "steer", "interrupt unsupported for this agent in tmux"
//	...
//	if err == nil && use == "interrupt" {
//		err = a.interruptPane(ctx, f.TmuxPane)
//	}
//	...
//	if use == "interrupt" {
//		disp = "interrupted"
//	}
//
// It was never verified by hand against real Claude Code or Codex panes.

package cli

import (
	"context"
	"fmt"
	"time"
)

// tmuxInterrupts are the agents whose turn Escape cancels, leaving the
// prompt ready for a paste.
var tmuxInterrupts = map[string]bool{"claude": true, "codex": true}

func tmuxInterruptible(agent string) bool { return tmuxInterrupts[agent] }

// interruptPane cancels the running turn of Claude Code or Codex with
// Escape, then waits for the prompt to come back before the paste.
func (a *App) interruptPane(ctx context.Context, pane string) error {
	if out, err := a.runExt(ctx, nil, "tmux", "send-keys", "-t", pane, "Escape"); err != nil {
		return fmt.Errorf("tmux send-keys Escape: %v: %s", err, out)
	}
	time.Sleep(interruptSettle)
	return nil
}

// interruptSettle is how long a cancelled turn takes to give the prompt back.
var interruptSettle = 1500 * time.Millisecond
