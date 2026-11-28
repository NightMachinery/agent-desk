package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// All values are fabricated.
const (
	sessA = "00000000-aaaa-4000-8000-00000000000a"
	sessB = "00000000-bbbb-4000-8000-00000000000b"
	sessC = "00000000-cccc-4000-8000-00000000000c"
)

type harness struct {
	t     *testing.T
	home  string // the user's home directory
	desk  string // AGENT_DESK_HOME
	cwd   string
	host  string
	now   time.Time
	env   map[string]string
	tmux  map[string]string // pane -> "session\trole\tpid"
	paseo string            // PASEO_HOME
	// calls records the external programs delivery ran; respond answers them.
	calls   *[]string
	respond func(name string, args []string) (string, error)
	// real names the programs that run for real, recorded in calls too.
	real map[string]bool
	// httpc sends Web Push (to a fake push service).
	httpc *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{
		t:     t,
		home:  filepath.Join(root, "home"),
		desk:  filepath.Join(root, "desk"),
		cwd:   filepath.Join(root, "home", "code", "example-proj"),
		host:  "host-a",
		now:   time.Date(2026, 3, 4, 10, 0, 0, 0, time.FixedZone("", 3600)),
		env:   map[string]string{},
		tmux:  map[string]string{},
		calls: &[]string{},
	}
	h.paseo = filepath.Join(h.home, ".paseo")
	must(t, os.MkdirAll(filepath.Join(h.cwd, ".git"), 0o755))
	return h
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (h *harness) as(session string) map[string]string {
	return map[string]string{"AI_AGENT": "claude-code_test", "CLAUDE_CODE_SESSION_ID": session}
}

func (h *harness) app(env map[string]string, stdin string) (*App, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	h.now = h.now.Add(time.Second)
	now := h.now
	a := &App{
		Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb,
		Env: ident.Env{
			Getenv: func(k string) string { return env[k] },
			Home:   h.home,
			Cwd:    h.cwd,
			Tmux: func(args ...string) (string, error) {
				pane := args[len(args)-2]
				v, ok := h.tmux[pane]
				if !ok {
					return "", fmt.Errorf("no pane")
				}
				return v + "\n", nil
			},
		},
		Now:       func() time.Time { return now },
		Host:      h.host,
		Home:      h.desk,
		WaitHooks: true,
		HTTP:      h.httpc,
		Exec: func(ctx context.Context, stdin []byte, name string, args ...string) (string, error) {
			call := name + " " + strings.Join(args, " ")
			if stdin != nil {
				call += " <<< " + string(stdin)
			}
			// Help probes (queue detection) are not deliveries; respond
			// still answers them.
			if args[len(args)-1] != "--help" {
				*h.calls = append(*h.calls, call)
			}
			if h.real[name] {
				return runReal(ctx, stdin, name, args...)
			}
			if h.respond != nil {
				return h.respond(name, args)
			}
			return "", fmt.Errorf("no %s in tests", name)
		},
	}
	return a, &out, &errb
}

// run runs one command and returns stdout, stderr and the exit status.
func (h *harness) run(env map[string]string, stdin string, args ...string) (string, string, int) {
	a, out, errb := h.app(env, stdin)
	code := a.Run(args)
	return strings.TrimSpace(out.String()), strings.TrimSpace(errb.String()), code
}

// ok runs a command that must succeed and returns its stdout.
func (h *harness) ok(env map[string]string, stdin string, args ...string) string {
	h.t.Helper()
	out, errOut, code := h.run(env, stdin, args...)
	if code != 0 {
		h.t.Fatalf("%v: exit %d: %s", args, code, errOut)
	}
	return out
}

func (h *harness) fails(env map[string]string, stdin string, want string, args ...string) {
	h.t.Helper()
	_, errOut, code := h.run(env, stdin, args...)
	if code == 0 {
		h.t.Fatalf("%v: succeeded, want failure", args)
	}
	if !strings.Contains(errOut, want) {
		h.t.Fatalf("%v: stderr %q does not contain %q", args, errOut, want)
	}
}

func (h *harness) count() int {
	st, err := store.Open(h.desk)
	must(h.t, err)
	defer st.Close()
	evs, err := st.All()
	must(h.t, err)
	return len(evs)
}

func (h *harness) showJSON(prefix string, all bool) map[string]any {
	args := []string{"show", "--format", "json"}
	if prefix != "" {
		args = append(args, prefix)
	}
	if all {
		args = append(args, "--all")
	}
	var v map[string]any
	must(h.t, json.Unmarshal([]byte(h.ok(nil, "", args...)), &v))
	return v
}

func TestJoinAndWrite(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	if id := h.ok(a, "", "join", "rerun-nov/coord"); !strings.HasPrefix(id, "j-") {
		t.Fatalf("join printed %q", id)
	}
	if id := h.ok(a, "", "set", "status", "Seeds 3/5 done."); !strings.HasPrefix(id, "s-") {
		t.Fatalf("set printed %q", id)
	}
	q := h.ok(a, "context\nA or B; I recommend B.", "ask", "--blocked", "--title", "Which split?")
	if !strings.HasPrefix(q, "q-") || len(q) < 6 {
		t.Fatalf("ask printed %q", q)
	}
	h.ok(a, "ctx", "ask", "--default", "reuse the cache", "--title", "Drop the legacy feed?")

	st, err := store.Open(h.desk)
	must(t, err)
	defer st.Close()
	evs, err := st.Query("events", "kind = 'set'")
	must(t, err)
	if len(evs) != 1 || evs[0].Path != "example-proj/rerun-nov/coord/status" {
		t.Fatalf("set event: %+v", evs)
	}
	var m struct {
		From   ident.From `json:"from"`
		Source string     `json:"source"`
	}
	must(t, json.Unmarshal(evs[0].Meta, &m))
	if m.From.Session != sessA || m.From.Agent != "claude" || m.Source != "cli" {
		t.Fatalf("meta: %s", evs[0].Meta)
	}

	v := h.showJSON("", false)
	needs := v["needs_you"].([]any)[0].(map[string]any)["items"].([]any)
	if len(needs) != 2 || needs[0].(map[string]any)["event"].(map[string]any)["level"] != "blocked" {
		t.Fatalf("needs you: blocked first: %v", needs)
	}
	h.ok(nil, "", "render") // writes no longer render; the server or this does
	for _, f := range []string{"inbox.org", "board/example-proj.org", "ledger/2026-03.org"} {
		b, err := os.ReadFile(filepath.Join(h.desk, "render", f))
		must(t, err)
		if !bytes.Contains(b, []byte("DESK_ID")) {
			t.Fatalf("%s has no items:\n%s", f, b)
		}
	}
	inbox, _ := os.ReadFile(filepath.Join(h.desk, "render", "inbox.org"))
	if !bytes.Contains(inbox, []byte("ASK "+q)) {
		t.Fatalf("inbox lacks the question:\n%s", inbox)
	}
}

func TestOverCapSetWritesNothing(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	before := h.count()
	h.fails(a, "one\ntwo\nthree\n", "at most 2 lines; this text has 3", "set", "status")
	h.fails(a, strings.Repeat("x\n", 16), "at most 15 lines; this text has 16", "set", "notes")
	h.fails(a, "", "reserved", "set", "tasks", "x")
	if n := h.count(); n != before {
		t.Fatalf("over-cap set wrote %d events", n-before)
	}
	h.ok(a, "one\ntwo\n", "set", "status")
}

func TestUpdateAndDeleteRejected(t *testing.T) {
	h := newHarness(t)
	h.ok(h.as(sessA), "", "join", "r1/coord")
	st, err := store.Open(h.desk)
	must(t, err)
	defer st.Close()
	for _, q := range []string{"UPDATE events SET title = 'x'", "DELETE FROM events"} {
		_, err := st.DB.Exec(q)
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%s: err = %v", q, err)
		}
	}
	if h.count() != 1 {
		t.Fatal("an event changed")
	}
}

