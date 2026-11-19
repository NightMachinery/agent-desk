package cli

import (
	"reflect"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// Ref is what the dashboard needs to link an id mention: the item's title,
// kind and state for the hover text, and its page (an itemHash).
type Ref struct {
	Title string `json:"title"`
	Kind  string `json:"kind"`
	State string `json:"state,omitempty"`
	URL   string `json:"url"`
}

// refsOf resolves the id mentions in texts: only exact display ids of
// events in the store are kept, so "t-test" never links.
func refsOf(d *desk.Desk, texts []string) map[string]Ref {
	var out map[string]Ref
	for _, m := range desk.Mentions(texts...) {
		e, ok := d.ByDisplay(m)
		if !ok {
			continue
		}
		if out == nil {
			out = map[string]Ref{}
		}
		out[m] = Ref{Title: refTitle(e), Kind: refKind(e), State: refState(d, e), URL: itemHash(d, e)}
	}
	return out
}

func refTitle(e store.Event) string {
	switch {
	case desk.IsSlotKind(e.Kind):
		return desk.SlotName(e)
	case e.Title != "":
		return e.Title
	}
	return clipLine(e.Body, 80)
}

func refKind(e store.Event) string {
	switch {
	case e.Kind == "ask":
		return "question"
	case desk.IsResponse(e):
		return "response"
	case e.Kind == "tell":
		return "notice"
	case e.Kind == "done":
		return "done report"
	case e.Kind == "reply":
		return "message"
	case desk.IsSlotKind(e.Kind):
		return "slot"
	}
	return e.Kind
}

func refState(d *desk.Desk, e store.Event) string {
	switch {
	case e.Kind == "ask" && !d.QuestionOpen(e.ID):
		return "answered"
	case e.Kind == "ask" && d.Dismissed(e.ID):
		return "open, dismissed"
	case e.Kind == "ask":
		return "open, " + e.Level
	case d.Unread(e.ID):
		return "unread"
	}
	return ""
}

// refText are the fields whose text the dashboard renders, and so may hold
// id mentions.
var refText = map[string]bool{"Title": true, "Body": true, "FileText": true, "Asked": true}

// textsOf collects the renderable text in a response: every exported
// string field named in refText, at any depth.
func textsOf(v any) []string {
	var out []string
	var walk func(reflect.Value, int)
	walk = func(v reflect.Value, depth int) {
		if depth > 32 {
			return
		}
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), depth+1)
			}
		case reflect.Struct:
			t := v.Type()
			for i := 0; i < v.NumField(); i++ {
				f := t.Field(i)
				if !f.IsExported() {
					continue
				}
				fv := v.Field(i)
				if fv.Kind() == reflect.String {
					if refText[f.Name] && fv.Len() > 0 {
						out = append(out, fv.String())
					}
					continue
				}
				walk(fv, depth+1)
			}
		case reflect.Slice, reflect.Array:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return // raw JSON or bytes
			}
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), depth+1)
			}
		case reflect.Map:
			it := v.MapRange()
			for it.Next() {
				walk(it.Value(), depth+1)
			}
		}
	}
	walk(reflect.ValueOf(v), 0)
	return out
}
