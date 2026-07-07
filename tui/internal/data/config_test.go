package data

import "testing"

// TestLoadDefaultsToPoll pins the live-source default to polling. The
// realtime-go v0.1.1 go/no-go landed on NO-GO (self-deadlocking reconnect,
// root SPEC §6), so an unset live_source must resolve to poll — never
// realtime — to keep the app recoverable out of the box.
func TestLoadDefaultsToPoll(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no config file → env-only load
	t.Setenv("BURNBAR_SUPABASE_URL", "https://example.supabase.co")
	t.Setenv("BURNBAR_SUPABASE_ANON_KEY", "anon-key")
	t.Setenv("BURNBAR_LIVE_SOURCE", "") // explicitly unset

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.LiveSource != LiveSourcePoll {
		t.Fatalf("default LiveSource = %q, want %q", cfg.LiveSource, LiveSourcePoll)
	}
}

// TestLoadHonorsRealtimeOptIn confirms realtime is still reachable as an
// explicit opt-in even though it is no longer the default.
func TestLoadHonorsRealtimeOptIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("BURNBAR_SUPABASE_URL", "https://example.supabase.co")
	t.Setenv("BURNBAR_SUPABASE_ANON_KEY", "anon-key")
	t.Setenv("BURNBAR_LIVE_SOURCE", "realtime")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.LiveSource != LiveSourceRealtime {
		t.Fatalf("LiveSource = %q, want %q", cfg.LiveSource, LiveSourceRealtime)
	}
}
