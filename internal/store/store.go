// Package store holds the append-only events table: opening it, appending to
// it, reading its views, and moving events between stores.
package store

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Event is one immutable row. Empty strings stand for SQL NULL in the
// nullable columns (ref, level, title, body).
type Event struct {
	ID    string          `json:"id"`
	TS    string          `json:"ts"`
	Host  string          `json:"host"`
	Kind  string          `json:"kind"`
	Path  string          `json:"path"`
	Ref   string          `json:"ref,omitempty"`
	Level string          `json:"level,omitempty"`
	Title string          `json:"title,omitempty"`
	Body  string          `json:"body,omitempty"`
	Meta  json.RawMessage `json:"meta"`
}

// Kinds lists every event kind the store accepts.
var Kinds = []string{"join", "ask", "answer", "delivered", "tell", "done", "set", "clear", "link", "read", "reply", "pin", "pref", "notified", "retracted", "instruction", "task", "taskstate", "screen"}

func validKind(k string) bool {
	for _, x := range Kinds {
		if x == k {
			return true
		}
	}
	return false
}

const schema = `
CREATE TABLE IF NOT EXISTS events (
  id    TEXT PRIMARY KEY,
  ts    TEXT NOT NULL,
  host  TEXT NOT NULL,
  kind  TEXT NOT NULL,
  path  TEXT NOT NULL,
  ref   TEXT,
  level TEXT,
  title TEXT,
  body  TEXT,
  meta  TEXT NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS events_kind ON events(kind);
CREATE INDEX IF NOT EXISTS events_path ON events(path);
CREATE INDEX IF NOT EXISTS events_ref  ON events(ref);

CREATE TRIGGER IF NOT EXISTS events_no_update BEFORE UPDATE ON events
BEGIN SELECT RAISE(ABORT, 'events are append-only: UPDATE is not allowed'); END;

CREATE TRIGGER IF NOT EXISTS events_no_delete BEFORE DELETE ON events
BEGIN SELECT RAISE(ABORT, 'events are append-only: DELETE is not allowed'); END;

-- The latest set, clear or link of each slot path.
CREATE VIEW IF NOT EXISTS slot_current AS
SELECT e.* FROM events e
WHERE e.kind IN ('set', 'clear', 'link')
  AND e.id = (SELECT max(s.id) FROM events s
              WHERE s.path = e.path AND s.kind IN ('set', 'clear', 'link'));

-- Every question with the answer that counts (the latest) and whether that
-- answer has been delivered.
CREATE VIEW IF NOT EXISTS question_state AS
SELECT q.*,
       a.id   AS answer_id,
       a.body AS answer_body,
       a.ts   AS answer_ts,
       (a.id IS NOT NULL AND EXISTS (SELECT 1 FROM events d
                                     WHERE d.kind = 'delivered' AND d.ref = a.id)) AS delivered
FROM events q
LEFT JOIN events a
  ON a.id = (SELECT max(x.id) FROM events x WHERE x.kind = 'answer' AND x.ref = q.id)
WHERE q.kind = 'ask';

-- The read marker that counts for each event: the latest one naming it.
-- A marker with meta.cancel set makes the event unread again.
CREATE VIEW IF NOT EXISTS read_current AS
SELECT r.ref AS ref, r.id AS read_id,
       coalesce(json_extract(r.meta, '$.cancel'), 0) AS cancel
FROM events r
WHERE r.kind = 'read'
  AND r.id = (SELECT max(r2.id) FROM events r2 WHERE r2.kind = 'read' AND r2.ref = r.ref);

`

// unreadView is recreated when the stored one differs (unreadMarker tells
// them apart): unread are notices, done reports and the current text of each
// slot other than status, until a read marker names that exact event. A slot
// written for agents only (meta.audience "agents") is never unread.
const unreadMarker = "-- unread v2"

const unreadView = `CREATE VIEW unread AS ` + unreadMarker + `
SELECT e.* FROM events e
WHERE (e.kind IN ('tell', 'done')
       OR (e.kind = 'set'
           AND e.id IN (SELECT id FROM slot_current)
           AND e.path NOT LIKE '%/status'
           AND coalesce(json_extract(e.meta, '$.audience'), '') != 'agents'))
  AND NOT EXISTS (SELECT 1 FROM read_current rc WHERE rc.ref = e.id AND rc.cancel = 0)`

