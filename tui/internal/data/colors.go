// The [colors] table's on-disk shape and the app's one file-write path
// (Stage E.1 live theme picker, tui/SPEC.md §5/§7). Colors are kept
// separate from config.go because this is the only file in the repo that
// writes to disk — everywhere else, config.toml is read-only at runtime.

package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ColorsConfig is the optional [colors] table — plain strings; this
// package doesn't know about ANSI colors or lipgloss, only the ui package
// resolves/produces these (internal/ui/colors.go). Field order also fixes
// colorsSectionText's write order.
type ColorsConfig struct {
	Accent          string `toml:"accent"`
	BarInput        string `toml:"bar_input"`
	BarInputBright  string `toml:"bar_input_bright"`
	BarOutput       string `toml:"bar_output"`
	BarOutputBright string `toml:"bar_output_bright"`
	BarTransition   string `toml:"bar_transition"`
	TextPrimary     string `toml:"text_primary"`
	TextMuted       string `toml:"text_muted"`
}

// colorsSectionText renders c as a literal "[colors]" TOML table, one key
// per line in ColorsConfig's declared field order, always ending in a
// single trailing newline. Empty fields are omitted — SaveColors always
// supplies all 8 in practice (the theme picker never leaves one unset),
// but this keeps the function honest for partial input too.
func colorsSectionText(c ColorsConfig) string {
	var b strings.Builder
	b.WriteString("[colors]\n")
	field := func(key, val string) {
		if val == "" {
			return
		}
		fmt.Fprintf(&b, "%s = %q\n", key, val)
	}
	field("accent", c.Accent)
	field("bar_input", c.BarInput)
	field("bar_input_bright", c.BarInputBright)
	field("bar_output", c.BarOutput)
	field("bar_output_bright", c.BarOutputBright)
	field("bar_transition", c.BarTransition)
	field("text_primary", c.TextPrimary)
	field("text_muted", c.TextMuted)
	return b.String()
}

// spliceColorsSection replaces fileText's "[colors]" table with a freshly
// generated one, touching nothing else in the file (tui/SPEC.md §7:
// config.toml is the user's real, hand-commented file). If no "[colors]"
// section exists, the new section is appended at the end instead. Pure
// text in, text out — unit-testable without a filesystem, and idempotent
// (splicing the same ColorsConfig twice yields the same text).
func spliceColorsSection(fileText string, c ColorsConfig) string {
	newSection := colorsSectionText(c)

	// SplitAfter keeps each line's trailing "\n" attached (the last
	// element is "" if fileText ends in "\n", or an unterminated final
	// line otherwise), so re-joining any sub-slice reconstructs exactly
	// the original bytes — no separate newline bookkeeping needed.
	lines := strings.SplitAfter(fileText, "\n")

	start := -1
	for i, line := range lines {
		if strings.TrimSpace(strings.TrimSuffix(line, "\n")) == "[colors]" {
			start = i
			break
		}
	}
	if start == -1 {
		return appendColorsSection(fileText, newSection)
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		content := strings.TrimSuffix(lines[i], "\n")
		if content != "" && strings.HasPrefix(strings.TrimSpace(content), "[") {
			end = i
			break
		}
	}

	// Preserve blank-line spacing that separated the old section from
	// whatever follows it, if anything does.
	blankTail := 0
	if end < len(lines) {
		for j := end - 1; j > start; j-- {
			if strings.TrimSpace(strings.TrimSuffix(lines[j], "\n")) != "" {
				break
			}
			blankTail++
		}
	}

	var b strings.Builder
	b.WriteString(strings.Join(lines[:start], ""))
	b.WriteString(newSection)
	b.WriteString(strings.Join(lines[end-blankTail:end], ""))
	b.WriteString(strings.Join(lines[end:], ""))
	return b.String()
}

// appendColorsSection appends newSection (already single-trailing-newline
// terminated) at the end of fileText, separated by exactly one blank line
// from any existing content.
func appendColorsSection(fileText, newSection string) string {
	switch {
	case fileText == "":
		return newSection
	case strings.HasSuffix(fileText, "\n\n"):
		return fileText + newSection
	case strings.HasSuffix(fileText, "\n"):
		return fileText + "\n" + newSection
	default:
		return fileText + "\n\n" + newSection
	}
}

// SaveColors is the app's only file-write path: reads path (a missing
// file is fine — a first-ever save creates it and its parent directory),
// splices in a fresh [colors] section, and writes the result back. Every
// other section/comment in the user's hand-maintained file is untouched.
func SaveColors(path string, c ColorsConfig) error {
	existing := ""
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		existing = string(raw)
	case os.IsNotExist(err):
		// First-ever save — start from an empty file.
	default:
		return fmt.Errorf("reading %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	updated := spliceColorsSection(existing, c)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
