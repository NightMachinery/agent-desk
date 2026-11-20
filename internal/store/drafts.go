package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Drafts are what the user has typed in the dashboard and not sent, one per
// box ("reply:t-k2m9", "choices:q-7f3a"), kept on the desk so a draft
// started on one device is there on the next. They are not events: they
// change at every pause in typing, they go when sent, and merge does not
// copy them to other machines' desks.
const draftSchema = `
CREATE TABLE IF NOT EXISTS drafts (
  key     TEXT PRIMARY KEY,
  value   TEXT,              -- JSON; NULL once dropped, so other devices drop theirs
  updated INTEGER NOT NULL,  -- unix milliseconds on the desk's clock
  device  TEXT NOT NULL DEFAULT ''
);
`

// DraftKeep is how long a draft, or the mark that one was dropped, is kept
// after its last change.
const DraftKeep = 30 * 24 * time.Hour

// MaxDraft is the largest draft value accepted, in bytes of JSON.
const MaxDraft = 256 << 10

// Draft is one box's draft. Value is JSON null when there is none, either
// never saved (Updated 0) or dropped (sent or emptied) at Updated.
type Draft struct {
	Key     string          `json:"key"`
	Value   json.RawMessage `json:"value"`
	Updated int64           `json:"updated"`
	Device  string          `json:"device,omitempty"`
}

// Draft returns key's draft.
func (s *Store) Draft(key string) (Draft, error) {
	d := Draft{Key: key, Value: json.RawMessage("null")}
	var v sql.NullString
	err := s.DB.QueryRow(`SELECT value, updated, device FROM drafts WHERE key = ?`, key).Scan(&v, &d.Updated, &d.Device)
	if errors.Is(err, sql.ErrNoRows) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	if v.Valid {
		d.Value = json.RawMessage(v.String)
	}
	return d, nil
}

// PutDraft saves value as key's draft, or drops it when value is null or
// empty, from device at now. A key's Updated only grows, so every later
// save reads as newer to every device. Drafts and drop marks older than
// DraftKeep go.
func (s *Store) PutDraft(key string, value json.RawMessage, device string, now time.Time) (Draft, error) {
	var v any
	if len(value) > 0 && string(value) != "null" {
		if !json.Valid(value) {
			return Draft{}, errors.New("a draft must be JSON")
		}
		v = string(value)
	}
	ms := now.UnixMilli()
	d := Draft{Key: key, Value: json.RawMessage("null"), Device: device}
	if v != nil {
		d.Value = value
	}
	err := s.DB.QueryRow(`INSERT INTO drafts (key, value, updated, device) VALUES (?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, device = excluded.device,
  updated = max(excluded.updated, drafts.updated + 1)
RETURNING updated`, key, v, ms, device).Scan(&d.Updated)
	if err != nil {
		return Draft{}, err
	}
	_, err = s.DB.Exec(`DELETE FROM drafts WHERE updated < ?`, ms-DraftKeep.Milliseconds())
	return d, err
}
