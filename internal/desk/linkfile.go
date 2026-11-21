package desk

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"github.com/NightMachinery/agent-desk/internal/media"
)

// LinkTextLimit is how much of a linked file is shown in a view: a link
// slot's file can be anything, an APK as easily as a log, and every write
// re-renders the boards that show it.
const LinkTextLimit = 64 << 10

// LinkText is what a view shows of the file a link slot names: its text up
// to LinkTextLimit, with a note on what was left out; a note alone for a
// binary file; why not, when it cannot be read. Only the shown part is
// read.
func LinkText(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "(cannot read: " + err.Error() + ")"
	}
	defer f.Close()
	var size int64 = -1
	if st, err := f.Stat(); err == nil {
		size = st.Size()
	}
	b, err := io.ReadAll(io.LimitReader(f, LinkTextLimit+1))
	if err != nil {
		return "(cannot read: " + err.Error() + ")"
	}
	more := len(b) > LinkTextLimit
	if more {
		b = b[:LinkTextLimit]
	}
	if binary(b) {
		if size >= 0 {
			return fmt.Sprintf("(a binary file of %s: not shown)", media.Size(size))
		}
		return "(a binary file: not shown)"
	}
	if !more {
		return string(b)
	}
	// Cut at a line end where there is one near, and never inside a
	// character.
	if i := bytes.LastIndexByte(b, '\n'); i > len(b)-4096 {
		b = b[:i+1]
	}
	b = trimCut(b)
	rest := "the rest is in the file"
	if size >= 0 {
		rest = fmt.Sprintf("%s more in the file", media.Size(size-int64(len(b))))
	}
	return fmt.Sprintf("%s\n… (%s)", bytes.TrimRight(b, "\n"), rest)
}

// binary reports whether b looks like anything but text: a NUL byte, or
// bytes that are not UTF-8, in its first 8 KB.
func binary(b []byte) bool {
	head := b[:min(len(b), 8<<10)]
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	if len(head) < len(b) {
		head = trimCut(head)
	}
	return !utf8.Valid(head)
}

// trimCut drops a character cut short at the end of b.
func trimCut(b []byte) []byte {
	for i := 1; i <= utf8.UTFMax && i <= len(b); i++ {
		if utf8.RuneStart(b[len(b)-i]) {
			if !utf8.FullRune(b[len(b)-i:]) {
				return b[:len(b)-i]
			}
			break
		}
	}
	return b
}
