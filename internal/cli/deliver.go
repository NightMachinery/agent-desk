package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/NightMachinery/agent-desk/internal/media"
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

// scrubbed names the variables each program must not inherit. paseo send
// reads the sender from PASEO_AGENT_ID: inherited from an agent's shell, an
// answer from the user would arrive as a message from that agent, wrapped in
// an agent-message envelope. env starts the on-answer command and the
// screen banner, which may run paseo themselves.
var scrubbed = map[string][]string{"paseo": {"PASEO_AGENT_ID"}, "env": {"PASEO_AGENT_ID"}}

// envFor is environ without the variables program name must not see.
func envFor(name string, environ []string) []string {
	drop := scrubbed[name]
	if len(drop) == 0 {
		return environ
	}
	out := make([]string, 0, len(environ))
next:
	for _, kv := range environ {
		for _, k := range drop {
			if strings.HasPrefix(kv, k+"=") {
				continue next
			}
		}
		out = append(out, kv)
	}
	return out
}

func runReal(ctx context.Context, stdin []byte, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = envFor(name, os.Environ())
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

// job is one message to type into a session: an answer to the question it
// asked, or a reply from the user.
type job struct {
	msg   store.Event // the answer or reply
	about store.Event // the question, or what the reply is about (may be empty)
	to    ident.From
	host  string
	text  string
	// behavior is what the message should do to a busy agent: steer, queue
	// or interrupt.
	behavior string
}

// receipt is how a message reached its session.
type receipt struct {
	Via      string // paseo, tmux or command
	Behavior string // the behaviour used
	// Disposition is what the transport reports happened: Paseo's
	// turn_started, steered, interrupted, queued or out_of_band; typed for
	// tmux; command, or the command's own word, for deliver_command. Empty
	// when the transport does not say.
	Disposition   string
	QueuePosition int
	Revived       bool
	Fallback      string // why Behavior differs from the one requested
	// OnAnswer is what the question's on-answer command did, if it has one.
	OnAnswer *onAnswerRun
}

// meta is the receipt as a delivered event's meta.
func (r receipt) meta() map[string]any {
	m := map[string]any{"via": r.Via, "behavior": r.Behavior}
	if r.Disposition != "" {
		m["disposition"] = r.Disposition
	}
	if r.QueuePosition > 0 {
		m["queue_position"] = r.QueuePosition
	}
	if r.Revived {
		m["revived"] = true
	}
	if r.Fallback != "" {
		m["fallback"] = r.Fallback
	}
	if r.OnAnswer != nil {
		m["on_answer"] = r.OnAnswer
	}
	return m
}

// paseoReceipt reads what paseo send --json printed. Paseo releases differ
// in what they report; fields they leave out stay empty.
func paseoReceipt(out string, r *receipt) {
	var v struct {
		Disposition    string `json:"disposition"`
		QueuePosition  int    `json:"queuePosition"`
		Revived        bool   `json:"revived"`
		DeliveryStatus string `json:"deliveryStatus"`
		Status         string `json:"status"`
	}
	// The JSON is the last line that parses; a warning may come first.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if json.Unmarshal([]byte(lines[i]), &v) == nil {
			break
		}
	}
	if v.Disposition == "" && (v.DeliveryStatus == "queued" || v.Status == "queued") {
		v.Disposition = "queued"
	}
	r.Disposition, r.QueuePosition, r.Revived = v.Disposition, v.QueuePosition, v.Revived
}

// deliverOne types a message into its session, and says how. An answer to
// a question asked with --on-answer goes to that command first; the session
// gets it only when the command passes it on (see onAnswer).
func (a *App) deliverOne(d *desk.Desk, j job) (receipt, error) {
	if j.host != a.Host {
		return receipt{}, fmt.Errorf("its session is on %s; it is delivered there after the next merge", j.host)
	}
	note, missing := a.attachmentNote(j.msg)
	if missing != "" {
		return receipt{}, fmt.Errorf("its attachment %s is not on this machine yet; it is delivered after the next merge", missing)
	}
	j.text += note
	command := desk.MetaString(j.about, "on_answer")
	if command == "" || j.msg.Kind != "answer" {
		return a.deliverSession(d, j)
	}
	run, err := a.onAnswer(d, j, command)
	if err != nil {
		return receipt{}, err
	}
	if run.Exit == 0 {
		return receipt{Via: "on-answer", Behavior: j.behavior, Disposition: "handled", OnAnswer: run}, nil
	}
	j.text += onAnswerNote(run)
	r, err := a.deliverSession(d, j)
	if err != nil {
		return r, fmt.Errorf("%w (its on-answer command exited %d: %s)", err, run.Exit, clipLine(run.Output, 200))
	}
	r.OnAnswer = run
	return r, nil
}

