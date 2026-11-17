package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// A read notice the page still holds stays in New when named in linger.
func TestViewLinger(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	tl := h.ok(a, "x", "tell", "--title", "Read me")
	h.ok(nil, "", "read", tl)
	_, hd := testServer(t, h)
	newIDs := func(q string) string {
		_, body, _ := do(t, hd, "GET", "/api/view"+q, "", nil)
		var v struct {
			View struct {
				New []struct {
					Items []struct {
						ID    string `json:"display_id"`
						Event struct {
							ID string `json:"id"`
						} `json:"event"`
					} `json:"items"`
				} `json:"new"`
			} `json:"view"`
		}
		must(t, json.Unmarshal([]byte(body), &v))
		var out []string
		for _, g := range v.View.New {
			for _, it := range g.Items {
				out = append(out, it.ID+"="+it.Event.ID)
			}
		}
		return strings.Join(out, ",")
	}
	if got := newIDs(""); got != "" {
		t.Fatalf("read notice listed: %q", got)
	}
	d, err := func() (string, error) {
		app, _, _ := h.app(nil, "")
		dd, err := app.load()
		if err != nil {
			return "", err
		}
		e, err := dd.Resolve(tl)
		return e.ID, err
	}()
	must(t, err)
	if got := newIDs("?linger=" + d); !strings.HasPrefix(got, tl+"=") {
		t.Fatalf("lingering notice not listed: %q", got)
	}
}
