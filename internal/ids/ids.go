// Package ids makes event ids (ULIDs) and the short display ids shown to
// people, and resolves what a person types back to an event.
package ids

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	mu      sync.Mutex
	entropy = ulid.Monotonic(rand.Reader, 0)
)

// New returns a fresh ULID for time t.
func New(t time.Time) string {
	mu.Lock()
	defer mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(t), entropy).String()
}

// Letter is the display letter of an event kind.
func Letter(kind string) string {
	switch kind {
	case "ask":
		return "q"
	case "answer":
		return "a"
	case "tell":
		return "t"
	case "done":
		return "d"
	case "set":
		return "s"
	case "clear":
		return "c"
	case "link":
		return "l"
	case "join":
		return "j"
	case "read":
		return "r"
	case "delivered":
		return "v"
	case "reply":
		return "m"
	case "pin":
		return "k"
	case "pref":
		return "p"
	case "notified":
		return "n"
	case "retracted":
		return "x"
	}
	return "e"
}

// MinSuffix is the shortest display suffix.
const MinSuffix = 4

// randomLen is the length of a ULID's random part (after 10 time characters).
const randomLen = 16

// Table maps event ids to their display suffix: the shortest suffix of the
// ULID's random part that no other id shares, at least MinSuffix long.
type Table struct {
	suffix map[string]string
	ids    []string
}

func reverse(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// NewTable builds the display table for a set of ids.
func NewTable(all []string) *Table {
	rev := make([]string, len(all))
	for i, id := range all {
		rev[i] = reverse(id)
	}
	sort.Strings(rev)
	t := &Table{suffix: make(map[string]string, len(all)), ids: append([]string(nil), all...)}
	for i, r := range rev {
		need := 0
		if i > 0 {
			need = max(need, commonPrefix(r, rev[i-1]))
		}
		if i+1 < len(rev) {
			need = max(need, commonPrefix(r, rev[i+1]))
		}
		n := min(max(MinSuffix, need+1), randomLen)
		id := reverse(r)
		t.suffix[id] = strings.ToLower(id[len(id)-n:])
	}
	return t
}

// Display returns the display id of an event, such as q-7f3a.
func (t *Table) Display(id, kind string) string {
	s, ok := t.suffix[id]
	if !ok {
		s = strings.ToLower(id[len(id)-MinSuffix:])
	}
	return Letter(kind) + "-" + s
}

// Resolve finds the one event a typed token names. A token is a display id
// (q-7f3a), its suffix alone (7f3a), any unique prefix of a display suffix
// (q-7f), or a full ULID or a prefix of one. kindOf gives an id's kind, used
// to check the letter when one is typed.
func (t *Table) Resolve(token string, kindOf func(id string) string) (string, error) {
	tok := strings.TrimSpace(token)
	letter := ""
	if len(tok) > 2 && tok[1] == '-' {
		letter, tok = strings.ToLower(tok[:1]), tok[2:]
	}
	low := strings.ToLower(tok)
	up := strings.ToUpper(tok)
	if low == "" {
		return "", fmt.Errorf("empty id")
	}
	var hits []string
	for _, id := range t.ids {
		if letter != "" && Letter(kindOf(id)) != letter {
			continue
		}
		suf := t.suffix[id]
		if strings.HasPrefix(suf, low) || strings.HasSuffix(id, up) ||
			(len(up) >= 10 && strings.HasPrefix(id, up)) {
			hits = append(hits, id)
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("no event matches %q", token)
	case 1:
		return hits[0], nil
	}
	var names []string
	for _, h := range hits {
		names = append(names, t.Display(h, kindOf(h)))
		if len(names) == 8 {
			names = append(names, "...")
			break
		}
	}
	return "", fmt.Errorf("%q matches %d events: %s", token, len(hits), strings.Join(names, ", "))
}
