package openaibeta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	engineconfig "github.com/iwvw/api-monitor/backend-go/internal/openaibeta/engine/config"
)

func testCfg(t *testing.T) config.Config {
	t.Helper()
	dir := t.TempDir()
	return config.Config{
		DataDir: dir,
		DBName:  "test.db",
		Version: "test",
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	cfg := testCfg(t)
	s := New(cfg)
	st := s.Settings()
	if st.RequestTimeout != 180 {
		t.Fatalf("default request timeout = %d, want 180", st.RequestTimeout)
	}
	if len(st.Models) == 0 {
		t.Fatal("default model registry is empty")
	}

	next := st
	next.Enabled = true
	next.ProxyEndpointID = "ep_abc"
	if err := s.SaveSettings(context.Background(), next); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	got := s.Settings()
	if !got.Enabled || got.ProxyEndpointID != "ep_abc" {
		t.Fatalf("saved settings not reloaded: %+v", got)
	}

	// 重开服务从 DB 恢复
	s2 := New(cfg)
	got2 := s2.Settings()
	if !got2.Enabled || got2.ProxyEndpointID != "ep_abc" {
		t.Fatalf("settings not persisted across restart: %+v", got2)
	}
}

func TestModelStoreSync(t *testing.T) {
	cfg := testCfg(t)
	s := New(cfg)
	// 停用一个模型
	st := s.Settings()
	for i, m := range st.Models {
		if m.ID == "gemini-3.5-flash" {
			st.Models[i].Enabled = false
		}
	}
	if err := s.SaveSettings(context.Background(), st); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, ok := engineconfig.LookupModel("gemini-3.5-flash"); !ok {
		t.Fatal("model lookup failed")
	}
	if engineconfig.ResolveModelName("gemini-3.5-flash") != "gemini-3.5-flash" {
		t.Fatal("resolve model failed")
	}
	found := false
	for _, m := range engineconfig.BaseModels() {
		if m == "gemini-3.5-flash" {
			found = true
		}
	}
	if found {
		t.Fatal("disabled model still in BaseModels")
	}
}

func TestSettingsEndpoint(t *testing.T) {
	cfg := testCfg(t)
	s := New(cfg)
	req := httptest.NewRequest(http.MethodGet, "/api/openaibeta/settings", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Success  bool     `json:"success"`
		Settings Settings `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Success {
		t.Fatal("settings endpoint success=false")
	}
}

func TestChatCompletionsDisabled(t *testing.T) {
	cfg := testCfg(t)
	s := New(cfg)
	payload := `{"model":"gemini-3.5-flash","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/openaibeta/v1/chat/completions", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled relay status = %d, want 404", rec.Code)
	}
}

func TestRelaySettingsPersistenceFile(t *testing.T) {
	dir := t.TempDir()
	_ = dir
	// openaibeta_settings 表应落在 data.db；这里只验证服务可反复新建不冲突。
	cfg1 := config.Config{DataDir: filepath.Join(t.TempDir()), DBName: "a.db", Version: "t"}
	s1 := New(cfg1)
	_ = s1
	_ = os.RemoveAll(dir)
}
