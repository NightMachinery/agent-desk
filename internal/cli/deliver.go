package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// DeliverTimeout bounds one attempt to reach a session.
const DeliverTimeout = 60 * time.Second

// DeliveryText is the prompt typed into the asking session.
func DeliveryText(d *desk.Desk, q, ans store.Event) string {
	return fmt.Sprintf("Answer to %s \"%s\": %s", d.Display(q.ID), q.Title, ans.Body)
}

// Runner runs an external program and returns its combined output.
type Runner func(ctx context.Context, stdin []byte, name string, args ...string) (string, error)

func (a *App) runExt(ctx context.Context, stdin []byte, name string, args ...string) (string, error) {
	if a.Exec != nil {
		return a.Exec(ctx, stdin, name, args...)
	}
	return runReal(ctx, stdin, name, args...)
}

func runReal(ctx context.Context, stdin []byte, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// sameTmuxSession reports whether a pane still runs the session that asked:
// its process is the one recorded, and the identity the agents' tmux hooks
// leave on its tmux session (agent, id, transcript), if any, names it.
func (a *App) sameTmuxSession(ctx context.Context, f ident.From) error {
	out, err := a.runExt(ctx, nil, "tmux", "display-message", "-p", "-t", f.TmuxPane,
		"#{pane_id}\t#{pane_dead}\t#{pane_pid}\t#{@agent_session}")
	if err != nil {
		return fmt.Errorf("tmux pane %s is gone", f.TmuxPane)
	}
	parts := strings.Split(out, "\t")
	if len(parts) < 3 || parts[0] != f.TmuxPane || parts[1] == "1" {
		return fmt.Errorf("tmux pane %s is gone", f.TmuxPane)
	}
	if f.TmuxPID != "" && parts[2] != f.TmuxPID {
		return fmt.Errorf("tmux pane %s now runs another process", f.TmuxPane)
	}
	if len(parts) > 4 && parts[4] != "" && parts[4] != f.Session {
		return fmt.Errorf("tmux pane %s now runs another agent session", f.TmuxPane)
	}
	return nil
}

func (a *App) typeIntoPane(ctx context.Context, pane, text string) error {
	buf := "agent-desk-" + strings.TrimPrefix(pane, "%")
	if _, err := a.runExt(ctx, []byte(text), "tmux", "load-buffer", "-b", buf, "-"); err != nil {
		return err
	}
	// -p pastes with bracketed-paste markers when the program asked for
	// them, so a multi-line answer arrives as one message.
	if out, err := a.runExt(ctx, nil, "tmux", "paste-buffer", "-p", "-d", "-b", buf, "-t", pane); err != nil {
		return fmt.Errorf("tmux paste-buffer: %v: %s", err, out)
	}
	time.Sleep(300 * time.Millisecond) // let the program take the paste before Enter
	if out, err := a.runExt(ctx, nil, "tmux", "send-keys", "-t", pane, "Enter"); err != nil {
		return fmt.Errorf("tmux send-keys: %v: %s", err, out)
	}
	return nil
}

var errUnreachable = errors.New("unreachable")

// deliverOne types an answer into the session that asked, and says how.
func (a *App) deliverOne(d *desk.Desk, q, ans store.Event) (string, error) {
	if q.Host != a.Host {
		return "", fmt.Errorf("asked on %s; it is delivered there after the next merge", q.Host)
	}
	f := fromOf(q)
	text := DeliveryText(d, q, ans)
	ctx, cancel := context.WithTimeout(context.Background(), DeliverTimeout)
	defer cancel()
	var reasons []string
	if f.Paseo != "" {
		// The default behaviour queues the message for a busy agent; never
		// --behavior interrupt.
		out, err := a.runExt(ctx, nil, "paseo", "send", "--no-wait", f.Paseo, text)
		if err == nil {
			return "paseo", nil
		}
		reasons = append(reasons, fmt.Sprintf("paseo send: %v: %s", err, out))
	}
	if f.Paseo == "" && f.TmuxPane != "" {
		err := a.sameTmuxSession(ctx, f)
		if err == nil {
			err = a.typeIntoPane(ctx, f.TmuxPane, text)
		}
		if err == nil {
			return "tmux", nil
		}
		reasons = append(reasons, err.Error())
	}
	c, err := a.config()
	if err != nil {
		return "", err
	}
	if c.DeliverCommand != "" {
		p := a.payload(d, "deliver", q)
		p.Text = text
		ansCopy := ans
		p.Cause = &ansCopy
		var out bytes.Buffer
		saved := a.Stdout
		a.Stdout = &out
		err := a.runHook(c.DeliverCommand, p, true)
		a.Stdout = saved
		if err == nil {
			return "command", nil
		}
		reasons = append(reasons, fmt.Sprintf("deliver_command: %v", err))
	}
	msg := "no way to reach the session"
	if len(reasons) > 0 {
		msg = strings.Join(reasons, "; ")
	}
	if r := ResumeCommand(f); r != "" {
		msg += ". Resume it with: " + r
	}
	return "", fmt.Errorf("%w: %s", errUnreachable, msg)
}

// deliverLock serialises deliveries, so two runs never type one answer twice.
func (a *App) deliverLock() (func(), error) {
	f, err := os.OpenFile(filepath.Join(a.Home, "deliver.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

// pending lists the answers that count, to local questions, not yet
// delivered. An answer recorded from chat already reached its session.
func pending(d *desk.Desk, host string, only string) [][2]store.Event {
	var out [][2]store.Event
	for _, q := range d.Questions {
		if q.AnswerID == "" || q.Delivered || q.Host != host || (only != "" && q.ID != only) {
			continue
		}
		ans := d.ByID[q.AnswerID]
		if desk.MetaString(ans, "source") == "chat" {
			continue
		}
		out = append(out, [2]store.Event{q.Event, ans})
	}
	return out
}

// deliver sends pending answers (all, or only question `only`), appending
// a delivered event for each. It reports per answer on stderr and returns
// how many it could not deliver.
func (a *App) deliver(only string) (int, error) {
	unlock, err := a.deliverLock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	d, err := a.load()
	if err != nil {
		return 0, err
	}
	failed := 0
	for _, pa := range pending(d, a.Host, only) {
		q, ans := pa[0], pa[1]
		via, err := a.deliverOne(d, q, ans)
		if err != nil {
			failed++
			fmt.Fprintf(a.Stderr, "agent-desk: %s not delivered: %v. `agent-desk deliver` retries.\n",
				d.Display(ans.ID), err)
			continue
		}
		e := a.event("delivered", q.Path, ident.Resolve(a.Env), "cli", map[string]any{"via": via})
		e.Ref = ans.ID
		st, err := a.store()
		if err != nil {
			return failed, err
		}
		if err := st.Append(e); err != nil {
			return failed, err
		}
		fmt.Fprintf(a.Stderr, "agent-desk: %s delivered to %s via %s\n", d.Display(ans.ID), d.Display(q.ID), via)
	}
	return failed, nil
}

func (a *App) cmdDeliver(args []string) error {
	if _, err := parse(args, nil, nil, 0, 0); err != nil {
		return err
	}
	if _, err := a.store(); err != nil {
		return err
	}
	failed, err := a.deliver("")
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, "ok")
	if failed > 0 {
		return fmt.Errorf("%d answer(s) not delivered", failed)
	}
	return a.renderWarn()
}

// mergeSSH merges both ways with another machine without copying a database
// file: its export into a local import, then the local export into its
// import, and each side delivers the answers that are now its to deliver.
// AGENT_DESK_SSH replaces ssh, AGENT_DESK_REMOTE_COMMAND the remote binary.
func (a *App) mergeSSH(host string, notify bool) error {
	sshBin := a.Env.Getenv("AGENT_DESK_SSH")
	if sshBin == "" {
		sshBin = "ssh"
	}
	remote := a.Env.Getenv("AGENT_DESK_REMOTE_COMMAND")
	if remote == "" {
		remote = "agent-desk"
	}
	if host == "" || strings.HasPrefix(host, "-") {
		return usagef("merge ssh:<host>")
	}
	st, err := a.store()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pull := exec.CommandContext(ctx, sshBin, "-o", "BatchMode=yes", host, remote+" export")
	var theirs, errb bytes.Buffer
	pull.Stdout, pull.Stderr = &theirs, &errb
	if err := pull.Run(); err != nil {
		return fmt.Errorf("%s export on %s: %v: %s", remote, host, err, strings.TrimSpace(errb.String()))
	}
	added, err := st.Import(&theirs)
	if err != nil {
		return fmt.Errorf("importing from %s: %w", host, err)
	}

	var ours bytes.Buffer
	if err := st.Export(&ours); err != nil {
		return err
	}
	errb.Reset()
	push := exec.CommandContext(ctx, sshBin, "-o", "BatchMode=yes", host,
		// An answer the other side cannot deliver yet is a normal state, not
		// a failed merge; its reasons are printed below.
		remote+" import && { "+remote+" deliver || true; }")
	push.Stdin, push.Stdout, push.Stderr = &ours, &errb, &errb
	pushErr := push.Run()
	if out := strings.TrimSpace(errb.String()); out != "" {
		for _, l := range strings.Split(out, "\n") {
			if l != "ok" {
				fmt.Fprintf(a.Stderr, "%s: %s\n", host, l)
			}
		}
	}
	if err := a.imported(added, notify); err != nil {
		return err
	}
	if _, err := a.deliver(""); err != nil {
		return err
	}
	if pushErr != nil {
		return fmt.Errorf("on %s: %v", host, pushErr)
	}
	return nil
}
