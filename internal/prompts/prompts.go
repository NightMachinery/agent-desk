// Package prompts holds the prompts agent-desk sends agents, as Go
// text/template files. A file of the same name in the user's prompts
// directory (~/.config/agent-desk/prompts/) replaces the built-in one.
package prompts

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
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
