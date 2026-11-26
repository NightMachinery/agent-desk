// Package ident works out which agent session is running a command, whether
// it has a parent agent, and which project its working directory belongs to.
package ident

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// From is stored as meta.from on every event.
type From struct {
	Agent      string `json:"agent,omitempty"`      // claude, codex, agy
	Seat       string `json:"seat,omitempty"`       // config home selecting the account, when not the default
	Session    string `json:"session,omitempty"`    // the agent's native session id
	Paseo      string `json:"paseo,omitempty"`      // the Paseo agent this session is
	Parent     string `json:"parent,omitempty"`     // paseo:<uuid> or tmux:<session>
	Cwd        string `json:"cwd,omitempty"`        // working directory
	Transcript string `json:"transcript,omitempty"` // the session's transcript file
	TmuxPane   string `json:"tmux_pane,omitempty"`  // reply address outside Paseo
	TmuxPID    string `json:"tmux_pid,omitempty"`   // the pane's process, to tell a reused pane
	Title      string `json:"title,omitempty"`      // Paseo title, for adhoc slugs and notifications
	Model      string `json:"model,omitempty"`      // the model it ran when it filed this (claude-opus-5-5)
	Effort     string `json:"effort,omitempty"`     // its reasoning effort then (xhigh), when known
}

// Env is what Resolve reads; tests fake it.
type Env struct {
	Getenv func(string) string
	Home   string
	Cwd    string
	// Tmux runs tmux with args and returns its output.
	Tmux func(args ...string) (string, error)
}

// System is the real environment.
func System() Env {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	return Env{
		Getenv: os.Getenv,
		Home:   home,
		Cwd:    cwd,
		Tmux: func(args ...string) (string, error) {
			out, err := exec.Command("tmux", args...).Output()
			return string(out), err
		},
	}
}

// agentOf picks the agent running this shell. AI_AGENT is set by the
// innermost agent and overwrites what an outer agent left, so it decides
// when present; the other markers are all inherited by nested agents.
func agentOf(get func(string) string) string {
	a := strings.ToLower(get("AI_AGENT"))
	switch {
	case strings.HasPrefix(a, "claude"):
		return "claude"
	case strings.HasPrefix(a, "codex"):
		return "codex"
	case strings.HasPrefix(a, "agy"), strings.HasPrefix(a, "antigravity"):
		return "agy"
	}
	switch {
	case get("CODEX_THREAD_ID") != "" || get("CODEX_SESSION_ID") != "":
		return "codex"
	case get("ANTIGRAVITY_CONVERSATION_ID") != "":
		return "agy"
	case get("CLAUDE_CODE_SESSION_ID") != "":
		return "claude"
	}
	return ""
}

func sessionOf(agent string, get func(string) string) string {
	switch agent {
	case "claude":
		return get("CLAUDE_CODE_SESSION_ID")
	case "codex":
		if v := get("CODEX_THREAD_ID"); v != "" {
			return v
		}
		return get("CODEX_SESSION_ID")
	case "agy":
		return get("ANTIGRAVITY_CONVERSATION_ID")
	}
	return ""
}

func firstGlob(pattern string) string {
	m, _ := filepath.Glob(pattern)
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

// PaseoAgent is the part of a stored Paseo agent record agent-desk reads.
type PaseoAgent = paseoRecord

type paseoRecord struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Labels      map[string]string `json:"labels"`
	ArchivedAt  *string           `json:"archivedAt"`
	Provider    string            `json:"provider"`
	Cwd         string            `json:"cwd"`
	LastStatus  string            `json:"lastStatus"`
	LastActive  string            `json:"lastActivityAt"`
	Persistence struct {
		NativeHandle string `json:"nativeHandle"`
	} `json:"persistence"`
	// Config is what the agent was started with, or last switched to.
	Config struct {
		Model    string `json:"model"`
		Thinking string `json:"thinkingOptionId"`
	} `json:"config"`
	// RuntimeInfo is what Paseo saw the agent run.
	RuntimeInfo struct {
		Model string `json:"model"`
	} `json:"runtimeInfo"`
}

// Parent is the Paseo agent that started this one, if any.
func (r paseoRecord) Parent() string { return r.Labels["paseo.parent-agent-id"] }

