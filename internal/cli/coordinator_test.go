package cli

import (
	"encoding/json"
	"testing"
)

// New task at a project or a run tells the coordinator working under it:
// api/target?coordinator=1 resolves the node no agent holds to the newest
// unfinished coordinator there. Without the flag a project is still not a
// node to message.
func TestCoordinatorUnder(t *testing.T) {
	h := newHarness(t)
	const pA, pB = "33333333-0000-4000-8000-0000000000a1", "33333333-0000-4000-8000-0000000000b1"
	writePaseoRecord(t, h.paseo, pA, sessA, "", "Old coordinator")
	writePaseoRecord(t, h.paseo, pB, sessB, "", "New coordinator")
	a := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": pA}
	b := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessB, "PASEO_AGENT_ID": pB}
	h.ok(a, "", "join", "r1/coord")
	h.ok(b, "", "join", "r2/coord")
	h.ok(a, "", "set", "status", "Still going.") // r1 is newer...
	h.ok(a, "All done.", "done", "--title", "r1 finished") // ...but finished

	_, hd := testServer(t, h)
	target := func(q string) (int, Target) {
		t.Helper()
		code, body, _ := do(t, hd, "GET", "/api/target?"+q, "", nil)
		var tg Target
		if code == 200 {
			must(t, json.Unmarshal([]byte(body), &tg))
		}
		return code, tg
	}
	if code, _ := target("to=example-proj"); code != 404 {
		t.Fatalf("a project without the flag: %d, want 404", code)
	}
	if code, tg := target("to=example-proj&coordinator=1"); code != 200 || tg.Node != "example-proj/r2/coord" || tg.For != "example-proj" || tg.Paseo != pB {
		t.Fatalf("project: %d %+v", code, tg)
	}
	if code, tg := target("to=example-proj/r1&coordinator=1"); code != 200 || tg.Node != "example-proj/r1/coord" || tg.For != "example-proj/r1" {
		t.Fatalf("run with only a finished coordinator: %d %+v", code, tg)
	}
	if code, tg := target("to=example-proj/r2/coord&coordinator=1"); code != 200 || tg.Node != "example-proj/r2/coord" || tg.For != "" {
		t.Fatalf("a held node is itself: %d %+v", code, tg)
	}
	if code, _ := target("to=nobody&coordinator=1"); code != 404 {
		t.Fatalf("no agent under it: %d, want 404", code)
	}
}
