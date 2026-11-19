package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/NightMachinery/agent-desk/internal/config"
	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Payload is the JSON a configured command reads on stdin.
type Payload struct {
	Action    string       `json:"action"` // notify, dismiss, open, deliver
	DisplayID string       `json:"display_id,omitempty"`
	Path      string       `json:"path"`
	Event     *store.Event `json:"event,omitempty"`
	Cause     *store.Event `json:"cause,omitempty"`    // the answer or read that dismisses
	Text      string       `json:"text,omitempty"`     // deliver: the prompt to type
	Behavior  string       `json:"behavior,omitempty"` // deliver: steer, queue or interrupt
	Local     bool         `json:"local"`              // the event was written on this host
	// Routing (notify, dismiss and open): the channel (desktop or remote),
	// its destination, and for dismiss the handle notify printed.
	Channel string `json:"channel,omitempty"`
	Dest    string `json:"dest,omitempty"`
	Handle  string `json:"handle,omitempty"`
	// URL is the item's page in the web dashboard (needs base_url).
	URL        string `json:"url,omitempty"`
	Project    string `json:"project,omitempty"`
	Class      string `json:"class,omitempty"`
	AgentTitle string `json:"agent_title,omitempty"`
	AgentDesk  string `json:"agent_desk"` // this binary, for click actions
	DeskHome   string `json:"desk_home"`
}

func (a *App) config() (config.Config, error) {
	if a.cfg != nil {
		return *a.cfg, nil
	}
	c, err := config.Load(config.Path(a.Env.Getenv, a.Env.Home))
	if err != nil {
		return c, err
	}
	a.cfg = &c
	return c, nil
}

func (a *App) payload(d *desk.Desk, action string, e store.Event) Payload {
	self, _ := os.Executable()
	ev := e
	c, _ := a.config()
	node := desk.NodeOf(e)
	return Payload{
		Action: action, DisplayID: d.Display(e.ID), Path: node, Event: &ev,
		Local: e.Host == a.Host, AgentDesk: self, DeskHome: a.Home,
		URL: itemURL(c, d, e), Project: strings.SplitN(node, "/", 2)[0], Class: desk.ClassOf(e),
		AgentTitle: agentTitle(e),
	}
}

// runHook runs a configured command under sh with the payload on stdin.
// Detached (wait false), it outlives this process, so a slow notifier never
// holds up an agent's write; its output goes to hooks.log in the store.
func (a *App) runHook(command string, p Payload, wait bool) error {
	if command == "" {
		return nil
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	in, err := os.CreateTemp("", "agent-desk-hook-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(in.Name()) // the child keeps its open descriptor
	if _, err := in.Write(append(data, '\n')); err != nil {
		in.Close()
		return err
	}
	if _, err := in.Seek(0, 0); err != nil {
		in.Close()
		return err
	}
	defer in.Close()

	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin = in
	cmd.Env = append(os.Environ(), "AGENT_DESK_HOME="+a.Home)
	if wait {
		cmd.Stdout, cmd.Stderr = a.Stdout, a.Stderr
		return cmd.Run()
	}
	logf, err := os.OpenFile(filepath.Join(a.Home, "hooks.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	fmt.Fprintf(logf, "%s %s %s\n", time.Now().Format(time.RFC3339), p.Action, p.DisplayID)
	cmd.Stdout, cmd.Stderr = logf, logf
	// A new session, so no controlling terminal: a hook that touches
	// /dev/tty cannot be stopped by job control (SIGTTIN/SIGTTOU), which
	// once froze notify-run, holding notify.lock while every later run
	// queued behind it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	if a.WaitHooks {
		return cmd.Wait()
	}
	return cmd.Process.Release()
}
