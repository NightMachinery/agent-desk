package desk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkText(t *testing.T) {
	dir := t.TempDir()
	put := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if got := LinkText(put("short.txt", []byte("hello\nworld\n"))); got != "hello\nworld\n" {
		t.Errorf("short text: %q", got)
	}

	// An APK, or any binary: a note with its size, never its bytes.
	bin := append([]byte("PK\x03\x04"), make([]byte, 3<<20)...)
	if got := LinkText(put("app.apk", bin)); !strings.HasPrefix(got, "(a binary file of ") || len(got) > 100 {
		t.Errorf("binary: %q", got)
	}

	// Long text is cut at a line end, never mid-character, with what is left.
	line := strings.Repeat("é", 50) + "\n"
	long := strings.Repeat(line, 2000)
	got := LinkText(put("long.log", []byte(long)))
	if len(got) > LinkTextLimit+200 {
		t.Errorf("long text: %d bytes shown", len(got))
	}
	body, note, ok := strings.Cut(got, "\n… (")
	if !ok || !strings.HasSuffix(note, "more in the file)") {
		t.Errorf("long text note: %q", got[max(0, len(got)-80):])
	}
	if !strings.HasPrefix(long, body+"\n") {
		t.Error("long text: not cut at a line end")
	}

	// Text with a character cut by the 8 KB sniff is still text.
	odd := strings.Repeat("a", 8<<10-1) + "é" + strings.Repeat("b", 100)
	if got := LinkText(put("odd.txt", []byte(odd))); got != odd {
		t.Errorf("cut character taken for binary: %q", got[:min(40, len(got))])
	}

	if got := LinkText(filepath.Join(dir, "missing")); !strings.HasPrefix(got, "(cannot read: ") {
		t.Errorf("missing: %q", got)
	}
}