// deliverSession types a message into its session: through Paseo, tmux or
// deliver_command.
func (a *App) deliverSession(d *desk.Desk, j job) (receipt, error) {
	beh := j.behavior
	if beh == "" {
		beh = "steer"
	}
	f, text := j.to, j.text
	ctx, cancel := context.WithTimeout(context.Background(), DeliverTimeout)
	defer cancel()
	var reasons []string
	if f.Paseo != "" {
		// The behaviour is always explicit: paseo send interrupts the turn by
		// default unless it is sent by an agent (PASEO_AGENT_ID set), and
		// that variable is removed from its environment (see scrubbed).
		use, fallback := beh, ""
		if use == "queue" && !a.paseoCanQueue() {
			use, fallback = "steer", "queue unsupported"
		}
		if use != "interrupt" {
			if err := slashRefusal(text); err != nil {
				if !a.paseoCanQueue() {
					return receipt{}, err
				}
				if use != "queue" {
					use, fallback = "queue", "text starts with /"
				}
			}
		}
		send := func(b string) (string, error) {
			return a.runExt(ctx, nil, "paseo", "send", "--no-wait", "--json", "--behavior", b, f.Paseo, text)
		}
		out, err := send(use)
		if err != nil && use == "queue" && fallback == "" {
			// A daemon without the queue refuses the send and delivers
			// nothing, so steering after it cannot deliver twice.
			if out2, err2 := send("steer"); err2 == nil {
				out, err, use, fallback = out2, nil, "steer", "queue unsupported by the Paseo daemon"
			}
		}
		if err == nil {
			r := receipt{Via: "paseo", Behavior: use, Fallback: fallback}
			paseoReceipt(out, &r)
			return r, nil
		}
		reasons = append(reasons, fmt.Sprintf("paseo send: %s (%v)", paseoError(out), err))
	}
	if f.Paseo == "" && f.TmuxPane != "" {
		use, fallback := beh, ""
		switch {
		case use == "queue":
			use, fallback = "steer", "queue unsupported"
		case use == "interrupt":
			// Disabled by choice; the code is in disabled/tmux_interrupt.go.
			use, fallback = "steer", "interrupt unsupported in tmux"
		}
		err := a.sameTmuxSession(ctx, f)
		if err == nil {
			err = a.typeIntoPane(ctx, f.TmuxPane, text)
		}
		if err == nil {
			return receipt{Via: "tmux", Behavior: use, Disposition: "typed", Fallback: fallback}, nil
		}
		reasons = append(reasons, err.Error())
	}
	c, err := a.config()
	if err != nil {
		return receipt{}, err
	}
	if c.DeliverCommand != "" {
		subject := j.about
		if subject.ID == "" {
			subject = j.msg
		}
		p := a.payload(d, "deliver", subject)
		p.Text = text
		p.Behavior = beh
		msgCopy := j.msg
		p.Cause = &msgCopy
		var out bytes.Buffer
		saved := a.Stdout
		a.Stdout = &out
		err := a.runHook(c.DeliverCommand, p, true)
		a.Stdout = saved
		if err == nil {
			r := receipt{Via: "command", Behavior: beh, Disposition: "command"}
			var v struct {
				Disposition string `json:"disposition"`
			}
			if line := strings.TrimSpace(out.String()); line != "" && json.Unmarshal([]byte(line), &v) == nil && v.Disposition != "" {
				r.Disposition = v.Disposition
			}
			return r, nil
		}
		reasons = append(reasons, fmt.Sprintf("deliver_command: %v", err))
	}
	msg := "no way to reach the session"
	if len(reasons) > 0 {
		msg = strings.Join(reasons, "; ")
	}
	if r := ResumeCommand(f); r != "" {
		msg += resumeSep + r
	}
	return receipt{}, fmt.Errorf("%w: %s", errUnreachable, msg)
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

// pending lists what is due for a session on this host and not yet
// delivered: the answers that count, to local questions, and replies. An
// answer recorded from chat already reached its session. only, when set, is
// a question or reply id.
func pending(d *desk.Desk, host string, only string) []job {
	var out []job
	for _, q := range d.Questions {
		if q.AnswerID == "" || q.Delivered || q.Host != host || (only != "" && q.ID != only) {
			continue
		}
		ans := d.ByID[q.AnswerID]
		if desk.MetaString(ans, "source") == "chat" {
			continue
		}
		out = append(out, job{msg: ans, about: q.Event, to: fromOf(q.Event), host: q.Host, text: DeliveryText(d, q.Event, ans),
			behavior: desk.MetaString(ans, "behavior")})
	}
	delivered := map[string]bool{}
	for _, e := range d.Events {
		if e.Kind == "delivered" {
			delivered[e.Ref] = true
		}
	}
	for _, e := range d.Events {
		if e.Kind != "reply" || delivered[e.ID] || (only != "" && e.ID != only) {
			continue
		}
		to, toHost := toOf(e)
		if toHost != host {
			continue
		}
		out = append(out, job{msg: e, about: d.ByID[e.Ref], to: to, host: toHost, text: ReplyText(d, e),
			behavior: desk.MetaString(e, "behavior")})
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
	for _, j := range pending(d, a.Host, only) {
		dest := j.msg.Path
		if j.about.ID != "" {
			dest = d.Display(j.about.ID)
		}
		st, err := a.store()
		if err != nil {
			return failed, err
		}
		r, err := a.deliverOne(d, j)
		if err != nil {
			failed++
			fmt.Fprintf(a.Stderr, "agent-desk: %s not delivered: %v. `agent-desk deliver` retries.\n",
				d.Display(j.msg.ID), err)
			reason, resume, _ := strings.Cut(err.Error(), resumeSep)
			reason = strings.TrimPrefix(reason, errUnreachable.Error()+": ")
			a.deliverLog(d.Display(j.msg.ID), dest, "failed", reason)
			// The reason is kept on the desk, where the dashboard shows it
			// beside the message; a retry failing the same way adds nothing.
			if last, ok := d.DeliveryFailure(j.msg.ID); !ok || desk.MetaString(last, "error") != reason {
				m := map[string]any{"error": reason}
				if resume != "" {
					m["resume"] = resume
				}
				e := a.event("undelivered", j.msg.Path, ident.Resolve(a.Env), "cli", m)
				e.Ref = j.msg.ID
				if err := st.Append(e); err != nil {
					return failed, err
				}
			}
			continue
		}
		e := a.event("delivered", j.msg.Path, ident.Resolve(a.Env), "cli", r.meta())
		e.Ref = j.msg.ID
		if err := st.Append(e); err != nil {
			return failed, err
		}
		a.deliverLog(d.Display(j.msg.ID), dest, "delivered", strings.TrimSpace(r.Via+" "+r.Disposition))
		fmt.Fprintf(a.Stderr, "agent-desk: %s delivered to %s via %s\n", d.Display(j.msg.ID), dest, r.Via)
	}
	return failed, nil
}

// resumeSep leads the resume command at the end of a delivery error.
const resumeSep = ". Resume it with: "

// deliverLog appends one line per delivery attempt to <desk>/deliver.log:
// when, which message, where it was going, and how it went.
func (a *App) deliverLog(id, dest, outcome, detail string) {
	f, err := os.OpenFile(filepath.Join(a.Home, "deliver.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s -> %s %s: %s\n", a.Now().Format(time.RFC3339), id, dest, outcome, clipLine(detail, 2000))
}

// paseoError is the message in paseo's JSON error output, else the output.
func paseoError(out string) string {
	var v struct {
		Error struct {
			Code, Message string
		}
	}
	if i := strings.Index(out, "{"); i >= 0 && json.Unmarshal([]byte(out[i:]), &v) == nil && v.Error.Message != "" {
		return v.Error.Message
	}
	return strings.TrimSpace(out)
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
	if pushErr == nil && a.mergeMediaSSH(ctx, sshBin, host, remote) > 0 {
		// An answer there that carries files waited for them: deliver it now.
		again := exec.CommandContext(ctx, sshBin, "-o", "BatchMode=yes", host, remote+" deliver")
		again.Stdout, again.Stderr = &errb, &errb
		_ = again.Run()
	}
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

// attachedOnly is the text of an answer or message that is only files.
const attachedOnly = "(see the attached files)"

// attachmentNote lists, after a message to a session, the files the user
// sent with it, by their paths on this machine. missing names the first
// that is not here yet.
func (a *App) attachmentNote(msg store.Event) (note, missing string) {
	refs := media.Of(msg)
	if len(refs) == 0 {
		return "", ""
	}
	var b strings.Builder
	b.WriteString("\n\nThe user attached these files; read them from these paths:")
	for _, r := range refs {
		if !r.Present(a.Home) {
			return "", r.Name
		}
		fmt.Fprintf(&b, "\n- %s (%s, %s): %s", r.Name, r.MIME, media.Size(r.Size), r.Path(a.Home))
		if r.Caption != r.Name {
			fmt.Fprintf(&b, "\n  Caption: %s", r.Caption)
		}
	}
	return b.String(), ""
}

// attachmentPaths are the paths of msg's files, one per line.
func (a *App) attachmentPaths(msg store.Event) string {
	var ps []string
	for _, r := range media.Of(msg) {
		ps = append(ps, r.Path(a.Home))
	}
	return strings.Join(ps, "\n")
}