func writePaseoRecord(t *testing.T, home, uuid, native, parent, title string) {
	rec := map[string]any{
		"id": uuid, "title": title,
		"labels":      map[string]string{},
		"persistence": map[string]any{"nativeHandle": native},
	}
	if parent != "" {
		rec["labels"] = map[string]string{"paseo.parent-agent-id": parent}
	}
	b, _ := json.Marshal(rec)
	dir := filepath.Join(home, "agents", "Users-example-code")
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, uuid+".json"), b, 0o644))
}

func TestWorkerRefused(t *testing.T) {
	h := newHarness(t)
	const parent = "11111111-0000-4000-8000-000000000001"
	const child = "11111111-0000-4000-8000-000000000002"
	writePaseoRecord(t, h.paseo, parent, sessA, "", "Rerun coordinator")
	writePaseoRecord(t, h.paseo, child, sessC, parent, "Eval worker")
	h.tmux["%7"] = "ag--eval\t"
	h.tmux["%8"] = "work\tsub"
	h.tmux["%9"] = "ag--x\tmain"

	cases := map[string]map[string]string{
		// A claude started by a Paseo agent inherits PASEO_AGENT_ID, but the
		// record names another native session.
		"inherited paseo id": {"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessB, "PASEO_AGENT_ID": parent},
		// A Paseo agent whose record has a parent.
		"paseo child":       {"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessC, "PASEO_AGENT_ID": child},
		"tmux ag-- session": {"AI_AGENT": "codex", "CODEX_THREAD_ID": sessB, "TMUX_PANE": "%7"},
		"tmux role sub":     {"AI_AGENT": "codex", "CODEX_THREAD_ID": sessB, "TMUX_PANE": "%8"},
		"tmux subagent node": {"AI_AGENT": "agy", "ANTIGRAVITY_CONVERSATION_ID": sessB,
			"TMUX_SUBAGENT_NODE": "eval"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			before := h.count()
			h.fails(env, "", "workers report to their parent", "set", "status", "x")
			h.fails(env, "", "workers report to their parent", "tell", "--title", "x")
			if h.count() != before {
				t.Fatal("a refused write wrote events")
			}
		})
	}

	// Explicit main beats the ag-- name; a confirmed Paseo agent with no
	// parent auto-joins adhoc under its title.
	h.ok(map[string]string{"AI_AGENT": "codex", "CODEX_THREAD_ID": sessB, "TMUX_PANE": "%9"}, "", "tell", "--title", "x")
	h.ok(map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": parent},
		"", "set", "status", "x")
	v := h.showJSON("example-proj/adhoc/rerun-coordinator", false)
	if len(v["runs"].([]any)) != 1 {
		t.Fatalf("no adhoc node from the Paseo title: %v", v)
	}
	// A worker may still be given a node explicitly.
	h.ok(cases["paseo child"], "", "join", "adhoc/rerun-coordinator/eval")
	h.ok(cases["paseo child"], "", "set", "status", "evaluating")
}

func TestNoSessionRefused(t *testing.T) {
	h := newHarness(t)
	h.fails(map[string]string{}, "", "cannot tell which agent session", "set", "status", "x")
	h.fails(map[string]string{}, "", "cannot tell which agent session", "join", "r/coord")
}

func TestJoinRules(t *testing.T) {
	h := newHarness(t)
	a, b := h.as(sessA), h.as(sessB)
	h.fails(a, "", "no coordinator has joined", "join", "r1/coord/eval-lead")
	h.ok(a, "", "join", "r1/coord")
	h.fails(b, "", "add --takeover", "join", "r1/coord")
	h.fails(a, "", "not a slug", "join", "r1/Coord")
	h.ok(b, "", "join", "r1/coord/eval-lead")
	h.ok(b, "", "set", "findings", "seed 2 diverges")

	h.ok(h.as(sessC), "", "join", "r1/coord", "--takeover")
	h.fails(a, "", "taken over", "set", "status", "x")
	h.ok(h.as(sessC), "", "set", "status", "resumed")
	// The nested coordinator keeps its own node.
	h.ok(b, "", "set", "status", "still evaluating")
	// --project overrides the derived project.
	h.ok(h.as(sessA), "", "join", "r2/coord", "--project", "other-proj")
	h.ok(h.as(sessA), "", "set", "status", "x")
	if v := h.showJSON("other-proj", false); len(v["runs"].([]any)) != 1 {
		t.Fatalf("project override: %v", v)
	}
}

func TestUnreadRules(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	h.ok(a, "", "set", "status", "busy")
	f1 := h.ok(a, "", "set", "findings", "first")
	tl := h.ok(a, "body", "tell", "--title", "deploy ok")

	newCount := func() int {
		n := 0
		for _, g := range h.showJSON("", false)["new"].([]any) {
			n += len(g.(map[string]any)["items"].([]any))
		}
		return n
	}
	if n := newCount(); n != 2 {
		t.Fatalf("new = %d, want 2 (status is never unread)", n)
	}
	h.ok(nil, "", "read", f1)
	h.ok(nil, "", "read", tl)
	if n := newCount(); n != 0 {
		t.Fatalf("new = %d after reading", n)
	}
	// A changed slot is unread again.
	h.ok(a, "", "set", "findings", "second")
	if n := newCount(); n != 1 {
		t.Fatalf("new = %d after the slot changed", n)
	}
	// unread cancels a read.
	h.ok(nil, "", "unread", tl)
	if n := newCount(); n != 2 {
		t.Fatalf("new = %d after unread", n)
	}
	// Marking a node read expands to every unread item beneath it.
	if out := h.ok(nil, "", "read", "example-proj/r1"); out != "ok" {
		t.Fatalf("read path printed %q", out)
	}
	if n := newCount(); n != 0 {
		t.Fatalf("new = %d after reading the run", n)
	}
	// Reading a node leaves its open questions alone.
	q := h.ok(a, "ctx", "ask", "--blocked", "--title", "Which?")
	h.ok(nil, "", "read", "example-proj")
	if len(h.showJSON("", false)["needs_you"].([]any)) != 1 {
		t.Fatal("reading the project hid the open question")
	}
	// Naming a question dismisses it: out of Needs you, still open, and no
	// longer counted as waiting on you in the run tree.
	_, errOut, code := h.run(nil, "", "read", q)
	if code != 0 || !strings.Contains(errOut, "stays open") {
		t.Fatalf("dismiss: %d %q", code, errOut)
	}
	if len(h.showJSON("", false)["needs_you"].([]any)) != 0 {
		t.Fatal("a dismissed question is still in Needs you")
	}
	all := h.showJSON("", true)["needs_you"].([]any)
	if len(all) != 1 || all[0].(map[string]any)["items"].([]any)[0].(map[string]any)["dismissed"] != true {
		t.Fatalf("--all does not show the dismissed question: %v", all)
	}
	run := h.showJSON("", false)["runs"].([]any)[0].(map[string]any)
	if run["state"] == "waiting on you" || run["open_questions"] != float64(0) {
		t.Fatalf("run of a dismissed question: state %v, open %v", run["state"], run["open_questions"])
	}
	if out := h.ok(a, "", "show", "--mine", "--format", "md"); !strings.Contains(out, "Which?") {
		t.Fatalf("the agent lost sight of its dismissed question:\n%s", out)
	}
	// unread brings it back; several targets at once.
	h.ok(nil, "", "unread", q)
	if len(h.showJSON("", false)["needs_you"].([]any)) != 1 {
		t.Fatal("unread did not restore the question")
	}
	h.ok(a, "", "tell", "--title", "one")
	h.ok(nil, "", "read", q, "example-proj")
	if newCount() != 0 || len(h.showJSON("", false)["needs_you"].([]any)) != 0 {
		t.Fatal("bulk read missed something")
	}
	// An answered question has nothing to dismiss.
	h.ok(nil, "", "answer", q, "B")
	if _, errOut, _ := h.run(nil, "", "read", q); !strings.Contains(errOut, "already answered") {
		t.Fatalf("read of an answered question: %q", errOut)
	}
}

func TestAnswerFromChat(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	q := h.ok(a, "ctx", "ask", "--blocked", "--title", "Which split?")
	h.fails(a, "", "not a question", "answer", h.ok(a, "", "set", "status", "x"), "--from-chat", "B")
	ans := h.ok(a, "", "answer", q, "--from-chat", "B, keep the old cache")
	if !strings.HasPrefix(ans, "a-") {
		t.Fatalf("answer printed %q", ans)
	}
	if len(h.showJSON("", false)["needs_you"].([]any)) != 0 {
		t.Fatal("answered question still open")
	}
	// A prefix of the display suffix resolves too.
	_, errOut, code := h.run(nil, "", "answer", q[:4], "A after all")
	if code != 0 {
		t.Fatalf("prefix answer: %s", errOut)
	}
	if !strings.Contains(errOut, "replaces") {
		t.Fatalf("second answer not flagged: %q", errOut)
	}
	st, _ := store.Open(h.desk)
	defer st.Close()
	qs, _ := st.Questions()
	if len(qs) != 1 || qs[0].AnswerBody != "A after all" {
		t.Fatalf("the latest answer must count: %+v", qs)
	}
	var m map[string]any
	evs, _ := st.Query("events", "kind = 'answer'")
	json.Unmarshal(evs[0].Meta, &m)
	if m["source"] != "chat" {
		t.Fatalf("from-chat source: %v", m)
	}
}

func TestSearchAndLink(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	h.ok(a, "the cache is stale", "tell", "--title", "Cache report")
	out := h.ok(nil, "", "search", "STALE")
	if !strings.Contains(out, "Cache report") {
		t.Fatalf("search: %q", out)
	}
	h.fails(nil, "", "no matches", "search", "nothing-like-this")

	f := filepath.Join(h.cwd, "TASKS.org")
	must(t, os.WriteFile(f, []byte("* TODO fabricated task\n"), 0o644))
	h.ok(a, "", "link", "tasks", "TASKS.org")
	h.ok(nil, "", "render") // writes no longer render; the server or this does
	board, _ := os.ReadFile(filepath.Join(h.desk, "render", "board", "example-proj.org"))
	if !bytes.Contains(board, []byte("fabricated task")) {
		t.Fatalf("board does not render the linked file:\n%s", board)
	}
}

func TestProjectInHomeRepository(t *testing.T) {
	h := newHarness(t)
	must(t, os.MkdirAll(filepath.Join(h.home, ".git"), 0o755))
	if got := ident.ProjectName(filepath.Join(h.home, "scripts", "zshlang"), h.home); got != "scripts" {
		t.Fatalf("dotfiles project = %q", got)
	}
	if got := ident.ProjectName(h.cwd, h.home); got != "example-proj" {
		t.Fatalf("repo project = %q", got)
	}
	plain := t.TempDir()
	if got := ident.ProjectName(filepath.Join(plain, "Some Dir"), "/nonexistent"); got != "some-dir" {
		t.Fatalf("plain project = %q", got)
	}
}

// --- concurrency -----------------------------------------------------------

// TestHelperWriter is run as a subprocess by TestConcurrentWriters.
func TestHelperWriter(t *testing.T) {
	if os.Getenv("AGENT_DESK_HELPER") == "" {
		t.Skip("helper")
	}
	n, _ := strconv.Atoi(os.Getenv("AGENT_DESK_HELPER_N"))
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": os.Getenv("AGENT_DESK_HELPER_SESSION")}
	cwd, _ := os.Getwd()
	for i := 0; i < n; i++ {
		var out, errb bytes.Buffer
		a := &App{
			Stdin: strings.NewReader(fmt.Sprintf("line %d", i)), Stdout: &out, Stderr: &errb,
			Env: ident.Env{Getenv: func(k string) string { return env[k] }, Home: os.TempDir(), Cwd: cwd},
			Now: time.Now, Host: "host-a", Home: os.Getenv("AGENT_DESK_HELPER_HOME"),
		}
		args := []string{"tell", "--title", fmt.Sprintf("%s %d", env["CLAUDE_CODE_SESSION_ID"][:13], i)}
		if i%3 == 0 {
			args = []string{"set", "findings"}
		}
		if code := a.Run(args); code != 0 {
			t.Fatalf("write %d: %s", i, errb.String())
		}
	}
}

func TestConcurrentWriters(t *testing.T) {
	h := newHarness(t)
	h.ok(h.as(sessA), "", "join", "r1/coord")
	h.ok(h.as(sessB), "", "join", "r1/coord/eval")
	const n = 150
	self, err := os.Executable()
	must(t, err)
	var cmds []*exec.Cmd
	for _, s := range []string{sessA, sessB} {
		cmd := exec.Command(self, "-test.run=^TestHelperWriter$", "-test.count=1")
		cmd.Env = append(os.Environ(), "AGENT_DESK_HELPER=1", "AGENT_DESK_HELPER_N="+strconv.Itoa(n),
			"AGENT_DESK_HELPER_SESSION="+s, "AGENT_DESK_HELPER_HOME="+h.desk)
		cmd.Dir = h.cwd
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		must(t, cmd.Start())
		cmds = append(cmds, cmd)
		t.Cleanup(func() {
			if t.Failed() {
				t.Log(buf.String())
			}
		})
	}
	for _, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("writer: %v", err)
		}
	}
	if got, want := h.count(), 2+2*n; got != want {
		t.Fatalf("store has %d events, want %d", got, want)
	}
	// The last render reflects every event.
	h.ok(nil, "", "render")
	ledger, _ := os.ReadFile(filepath.Join(h.desk, "render", "ledger", time.Now().Format("2006-01")+".org"))
	tells := bytes.Count(ledger, []byte(" tell: "))
	if want := 2 * (n - (n+2)/3); tells != want {
		t.Fatalf("ledger lists %d tells, want %d", tells, want)
	}
}

