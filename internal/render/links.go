package render

import (
	"regexp"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/desk"
)

var customID = regexp.MustCompile(`(?m)^:CUSTOM_ID: (\S+)$`)

// LinkMentions turns the id mentions in an org file's text into
// [[#q-8tqk][q-8tqk]] links to the heading with that CUSTOM_ID, for ids
// that have a heading in the same file (a link to another file's heading
// would not resolve). Headings, the title, property drawers and source blocks stay as
// they are; in a line, text inside `code` is left alone unless the span is
// exactly one id, and existing [[links]] are kept.
func LinkMentions(org []byte) []byte {
	targets := map[string]bool{}
	for _, m := range customID.FindAllSubmatch(org, -1) {
		targets[string(m[1])] = true
	}
	if len(targets) == 0 {
		return org
	}
	lines := strings.Split(string(org), "\n")
	inBlock, inDrawer := false, false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		low := strings.ToLower(t)
		switch {
		case strings.HasPrefix(low, "#+begin_"), strings.HasPrefix(t, "```"):
			inBlock = !(strings.HasPrefix(t, "```") && inBlock)
			continue
		case strings.HasPrefix(low, "#+end_"):
			inBlock = false
			continue
		case inBlock, strings.HasPrefix(line, "*"), strings.HasPrefix(low, "#+title:"):
			continue
		case t == ":PROPERTIES:":
			inDrawer = true
			continue
		case inDrawer:
			inDrawer = t != ":END:"
			continue
		}
		lines[i] = linkLine(line, targets)
	}
	return []byte(strings.Join(lines, "\n"))
}

// linkLine links the mentions in one line of text.
func linkLine(line string, targets map[string]bool) string {
	if !strings.Contains(line, "-") {
		return line
	}
	var b strings.Builder
	rest := line
	for rest != "" {
		// The next code span or link, whichever comes first.
		code, link := strings.Index(rest, "`"), strings.Index(rest, "[[")
		next, kind := -1, ""
		if code >= 0 && (link < 0 || code < link) {
			next, kind = code, "code"
		} else if link >= 0 {
			next, kind = link, "link"
		}
		if next < 0 {
			b.WriteString(linkPlain(rest, targets))
			break
		}
		b.WriteString(linkPlain(rest[:next], targets))
		rest = rest[next:]
		closer := "`"
		if kind == "link" {
			closer = "]]"
		}
		end := strings.Index(rest[len(closer):], closer)
		if kind == "link" {
			end = strings.Index(rest, "]]")
		}
		if end < 0 {
			b.WriteString(rest)
			break
		}
		if kind == "code" {
			span := rest[1 : 1+end]
			if targets[span] {
				b.WriteString("[[#" + span + "][" + span + "]]")
			} else {
				b.WriteString(rest[:end+2])
			}
			rest = rest[end+2:]
			continue
		}
		b.WriteString(rest[:end+2])
		rest = rest[end+2:]
	}
	return b.String()
}

func linkPlain(s string, targets map[string]bool) string {
	return desk.MentionRE().ReplaceAllStringFunc(s, func(m string) string {
		if targets[m] {
			return "[[#" + m + "][" + m + "]]"
		}
		return m
	})
}
