// Package prompts holds the prompts agent-desk sends agents, as Go
// text/template files. A file of the same name in the user's prompts
// directory (~/.config/agent-desk/prompts/) replaces the built-in one.
package prompts

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed *.md
var builtin embed.FS

// Render fills the named prompt with data. dir is the user's prompts
// directory; an empty dir uses only the built-in prompts.
func Render(dir, name string, data any) (string, error) {
	src, err := builtin.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("no prompt %s: %w", name, err)
	}
	from := "built-in " + name
	if dir != "" {
		p := filepath.Join(dir, name)
		if b, err := os.ReadFile(p); err == nil {
			src, from = b, p
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	t, err := template.New(name).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return "", fmt.Errorf("%s: %w", from, err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("%s: %w", from, err)
	}
	return strings.TrimSpace(b.String()), nil
}

// Names are the built-in prompts, sorted.
func Names() []string {
	entries, _ := builtin.ReadDir(".")
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// Known reports whether name is a built-in prompt.
func Known(name string) bool {
	_, err := builtin.ReadFile(name)
	return err == nil && !strings.ContainsAny(name, `/\`)
}

// Source is one prompt as the user sees it: the built-in text, and the
// user's copy in dir when there is one.
type Source struct {
	Name     string `json:"name"`
	Builtin  string `json:"builtin"`
	Override string `json:"override,omitempty"`
	// Path is where the user's copy is, or would be.
	Path       string `json:"path"`
	Overridden bool   `json:"overridden"`
}

// Read returns the named prompt's built-in text and the user's copy.
func Read(dir, name string) (Source, error) {
	if !Known(name) {
		return Source{}, fmt.Errorf("no prompt %s", name)
	}
	b, _ := builtin.ReadFile(name)
	s := Source{Name: name, Builtin: string(b), Path: filepath.Join(dir, name)}
	if dir == "" {
		return s, nil
	}
	o, err := os.ReadFile(s.Path)
	switch {
	case err == nil:
		s.Override, s.Overridden = string(o), true
	case !errors.Is(err, fs.ErrNotExist):
		return s, err
	}
	return s, nil
}

// MaxBytes caps a prompt the user saves.
const MaxBytes = 64 << 10

// Save writes the user's copy of the named prompt into dir, after checking
// that it parses, and that it fills with sample when sample is not nil.
func Save(dir, name, text string, sample any) error {
	if !Known(name) {
		return fmt.Errorf("no prompt %s", name)
	}
	if dir == "" {
		return errors.New("no prompts directory")
	}
	if len(text) > MaxBytes {
		return fmt.Errorf("a prompt takes at most %d KiB", MaxBytes>>10)
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("the prompt is empty; to use the built-in one, reset it")
	}
	t, err := template.New(name).Option("missingkey=error").Parse(text)
	if err != nil {
		return err
	}
	if sample != nil {
		if err := t.Execute(io.Discard, sample); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+name+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// Reset removes the user's copy of the named prompt, so the built-in one
// is used again.
func Reset(dir, name string) error {
	if !Known(name) {
		return fmt.Errorf("no prompt %s", name)
	}
	if dir == "" {
		return nil
	}
	err := os.Remove(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
