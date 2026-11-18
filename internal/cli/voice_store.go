package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/store"
)

const (
	voiceRecording    = "recording"
	voiceTranscribing = "transcribing"
	voiceReady        = "ready"
	voiceFailed       = "failed"
	voiceDeleted      = "deleted"
)

var voiceID = regexp.MustCompile(`^v-[a-z0-9]{12}$`)
var voiceToken = regexp.MustCompile(`\[voice (v-[a-z0-9]{12}) [0-9]+:[0-5][0-9], (?:recording|transcribing|failed)\]`)

type recording struct {
	ID       string    `json:"id"`
	Node     string    `json:"node"`
	Created  time.Time `json:"created"`
	Backend  string    `json:"backend"`
	Status   string    `json:"status"`
	Text     string    `json:"text,omitempty"`
	Partial  string    `json:"partial,omitempty"`
	Error    string    `json:"error,omitempty"`
	Bytes    int64     `json:"bytes"`
	Audio    string    `json:"audio,omitempty"`
	Messages []string  `json:"messages,omitempty"`
}

func voiceDir(home string) string      { return filepath.Join(home, "media", "recordings") }
func voiceFile(home, id string) string { return filepath.Join(voiceDir(home), id+".wav") }

func (a *App) appendRecording(st *store.Store, rec recording) error {
	e := a.event("recording", rec.Node, ident.From{}, "web", nil)
	e.Ref, e.Level, e.Body = rec.ID, rec.Status, rec.Text
	rec.ID, rec.Node, rec.Status, rec.Text, rec.Partial, rec.Audio = "", "", "", "", "", ""
	e.Meta, _ = json.Marshal(rec)
	return st.Append(e)
}

func readRecording(e store.Event) recording {
	var rec recording
	_ = json.Unmarshal(e.Meta, &rec)
	rec.ID, rec.Node, rec.Status, rec.Text = e.Ref, e.Path, e.Level, e.Body
	return rec
}

func loadRecording(st *store.Store, id string) (recording, error) {
	events, err := st.Query("events", "kind = 'recording' AND ref = ?", id)
	if err != nil {
		return recording{}, err
	}
	if len(events) == 0 {
		return recording{}, os.ErrNotExist
	}
	for _, e := range events {
		if e.Level == voiceDeleted {
			return readRecording(e), nil
		}
	}
	return readRecording(events[len(events)-1]), nil
}

func listRecordings(st *store.Store, node, host string) ([]recording, error) {
	where := "kind = 'recording' AND host = ?"
	args := []any{host}
	if node != "" {
		where += " AND path = ?"
		args = append(args, node)
	}
	events, err := st.Query("events", where, args...)
	if err != nil {
		return nil, err
	}
	records := []recording{}
	seen := make(map[string]bool)
	for _, e := range events {
		if e.Level == voiceDeleted {
			seen[e.Ref] = true
		}
	}
	for _, e := range slices.Backward(events) {
		if seen[e.Ref] || !voiceID.MatchString(e.Ref) {
			continue
		}
		seen[e.Ref] = true
		rec := readRecording(e)
		if rec.Status != voiceDeleted {
			records = append(records, rec)
		}
	}
	slices.SortStableFunc(records, func(a, b recording) int { return b.Created.Compare(a.Created) })
	return records, nil
}

func voiceBlock(text string) string {
	fenceLen, run := 3, 0
	for _, char := range text {
		if char == '`' {
			run++
			fenceLen = max(fenceLen, run+1)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", fenceLen)
	return fence + "speech-to-text\n" + strings.TrimSpace(text) + "\n" + fence
}

// resolveVoiceTokens resolves the immutable queue body before a transport
// attempt starts. A failure takes precedence over a still-running recording.
func resolveVoiceTokens(st *store.Store, node, text string, omitted []string) (string, []string, error) {
	var result strings.Builder
	var included []string
	var waiting *voiceBlocked
	last := 0
	for _, m := range voiceToken.FindAllStringSubmatchIndex(text, -1) {
		id := text[m[2]:m[3]]
		if slices.Contains(omitted, id) {
			result.WriteString(text[last:m[0]])
			result.WriteString("(Voice recording omitted.)")
			last = m[1]
			continue
		}
		rec, err := loadRecording(st, id)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", nil, err
		}
		if errors.Is(err, os.ErrNotExist) || rec.Node != node || rec.Status == voiceDeleted {
			return "", nil, &voiceBlocked{id: id, reason: "Recording is unavailable", failed: true}
		}
		if rec.Status == voiceFailed {
			return "", nil, &voiceBlocked{id: id, reason: rec.Error, failed: true, retryable: rec.Bytes > 0}
		}
		if rec.Status != voiceReady {
			if waiting == nil {
				waiting = &voiceBlocked{id: id, reason: "waiting for transcription"}
			}
			continue
		}
		if !slices.Contains(included, id) {
			included = append(included, id)
		}
		result.WriteString(text[last:m[0]])
		result.WriteString("\n\n" + voiceBlock(rec.Text) + "\n\n")
		last = m[1]
	}
	if waiting != nil {
		return "", nil, waiting
	}
	result.WriteString(text[last:])
	return result.String(), included, nil
}

type voiceBlocked struct {
	id, reason        string
	failed, retryable bool
}

func (e *voiceBlocked) Error() string { return "voice " + e.id + ": " + e.reason }

func voiceReferences(e store.Event) []string {
	var m struct {
		Recordings []string `json:"voice_recordings"`
	}
	_ = json.Unmarshal(e.Meta, &m)
	return m.Recordings
}

func (a *App) gcVoice(days int, dry bool) (int, int64, error) {
	if days == 0 {
		return 0, 0, nil
	}
	st, err := a.store()
	if err != nil {
		return 0, 0, err
	}
	records, err := listRecordings(st, "", a.Host)
	if err != nil {
		return 0, 0, err
	}
	cutoff := a.Now().AddDate(0, 0, -days)
	n, freed := 0, int64(0)
	for _, rec := range records {
		if rec.Created.After(cutoff) {
			continue
		}
		p := voiceFile(a.Home, rec.ID)
		info, err := os.Stat(p)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return n, freed, err
		}
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		fmt.Fprintf(a.Stderr, "agent-desk: gc %s (recorded %s ago)\n", rec.ID, desk.Age(a.Now().Sub(rec.Created)))
		if !dry {
			rec.Status, rec.Text, rec.Error = voiceDeleted, "", ""
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				return n, freed, err
			}
			if err := a.appendRecording(st, rec); err != nil {
				return n, freed, err
			}
		}
		n, freed = n+1, freed+size
	}
	return n, freed, nil
}