// PaseoRecords reads every stored agent record that is not archived.
func PaseoRecords(paseoHome string) []PaseoAgent {
	var out []PaseoAgent
	for _, pat := range []string{filepath.Join(paseoHome, "agents", "*.json"), filepath.Join(paseoHome, "agents", "*", "*.json")} {
		files, _ := filepath.Glob(pat)
		for _, f := range files {
			var rec paseoRecord
			b, err := os.ReadFile(f)
			if err != nil || json.Unmarshal(b, &rec) != nil || rec.ID == "" || rec.ArchivedAt != nil {
				continue
			}
			out = append(out, rec)
		}
	}
	return out
}

// PaseoStatuses maps every stored Paseo agent to its last status, or to
// "archived".
func PaseoStatuses(paseoHome string) map[string]string {
	out := map[string]string{}
	for _, pat := range []string{filepath.Join(paseoHome, "agents", "*.json"), filepath.Join(paseoHome, "agents", "*", "*.json")} {
		files, _ := filepath.Glob(pat)
		for _, f := range files {
			var rec struct {
				ID         string  `json:"id"`
				ArchivedAt *string `json:"archivedAt"`
				LastStatus string  `json:"lastStatus"`
			}
			b, err := os.ReadFile(f)
			if err != nil || json.Unmarshal(b, &rec) != nil || rec.ID == "" {
				continue
			}
			if rec.ArchivedAt != nil {
				rec.LastStatus = "archived"
			}
			out[rec.ID] = rec.LastStatus
		}
	}
	return out
}

// PaseoRecord reads the stored record of a Paseo agent.
func PaseoRecord(paseoHome, uuid string) (paseoRecord, bool) {
	var rec paseoRecord
	if uuid == "" || strings.ContainsAny(uuid, "/*?[") {
		return rec, false
	}
	for _, p := range []string{
		filepath.Join(paseoHome, "agents", uuid+".json"),
		filepath.Join(paseoHome, "agents", "*", uuid+".json"),
	} {
		f := firstGlob(p)
		if f == "" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil || json.Unmarshal(b, &rec) != nil {
			continue
		}
		return rec, true
	}
	return rec, false
}

// Resolve reads the identity of the session behind this process.
func Resolve(env Env) From {
	get := env.Getenv
	f := From{Cwd: env.Cwd}
	f.Agent = agentOf(get)
	f.Session = sessionOf(f.Agent, get)

	switch f.Agent {
	case "claude":
		f.Seat = get("CLAUDE_CONFIG_DIR")
		dir := f.Seat
		if dir == "" {
			dir = filepath.Join(env.Home, ".claude")
		}
		if f.Session != "" {
			f.Transcript = firstGlob(filepath.Join(dir, "projects", "*", f.Session+".jsonl"))
		}
	case "codex":
		f.Seat = get("CODEX_HOME")
		dir := f.Seat
		if dir == "" {
			dir = filepath.Join(env.Home, ".codex")
		}
		if f.Session != "" {
			f.Transcript = firstGlob(filepath.Join(dir, "sessions", "*", "*", "*", "*"+f.Session+".jsonl"))
		}
	}

	// Paseo. PASEO_AGENT_ID is inherited by everything a Paseo agent starts,
	// so it names this session only when that agent's record says it runs
	// this very native session. Otherwise this session was started by it.
	if uuid := get("PASEO_AGENT_ID"); uuid != "" {
		home := get("PASEO_HOME")
		if home == "" {
			home = filepath.Join(env.Home, ".paseo")
		}
		rec, ok := PaseoRecord(home, uuid)
		if ok && f.Session != "" && rec.Persistence.NativeHandle == f.Session {
			f.Paseo = uuid
			f.Title = rec.Title
			f.Model, f.Effort = rec.RuntimeInfo.Model, rec.Config.Thinking
			if f.Model == "" {
				f.Model = rec.Config.Model
			}
			if p := rec.Labels["paseo.parent-agent-id"]; p != "" {
				f.Parent = "paseo:" + p
			}
		} else {
			f.Parent = "paseo:" + uuid
		}
	}

	// tmux, after h-bell-agent-role: an explicit @agent_role on the session
	// wins; then TMUX_SUBAGENT_NODE; then an ag--* session name. A Paseo
	// agent's TMUX_PANE, if any, is the daemon's, so it says nothing here.
	if pane := get("TMUX_PANE"); pane != "" && env.Tmux != nil && f.Paseo == "" {
		out, err := env.Tmux("display-message", "-p", "-t", pane, "#{session_name}\t#{@agent_role}\t#{pane_pid}")
		name, role := "", ""
		if err == nil {
			parts := strings.SplitN(strings.TrimRight(out, "\n"), "\t", 3)
			name = parts[0]
			if len(parts) > 1 {
				role = parts[1]
			}
			if len(parts) > 2 {
				f.TmuxPID = parts[2]
			}
		}
		f.TmuxPane = pane
		switch {
		case role == "main":
			f.Parent = ""
		case role == "sub":
			if f.Parent == "" {
				f.Parent = "tmux:" + name
			}
		case get("TMUX_SUBAGENT_NODE") != "" || strings.HasPrefix(name, "ag--"):
			if f.Parent == "" {
				f.Parent = "tmux:" + name
			}
		}
	} else if get("TMUX_SUBAGENT_NODE") != "" && f.Parent == "" && f.Paseo == "" {
		f.Parent = "tmux:" + get("TMUX_SUBAGENT_NODE")
	}
	if f.Model == "" || f.Effort == "" {
		model, effort := lastTurn(f.Agent, f.Transcript)
		if f.Model == "" {
			f.Model = model
		}
		if f.Effort == "" {
			f.Effort = effort
		}
	}
	return f
}

