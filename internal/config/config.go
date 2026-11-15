// Package config reads ~/.config/agent-desk/config.toml, which names the
// machine-specific commands the core runs: notifications, opening items and
// delivering answers. Each command runs under sh with the event as JSON on
// stdin.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the parsed file. Empty commands are not run.
type Config struct {
	NotifyCommand  string `toml:"notify_command"`
	DismissCommand string `toml:"dismiss_command"`
	OpenCommand    string `toml:"open_command"`
	DeliverCommand string `toml:"deliver_command"`
}

// Path is ${AGENT_DESK_CONFIG}, else ${XDG_CONFIG_HOME:-~/.config}/agent-desk/config.toml.
func Path(getenv func(string) string, home string) string {
	if p := getenv("AGENT_DESK_CONFIG"); p != "" {
		return p
	}
	base := getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "agent-desk", "config.toml")
}

// Load reads the file; a missing file is an empty config.
func Load(path string) (Config, error) {
	var c Config
	md, err := toml.DecodeFile(path, &c)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return Config{}, fmt.Errorf("%s: unknown key %q", path, und[0].String())
	}
	return c, nil
}

// Exists reports whether a config file is present.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
