package store

import (
	"strings"
	"testing"
)

func TestLocalFSAccepted(t *testing.T) {
	dir := t.TempDir()
	if err := checkLocalFS(dir); err != nil {
		t.Fatalf("a temporary directory was refused: %v", err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var mode string
	if err := s.DB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal mode %q, %v", mode, err)
	}
}

// A newer desk's events of unknown kinds are left out of an import and a
// merge, not fatal, and taken in once this version knows them.
func TestUnknownKindsSkipped(t *testing.T) {
	src, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	ev := func(id, kind string) Event {
		return Event{ID: id, TS: "2026-01-01T00:00:00Z", Host: "h", Kind: kind, Path: "p/r/c", Meta: []byte("{}")}
	}
	if _, err := src.DB.Exec(insertSQL, "01A", "2026-01-01T00:00:00Z", "h", "future", "p/r/c", nil, nil, nil, nil, "{}"); err != nil {
		t.Fatal(err)
	}
	if err := src.Append(ev("01B", "tell")); err != nil {
		t.Fatal(err)
	}
	dst, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	added, err := dst.Merge(src.Path)
	if err != nil || len(added) != 1 || dst.Skipped != 1 {
		t.Fatalf("merge: %v, %d added, %d skipped", err, len(added), dst.Skipped)
	}
	if all, _ := dst.All(); len(all) != 1 || all[0].Kind != "tell" {
		t.Fatalf("stored: %+v", all)
	}
	in := `{"id":"01C","ts":"2026-01-01T00:00:00Z","host":"h","kind":"future","path":"p/r/c","meta":{}}` + "\n"
	if added, err := dst.Import(strings.NewReader(in)); err != nil || len(added) != 0 || dst.Skipped != 2 {
		t.Fatalf("import: %v, %d added, %d skipped", err, len(added), dst.Skipped)
	}
}
