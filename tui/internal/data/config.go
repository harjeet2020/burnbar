// Package data owns everything that crosses the process boundary: user
// configuration, and (in later stages) the PostgREST baseline fetch, the
// OpenRouter credits client, and the LiveSource realtime/polling
// implementations. Stage A ships only configuration.
package data

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// LiveSource values accepted by the live_source config key. "realtime" is
// the default — a phx Phoenix socket to Supabase Realtime that lights a bar
// within ~1–2s of a request (root SPEC §6). "poll" is the manual backup: a
// 20s PostgREST walk of the requests table, built on nothing but net/http,
// selectable here or toggled at runtime with `p`.
const (
	LiveSourceRealtime = "realtime"
	LiveSourcePoll     = "poll"
)

// Config holds every user-provided setting the TUI needs. Values come from
// ~/.config/burnbar/config.toml with BURNBAR_* environment variables taking
// precedence (tui/SPEC.md §7); env-only operation (no file at all) is valid.
type Config struct {
	// SupabaseURL is the user's Supabase project URL
	// (https://<ref>.supabase.co). Required.
	SupabaseURL string `toml:"supabase_url"`
	// SupabaseAnonKey is the project's publishable (anon) key — read-only
	// under RLS. Required.
	SupabaseAnonKey string `toml:"supabase_anon_key"`
	// OpenRouterAPIKey is used only for polling the credits endpoint,
	// directly from this machine. Optional: when absent the credits header
	// shows "—" with a hint.
	OpenRouterAPIKey string `toml:"openrouter_api_key"`
	// LiveSource selects the live event mechanism: "realtime" (default) or
	// "poll".
	LiveSource string `toml:"live_source"`
	// Colors is the optional [colors] table (Stage E.1 live theme picker,
	// tui/SPEC.md §5/§7) — raw, unvalidated strings; the ui package
	// resolves them into ANSI-16 colors, falling back to defaults per
	// token rather than failing validation, since a hand-typo'd or
	// missing color is never a reason to refuse to start.
	Colors ColorsConfig `toml:"colors"`
}

// HasCredentialsForCredits reports whether the credits endpoint can be
// polled at all (tui/SPEC.md §7: the key is optional).
func (c Config) HasCredentialsForCredits() bool { return c.OpenRouterAPIKey != "" }

// ConfigError is a user-facing configuration failure. It renders as a
// plain, actionable message — including a ready-to-paste TOML template —
// designed to be printed *before* the alternate screen is entered
// (tui/SPEC.md §7, §8).
type ConfigError struct {
	// Path is the config file location that was consulted.
	Path string
	// Problems are the individual human-readable validation failures.
	Problems []string
}

// Error formats the full multi-line help text shown on stderr.
func (e *ConfigError) Error() string {
	var b strings.Builder
	b.WriteString("burnbar: cannot start — configuration problem")
	if len(e.Problems) == 1 {
		b.WriteString(": " + e.Problems[0] + "\n")
	} else {
		b.WriteString("s:\n")
		for _, p := range e.Problems {
			b.WriteString("  • " + p + "\n")
		}
	}
	b.WriteString(fmt.Sprintf(`
Burnbar reads %s
(environment variables BURNBAR_SUPABASE_URL, BURNBAR_SUPABASE_ANON_KEY,
BURNBAR_OPENROUTER_API_KEY and BURNBAR_LIVE_SOURCE override file values).

Paste this template into the file and fill in your values:

    # Supabase dashboard → Project Settings → API
    supabase_url      = "https://YOUR-PROJECT-REF.supabase.co"
    supabase_anon_key = "YOUR-ANON-KEY"

    # Optional — enables the credits balance display.
    # https://openrouter.ai/settings/keys
    openrouter_api_key = ""

    # Optional — "realtime" (default) or "poll".
    live_source = "realtime"
`, e.Path))
	return b.String()
}

// DefaultPath returns the config file location. On macOS and Linux this
// is the CLI-conventional ~/.config/burnbar/config.toml (honoring
// XDG_CONFIG_HOME) — deliberately NOT os.UserConfigDir, which on macOS
// points at ~/Library/Application Support where nobody expects a terminal
// tool's config (tui/SPEC.md §7). Other platforms use os.UserConfigDir.
func DefaultPath() (string, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine the user config directory: %w", err)
		}
		return filepath.Join(dir, "burnbar", "config.toml"), nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "burnbar", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine the home directory: %w", err)
	}
	return filepath.Join(home, ".config", "burnbar", "config.toml"), nil
}

// Load reads, overlays, and validates the configuration: file first (if
// present), then BURNBAR_* env overrides, then validation. All failures
// come back as a *ConfigError whose message is safe to print directly.
func Load() (Config, error) {
	path, err := DefaultPath()
	if err != nil {
		// No config dir means we can still run on env vars alone; keep the
		// path purely informational in errors.
		path = "~/.config/burnbar/config.toml"
	}

	var cfg Config
	if _, statErr := os.Stat(path); statErr == nil {
		if _, decodeErr := toml.DecodeFile(path, &cfg); decodeErr != nil {
			return Config{}, &ConfigError{
				Path:     path,
				Problems: []string{fmt.Sprintf("%s is not valid TOML: %v", path, decodeErr)},
			}
		}
	}

	applyEnvOverrides(&cfg)

	if cfg.LiveSource == "" {
		cfg.LiveSource = LiveSourceRealtime
	}

	if problems := validate(cfg); len(problems) > 0 {
		return Config{}, &ConfigError{Path: path, Problems: problems}
	}
	return cfg, nil
}

// applyEnvOverrides overlays BURNBAR_* environment variables onto file
// values — env wins, per the precedence table in tui/SPEC.md §7.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("BURNBAR_SUPABASE_URL"); v != "" {
		cfg.SupabaseURL = v
	}
	if v := os.Getenv("BURNBAR_SUPABASE_ANON_KEY"); v != "" {
		cfg.SupabaseAnonKey = v
	}
	if v := os.Getenv("BURNBAR_OPENROUTER_API_KEY"); v != "" {
		cfg.OpenRouterAPIKey = v
	}
	if v := os.Getenv("BURNBAR_LIVE_SOURCE"); v != "" {
		cfg.LiveSource = v
	}
}

// validate returns one human-readable problem per broken invariant; an
// empty slice means the config is usable.
func validate(cfg Config) []string {
	var problems []string

	switch {
	case cfg.SupabaseURL == "":
		problems = append(problems, "supabase_url is missing")
	default:
		u, err := url.Parse(cfg.SupabaseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			problems = append(problems, fmt.Sprintf(
				"supabase_url %q is not a valid URL (expected https://<ref>.supabase.co)", cfg.SupabaseURL))
		}
	}

	if cfg.SupabaseAnonKey == "" {
		problems = append(problems, "supabase_anon_key is missing")
	}

	if cfg.LiveSource != LiveSourceRealtime && cfg.LiveSource != LiveSourcePoll {
		problems = append(problems, fmt.Sprintf(
			"live_source %q is invalid (must be %q or %q)", cfg.LiveSource, LiveSourceRealtime, LiveSourcePoll))
	}

	return problems
}
