package cli

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/NightMachinery/agent-desk/internal/config"
)

// The user's keys file overlays the dashboard's built-in keymap. It lives
// next to config.toml (normally ~/.config/agent-desk/keys.edn), on the desk
// machine, so every device gets the same keys. The server only serves the
// text: the dashboard parses and checks it, since the commands it names are
// defined there (web/app/src/lib/keyfile.js, docs/keyboard.md).

// maxKeysFile bounds the file the dashboard is sent; it rejects larger ones too.
const maxKeysFile = 64 << 10

// keysTemplate is the file serve creates where there is none: comments that
// teach the format, and examples left commented out, so it changes nothing
// until edited. keyfile.test.js checks that it and its examples build.
//
//go:embed keys-template.edn
var keysTemplate string

func (a *App) keysPath() string {
	return filepath.Join(filepath.Dir(config.Path(a.Env.Getenv, a.Env.Home)), "keys.edn")
}

// ensureKeysFile creates the user's keys file from keysTemplate when there
// is none (serve does, at start), and never touches one that exists.
func (a *App) ensureKeysFile() (string, error) {
	p := a.keysPath()
	if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if _, err := f.WriteString(keysTemplate); err != nil {
		f.Close()
		return "", err
	}
	return p, f.Close()
}

// keysStamp names the file's version (its modification time and size), or
// "" when there is none, so the stream can say when it changed.
func (a *App) keysStamp() string {
	fi, err := os.Stat(a.keysPath())
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%d", fi.ModTime().UnixNano(), fi.Size())
}

// tilde writes p under the home directory as ~/..., for showing it.
func (a *App) tilde(p string) string {
	if h := a.Env.Home; h != "" {
		if rel, err := filepath.Rel(h, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return p
}

type keysResponse struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Text   string `json:"text,omitempty"`
	Stamp  string `json:"stamp,omitempty"`
	// Error says why an existing file could not be read; the dashboard
	// keeps the built-in keys and shows it.
	Error string `json:"error,omitempty"`
}

// apiKeys serves the user's keys file (GET api/keys).
func (s *server) apiKeys(w http.ResponseWriter, r *http.Request) {
	p := s.a.keysPath()
	resp := keysResponse{Path: s.a.tilde(p), Stamp: s.a.keysStamp()}
	fi, err := os.Stat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		resp.Exists, resp.Error = true, err.Error()
	case fi.IsDir():
		resp.Exists, resp.Error = true, "it is a directory"
	case fi.Size() > maxKeysFile:
		resp.Exists, resp.Error = true, fmt.Sprintf("it is larger than %d KiB", maxKeysFile>>10)
	default:
		resp.Exists = true
		b, err := os.ReadFile(p)
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Text = string(b)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, resp)
}
