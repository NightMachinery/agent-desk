package desk

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/store"
)

// Choices are the multiple-choice questions a question carries (ask
// --choices FILE). The user picks options in each, may add a note to a pick,
// or writes their own answer instead; the answer is delivered as text, so
// any agent can read it.
type Choices struct {
	Questions []ChoiceQ `json:"questions"`
}

// ChoiceQ is one question of a set.
type ChoiceQ struct {
	Text        string   `json:"text"`
	Options     []Option `json:"options"`
	Recommended string   `json:"recommended,omitempty"` // an option's key
	Multi       bool     `json:"multi,omitempty"`       // several options may be picked
}

// Option is one choice. Key is what the answer names it by (A, B, ...).
type Option struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// UnmarshalJSON takes an option as an object or as a bare label.
func (o *Option) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*o = Option{Label: s}
		return nil
	}
	type plain Option
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*o = Option(p)
	return nil
}

// Limits on a set of choices.
const (
	MaxChoiceQuestions = 30
	MaxChoiceOptions   = 12
	maxChoiceText      = 4000
)

// ParseChoices reads and checks a set, giving each option without a key the
// next letter, and accepting a recommendation by key or by label.
func ParseChoices(b []byte) (Choices, error) {
	var c Choices
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("choices: %w; the form is "+
			`{"questions": [{"text": "...", "options": ["...", {"key": "B", "label": "...", "detail": "..."}], "recommended": "B", "multi": false}]}`, err)
	}
	if len(c.Questions) == 0 || len(c.Questions) > MaxChoiceQuestions {
		return c, fmt.Errorf("choices: give 1 to %d questions, not %d", MaxChoiceQuestions, len(c.Questions))
	}
	for i := range c.Questions {
		q := &c.Questions[i]
		q.Text = strings.TrimSpace(q.Text)
		if q.Text == "" || len(q.Text) > maxChoiceText {
			return c, fmt.Errorf("choices: question %d needs a text of at most %d bytes", i+1, maxChoiceText)
		}
		if len(q.Options) < 2 || len(q.Options) > MaxChoiceOptions {
			return c, fmt.Errorf("choices: question %d needs 2 to %d options", i+1, MaxChoiceOptions)
		}
		seen := map[string]bool{}
		for j := range q.Options {
			o := &q.Options[j]
			o.Label, o.Key = strings.TrimSpace(o.Label), strings.TrimSpace(o.Key)
			if o.Label == "" || len(o.Label)+len(o.Detail) > maxChoiceText {
				return c, fmt.Errorf("choices: question %d, option %d needs a label", i+1, j+1)
			}
			if o.Key == "" {
				o.Key = string(rune('A' + j))
			}
			if seen[strings.ToLower(o.Key)] || strings.EqualFold(o.Key, "other") {
				return c, fmt.Errorf("choices: question %d: option key %q is repeated or reserved", i+1, o.Key)
			}
			seen[strings.ToLower(o.Key)] = true
		}
		if r := strings.TrimSpace(q.Recommended); r != "" {
			q.Recommended = ""
			for _, o := range q.Options {
				if strings.EqualFold(o.Key, r) || strings.EqualFold(o.Label, r) {
					q.Recommended = o.Key
				}
			}
			if q.Recommended == "" {
				return c, fmt.Errorf("choices: question %d recommends %q, which is not one of its options", i+1, r)
			}
		}
	}
	return c, nil
}

// ChoicesOf returns the set an event carries, if any.
func ChoicesOf(e store.Event) (Choices, bool) {
	var m struct {
		Choices *Choices `json:"choices"`
	}
	if json.Unmarshal(e.Meta, &m) != nil || m.Choices == nil || len(m.Choices.Questions) == 0 {
		return Choices{}, false
	}
	return *m.Choices, true
}

// Text lists the set for a terminal or an org file.
func (c Choices) Text() string {
	var b strings.Builder
	for i, q := range c.Questions {
		if i > 0 {
			b.WriteString("\n")
		}
		pick := "one"
		if q.Multi {
			pick = "any"
		}
		if strings.Contains(q.Text, "\n") {
			fmt.Fprintf(&b, "%d. %s\n   (pick %s, or Other)\n", i+1, q.Text, pick)
		} else {
			fmt.Fprintf(&b, "%d. %s  (pick %s, or Other)\n", i+1, q.Text, pick)
		}
		for _, o := range q.Options {
			rec := ""
			if o.Key == q.Recommended {
				rec = "  (recommended)"
			}
			fmt.Fprintf(&b, "   %s. %s%s\n", o.Key, o.Label, rec)
			if o.Detail != "" {
				fmt.Fprintf(&b, "      %s\n", strings.ReplaceAll(strings.TrimSpace(o.Detail), "\n", "\n      "))
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Recommendations is the answer that takes every recommended option, and
// whether every question has one.
func (c Choices) Recommendations() (string, bool) {
	var lines []string
	for i, q := range c.Questions {
		if q.Recommended == "" {
			return "", false
		}
		for _, o := range q.Options {
			if o.Key == q.Recommended {
				lines = append(lines, fmt.Sprintf("%d. %s: %s", i+1, o.Key, o.Label))
			}
		}
	}
	return strings.Join(lines, "\n"), true
}
