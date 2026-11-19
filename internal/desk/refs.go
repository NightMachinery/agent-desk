package desk

import (
	"regexp"
	"strings"
	"sync"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// An id mention is a display id written in text ("see q-8tqk"). The shape
// alone does not make one ("t-test" fits it), so a candidate counts only
// when it is exactly the display id of an event (ByDisplay).
var mentionRE = regexp.MustCompile(`\b[a-z]-[0-9a-hjkmnp-tv-z]{4,}\b`)

// MentionRE matches candidate id mentions.
func MentionRE() *regexp.Regexp { return mentionRE }

// Mentions are the distinct candidate id mentions in texts, in order.
func Mentions(texts ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range texts {
		if !strings.Contains(t, "-") {
			continue
		}
		for _, m := range mentionRE.FindAllString(t, -1) {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// ByDisplay is the event whose display id is exactly id: never a prefix,
// suffix or partial match, unlike Resolve.
func (d *Desk) ByDisplay(id string) (store.Event, bool) {
	x := d.display
	if x == nil {
		x = &displayIndex{} // a Desk not made by Load
		d.display = x
	}
	x.once.Do(func() {
		x.ids = make(map[string]string, len(d.Events))
		for _, e := range d.Events {
			x.ids[d.Display(e.ID)] = e.ID
		}
	})
	full, ok := x.ids[id]
	if !ok {
		return store.Event{}, false
	}
	return d.ByID[full], true
}

type displayIndex struct {
	once sync.Once
	ids  map[string]string
}