// Store is an open store.
type Store struct {
	DB   *sql.DB
	Path string
	// Skipped counts the events of unknown kinds Import and Merge left out.
	Skipped int
}

// DefaultHome is ${AGENT_DESK_HOME:-~/.agents/desk}.
func DefaultHome() (string, error) {
	if h := os.Getenv("AGENT_DESK_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agents", "desk"), nil
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	if readOnly {
		q.Add("mode", "ro")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(FULL)")
		q.Add("_txlock", "immediate")
	}
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
}

// Open opens (creating if needed) the store in directory home.
func Open(home string) (*Store, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	if err := checkLocalFS(home); err != nil {
		return nil, err
	}
	path := filepath.Join(home, "desk.db")
	db, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating schema in %s: %w", path, err)
	}
	var view string
	_ = db.QueryRow("SELECT coalesce(sql, '') FROM sqlite_master WHERE type = 'view' AND name = 'unread'").Scan(&view)
	if !strings.Contains(view, unreadMarker) {
		if _, err := db.Exec("DROP VIEW IF EXISTS unread; " + unreadView); err != nil {
			db.Close()
			return nil, fmt.Errorf("updating the unread view in %s: %w", path, err)
		}
	}
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		db.Close()
		return nil, err
	}
	if !strings.EqualFold(mode, "wal") {
		db.Close()
		return nil, fmt.Errorf("%s: journal mode is %q, not WAL; is this a network filesystem?", path, mode)
	}
	return &Store{DB: db, Path: path}, nil
}

// Close closes the store.
func (s *Store) Close() error { return s.DB.Close() }

func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}

const insertSQL = `INSERT INTO events (id, ts, host, kind, path, ref, level, title, body, meta)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// ErrUnknownKind marks an event of a kind this version does not know. Import
// and merge skip such events, counting them in Skipped, so an older binary
// still merges a newer desk; a later merge, after updating, takes them in.
var ErrUnknownKind = errors.New("unknown kind")

// Validate checks an event's required fields.
func Validate(e Event) error {
	// A pref's path is its scope, and the global scope is the empty path.
	if e.ID == "" || e.TS == "" || e.Host == "" || (e.Path == "" && e.Kind != "pref") {
		return fmt.Errorf("event %q: id, ts, host and path are required", e.ID)
	}
	if !validKind(e.Kind) {
		return fmt.Errorf("event %s: %w %q", e.ID, ErrUnknownKind, e.Kind)
	}
	if len(e.Meta) == 0 || !json.Valid(e.Meta) {
		return fmt.Errorf("event %s: meta must be JSON", e.ID)
	}
	return nil
}

// Append inserts events in one transaction.
func (s *Store) Append(events ...Event) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range events {
		if err := Validate(e); err != nil {
			return err
		}
		if _, err := tx.Exec(insertSQL, e.ID, e.TS, e.Host, e.Kind, e.Path,
			null(e.Ref), null(e.Level), null(e.Title), null(e.Body), string(e.Meta)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const cols = `id, ts, host, kind, path, coalesce(ref, ''), coalesce(level, ''),
