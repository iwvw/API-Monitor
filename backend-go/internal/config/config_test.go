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

// GATEWAY_BODY_MAX_MB 配置网关请求体上限；未配置时默认 16MB。
func TestGatewayBodyMaxConfig(t *testing.T) {
	t.Setenv("GATEWAY_BODY_MAX_MB", "32")
	cfg := Load("test")
	if cfg.GatewayBodyMaxBytes != 32*1024*1024 {
		t.Fatalf("GatewayBodyMaxBytes = %d, want %d", cfg.GatewayBodyMaxBytes, 32*1024*1024)
	}

	t.Setenv("GATEWAY_BODY_MAX_MB", "")
	cfg = Load("test")
	if cfg.GatewayBodyMaxBytes != 16*1024*1024 {
		t.Fatalf("默认 GatewayBodyMaxBytes = %d, want %d", cfg.GatewayBodyMaxBytes, 16*1024*1024)
	}
}

func TestProductionSecurityDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("NODE_ENV", "")
	t.Setenv("SECURE_COOKIES", "")
	t.Setenv("ALLOW_LOCAL_SHELL_TASKS", "")
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8,192.0.2.10")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://panel.example.com")

	cfg := Load("test")
	if !cfg.IsProduction() || !cfg.SecureCookies || cfg.LocalShellTasksAllowed() {
		t.Fatalf("unexpected production defaults: %#v", cfg)
	}
	if len(cfg.TrustedProxyCIDRs) != 2 || len(cfg.CORSAllowedOrigins) != 1 {
		t.Fatalf("environment lists not parsed: proxies=%v cors=%v", cfg.TrustedProxyCIDRs, cfg.CORSAllowedOrigins)
	}
}

func TestTrustedProxyCIDRsDefaults(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	cfg := Load("test")
	if len(cfg.TrustedProxyCIDRs) == 0 {
		t.Fatal("unset TRUSTED_PROXY_CIDRS should fall back to safe private-network defaults")
	}
	trusted := false
	for _, entry := range cfg.TrustedProxyCIDRs {
		if entry == "172.16.0.0/12" {
			trusted = true
		}
	}
	if !trusted {
		t.Fatalf("defaults must include container bridge range, got %v", cfg.TrustedProxyCIDRs)
	}

	t.Setenv("TRUSTED_PROXY_CIDRS", "none")
	cfg = Load("test")
	if len(cfg.TrustedProxyCIDRs) != 0 {
		t.Fatalf("none should disable trusted proxies, got %v", cfg.TrustedProxyCIDRs)
	}

	t.Setenv("TRUSTED_PROXY_CIDRS", "203.0.113.0/24")
	cfg = Load("test")
	if len(cfg.TrustedProxyCIDRs) != 1 || cfg.TrustedProxyCIDRs[0] != "203.0.113.0/24" {
		t.Fatalf("explicit value should override defaults, got %v", cfg.TrustedProxyCIDRs)
	}
}

func TestProductionSecurityValidation(t *testing.T) {
	cfg := Config{Environment: "production"}
	t.Setenv("ENCRYPTION_KEY", "")
	t.Setenv("JWT_SECRET", "")
	if err := cfg.ValidateSecurity(); err == nil {
		t.Fatal("expected missing production secrets to be rejected")
	}

	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("JWT_SECRET", "abcdef0123456789abcdef0123456789")
	if err := cfg.ValidateSecurity(); err != nil {
		t.Fatalf("valid production secrets rejected: %v", err)
	}

	if err := (Config{Environment: "development"}).ValidateSecurity(); err != nil {
		t.Fatalf("development should not require production secrets: %v", err)
	}
}
