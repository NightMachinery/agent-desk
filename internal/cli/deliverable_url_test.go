package cli

import (
	"strings"
	"testing"

	"github.com/NightMachinery/agent-desk/internal/desk"
)

// OpenableURL takes http(s) with a host and app links; it refuses the
// schemes the dashboard's Markdown renderer refuses, and anything relative.
func TestOpenableURL(t *testing.T) {
	for _, c := range []struct {
		url string
		ok  bool
	}{
		{"http://example.org/", true},
		{"https://staging.example.org/app.apk", true},
		{"sioyek://open?path=~/x.pdf&page=3", true},
		{"myapp:open?x=1", true},
		{"https:///no-host", false},
		{"javascript:alert(1)", false},
		{"JavaScript:alert(1)", false},
		{"vbscript:msgbox(1)", false},
		{"file:///etc/hosts", false},
		{"data:text/html,<b>x</b>", false},
		{"docs/x.pdf", false},
		{"/abs/path.pdf", false},
		{"://open?x=1", false},
		{"", false},
	} {
		if got := desk.OpenableURL(c.url); got != c.ok {
			t.Errorf("OpenableURL(%q) = %v, want %v", c.url, got, c.ok)
		}
	}
}

// An app link is filed as given and rendered in org as a bracket link, so
// Emacs follows it with the link type's handler.
func TestDeliverableAppLink(t *testing.T) {
	h := newHarness(t)
	env := map[string]string{"AI_AGENT": "claude-code", "CLAUDE_CODE_SESSION_ID": sessA}
	h.ok(env, "", "join", "app/coord")
	h.fails(env, "", "app link", "deliverable", "--title", "Relative", "--url", "docs/x.pdf")
	h.fails(env, "", "app link", "deliverable", "--title", "File", "--url", "file:///tmp/x.pdf")
	const app = "myapp://open?path=~/x.pdf&page=3"
	h.ok(env, "", "deliverable", "--title", "Paper draft", "--url", app)
	h.ok(env, "", "deliverable", "--title", "Site", "--url", "https://staging.example.org/")
	org := h.ok(nil, "", "show", "--format", "org")
	for _, want := range []string{"Open: [[" + app + "][Open]]", "Open: [[https://staging.example.org/][Open]]"} {
		if !strings.Contains(org, want) {
			t.Fatalf("org lacks %q:\n%s", want, org)
		}
	}
	if md := h.ok(nil, "", "show", "--format", "md"); !strings.Contains(md, "Open: "+app) {
		t.Fatalf("md:\n%s", md)
	}
}