coalesce(title, ''), coalesce(body, ''), meta`

func scanEvents(rows *sql.Rows) ([]Event, error) {
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var meta string
		if err := rows.Scan(&e.ID, &e.TS, &e.Host, &e.Kind, &e.Path, &e.Ref, &e.Level,
			&e.Title, &e.Body, &meta); err != nil {
			return nil, err
		}
		e.Meta = json.RawMessage(meta)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Query returns events from a table or view, with an optional WHERE clause.
func (s *Store) Query(from, where string, args ...any) ([]Event, error) {
	q := "SELECT " + cols + " FROM " + from
	if where != "" {
		q += " WHERE " + where
	}
	q += " ORDER BY id"
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

// All returns every event, oldest first.
func (s *Store) All() ([]Event, error) { return s.Query("events", "") }

// Get returns one event by its full id.
func (s *Store) Get(id string) (Event, error) {
	evs, err := s.Query("events", "id = ?", id)
	if err != nil {
		return Event{}, err
	}
	if len(evs) == 0 {
		return Event{}, fmt.Errorf("no event %s", id)
	}
	return evs[0], nil
}

// IDs returns every event id.
func (s *Store) IDs() ([]string, error) {
	rows, err := s.DB.Query("SELECT id FROM events ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Question is a row of question_state.
type Question struct {
	Event
	AnswerID   string `json:"answer_id,omitempty"`
	AnswerBody string `json:"answer_body,omitempty"`
	AnswerTS   string `json:"answer_ts,omitempty"`
	Delivered  bool   `json:"delivered"`
}

// Open reports whether the question has no answer.
func (q Question) Open() bool { return q.AnswerID == "" }

// Questions returns every question with its state.
func (s *Store) Questions() ([]Question, error) {
	rows, err := s.DB.Query("SELECT " + cols + `, coalesce(answer_id, ''), coalesce(answer_body, ''),
		coalesce(answer_ts, ''), delivered FROM question_state ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Question
	for rows.Next() {
		var q Question
		var meta string
		var delivered int64
		if err := rows.Scan(&q.ID, &q.TS, &q.Host, &q.Kind, &q.Path, &q.Ref, &q.Level,
			&q.Title, &q.Body, &meta, &q.AnswerID, &q.AnswerBody, &q.AnswerTS, &delivered); err != nil {
			return nil, err
		}
		q.Meta = json.RawMessage(meta)
		q.Delivered = delivered != 0
		out = append(out, q)
	}
	return out, rows.Err()
}

// Export writes every event as one JSON object per line.
func (s *Store) Export(w io.Writer) error {
	evs, err := s.All()
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	for _, e := range evs {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// Import inserts the events of a JSONL stream whose ids are new, in one
// transaction. It returns the events that were actually added.
func (s *Store) Import(r io.Reader) ([]Event, error) {
	var in []Event
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if err := Validate(e); errors.Is(err, ErrUnknownKind) {
			s.Skipped++
			continue
		} else if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		in = append(in, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var added []Event
	for _, e := range in {
		res, err := tx.Exec(strings.Replace(insertSQL, "INSERT", "INSERT OR IGNORE", 1),
			e.ID, e.TS, e.Host, e.Kind, e.Path, null(e.Ref), null(e.Level), null(e.Title),
			null(e.Body), string(e.Meta))
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added = append(added, e)
		}
	}
	return added, tx.Commit()
}

// Snapshot writes a consistent copy of the store to a new file.
func (s *Store) Snapshot(file string) error {
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err == nil {
		return fmt.Errorf("%s already exists", abs)
	}
	_, err = s.DB.Exec("VACUUM INTO ?", abs)
	return err
}

// Merge inserts every event of the store file `file` whose id is absent here,
// in one BEGIN IMMEDIATE transaction. It returns the events added.
func (s *Store) Merge(file string) ([]Event, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, err
	}
	ctx := context.Background()
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	uri := "file:" + (&url.URL{Path: abs}).EscapedPath() + "?mode=ro"
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS other", uri); err != nil {
		return nil, fmt.Errorf("opening %s: %w", abs, err)
	}
	defer conn.ExecContext(ctx, "DETACH DATABASE other")

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			conn.ExecContext(ctx, "ROLLBACK")
		}
	}()
	const plain = "id, ts, host, kind, path, ref, level, title, body, meta"
	rows, err := conn.QueryContext(ctx, "SELECT "+strings.ReplaceAll(cols, "\n", " ")+
		" FROM other.events WHERE id NOT IN (SELECT id FROM main.events) ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", abs, err)
	}
	added, err := scanEvents(rows)
	if err != nil {
		return nil, err
	}
	known := added[:0]
	for _, e := range added {
		if err := Validate(e); errors.Is(err, ErrUnknownKind) {
			s.Skipped++
			continue
		} else if err != nil {
			return nil, fmt.Errorf("%s: %w", abs, err)
		}
		known = append(known, e)
	}
	added = known
	if _, err := conn.ExecContext(ctx, "INSERT OR IGNORE INTO main.events ("+plain+") SELECT "+
		plain+" FROM other.events WHERE kind IN ('"+strings.Join(Kinds, "', '")+"')"); err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, err
	}
	ok = true
	return added, nil
}
