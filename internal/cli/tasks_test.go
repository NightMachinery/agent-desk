package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/desk"
)

func writeConfig(t *testing.T, h *harness, body string) {
	cfg := filepath.Join(h.home, ".config", "agent-desk", "config.toml")
	must(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
	must(t, os.WriteFile(cfg, []byte(body), 0o644))
}

func TestTasks(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")

	// An agent's own list.
	w1 := h.ok(a, "", "task", "add", "--title", "Rerun seed 4", "--brief", "b")
	w2 := h.ok(a, "never read", "task", "add", "--title", "Write the table", "--priority", "1", "--brief", "Seeds 1-5, one row each.")
	h.ok(a, "", "task", "start", w1)
	// An agent says why it files a task and how it finished one; the user
	// at a shell (or the dashboard) need not.
	h.fails(a, "", "task add needs --brief", "task", "add", "--title", "Bare")
	h.fails(a, "", "task done needs --note", "task", "done", w2)
	h.fails(a, "", "task drop needs --note", "task", "drop", w2)
	h.ok(nil, "", "task", "add", "--title", "The user's own", "--node", "example-proj/r1/coord")
	h.ok(a, "", "task", "done", w2, "--note", "in results.md")
	stdinNote := h.ok(a, "", "task", "add", "--title", "Note from stdin", "--brief", "b")
	h.ok(a, "Checked with go test.\n", "task", "done", stdinNote, "--note", "-")
	noted := ""
	for _, tk := range h.loadDesk().Tasks(desk.Filter{All: true}) {
		if tk.ID == stdinNote {
			noted = tk.Note
		}
	}
	if noted != "Checked with go test." {
		t.Fatalf("note from stdin: %q", noted)
	}
	if p := h.ok(nil, "", "task", "prompt", w2); !strings.Contains(p, "Seeds 1-5, one row each.") {
		t.Fatalf("inline brief: %s", p)
	}
	out := h.ok(nil, "", "task", "list")
	if !strings.Contains(out, w1+"  started  Rerun seed 4") || strings.Contains(out, "Write the table") {
		t.Fatalf("list:\n%s", out)
	}
	if out := h.ok(nil, "", "task", "list", "--all"); !strings.Contains(out, "done") {
		t.Fatalf("list --all:\n%s", out)
	}

	// Deferred tasks wait for quota.
	h.fails(a, "", "needs a brief", "task", "add", "--defer", "--title", "x")
	h.fails(a, "b", "--size is S, M or L", "task", "add", "--defer", "--title", "x", "--size", "XL")
	d1 := h.ok(a, "Profile the loader.", "task", "add", "--defer", "--title", "Profile", "--size", "M", "--priority", "3")
	d2 := h.ok(a, "Port the plots to the new style.", "task", "add", "--defer", "--title", "Plots", "--profile", "codex-high", "--priority", "1")
	d3 := h.ok(nil, "Tidy the README.", "task", "add", "--defer", "--title", "README", "--node", "example-proj/r1/coord")
	if out := h.ok(nil, "", "task", "launch", "--deferred", "--dry-run", "--max", "2"); out != d2+"  Plots\n"+d3+"  README" {
		t.Fatalf("pick order:\n%s", out)
	}
	prompt := h.ok(nil, "", "task", "prompt", d2)
	if !strings.Contains(prompt, "Port the plots to the new style.") ||
		!strings.Contains(prompt, "agent-desk join r1/task-"+strings.TrimPrefix(d2, "w-")+" --project example-proj") ||
		!strings.Contains(prompt, "agent-desk task done "+d2) {
		t.Fatalf("prompt:\n%s", prompt)
	}
	h.fails(nil, "", "no launch_command", "task", "launch", d1)

	writeConfig(t, h, "[tasks]\nlaunch_command = 'my-launcher'\nmax_launch = 2\n")
	n := 0
	h.respond = func(name string, args []string) (string, error) {
		if name == "sh" && args[1] == "my-launcher" {
			n++
			return fmt.Sprintf("a warning\nagent-%d", n), nil
		}
		return "", fmt.Errorf("unexpected %s", name)
	}
	h.fails(nil, "", "max_launch is 2", "task", "launch", d1, d2, d3)
	out = h.ok(nil, "", "task", "launch", "--deferred")
	if !strings.Contains(out, d2+"  launched  agent-1") || !strings.Contains(out, d3+"  launched  agent-2") {
		t.Fatalf("launch:\n%s", out)
	}
	if !strings.Contains((*h.calls)[0], `"prompt":"This is a task`) || !strings.Contains((*h.calls)[0], `"profile":"codex-high"`) {
		t.Fatalf("payload: %s", (*h.calls)[0])
	}
	out = h.ok(nil, "", "task", "list", "--deferred")
	if out != d1+"  deferred Profile  [M p3]  example-proj/r1/coord" {
		t.Fatalf("after launch:\n%s", out)
	}
	h.fails(nil, "", "is started", "task", "launch", d2)

	// The dashboard: deferred tasks in the view, and actions.
	_, hd := testServer(t, h)
	_, body, _ := do(t, hd, "GET", "/api/view?path=example-proj", "", nil)
	if !strings.Contains(body, `"deferred":[{`) || !strings.Contains(body, `"launch":{"ready":true,"max":2}`) ||
		!strings.Contains(body, `"tasks":[`) {
		t.Fatalf("view: %s", body)
	}
	if code, body, _ := do(t, hd, "POST", "/api/task", `{"action":"drop","id":"`+d1+`"}`, dashboard); code != 200 {
		t.Fatalf("drop: %d %s", code, body)
	}
	if code, _, _ := do(t, hd, "POST", "/api/task", `{"action":"explode","id":"`+d1+`"}`, dashboard); code != 400 {
		t.Fatalf("bad action: %d", code)
	}
	if out := h.ok(nil, "", "task", "list", "--deferred"); out != "" {
		t.Fatalf("after drop: %q", out)
	}
	h.ok(nil, "", "task", "defer", d1)

	// The nudge: a weekly window about to reset with quota unused.
	reset := h.now.Add(10*3600e9 + 1800e9).Unix()
	writeConfig(t, h, "[tasks]\nquota_command = 'quota'\n")
	h.respond = func(name string, args []string) (string, error) {
		return fmt.Sprintf(`note: partial
{"gauges": {"claude.default.7d": {"pct": 35, "reset": %d}, "claude.default.5h": {"pct": 1, "reset": %d},
 "codex.a.7d": {"pct": 90, "reset": %d}}}`, reset, reset, reset), fmt.Errorf("exit status 1")
	}
	out = h.ok(nil, "", "task", "nudge")
	if !strings.HasPrefix(out, "t-") {
		t.Fatalf("nudge: %q", out)
	}
	show := h.ok(nil, "", "show", "--format", "md")
	if !strings.Contains(show, "Quota to spare: claude.default.7d resets in 10h at 35% used") || strings.Contains(show, "codex.a.7d") ||
		!strings.Contains(show, d1+" Profile") {
		t.Fatalf("show:\n%s", show)
	}
	if out := h.ok(nil, "", "task", "nudge"); out != "no nudge due" {
		t.Fatalf("second nudge: %q", out)
	}
}

// Tasks as the checklist: types, subtasks, an org rendering, and tasks the
// user files from the dashboard.
func TestTaskTypesAndOrg(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")

	ms := h.ok(a, "", "task", "add", "--title", "Ship 1.4", "--brief", "b")
	bug := h.ok(a, "", "task", "add", "--title", "Login fails on Android 15", "--type", "bug", "--parent", ms, "--priority", "1")
	h.ok(a, "", "task", "add", "--title", "Dark mode", "--type", "Feature", "--parent", ms)
	h.fails(a, "", "--type is one of", "task", "add", "--title", "x", "--type", "epic")
	h.fails(a, "", "not a task", "task", "add", "--title", "x", "--parent", h.ok(a, "y", "tell", "--title", "n"))
	h.ok(a, "", "task", "start", bug)

	if out := h.ok(nil, "", "task", "list", "--type", "bug"); !strings.Contains(out, "bug: Login fails on Android 15") ||
		strings.Contains(out, "Dark mode") || !strings.Contains(out, "under "+ms) {
		t.Fatalf("list --type bug:\n%s", out)
	}
	org := h.ok(nil, "", "task", "list", "--org")
	want := "* example-proj/r1/coord\n- [ ] Ship 1.4 (" + ms + ")\n  - [-] bug: Login fails on Android 15 (" + bug + ", started, p1)\n  - [ ] feature: Dark mode ("
	if !strings.HasPrefix(org, want) {
		t.Fatalf("org:\n%s\nwant prefix:\n%s", org, want)
	}

	// From the dashboard: at a node on the desk, with a type and a parent.
	app, _, _ := h.app(nil, "")
	id, err := app.taskAddWeb("example-proj/r1/coord", "Crash on rotate", "bug", "Seen twice.", ms, nil, nil)
	must(t, err)
	if !strings.HasPrefix(id, "w-") {
		t.Fatalf("id %q", id)
	}
	for _, bad := range [][]string{{"", "t"}, {"nowhere/x", "t"}, {"example-proj/r1/coord", ""}, {"_desk/quota/nudge", "t"}} {
		if _, err := app.taskAddWeb(bad[0], bad[1], "", "", "", nil, nil); err == nil {
			t.Fatalf("filed %v", bad)
		}
	}
	if _, err := app.taskAddWeb("example-proj", "t", "epic", "", "", nil, nil); err == nil {
		t.Fatal("filed an unknown type")
	}
	d := h.loadDesk()
	var got *desk.Task
	for _, x := range d.Tasks(desk.Filter{}) {
		if x.ID == id {
			got = &x
		}
	}
	if got == nil || got.Type != "bug" || got.ParentID != ms || got.Event.Body != "Seen twice." || desk.MetaString(got.Event, "source") != "web" {
		t.Fatalf("web task: %+v", got)
	}
}

// Free tags beside the fixed type: cleaned on add, added and removed later
// without touching the note, filtered on, and shown in both listings.
func TestTaskTags(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	id := h.ok(a, "", "task", "add", "--title", "Sync drafts", "--brief", "b", "--type", "feature", "--tag", "#UI", "--tag", "phone, sync")
	h.ok(a, "", "task", "start", id, "--note", "halfway")
	h.fails(a, "", "tag", "task", "add", "--title", "x", "--tag", "-bad")
	h.ok(a, "", "task", "tag", id, "desk", "ui")
	h.ok(a, "", "task", "untag", id, "phone")
	d := h.loadDesk()
	var got desk.Task
	for _, x := range d.Tasks(desk.Filter{}) {
		if x.ID == id {
			got = x
		}
	}
	if strings.Join(got.Tags, ",") != "ui,sync,desk" || got.Note != "halfway" || got.State != desk.TaskStarted {
		t.Fatalf("after tag and untag: %+v", got)
	}
	h.ok(a, "", "task", "add", "--title", "Other", "--brief", "b")
	if out := h.ok(nil, "", "task", "list", "--tag", "#sync"); !strings.Contains(out, "Sync drafts") || strings.Contains(out, "Other") || !strings.Contains(out, "#desk") {
		t.Fatalf("list --tag:\n%s", out)
	}
	if out := h.ok(nil, "", "task", "list", "--org"); !strings.Contains(out, "feature: Sync drafts #ui #sync #desk") {
		t.Fatalf("org:\n%s", out)
	}
}

// A task the user files without a title is titled by its brief and marked
// untitled until an agent names it with task title, which shows everywhere,
// keeps the note, and does not make the namer its worker.
func TestUntitledTask(t *testing.T) {
	h := newHarness(t)
	const pA, pB = "66666666-0000-4000-8000-0000000000a1", "66666666-0000-4000-8000-0000000000b1"
	writePaseoRecord(t, h.paseo, pA, sessA, "", "Coordinator A")
	writePaseoRecord(t, h.paseo, pB, sessB, "", "Coordinator B")
	a := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": pA}
	b := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessB, "PASEO_AGENT_ID": pB}
	h.ok(a, "", "join", "r1/coord")
	h.ok(b, "", "join", "r2/coord")
	_, hd := testServer(t, h)

	if code, body, _ := do(t, hd, "POST", "/api/task", `{"action":"add","to":"example-proj/r1/coord"}`, dashboard); code != 400 || !strings.Contains(body, "a title, details or a file") {
		t.Fatalf("neither title nor details: %d %s", code, body)
	}
	long := "pasting images etc in task messages needs to work, and Esc should leave insert mode first"
	code, body, _ := do(t, hd, "POST", "/api/task", `{"action":"add","to":"example-proj/r1/coord","text":"`+long+`\nmore detail"}`, dashboard)
	if code != 200 {
		t.Fatalf("untitled: %d %s", code, body)
	}
	id := strings.Split(strings.Split(body, `"id":"`)[1], `"`)[0]
	task := func() desk.Task {
		t.Helper()
		for _, x := range h.loadDesk().Tasks(desk.Filter{}) {
			if x.ID == id {
				return x
			}
		}
		t.Fatalf("no task %s", id)
		return desk.Task{}
	}
	if x := task(); !x.Untitled || x.Event.Title != clipLine(long, untitledLen) || !strings.HasSuffix(x.Event.Title, "…") {
		t.Fatalf("untitled task: %+v", x)
	}

	h.ok(a, "", "task", "start", id, "--note", "on it")
	h.fails(b, "", "wrong number of arguments", "task", "title", id)
	h.ok(b, "", "task", "title", id, "Paste", "images", "into", "task", "messages")
	x := task()
	if x.Untitled || x.Event.Title != "Paste images into task messages" || x.Note != "on it" || x.State != desk.TaskStarted {
		t.Fatalf("named: %+v", x)
	}
	if out := h.ok(nil, "", "task", "list", "--all"); !strings.Contains(out, "Paste images into task messages") {
		t.Fatalf("list shows the old title:\n%s", out)
	}
	// A file pasted into the form: kept on the task, which a file alone titles.
	up := map[string]string{"X-Agent-Desk": "1", "Content-Type": "application/octet-stream", "Origin": "http://example.com"}
	code, body, _ = do(t, hd, "POST", "/api/upload?name=shot.png", string(tinyPNG), up)
	if code != 200 {
		t.Fatalf("upload: %d %s", code, body)
	}
	var res struct {
		Ref json.RawMessage `json:"ref"`
	}
	must(t, json.Unmarshal([]byte(body), &res))
	code, body, _ = do(t, hd, "POST", "/api/task", `{"action":"add","to":"example-proj/r1/coord","media":[`+string(res.Ref)+`]}`, dashboard)
	if code != 200 {
		t.Fatalf("file only: %d %s", code, body)
	}
	pic := strings.Split(strings.Split(body, `"id":"`)[1], `"`)[0]
	code, body, _ = do(t, hd, "GET", "/api/item?id="+pic, "", nil)
	if code != 200 || !strings.Contains(body, `"Files: shot.png"`) || !strings.Contains(body, `"kind":"image"`) {
		t.Fatalf("the task with a file: %d %s", code, body)
	}

	// B named it, but A works on it: messages about it still go to A.
	code, body, _ = do(t, hd, "GET", "/api/target?to="+id, "", nil)
	if code != 200 || !strings.Contains(body, pA) {
		t.Fatalf("target after the rename: %d %s", code, body)
	}
}
