package openaibeta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLinkEndpointLifecycle(t *testing.T) {
	cfg := testCfg(t)
	s := New(cfg)

	// 初始未接入
	req := httptest.NewRequest(http.MethodGet, "/api/openaibeta/link", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("status=%d", rec.Code) }
	var st struct{ Linked bool `json:"linked"` }
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil { t.Fatal(err) }
	if st.Linked { t.Fatal("should be unlinked initially") }

	// 启用插件并接入
	stNext := s.Settings()
	stNext.Enabled = true
	if err := s.SaveSettings(context.Background(), stNext); err != nil { t.Fatal(err) }
	req = httptest.NewRequest(http.MethodPost, "/api/openaibeta/link", strings.NewReader("{}"))
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("link status=%d body=%s", rec.Code, rec.Body.String()) }
	var link struct {
		Success bool `json:"success"`
		Linked bool `json:"linked"`
		EndpointID string `json:"endpointId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &link); err != nil { t.Fatal(err) }
	if !link.Success || !link.Linked || link.EndpointID != linkedEndpointID { t.Fatalf("link failed: %+v", link) }

	// 再查状态应已接入
	req = httptest.NewRequest(http.MethodGet, "/api/openaibeta/link", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if !st.Linked { t.Fatal("should be linked after POST") }

	// 断开
	req = httptest.NewRequest(http.MethodDelete, "/api/openaibeta/link", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("unlink status=%d", rec.Code) }
	req = httptest.NewRequest(http.MethodGet, "/api/openaibeta/link", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st.Linked { t.Fatal("should be unlinked after DELETE") }
}
