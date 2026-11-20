package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// Drafts are kept on the desk per box: a save is read back from any device,
// a later save always reads as newer, a null drops it (leaving a newer
// mark), and keys and sizes are checked.
func TestDrafts(t *testing.T) {
	_, hd := testServer(t, newHarness(t))
	get := func(key string) map[string]any {
		t.Helper()
		code, body, _ := do(t, hd, "GET", "/api/draft?key="+key, "", nil)
		if code != 200 {
			t.Fatalf("get %s: %d %s", key, code, body)
		}
		var m map[string]any
		must(t, json.Unmarshal([]byte(body), &m))
		return m
	}
	put := func(body string) (int, map[string]any) {
		t.Helper()
		code, out, _ := do(t, hd, "POST", "/api/draft", body, dashboard)
		var m map[string]any
		_ = json.Unmarshal([]byte(out), &m)
		return code, m
	}

	if m := get("reply:t-abcd"); m["value"] != nil || m["updated"].(float64) != 0 {
		t.Fatalf("no draft yet: %v", m)
	}
	code, first := put(`{"key":"reply:t-abcd","draft":"half a thought","device":"laptop"}`)
	if code != 200 || first["updated"].(float64) == 0 {
		t.Fatalf("save: %d %v", code, first)
	}
	if m := get("reply:t-abcd"); m["value"] != "half a thought" || m["device"] != "laptop" {
		t.Fatalf("read back: %v", m)
	}
	// A question's picks are any JSON; the same clock reading still moves on.
	code, second := put(`{"key":"choices:q-abcd","draft":[{"picked":["A"],"text":"note"}],"device":"phone"}`)
	if code != 200 {
		t.Fatalf("choices: %d %v", code, second)
	}
	_, again := put(`{"key":"reply:t-abcd","draft":"a whole thought","device":"phone"}`)
	if again["updated"].(float64) <= first["updated"].(float64) {
		t.Fatalf("a later save is not newer: %v then %v", first, again)
	}
	_, dropped := put(`{"key":"reply:t-abcd","draft":null,"device":"phone"}`)
	m := get("reply:t-abcd")
	if m["value"] != nil || m["updated"].(float64) != dropped["updated"].(float64) || dropped["updated"].(float64) <= again["updated"].(float64) {
		t.Fatalf("dropped: %v %v", dropped, m)
	}
	for _, bad := range []string{
		`{"key":"no-colon","draft":"x"}`,
		`{"key":"Reply:t-abcd","draft":"x"}`,
		`{"key":"reply:has space","draft":"x"}`,
		`{"key":"reply:t-abcd","draft":"` + strings.Repeat("x", 300<<10) + `"}`,
	} {
		if code, _ := put(bad); code != 400 {
			t.Fatalf("%.60s: %d, want 400", bad, code)
		}
	}
	if code, _, _ := do(t, hd, "GET", "/api/draft?key=bad", "", nil); code != 400 {
		t.Fatalf("bad get key: %d", code)
	}
	// Writing needs the dashboard's same-origin headers.
	if code, _, _ := do(t, hd, "POST", "/api/draft", `{"key":"reply:t-abcd","draft":"x"}`, map[string]string{"Content-Type": "application/json"}); code != 403 {
		t.Fatalf("cross-site save: %d", code)
	}
}

// The section order is shared by every device: checked names, read back,
// and null puts back the default.
func TestDashboardSections(t *testing.T) {
	_, hd := testServer(t, newHarness(t))
	if code, body, _ := do(t, hd, "GET", "/api/dashboard-prefs", "", nil); code != 200 || strings.TrimSpace(body) != "{}" {
		t.Fatalf("nothing set: %d %s", code, body)
	}
	for _, bad := range []string{
		`{"key":"sections","data":["needs","needs"]}`,
		`{"key":"sections","data":["needs","bogus"]}`,
		`{"key":"sections","data":"needs"}`,
		`{"key":"colour","data":"red"}`,
	} {
		if code, _, _ := do(t, hd, "POST", "/api/dashboard-prefs", bad, dashboard); code != 400 {
			t.Fatalf("%s: %d, want 400", bad, code)
		}
	}
	if code, body, _ := do(t, hd, "POST", "/api/dashboard-prefs", `{"key":"sections","data":["deliverables","needs","new"]}`, dashboard); code != 200 {
		t.Fatalf("set: %d %s", code, body)
	}
	if _, body, _ := do(t, hd, "GET", "/api/dashboard-prefs", "", nil); !strings.Contains(body, `"sections":["deliverables","needs","new"]`) {
		t.Fatalf("read back: %s", body)
	}
	do(t, hd, "POST", "/api/dashboard-prefs", `{"key":"sections","data":null}`, dashboard)
	if _, body, _ := do(t, hd, "GET", "/api/dashboard-prefs", "", nil); strings.TrimSpace(body) != "{}" {
		t.Fatalf("reset: %s", body)
	}
}
