package ids

import (
	"strings"
	"testing"
	"time"
)

func TestDisplayAndResolve(t *testing.T) {
	// Fabricated ids sharing a long suffix.
	all := []string{
		"01JAAAAAAAAAAAAAAAAAAA7F3A",
		"01JBBBBBBBBBBBBBBBBBBB7F3A",
		"01JCCCCCCCCCCCCCCCCCCCK2M9",
	}
	tb := NewTable(all)
	kind := func(id string) string {
		if strings.HasSuffix(id, "K2M9") {
			return "tell"
		}
		return "ask"
	}
	if got := tb.Display(all[2], "tell"); got != "t-k2m9" {
		t.Fatalf("display = %q", got)
	}
	if got := tb.Display(all[0], "ask"); got != "q-a7f3a" {
		t.Fatalf("display with a shared suffix = %q", got)
	}
	if id, err := tb.Resolve("t-k2m9", kind); err != nil || id != all[2] {
		t.Fatalf("resolve: %v %v", id, err)
	}
	if id, err := tb.Resolve("q-b7f", kind); err != nil || id != all[1] {
		t.Fatalf("resolve a prefix: %v %v", id, err)
	}
	if _, err := tb.Resolve("7f3a", kind); err == nil || !strings.Contains(err.Error(), "matches 2") {
		t.Fatalf("ambiguous: %v", err)
	}
	if _, err := tb.Resolve("q-k2m9", kind); err == nil {
		t.Fatal("the kind letter must match")
	}
	if id, err := tb.Resolve(strings.ToLower(all[0]), kind); err != nil || id != all[0] {
		t.Fatalf("full id: %v %v", id, err)
	}
}

func TestNewIsSortable(t *testing.T) {
	now := time.Now()
	a, b := New(now), New(now)
	if !(a < b) {
		t.Fatalf("ids in one millisecond out of order: %s %s", a, b)
	}
}
