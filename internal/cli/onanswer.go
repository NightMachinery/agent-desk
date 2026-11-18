package cli

import (
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
)

// On answer: `ask --on-answer CMD` stores CMD on the question. When the
// user answers, delivery runs CMD on the machine that asked, before it
// tries the session:
//
//   - the answer's text is on its standard input, and AGENT_DESK_QUESTION,
//     AGENT_DESK_ANSWER (display ids), their event ids, AGENT_DESK_PATH and
//     AGENT_DESK_TITLE describe it;
//   - it runs in the asker's working directory, under sh -c;
//   - exit 0 means it handled the answer: nothing reaches the session;
//   - any other exit passes the answer to the session as usual, with the
//     command's exit status and output after it.
//
// So a script can ask the user something and act on the reply itself, with
// no agent turn, and wake its agent only for what it cannot handle.

// OnAnswerTimeout bounds one run of an on-answer command.
const OnAnswerTimeout = 10 * time.Minute

// onAnswerRun is what one run did. It is kept in on-answer/<answer id>.json,
// so a delivery retried later (the session was unreachable) reuses it
// instead of running the command a second time.
type onAnswerRun struct {
	Exit   int    `json:"exit"`
	Output string `json:"output,omitempty"`
}

func (a *App) onAnswer(d *desk.Desk, j job, command string) (*onAnswerRun, error) {
	dir := filepath.Join(a.Home, "on-answer")
	path := filepath.Join(dir, j.msg.ID+".json")
	if b, err := os.ReadFile(path); err == nil {
		var r onAnswerRun
		if json.Unmarshal(b, &r) == nil {
			return &r, nil
		}
	}
	cwd := fromOf(j.about).Cwd
	if fi, err := os.Stat(cwd); cwd == "" || err != nil || !fi.IsDir() {
		cwd = a.Home
	}
	ctx, cancel := context.WithTimeout(context.Background(), OnAnswerTimeout)
	defer cancel()
	out, err := a.runExt(ctx, []byte(j.msg.Body), "env",
		"AGENT_DESK_HOME="+a.Home,
		"AGENT_DESK_QUESTION="+d.Display(j.about.ID), "AGENT_DESK_QUESTION_ID="+j.about.ID,
		"AGENT_DESK_ANSWER="+d.Display(j.msg.ID), "AGENT_DESK_ANSWER_ID="+j.msg.ID,
		"AGENT_DESK_PATH="+j.about.Path, "AGENT_DESK_TITLE="+j.about.Title,
		"AGENT_DESK_CWD="+cwd,
		"sh", "-c", `cd -- "$AGENT_DESK_CWD" || exit 125; exec sh -c "$1"`, "sh", command)
	r := onAnswerRun{Output: clipText(out, 4000)}
	if err != nil {
		var ee interface{ ExitCode() int }
		switch {
		case ctx.Err() != nil:
			r.Exit = 124
			r.Output = strings.TrimSpace(r.Output + "\n(stopped after " + humanDur(OnAnswerTimeout) + ")")
		case errors.As(err, &ee) && ee.ExitCode() > 0:
			r.Exit = ee.ExitCode()
		default:
			r.Exit = 127
			r.Output = strings.TrimSpace(r.Output + "\n" + err.Error())
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(r)
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return nil, err
	}
	return &r, nil
}

// onAnswerNote follows the answer to a session when the on-answer command
// passed it on.
func onAnswerNote(r *onAnswerRun) string {
	s := fmt.Sprintf("\n\n(Its on-answer command exited %d and passed the answer to you", r.Exit)
	if r.Output == "" {
		return s + ".)"
	}
	return s + ". Its output:\n" + r.Output + "\n)"
}

// clipText keeps the last n bytes of s, where errors usually are.
func clipText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// deliverDetached starts `agent-desk deliver` in the background, its output
// in hooks.log: an on-answer command may take minutes, and whoever recorded
// the answer (the dashboard, Emacs, a chat bridge) should not wait for it.
func (a *App) deliverDetached() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(a.Home, "hooks.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	fmt.Fprintf(logf, "%s deliver (on-answer)\n", time.Now().Format(time.RFC3339))
	cmd := exec.Command(exe, "deliver")
	cmd.Env = append(os.Environ(), "AGENT_DESK_HOME="+a.Home)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
