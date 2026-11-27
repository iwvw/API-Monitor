package apikeys

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestKeyLifecyclePermissionsExpiryAndUsage(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-only-api-key-encryption-secret")
	manager := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	created, err := manager.Create(context.Background(), Input{Name: "Browser", Kind: "plugin", Scopes: []string{ScopeAPIWrite}, ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	if created.APIKey == "" || len(created.Scopes) != 1 || created.Scopes[0] != ScopeTOTPRead {
		t.Fatalf("unexpected created key: %#v", created)
	}
	createActiveSession(t, manager)

	req := httptest.NewRequest(http.MethodGet, "/api/totp/accounts", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	req.Header.Set("Authorization", "Bearer "+created.APIKey)
	req.Header.Set("User-Agent", "api-key-test")
	if _, err := manager.Authorize(context.Background(), req, ScopeTOTPRead); err != nil {
		t.Fatalf("authorize plugin key: %v", err)
	}
	if _, err := manager.Authorize(context.Background(), req, ScopeAPIRead); !errors.Is(err, ErrDenied) {
		t.Fatalf("expected scope denial, got %v", err)
	}
	keys, err := manager.List(context.Background())
	if err != nil || len(keys) != 1 || keys[0].RequestCount != 1 || keys[0].LastIPAddress != "203.0.113.9" {
		t.Fatalf("usage was not recorded: keys=%#v err=%v", keys, err)
	}
	if err := manager.Revoke(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Authorize(context.Background(), req, ScopeTOTPRead); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected revoked key rejection, got %v", err)
	}
}

func TestExpiredKeyIsRejected(t *testing.T) {
	manager := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	expiredAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	created, err := manager.Create(context.Background(), Input{
		Name:      "Expired",
		Kind:      "api",
		Scopes:    []string{ScopeAPIRead},
		ExpiresAt: &expiredAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Header.Set("X-API-Key", created.APIKey)
	if _, err := manager.Authorize(context.Background(), req, ScopeAPIRead); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected expired key rejection, got %v", err)
	}
}

func TestListDoesNotExposeRawKeyAndUpdatePreservesEnabledState(t *testing.T) {
	manager := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	created, err := manager.Create(context.Background(), Input{Name: "General", Kind: "api", Scopes: []string{ScopeAPIRead}})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if err := manager.Update(context.Background(), created.ID, Input{Name: "General", Kind: "api", Scopes: []string{ScopeAPIRead}, Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Update(context.Background(), created.ID, Input{Name: "Renamed", Kind: "api", Scopes: []string{ScopeAPIRead}}); err != nil {
		t.Fatal(err)
	}
	keys, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].APIKey != "" || keys[0].Enabled || keys[0].Name != "Renamed" {
		t.Fatalf("unexpected listed key: %#v", keys)
	}
}

func TestUpdateWithoutExpiresAtPreservesExpiry(t *testing.T) {
	manager := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	created, err := manager.Create(context.Background(), Input{Name: "Limited", Kind: "api", Scopes: []string{ScopeAPIRead}, ExpiresAt: &expires})
	if err != nil {
		t.Fatal(err)
	}
	// 只改 name(不传 expiresAt):过期时间必须保留,不能把限期 Key 变成永久 Key。
	if err := manager.Update(context.Background(), created.ID, Input{Name: "Renamed", Kind: "api", Scopes: []string{ScopeAPIRead}}); err != nil {
		t.Fatal(err)
	}
	keys, err := manager.List(context.Background())
	if err != nil || len(keys) != 1 {
		t.Fatalf("list keys: %#v err=%v", keys, err)
	}
	if keys[0].ExpiresAt == nil || *keys[0].ExpiresAt != expires {
		t.Fatalf("expiry was cleared by partial update: got %v want %s", keys[0].ExpiresAt, expires)
	}
	// 显式传空串才表示清除。
	empty := ""
	if err := manager.Update(context.Background(), created.ID, Input{Name: "Renamed", Kind: "api", Scopes: []string{ScopeAPIRead}, ExpiresAt: &empty}); err != nil {
		t.Fatal(err)
	}
	keys, err = manager.List(context.Background())
	if err != nil || len(keys) != 1 || keys[0].ExpiresAt != nil {
		t.Fatalf("explicit empty expiry should clear: %#v err=%v", keys, err)
	}
}

func TestUsageOnlyTrustsForwardedIPFromConfiguredProxy(t *testing.T) {
	manager := New(config.Config{DataDir: t.TempDir(), DBName: "data.db", TrustedProxyCIDRs: []string{"10.0.0.0/8"}})
	created, err := manager.Create(context.Background(), Input{Name: "Browser", Kind: "plugin"})
	if err != nil {
		t.Fatal(err)
	}
	createActiveSession(t, manager)
	req := httptest.NewRequest(http.MethodGet, "/api/totp/accounts", nil)
	req.RemoteAddr = "10.2.3.4:9000"
	req.Header.Set("Authorization", "Bearer "+created.APIKey)
	req.Header.Set("X-Forwarded-For", "198.51.100.18")
	if _, err := manager.Authorize(context.Background(), req, ScopeTOTPRead); err != nil {
		t.Fatal(err)
	}
	keys, err := manager.List(context.Background())
	if err != nil || keys[0].LastIPAddress != "198.51.100.18" {
		t.Fatalf("trusted forwarded IP not recorded: keys=%#v err=%v", keys, err)
	}
}

func createActiveSession(t *testing.T, manager *Manager) {
	t.Helper()
	db, err := manager.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	_, err = db.Exec(`INSERT INTO sessions (session_id,password,created_at,last_accessed_at,expires_at,is_active) VALUES (?,?,?,?,?,1)`,
		"test-session", "authenticated", now.Format(time.RFC3339), now.Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
}

func TestPluginPairingCanOnlyBeClaimedOnce(t *testing.T) {
	manager := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	pairing, err := manager.CreatePluginPairing(context.Background(), "Chrome")
	if err != nil || !strings.HasPrefix(pairing.Code, "pair_") {
		t.Fatalf("pairing creation failed: pairing=%#v err=%v", pairing, err)
	}
	apiKey, err := manager.ClaimPluginPairing(context.Background(), pairing.Code)
	if err != nil || !strings.HasPrefix(apiKey, "akp_") {
		t.Fatalf("pairing claim failed: apiKey=%q err=%v", apiKey, err)
	}
	if _, err := manager.ClaimPluginPairing(context.Background(), pairing.Code); !errors.Is(err, ErrInvalid) {
		t.Fatalf("claimed pairing should be rejected, got %v", err)
	}
}
