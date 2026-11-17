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

	"github.com/NightMachinery/agent-desk/internal/settings"
)

// Config is the parsed file. Empty commands are not run.
type Config struct {
	NotifyCommand  string `toml:"notify_command"`
	DismissCommand string `toml:"dismiss_command"`
	OpenCommand    string `toml:"open_command"`
	DeliverCommand string `toml:"deliver_command"`
	// BaseURL is the web dashboard's address; a notification's url is an
	// item page under it.
	BaseURL string `toml:"base_url"`
	// AwayCommand exits 0 when the user is away from this machine, for
	// notify.remote = "away".
	AwayCommand string `toml:"away_command"`
	// RemoteLabel names the remote channel in the dashboard ("Phone").
	RemoteLabel string `toml:"remote_label"`
	// SSHHosts maps a desk host name to an ssh destination, for pushing
	// events there at once (merge ssh:).
	SSHHosts map[string]string `toml:"ssh_hosts"`
	// RawDefaults is the [defaults] table as written; Defaults is it checked.
	RawDefaults map[string]any    `toml:"defaults"`
	Defaults    settings.Defaults `toml:"-"`
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
	// The [defaults] tables are checked below, by defaults().
	for _, k := range md.Undecoded() {
		if len(k) == 0 || k[0] != "defaults" {
			return Config{}, fmt.Errorf("%s: unknown key %q", path, k.String())
		}
	}
	if c.Defaults, err = defaults(c.RawDefaults); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// defaults checks the [defaults] table: setting keys with string values, and
// [defaults.class.<class>] tables of the same.
func defaults(raw map[string]any) (settings.Defaults, error) {
	d := settings.Defaults{Any: map[string]string{}, Class: map[string]map[string]string{}}
	table := func(where, class string, t map[string]any, into map[string]string) error {
		for k, v := range t {
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("%s: %q must be a string", where, k)
			}
			if err := settings.Check(k, s); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
			if class != settings.Any {
				if _, err := settings.CheckClass(k, class); err != nil {
					return fmt.Errorf("%s: %w", where, err)
				}
			}
			into[k] = s
		}
		return nil
	}
	rest := map[string]any{}
	for k, v := range raw {
		if k != "class" {
			rest[k] = v
		}
	}
	if err := table("[defaults]", settings.Any, rest, d.Any); err != nil {
		return d, err
	}
	if cl, ok := raw["class"]; ok {
		classes, ok := cl.(map[string]any)
		if !ok {
			return d, fmt.Errorf("[defaults]: class must be tables, [defaults.class.<class>]")
		}
		for name, t := range classes {
			where := "[defaults.class." + name + "]"
			if _, err := settings.CheckClass("", name); err != nil || name == settings.Any {
				return d, fmt.Errorf("%s: no class %q; the classes are %v", where, name, settings.Classes)
			}
			tt, ok := t.(map[string]any)
			if !ok {
				return d, fmt.Errorf("%s must be a table", where)
			}
			d.Class[name] = map[string]string{}
			if err := table(where, name, tt, d.Class[name]); err != nil {
				return d, err
			}
		}
	}
	return d, nil
}

// Exists reports whether a config file is present.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
