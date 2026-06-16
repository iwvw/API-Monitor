package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesRuntimeDistDirectoryWhenSourceMarkersAreAbsent(t *testing.T) {
	t.Setenv("DIST_DIR", "")
	t.Setenv("PUBLIC_DIR", "")
	t.Setenv("DATA_DIR", "")
	t.Setenv("DB_NAME", "")
	t.Setenv("GO_HOST", "")
	t.Setenv("GO_PORT", "")
	t.Setenv("PORT", "")
	t.Setenv("NODE_LEGACY_URL", "")

	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<div id=\"root\"></div>"), 0o644); err != nil {
		t.Fatal(err)
	}

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	cfg := Load("test")
	if cfg.DistDir != dist {
		t.Fatalf("DistDir = %q, want %q", cfg.DistDir, dist)
	}
}
