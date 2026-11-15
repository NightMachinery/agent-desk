package store

import "testing"

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
