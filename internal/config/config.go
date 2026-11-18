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
	"regexp"

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
	// MediaKeepDays: attachments of items read or answered this many days
	// ago are deleted by gc (default 30; 0 keeps them for good).
	MediaKeepDays *int `toml:"media_keep_days"`
	// Tasks configures launching deferred tasks and the quota nudge ([tasks]).
	Tasks Tasks `toml:"tasks"`
	// Screen configures the screen lock, which an agent takes before it
	// drives this machine's GUI ([screen]).
	Screen Screen `toml:"screen"`
	// Push configures Web Push to the installed dashboard ([push]).
	Push Push `toml:"push"`
	// Quota configures the "Out of quota" section ([quota]).
	Quota Quota `toml:"quota"`
	// Instructions configures the prompt hook that records the user's
	// messages ([instructions]).
	Instructions Instructions `toml:"instructions"`
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

// Instructions is the [instructions] table.
type Instructions struct {
	// Record turns the hook off when false (default true).
	Record *bool `toml:"record"`
	// Skip are more regular expressions for prompts that are not the
	// user's, matched against the trimmed prompt.
	Skip []string `toml:"skip"`
}

// Off reports whether recording is turned off.
func (i Instructions) Off() bool { return i.Record != nil && !*i.Record }

// SkipPatterns compiles Skip.
func (i Instructions) SkipPatterns() ([]*regexp.Regexp, error) {
	var out []*regexp.Regexp
	for _, s := range i.Skip {
		re, err := regexp.Compile(s)
		if err != nil {
			return nil, fmt.Errorf("[instructions] skip %q: %w", s, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// Tasks is the [tasks] table.
type Tasks struct {
	// LaunchCommand starts an agent on a task: run under sh with the task
	// as JSON on stdin (its "prompt" is the rendered start-task prompt). Its
	// first line of output names the agent started.
	LaunchCommand string `toml:"launch_command"`
	// MaxLaunch caps one launch of several tasks (default 3).
	MaxLaunch *int `toml:"max_launch"`
	// QuotaCommand prints usage as {"gauges": {"<name>": {"pct": N,
	// "reset": <unix seconds>}}}, for the nudge.
	QuotaCommand string `toml:"quota_command"`
	// NudgeGauges matches the gauges worth spending before they reset
	// (default `\.7d$`, the weekly windows).
	NudgeGauges string `toml:"nudge_gauges"`
	// NudgeBelow: nudge while a gauge is at most this many percent used
	// (default 60).
	NudgeBelow *int `toml:"nudge_below"`
	// NudgeHours: nudge this many hours before the reset (default 24).
	NudgeHours *int `toml:"nudge_hours"`
}

func intOr(p *int, d int) int {
	if p == nil {
		return d
	}
	return *p
}

// Max is MaxLaunch or its default.
func (t Tasks) Max() int { return intOr(t.MaxLaunch, 3) }

// Below is NudgeBelow or its default.
func (t Tasks) Below() int { return intOr(t.NudgeBelow, 60) }

// Hours is NudgeHours or its default.
func (t Tasks) Hours() int { return intOr(t.NudgeHours, 24) }

// Gauges compiles NudgeGauges or its default.
func (t Tasks) Gauges() (*regexp.Regexp, error) {
	s := t.NudgeGauges
	if s == "" {
		s = `\.7d$`
	}
	re, err := regexp.Compile(s)
	if err != nil {
		return nil, fmt.Errorf("[tasks] nudge_gauges: %w", err)
	}
	return re, nil
}

// Screen is the [screen] table.
type Screen struct {
	// IdleMinutes: grant the screen without asking once the user has been
	// idle this long (default 30). Asking on the desk works at any time.
	IdleMinutes *int `toml:"idle_minutes"`
	// IdleCommand prints the user's idle time in seconds. Empty: macOS's
	// HID idle time from ioreg; elsewhere the screen is never granted
	// without asking.
	IdleCommand string `toml:"idle_command"`
	// BannerOn runs under sh when an agent takes the screen, with
	// AGENT_DESK_REASON (what it is doing) and AGENT_DESK_SECONDS (how long
	// it holds it) set; BannerOff when it lets go.
	BannerOn  string `toml:"banner_on"`
	BannerOff string `toml:"banner_off"`
}

// Idle is IdleMinutes or its default.
func (s Screen) Idle() int { return intOr(s.IdleMinutes, 30) }

// Push is the [push] table: Web Push to the browsers that subscribed in the
// dashboard's settings.
type Push struct {
	// Subject is the contact the desk gives push services (a mailto: or
	// https: URL); the default names the software, not you.
	Subject string `toml:"subject"`
	// Off stops pushing without forgetting the subscriptions.
	Off bool `toml:"off"`
}

// Quota is the [quota] table: Paseo agents stopped at a usage limit.
type Quota struct {
	// StoppedCommand prints the Paseo agent ids stopped at a usage limit,
	// one per line.
	StoppedCommand string `toml:"stopped_command"`
	// AutoContinueCommand runs under sh with "list", "on <id>" or "off
	// <id>" as $1 and $2: list prints the ids that resume themselves when
	// their quota returns; on and off register or unregister one.
	AutoContinueCommand string `toml:"auto_continue_command"`
}
