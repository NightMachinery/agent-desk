package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/store"
)

func TestAskChoices(t *testing.T) {
	h := newHarness(t)
	a := h.as(sessA)
	h.ok(a, "", "join", "r1/coord")
	f := filepath.Join(h.cwd, "c.json")
	must(t, os.WriteFile(f, []byte(`{"questions": [
	  {"text": "Which split?", "options": ["Reuse the cached split", {"key": "B", "label": "Regenerate", "detail": "an hour longer"}], "recommended": "B"},
	  {"text": "Which seeds?", "options": ["1-3", "4-5", "all"], "multi": true, "recommended": "all"}]}`), 0o644))
	q := h.ok(a, "Two choices for the rerun.", "ask", "--blocked", "--title", "Rerun choices", "--choices", "c.json")
	out := h.ok(nil, "", "show", "--format", "md")
	for _, want := range []string{"1. Which split?  (pick one, or Other)", "   A. Reuse the cached split", "   B. Regenerate  (recommended)", "      an hour longer", "2. Which seeds?  (pick any, or Other)", "   C. all  (recommended)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show lacks %q:\n%s", want, out)
		}
	}
	app, _, _ := h.app(nil, "")
	d, err := app.load()
	must(t, err)
	e, err := d.Resolve(q)
	must(t, err)
	if got := TakeText(e); got != "1. B: Regenerate\n2. C: all" {
		t.Fatalf("take: %q", got)
	}

	bad := map[string]string{
		`{"questions": []}`: "1 to 30 questions",
		`{"questions": [{"text": "x", "options": ["only"]}]}`:                              "2 to 12 options",
		`{"questions": [{"text": "x", "options": ["a", "b"], "recommended": "z"}]}`:        "not one of its options",
		`{"questions": [{"text": "x", "options": ["a", {"key": "other", "label": "b"}]}]}`: "reserved",
		`{"qs": []}`: "the form is",
	}
	for in, want := range bad {
		if _, err := desk.ParseChoices([]byte(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v, want %q", in, err, want)
		}
	}
	_ = store.Event{}
}
