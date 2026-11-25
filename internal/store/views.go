package store

import (
	"strconv"
	"strings"
	"time"
)

// Views count how often the dashboard has opened each item, on any device.
// Auto-navigation (n, and moving on after an answer) goes to the items
// opened least first, since some, such as an open question, stay listed
// after they are opened. Like drafts they are not events: every open would
// otherwise become one, and merge does not copy them to other desks.
const viewSchema = `
CREATE TABLE IF NOT EXISTS item_views (
  id   TEXT PRIMARY KEY,  -- the event's full id
  n    INTEGER NOT NULL,
  last INTEGER NOT NULL   -- unix milliseconds of the latest open
);
`

// AddView counts one open of the event id and returns its new count.
func (s *Store) AddView(id string, now time.Time) (int, error) {
	var n int
	err := s.DB.QueryRow(`INSERT INTO item_views (id, n, last) VALUES (?, 1, ?)
ON CONFLICT(id) DO UPDATE SET n = n + 1, last = excluded.last RETURNING n`, id, now.UnixMilli()).Scan(&n)
	return n, err
}

// Views returns the open counts of the given event ids; ids never opened
// are left out.
func (s *Store) Views(ids []string) (map[string]int, error) {
	out := map[string]int{}
	for len(ids) > 0 {
		chunk := ids[:min(len(ids), 500)]
		ids = ids[len(chunk):]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.DB.Query(`SELECT id, n FROM item_views WHERE id IN (?`+strings.Repeat(",?", len(chunk)-1)+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = n
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ViewsStamp changes with every counted open, for the page's stamp.
func (s *Store) ViewsStamp() (string, error) {
	var total int64
	err := s.DB.QueryRow(`SELECT coalesce(sum(n), 0) FROM item_views`).Scan(&total)
	return strconv.FormatInt(total, 10), err
}
