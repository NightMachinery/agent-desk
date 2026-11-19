package render

import (
	"strings"
	"testing"
)

func TestLinkMentions(t *testing.T) {
	in := strings.Join([]string{
		"* ASK q-8tqk Which split?",
		":PROPERTIES:",
		":CUSTOM_ID: q-8tqk",
		":DESK_ID: q-8tqk",
		":END:",
		"  See q-8tqk, then run a t-test; m-zzzz is not here.",
		"  Code `q-8tqk` links, `agent-desk read q-8tqk` does not.",
		"  ```",
		"  q-8tqk in a block",
		"  ```",
		"  Already [[#q-8tqk][q-8tqk]].",
	}, "\n")
	out := string(LinkMentions([]byte(in)))
	want := strings.Join([]string{
		"* ASK q-8tqk Which split?",
		":PROPERTIES:",
		":CUSTOM_ID: q-8tqk",
		":DESK_ID: q-8tqk",
		":END:",
		"  See [[#q-8tqk][q-8tqk]], then run a t-test; m-zzzz is not here.",
		"  Code [[#q-8tqk][q-8tqk]] links, `agent-desk read q-8tqk` does not.",
		"  ```",
		"  q-8tqk in a block",
		"  ```",
		"  Already [[#q-8tqk][q-8tqk]].",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}