// --- merging -----------------------------------------------------------------

func TestMergeEitherOrder(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	q := h.ok(a, "ctx", "ask", "--blocked", "--title", "Which split?")
	h.ok(a, "", "set", "findings", "base")

	// B starts as a snapshot of A and the two diverge.
	root := t.TempDir()
	snap := filepath.Join(root, "snap.db")
	h.ok(nil, "", "snapshot", snap)
	hb := *h
	hb.desk = filepath.Join(root, "desk-b")
	hb.host = "host-b"
	must(t, os.MkdirAll(hb.desk, 0o700))
	hb.ok(nil, "", "merge", snap)

	h.ok(a, "", "set", "findings", "from A")
	h.ok(a, "body", "tell", "--title", "A's notice")
	hb.ok(nil, "", "answer", q, "B, keep the cache")
	hb.ok(h.as(sessB), "", "join", "r2/coord")
	hb.ok(h.as(sessB), "", "set", "status", "B is busy")
	hb.ok(nil, "", "read", "example-proj/r1")

	snapA := filepath.Join(root, "a.db")
	snapB := filepath.Join(root, "b.db")
	h.ok(nil, "", "snapshot", snapA)
	hb.ok(nil, "", "snapshot", snapB)
	h.ok(nil, "", "merge", snapB)
	hb.ok(nil, "", "merge", snapA)
	// Merging again changes nothing.
	_, errOut, _ := h.run(nil, "", "merge", snapB)
	if !strings.Contains(errOut, "0 new") {
		t.Fatalf("second merge: %q", errOut)
	}

	// A delivery that fails after a merge (A has no way to reach the
	// session) records why on that side alone, until the next merge.
	exportOf := func(x *harness) string {
		var keep []string
		for _, l := range strings.Split(x.ok(nil, "", "export"), "\n") {
			if !strings.Contains(l, `"kind":"undelivered"`) {
				keep = append(keep, l)
			}
		}
		return strings.Join(keep, "\n")
	}
	if ea, eb := exportOf(h), exportOf(&hb); ea != eb {
		t.Fatalf("exports differ after merging both ways")
	}
	// Views are identical when rendered from the same host and time.
	hb.host = h.host
	for _, all := range []bool{false, true} {
		hb.now = h.now
		va, _ := json.Marshal(h.showJSON("", all))
		vb, _ := json.Marshal(hb.showJSON("", all))
		if !bytes.Equal(va, vb) {
			t.Fatalf("views differ (all=%v):\n%s\n%s", all, va, vb)
		}
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	h.ok(a, "multi\nline \"quoted\" <b>", "tell", "--title", "x")
	dump := h.ok(nil, "", "export")

	h2 := newHarness(t)
	_, errOut, code := h2.run(nil, dump+"\n", "import")
	if code != 0 || !strings.Contains(errOut, "2 new") {
		t.Fatalf("import: %d %s", code, errOut)
	}
	if h2.ok(nil, "", "export") != dump {
		t.Fatal("round trip changed events")
	}
	_, errOut, _ = h2.run(nil, dump, "import")
	if !strings.Contains(errOut, "0 new") {
		t.Fatalf("re-import: %s", errOut)
	}
	h2.fails(nil, `{"id":"x"}`, "required", "import")
}

func TestDone(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	q := h.ok(a, "ctx", "ask", "--blocked", "--title", "Still open?")
	out := h.ok(a, "", "show", "--mine", "--format", "json")
	if !strings.Contains(out, q) {
		t.Fatalf("--mine misses the open question: %s", out)
	}
	d := h.ok(a, "All done.", "done", "--title", "Rerun finished", "--report", "/tmp/example/report.org")
	if !strings.HasPrefix(d, "d-") {
		t.Fatalf("done printed %q", d)
	}
	st, err := store.Open(h.desk)
	must(t, err)
	defer st.Close()
	slots, _ := st.Query("slot_current", "path = 'example-proj/r1/coord/status'")
	if len(slots) != 1 || slots[0].Body != "Finished; see report." {
		t.Fatalf("status: %+v", slots)
	}
	dones, _ := st.Query("events", "kind = 'done'")
	if len(dones) != 1 || !strings.Contains(string(dones[0].Meta), `"report":"/tmp/example/report.org"`) || dones[0].Body != "All done." {
		t.Fatalf("done: %+v", dones)
	}
	// The run is finished, but its open question and unread report keep it shown.
	runs := h.showJSON("", false)["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("runs: %v", runs)
	}
	h.fails(h.as(sessB), "", "has not joined", "show", "--mine")
}

func TestCodexSandboxHint(t *testing.T) {
	h := newHarness(t)
	// A store that cannot be opened, as under Codex's workspace sandbox.
	must(t, os.WriteFile(h.desk, []byte("not a directory"), 0o644))
	env := map[string]string{"AI_AGENT": "codex", "CODEX_THREAD_ID": "t-1"}
	_, errOut, code := h.run(env, "", "show")
	if code == 0 || strings.Contains(errOut, "sandbox") {
		t.Fatalf("outside the sandbox: exit %d, %q", code, errOut)
	}
	env["CODEX_SANDBOX"] = "seatbelt"
	_, errOut, code = h.run(env, "", "show")
	if code == 0 || !strings.Contains(errOut, "Codex's sandbox blocks "+h.desk) || !strings.Contains(errOut, "writable_roots") {
		t.Fatalf("inside the sandbox: exit %d, %q", code, errOut)
	}
	agy := map[string]string{"AI_AGENT": "agy", "ANTIGRAVITY_CONVERSATION_ID": "c-1"}
	_, errOut, code = h.run(agy, "", "show")
	if code == 0 || !strings.Contains(errOut, "bypass the sandbox") {
		t.Fatalf("under Antigravity: exit %d, %q", code, errOut)
	}
}

// The standard slots come first on the board, in a fixed order.
func TestStandardSlotOrder(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	for _, s := range []string{"artifacts", "velocity", "findings", "done", "next", "status", "zeta"} {
		h.ok(a, "", "set", s, "x")
	}
	h.fails(a, "", "at most 5 lines", "set", "next", "1\n2\n3\n4\n5\n6")
	var got []string
	var walk func(ns []any)
	walk = func(ns []any) {
		for _, n := range ns {
			m := n.(map[string]any)
			ss, _ := m["slots"].([]any)
			for _, s := range ss {
				got = append(got, s.(map[string]any)["name"].(string))
			}
			if c, ok := m["children"].([]any); ok {
				walk(c)
			}
		}
	}
	walk(h.showJSON("", true)["runs"].([]any))
	if want := "status next done velocity findings artifacts zeta"; strings.Join(got, " ") != want {
		t.Fatalf("slot order %q, want %q", strings.Join(got, " "), want)
	}
}

// The retired next and done slots still take a set, which says where that
// text belongs now; other slots say nothing.
func TestRetiredSlotHint(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	for slot, want := range map[string]string{"next": "open and started tasks", "done": "finished tasks"} {
		_, errOut, code := h.run(a, "", "set", slot, "x")
		if code != 0 || !strings.Contains(errOut, want) || !strings.Contains(errOut, "clear "+slot) {
			t.Fatalf("set %s: %d %q", slot, code, errOut)
		}
	}
	if _, errOut, _ := h.run(a, "", "set", "findings", "x"); errOut != "" {
		t.Fatalf("set findings printed %q", errOut)
	}
	if !strings.Contains(h.ok(nil, "", "show", "--format", "md"), "next") {
		t.Fatal("the next slot was not written")
	}
}

// A slot written with literal \n in one shell argument gets line breaks, both
// when it is written and, for text already stored that way, when it is read.
func TestSlotLiteralNewlines(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	_, errOut, code := h.run(a, "", "set", "next", `one\ntwo`)
	if code != 0 || !strings.Contains(errOut, "line break") {
		t.Fatalf("set: %d %q", code, errOut)
	}
	h.fails(a, "", "at most 5 lines", "set", "next", `1\n2\n3\n4\n5\n6`)
	// Real line breaks win: such text is kept as written.
	h.ok(a, "", "set", "findings", "a\\nb\nc")
	out := h.ok(nil, "", "show", "--format", "md")
	if !strings.Contains(out, "  one\n  two\n") || !strings.Contains(out, `a\nb`) {
		t.Fatalf("board:\n%s", out)
	}
}
