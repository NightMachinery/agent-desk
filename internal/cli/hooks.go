package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	AgentDesk string       `json:"agent_desk"`         // this binary, for click actions
	DeskHome  string       `json:"desk_home"`
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
	return Payload{
		Action: action, DisplayID: d.Display(e.ID), Path: desk.NodeOf(e), Event: &ev,
		Local: e.Host == a.Host, AgentDesk: self, DeskHome: a.Home,
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	if a.WaitHooks {
		return cmd.Wait()
	}
	return cmd.Process.Release()
}

// announce runs the notify and dismiss commands that new events call for:
// notify on every ask, tell --notify and done; dismiss the question an
// answer closes and the item a read marks. Imported events notify only
// when notify is true.
func (a *App) announce(evs []store.Event, notify bool) {
	c, err := a.config()
	if err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: warning: %v\n", err)
		return
	}
	if c.NotifyCommand == "" && c.DismissCommand == "" {
		return
	}
	d, err := a.load()
	if err != nil {
		fmt.Fprintf(a.Stderr, "agent-desk: warning: %v\n", err)
		return
	}
	for _, e := range evs {
		var cmd string
		var p Payload
		switch {
		case !notify:
		case e.Kind == "ask", e.Kind == "done", e.Kind == "tell" && e.Level == "notify",
			desk.IsResponse(e) && desk.Meta(e)["quiet"] != true:
			cmd, p = c.NotifyCommand, a.payload(d, "notify", e)
		}
		switch {
		case e.Kind == "answer", e.Kind == "read" && desk.Meta(e)["cancel"] != true:
			target, ok := d.ByID[e.Ref]
			if !ok {
				continue
			}
			cause := e
			cmd, p = c.DismissCommand, a.payload(d, "dismiss", target)
			p.Cause = &cause
		}
		if err := a.runHook(cmd, p, false); err != nil {
			fmt.Fprintf(a.Stderr, "agent-desk: warning: %s command: %v\n", p.Action, err)
		}
	}
}