// lastTurnBytes is how much of a transcript's end lastTurn reads: enough
// for the last few turns, small enough to cost nothing on every filing.
const lastTurnBytes = 256 << 10

// lastTurn reads the model and reasoning effort of the latest turn in a
// session transcript: a Claude assistant message (message.model, effort) or
// a Codex turn_context (payload.model, payload.effort). Either may be "".
func lastTurn(agent, transcript string) (model, effort string) {
	if transcript == "" || (agent != "claude" && agent != "codex") {
		return "", ""
	}
	fh, err := os.Open(transcript)
	if err != nil {
		return "", ""
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return "", ""
	}
	off := st.Size() - lastTurnBytes
	if off < 0 {
		off = 0
	}
	buf := make([]byte, st.Size()-off)
	if _, err := fh.ReadAt(buf, off); err != nil {
		return "", ""
	}
	lines := strings.Split(string(buf), "\n")
	for i := len(lines) - 1; i >= 0 && (model == "" || effort == ""); i-- {
		l := lines[i]
		var m, e string
		switch {
		case agent == "claude" && strings.Contains(l, `"type":"assistant"`):
			var rec struct {
				Effort  string `json:"effort"`
				Message struct {
					Model string `json:"model"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(l), &rec) != nil {
				continue
			}
			m, e = rec.Message.Model, rec.Effort
			if strings.HasPrefix(m, "<") { // <synthetic>: no model ran
				m = ""
			}
		case agent == "codex" && strings.Contains(l, `"turn_context"`):
			var rec struct {
				Type    string `json:"type"`
				Payload struct {
					Model  string `json:"model"`
					Effort string `json:"effort"`
				} `json:"payload"`
			}
			if json.Unmarshal([]byte(l), &rec) != nil || rec.Type != "turn_context" {
				continue
			}
			m, e = rec.Payload.Model, rec.Payload.Effort
		default:
			continue
		}
		// The latest turn decides; an older one fills only what it lacks.
		if model == "" {
			model = m
		}
		if effort == "" {
			effort = e
		}
	}
	return model, effort
}

var slugBad = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns free text into a slug of at most n characters.
func Slug(s string, n int) string {
	s = strings.Trim(slugBad.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > n {
		s = strings.TrimRight(s[:n], "-")
	}
	return s
}

// ProjectName is the basename of the repository root of cwd, or of cwd
// outside a repository. When the root is home (a dotfiles repository), it is
// the first directory below home on the way to cwd.
func ProjectName(cwd, home string) string {
	cwd = filepath.Clean(cwd)
	home = filepath.Clean(home)
	root := ""
	for d := cwd; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			root = d
			break
		}
		if d == filepath.Dir(d) {
			break
		}
	}
	name := filepath.Base(cwd)
	switch {
	case root != "" && root == home:
		rel, err := filepath.Rel(home, cwd)
		if err == nil && rel != "." {
			name = strings.Split(rel, string(filepath.Separator))[0]
		} else {
			name = filepath.Base(home)
		}
	case root != "":
		name = filepath.Base(root)
	}
	if s := Slug(strings.TrimPrefix(name, "."), 40); s != "" {
		return s
	}
	return "home"
}
