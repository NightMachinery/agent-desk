package store

import (
	"encoding/json"
	"time"
)

// Dashboard settings shared by every device that opens this desk's
// dashboard, such as the order of the list's sections. Per-device ones
// stay in each browser.
const dashboardSchema = `
CREATE TABLE IF NOT EXISTS dashboard (
  key     TEXT PRIMARY KEY,
  value   TEXT NOT NULL,   -- JSON
  updated INTEGER NOT NULL -- unix milliseconds
);
`

// DashboardPrefs returns every shared dashboard setting by key.
func (s *Store) DashboardPrefs() (map[string]json.RawMessage, error) {
	rows, err := s.DB.Query(`SELECT key, value FROM dashboard`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = json.RawMessage(v)
	}
	return out, rows.Err()
}

// PutDashboardPref sets a shared dashboard setting, or removes it (back to
// the default) when value is null or empty.
func (s *Store) PutDashboardPref(key string, value json.RawMessage, now time.Time) error {
	if len(value) == 0 || string(value) == "null" {
		_, err := s.DB.Exec(`DELETE FROM dashboard WHERE key = ?`, key)
		return err
	}
	_, err := s.DB.Exec(`INSERT INTO dashboard (key, value, updated) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated = excluded.updated`, key, string(value), now.UnixMilli())
	return err
}
