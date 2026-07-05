// Command burnbar is the terminal frontend for Burnbar — a real-time LLM
// cost meter fed by OpenRouter Broadcast via the user's own Supabase
// backend. Stage A renders the full UI from fixture data; the live data
// layer arrives in Stage C (tui/SPEC.md §10).
package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/harjeet2020/burnbar/tui/internal/data"
	"github.com/harjeet2020/burnbar/tui/internal/ui"
)

func main() {
	// Config problems must surface as plain text BEFORE the alternate
	// screen is entered (tui/SPEC.md §7/§8) — hence load-then-run.
	cfg, err := data.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Debug logging goes to a file, never stdout — the terminal is the
	// UI (tui/SPEC.md §8). Enable with BURNBAR_DEBUG=1 and `tail -f` the
	// log in a second terminal.
	if os.Getenv("BURNBAR_DEBUG") != "" {
		logPath, logErr := debugLogPath()
		if logErr == nil {
			var f *os.File
			f, logErr = tea.LogToFile(logPath, "burnbar")
			if logErr == nil {
				defer f.Close() //nolint:errcheck // best-effort close on exit
			}
		}
		if logErr != nil {
			fmt.Fprintf(os.Stderr, "burnbar: debug logging disabled: %v\n", logErr)
		}
	}

	p := tea.NewProgram(ui.New(cfg))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "burnbar: %v\n", err)
		os.Exit(1)
	}
}

// debugLogPath resolves ~/.local/state/burnbar/debug.log (tui/SPEC.md §8),
// creating the directory if needed.
func debugLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "state", "burnbar")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "debug.log"), nil
}
