package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// fromOf decodes meta.from.
func fromOf(e store.Event) ident.From {
	var m struct {
		From ident.From `json:"from"`
	}
	_ = json.Unmarshal(e.Meta, &m)
	return m.From
}

// ResumeCommand is the shell line that resumes a session in its directory,
// with the environment that selects its account.
func ResumeCommand(f ident.From) string {
	if f.Session == "" {
		return ""
	}
	var cmd string
	switch f.Agent {
	case "claude":
		cmd = "claude --resume " + shellQuote(f.Session)
		if f.Seat != "" {
			cmd = "CLAUDE_CONFIG_DIR=" + shellQuote(f.Seat) + " " + cmd
		}
	case "codex":
		cmd = "codex resume " + shellQuote(f.Session)
		if f.Seat != "" {
			cmd = "CODEX_HOME=" + shellQuote(f.Seat) + " " + cmd
		}
	case "agy":
		cmd = "agy --conversation " + shellQuote(f.Session)
	default:
		return ""
	}
	if f.Cwd != "" {
		cmd = "cd " + shellQuote(f.Cwd) + " && " + cmd
	}
	return cmd
}

func copyToClipboard(s string) bool {
	var tries [][]string
	if runtime.GOOS == "darwin" {
		tries = [][]string{{"pbcopy"}}
	} else {
		tries = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	}
	for _, t := range tries {
		if _, err := exec.LookPath(t[0]); err != nil {
			continue
		}
		c := exec.Command(t[0], t[1:]...)
		c.Stdin = strings.NewReader(s)
		if c.Run() == nil {
			return true
		}
	}
	return false
}

// tmuxPaneAlive reports whether a tmux pane still exists.
func tmuxPaneAlive(pane string) bool {
	if pane == "" {
		return false
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", pane, "#{pane_id}").Output()
	return err == nil && strings.TrimSpace(string(out)) == pane
}

// openSession brings the session that wrote e in front of the user: in
// Paseo for a Paseo agent, by selecting its tmux pane, or else by copying
// its resume command to the clipboard.
func (a *App) openSession(e store.Event) error {
	f := fromOf(e)
	if f.Paseo != "" {
		if out, err := exec.Command("paseo", "agent", "open", f.Paseo).CombinedOutput(); err == nil {
			return nil
		} else {
			fmt.Fprintf(a.Stderr, "agent-desk: paseo agent open: %v: %s\n", err, strings.TrimSpace(string(out)))
		}
	}
	if a.Host == e.Host && tmuxPaneAlive(f.TmuxPane) {
		exec.Command("tmux", "switch-client", "-t", f.TmuxPane).Run()
		exec.Command("tmux", "select-window", "-t", f.TmuxPane).Run()
		if exec.Command("tmux", "select-pane", "-t", f.TmuxPane).Run() == nil {
			return nil
		}
	}
	cmd := ResumeCommand(f)
	if cmd == "" {
		return a.pause("no way to reach that session: it recorded no session id")
	}
	where := ""
	if e.Host != a.Host {
		where = " on " + e.Host
	}
	if copyToClipboard(cmd) {
		return a.pause("Copied the resume command" + where + ":\n\n  " + cmd)
	}
	return a.pause("Resume it" + where + " with:\n\n  " + cmd)
}
