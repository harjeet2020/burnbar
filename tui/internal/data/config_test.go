package data

import "testing"

// TestLoadDefaultsToRealtime pins the live-source default to realtime. Stage
// C.1 replaced the abandoned realtime-go with the maintained nshafer/phx
// transport, so an unset live_source resolves to realtime — the low-latency
// path — with poll kept as a manual backup (ARCHITECTURE.md §5).
func TestLoadDefaultsToRealtime(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no config file → env-only load
	t.Setenv("BURNBAR_SUPABASE_URL", "https://example.supabase.co")
	t.Setenv("BURNBAR_SUPABASE_ANON_KEY", "anon-key")
	t.Setenv("BURNBAR_LIVE_SOURCE", "") // explicitly unset

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.LiveSource != LiveSourceRealtime {
		t.Fatalf("default LiveSource = %q, want %q", cfg.LiveSource, LiveSourceRealtime)
	}
}

// TestLoadHonorsPollOptIn confirms poll is still reachable as an explicit
// opt-in / backup even though it is no longer the default.
func TestLoadHonorsPollOptIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("BURNBAR_SUPABASE_URL", "https://example.supabase.co")
	t.Setenv("BURNBAR_SUPABASE_ANON_KEY", "anon-key")
	t.Setenv("BURNBAR_LIVE_SOURCE", "poll")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.LiveSource != LiveSourcePoll {
		t.Fatalf("LiveSource = %q, want %q", cfg.LiveSource, LiveSourcePoll)
	}
}
