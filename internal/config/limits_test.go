package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func minimalConfig() Config {
	return Config{
		Listen:      "127.0.0.1:8080",
		Upstream:    UpstreamConfig{Zen: "https://zen.example", Go: "https://go.example", Anonymous: "https://anon.example"},
		Retry:       RetryConfig{MaxAttempts: 1, TimeoutSeconds: 1},
		Models:      ModelsConfig{RefreshSeconds: 1},
		Performance: PerformanceConfig{MaxIdleConns: 1, MaxIdleConnsPerHost: 1, IdleConnTimeoutSeconds: 1, ConnectTimeoutSeconds: 1, FailureCooldownSeconds: 1},
		Logging:     LoggingConfig{Level: "info", RingSize: 100},
		ServerKeys:  []string{"sk-test"},
	}
}

// TestNormalizePerformanceDefaults checks that zero values for the traffic
// limits are replaced by the documented defaults.
func TestNormalizePerformanceDefaults(t *testing.T) {
	cfg, err := Normalize("config.json", minimalConfig())
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if cfg.Performance.MaxInflight != 32 {
		t.Fatalf("max_inflight = %d, want 32", cfg.Performance.MaxInflight)
	}
	if cfg.Performance.MaxRequestBytes != 4<<20 {
		t.Fatalf("max_request_bytes = %d, want %d", cfg.Performance.MaxRequestBytes, 4<<20)
	}
	if cfg.Performance.MaxAttachmentBytes != 16<<20 {
		t.Fatalf("max_attachment_bytes = %d, want %d", cfg.Performance.MaxAttachmentBytes, 16<<20)
	}
}

// TestNormalizePerformanceLimits checks that out-of-range traffic limits are
// refused instead of silently clamped.
func TestNormalizePerformanceLimits(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*Config)
	}{
		{"negative inflight", func(c *Config) { c.Performance.MaxInflight = -1 }},
		{"inflight too large", func(c *Config) { c.Performance.MaxInflight = 4097 }},
		{"request bytes too small", func(c *Config) { c.Performance.MaxRequestBytes = 65535 }},
		{"request bytes too large", func(c *Config) { c.Performance.MaxRequestBytes = 32<<20 + 1 }},
		{"attachment bytes too small", func(c *Config) { c.Performance.MaxAttachmentBytes = 1 }},
		{"attachment bytes too large", func(c *Config) { c.Performance.MaxAttachmentBytes = 32<<20 + 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalConfig()
			tc.apply(&cfg)
			if _, err := Normalize("config.json", cfg); err == nil {
				t.Fatal("Normalize accepted an out-of-range limit")
			}
		})
	}
}

// TestSaveAtomicBackupPermissions checks that a backup created from a
// world-readable config is still owner-only, because it carries the same
// credentials.
func TestSaveAtomicBackupPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("chmod seed config: %v", err)
	}
	if err := SaveAtomic(path, minimalConfig()); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}
	for _, name := range []string{path, path + ".bak"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if perm := info.Mode().Perm(); perm&0077 != 0 {
			t.Fatalf("%s mode = %04o, want group/other bits cleared", name, perm)
		}
	}
}
