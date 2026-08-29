package config

import (
	"os"
	"testing"
)

func TestContainerDefaultConfigPath(t *testing.T) {
	t.Run("fallback to /app when /data is missing", func(t *testing.T) {
		// This test environment does not guarantee a writable/mounted /data.
		// If /data is absent we must keep /app fallback to avoid persistence failures.
		if _, err := os.Stat("/data"); err == nil {
			t.Skip("/data exists in this environment; cannot validate missing-/data fallback")
		}
		if got := containerDefaultConfigPath(); got != "/app/config.json" {
			t.Fatalf("containerDefaultConfigPath() = %q, want %q", got, "/app/config.json")
		}
	})

	t.Run("prefer /data when /data directory exists", func(t *testing.T) {
		if _, err := os.Stat("/data"); err != nil {
			t.Skip("/data does not exist in this environment")
		}
		if got := containerDefaultConfigPath(); got != "/data/config.json" {
			t.Fatalf("containerDefaultConfigPath() = %q, want %q", got, "/data/config.json")
		}
	})
}
