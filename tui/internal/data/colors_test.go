// Tests for the [colors] table's text splice (tui/SPEC.md §5/§7 Stage
// E.1) — the mechanism that lets SaveColors touch only its own section of
// the user's hand-maintained config.toml.

package data

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func sampleColors() ColorsConfig {
	return ColorsConfig{
		Accent:          "cyan",
		BarInput:        "cyan",
		BarInputBright:  "bright_cyan",
		BarOutput:       "blue",
		BarOutputBright: "bright_blue",
		BarTransition:   "yellow",
		TextPrimary:     "white",
		TextMuted:       "bright_black",
	}
}

func TestSpliceColorsSection_AppendsWhenAbsent_EmptyFile(t *testing.T) {
	got := spliceColorsSection("", sampleColors())
	if !strings.HasPrefix(got, "[colors]\n") {
		t.Fatalf("got %q, want it to start with [colors]", got)
	}
	if !strings.Contains(got, `accent = "cyan"`) {
		t.Errorf("got %q, missing accent field", got)
	}
}

func TestSpliceColorsSection_AppendsWhenAbsent_NoTrailingNewline(t *testing.T) {
	fileText := `supabase_url = "https://x.supabase.co"`
	got := spliceColorsSection(fileText, sampleColors())
	if !strings.HasPrefix(got, fileText+"\n\n[colors]\n") {
		t.Fatalf("got %q, want original content preserved with a blank-line separator before [colors]", got)
	}
}

func TestSpliceColorsSection_ReplacesExistingSectionInPlace(t *testing.T) {
	fileText := "supabase_url = \"x\"\n\n[colors]\naccent = \"red\"\nbar_input = \"red\"\n"
	got := spliceColorsSection(fileText, sampleColors())

	if !strings.HasPrefix(got, "supabase_url = \"x\"\n\n") {
		t.Fatalf("content before [colors] was disturbed: %q", got)
	}
	if strings.Contains(got, `"red"`) {
		t.Errorf("old [colors] content leaked through: %q", got)
	}
	if !strings.Contains(got, `accent = "cyan"`) {
		t.Errorf("new [colors] content missing: %q", got)
	}
}

func TestSpliceColorsSection_SandwichedBetweenTwoTables(t *testing.T) {
	fileText := "[a]\nx = 1\n\n[colors]\nold = 1\n\n[b]\ny = 2\n"
	got := spliceColorsSection(fileText, sampleColors())

	if !strings.HasPrefix(got, "[a]\nx = 1\n\n") {
		t.Fatalf("preceding table disturbed: %q", got)
	}
	if !strings.HasSuffix(got, "\n[b]\ny = 2\n") {
		t.Fatalf("following table disturbed: %q", got)
	}
	if strings.Contains(got, "old = 1") {
		t.Errorf("old [colors] content leaked through: %q", got)
	}
}

func TestSpliceColorsSection_Idempotent(t *testing.T) {
	c := sampleColors()
	once := spliceColorsSection("", c)
	twice := spliceColorsSection(once, c)
	if once != twice {
		t.Fatalf("splicing the same config twice changed the text:\nonce:  %q\ntwice: %q", once, twice)
	}

	// Also idempotent starting from a file with unrelated content around it.
	fileText := "[a]\nx = 1\n"
	first := spliceColorsSection(fileText, c)
	second := spliceColorsSection(first, c)
	if first != second {
		t.Fatalf("splicing twice against a file with other content changed the text:\nfirst:  %q\nsecond: %q", first, second)
	}
}

func TestSaveColors_RoundTripsThroughTOMLDecode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "burnbar", "config.toml")

	c := sampleColors()
	if err := SaveColors(path, c); err != nil {
		t.Fatalf("SaveColors() error = %v", err)
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		t.Fatalf("toml.DecodeFile() error = %v", err)
	}
	if cfg.Colors != c {
		t.Errorf("round-tripped Colors = %+v, want %+v", cfg.Colors, c)
	}
}

func TestSaveColors_PreservesExistingContentOnSecondSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	initial := "# a comment\nsupabase_url = \"https://x.supabase.co\"\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatalf("setup os.WriteFile() error = %v", err)
	}

	if err := SaveColors(path, sampleColors()); err != nil {
		t.Fatalf("SaveColors() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "# a comment") || !strings.Contains(got, `supabase_url = "https://x.supabase.co"`) {
		t.Errorf("existing hand-written content was disturbed: %q", got)
	}
}
