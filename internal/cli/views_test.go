package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// Each open the dashboard counts (api/seen) shows in the view as the
// item's views, on every device, and changes the page's stamp so open
// pages reload; an unknown id is refused.
func TestSeenCountsViews(t *testing.T) {
	h := newHarness(t)
	const uuid = "22222222-0000-4000-8000-0000000000f1"
	writePaseoRecord(t, h.paseo, uuid, sessA, "", "App coordinator")
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA, "PASEO_AGENT_ID": uuid}
	h.ok(env, "", "join", "app/coord")
	qid := h.ok(env, "Which port?\n\nA: 8080. B: 9090. I recommend A.", "ask", "--blocked", "--title", "Port")
	s, hd := testServer(t, h)
	type item struct {
		ID    string `json:"display_id"`
		Views int    `json:"views"`
		Event struct {
			ID string `json:"id"`
		} `json:"event"`
	}
	question := func() (item, string) {
		t.Helper()
		_, body, _ := do(t, hd, "GET", "/api/view", "", nil)
		var v struct {
			Stamp string `json:"stamp"`
			View  struct {
				Needs []struct {
					Items []item `json:"items"`
				} `json:"needs_you"`
			} `json:"view"`
		}
		must(t, json.Unmarshal([]byte(body), &v))
		for _, g := range v.View.Needs {
			for _, it := range g.Items {
				if it.ID == qid {
					return it, v.Stamp
				}
			}
		}
		t.Fatalf("%s not in Needs you: %s", qid, body)
		return item{}, ""
	}
	q, stamp := question()
	if q.Views != 0 {
		t.Fatalf("views before any open: %d", q.Views)
	}
	page, err := s.pageStamp()
	must(t, err)
	if page != stamp {
		t.Fatalf("view stamp %q, page stamp %q", stamp, page)
	}
	for want := 1; want <= 2; want++ {
		code, body, _ := do(t, hd, "POST", "/api/seen", `{"id":"`+q.Event.ID+`"}`, dashboard)
		if code != 200 || !strings.Contains(body, `"views":`+string(rune('0'+want))) {
			t.Fatalf("seen %d: %d %s", want, code, body)
		}
	}
	again, stamp2 := question()
	if again.Views != 2 || stamp2 == stamp {
		t.Fatalf("after two opens: views %d, stamp %q then %q", again.Views, stamp, stamp2)
	}
	if code, _, _ := do(t, hd, "POST", "/api/seen", `{"id":"01NOSUCHEVENT0000000000000"}`, dashboard); code != 400 {
		t.Fatalf("unknown id: %d, want 400", code)
	}
	if code, _, _ := do(t, hd, "POST", "/api/seen", `{"id":"`+q.Event.ID+`"}`, map[string]string{"Content-Type": "application/json"}); code != 403 {
		t.Fatalf("cross-site count: %d, want 403", code)
	}
}

// Auto-navigation's order is a shared setting: every kind once, age old or
// new.
func TestDashboardNav(t *testing.T) {
	_, hd := testServer(t, newHarness(t))
	for _, bad := range []string{
		`{"key":"nav","data":{"views":true,"kinds":["blocked","default","deliverable","response"],"age":"old"}}`,
		`{"key":"nav","data":{"views":true,"kinds":["blocked","blocked","default","deliverable","response","notice"],"age":"old"}}`,
		`{"key":"nav","data":{"views":true,"kinds":["blocked","default","deliverable","response","bogus"],"age":"old"}}`,
		`{"key":"nav","data":{"views":true,"kinds":["blocked","default","deliverable","response","notice"],"age":"soon"}}`,
		`{"key":"nav","data":"old"}`,
	} {
		if code, _, _ := do(t, hd, "POST", "/api/dashboard-prefs", bad, dashboard); code != 400 {
			t.Fatalf("%s: %d, want 400", bad, code)
		}
	}
	good := `{"views":false,"kinds":["deliverable","blocked","default","response","notice"],"age":"new"}`
	if code, body, _ := do(t, hd, "POST", "/api/dashboard-prefs", `{"key":"nav","data":`+good+`}`, dashboard); code != 200 {
		t.Fatalf("set: %d %s", code, body)
	}
	if _, body, _ := do(t, hd, "GET", "/api/dashboard-prefs", "", nil); !strings.Contains(body, `"nav":`+good) {
		t.Fatalf("read back: %s", body)
	}
}
