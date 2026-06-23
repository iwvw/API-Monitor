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

func TestLoadResolvesRelativeEnvPathsFromRepoRoot(t *testing.T) {
	t.Setenv("DIST_DIR", "./custom-dist")
	t.Setenv("PUBLIC_DIR", "./custom-public")
	t.Setenv("DATA_DIR", "./custom-data")
	t.Setenv("DB_NAME", "test.db")

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "backend-go", "nested"), 0o755); err != nil {
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
	if err := os.Chdir(filepath.Join(root, "backend-go", "nested")); err != nil {
		t.Fatal(err)
	}

	cfg := Load("test")
	if cfg.DataDir != filepath.Join(root, "custom-data") {
		t.Fatalf("DataDir = %q, want repo-root-relative path", cfg.DataDir)
	}
	if cfg.DatabasePath() != filepath.Join(root, "custom-data", "test.db") {
		t.Fatalf("DatabasePath = %q, want repo-root-relative database path", cfg.DatabasePath())
	}
	if cfg.DistDir != filepath.Join(root, "custom-dist") || cfg.PublicDir != filepath.Join(root, "custom-public") {
		t.Fatalf("asset dirs should be repo-root-relative: dist=%q public=%q", cfg.DistDir, cfg.PublicDir)
	}
}
