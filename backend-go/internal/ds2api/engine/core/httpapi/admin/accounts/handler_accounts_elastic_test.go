package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestUpdateElasticPoolEnableGlobal(t *testing.T) {
	router := newHTTPAdminHarness(t, `{
		"accounts":[
			{"email":"a@x.com","password":"p"},
			{"email":"b@x.com","password":"p"},
			{"email":"c@x.com","password":"p"},
			{"email":"d@x.com","password":"p"}
		]
	}`, &testingDSMock{})

	body := []byte(`{"enabled":true,"per_pool":false,"global_count":2}`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminReq(http.MethodPut, "/accounts/elastic-pool", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload["success"].(bool) {
		t.Fatal("expected success=true")
	}
	ep := payload["elastic_pool"].(map[string]any)
	if !ep["enabled"].(bool) {
		t.Error("expected enabled=true in response")
	}

	h := router.(*chi.Mux)
	_ = h
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, adminReq(http.MethodGet, "/accounts?page=1&page_size=10", nil))
	var listPayload map[string]any
	_ = json.Unmarshal(listRec.Body.Bytes(), &listPayload)
	items := listPayload["items"].([]any)
	enabledCount := 0
	for _, item := range items {
		m := item.(map[string]any)
		if m["enabled"].(bool) {
			enabledCount++
		}
	}
	if enabledCount != 2 {
		t.Errorf("expected 2 enabled accounts, got %d", enabledCount)
	}
}

func TestUpdateElasticPoolDisableRestoresAll(t *testing.T) {
	router := newHTTPAdminHarness(t, `{
		"accounts":[
			{"email":"a@x.com","password":"p","disabled":true},
			{"email":"b@x.com","password":"p","disabled":true}
		]
	}`, &testingDSMock{})

	body := []byte(`{"enabled":false,"per_pool":false,"global_count":0}`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminReq(http.MethodPut, "/accounts/elastic-pool", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, adminReq(http.MethodGet, "/accounts?page=1&page_size=10", nil))
	var listPayload map[string]any
	_ = json.Unmarshal(listRec.Body.Bytes(), &listPayload)
	items := listPayload["items"].([]any)
	for i, item := range items {
		m := item.(map[string]any)
		if !m["enabled"].(bool) {
			t.Errorf("account %d should be enabled after disabling elastic pool", i)
		}
	}
}

func TestUpdateElasticPoolMissingEnabled(t *testing.T) {
	router := newHTTPAdminHarness(t, `{"accounts":[]}`, &testingDSMock{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminReq(http.MethodPut, "/accounts/elastic-pool", []byte(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateElasticPoolGlobalCountZeroDisablesAll(t *testing.T) {
	router := newHTTPAdminHarness(t, `{
		"accounts":[
			{"email":"a@x.com","password":"p"},
			{"email":"b@x.com","password":"p"},
			{"email":"c@x.com","password":"p"}
		]
	}`, &testingDSMock{})

	body := []byte(`{"enabled":true,"per_pool":false,"global_count":0}`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminReq(http.MethodPut, "/accounts/elastic-pool", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	ep := payload["elastic_pool"].(map[string]any)
	if int(ep["global_count"].(float64)) != 0 {
		t.Errorf("expected global_count=0, got %v", ep["global_count"])
	}

	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, adminReq(http.MethodGet, "/accounts?page=1&page_size=10", nil))
	var listPayload map[string]any
	_ = json.Unmarshal(listRec.Body.Bytes(), &listPayload)
	items := listPayload["items"].([]any)
	for i, item := range items {
		m := item.(map[string]any)
		if m["enabled"].(bool) {
			t.Errorf("account %d should be disabled when global_count=0", i)
		}
	}
}

func TestUpdateElasticPoolInvalidJSON(t *testing.T) {
	router := newHTTPAdminHarness(t, `{"accounts":[]}`, &testingDSMock{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminReq(http.MethodPut, "/accounts/elastic-pool", []byte(`not json`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateElasticPoolPerPoolMode(t *testing.T) {
	router := newHTTPAdminHarness(t, `{
		"accounts":[
			{"email":"d1@x.com","password":"p","pool_type":"default"},
			{"email":"d2@x.com","password":"p","pool_type":"default"},
			{"email":"d3@x.com","password":"p","pool_type":"default"},
			{"email":"n1@x.com","password":"p","pool_type":"no_tools"},
			{"email":"n2@x.com","password":"p","pool_type":"no_tools"}
		]
	}`, &testingDSMock{})

	body := []byte(`{"enabled":true,"per_pool":true,"default_count":2,"no_tools_count":1,"tools_only_count":1}`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminReq(http.MethodPut, "/accounts/elastic-pool", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	ep := payload["elastic_pool"].(map[string]any)
	if !ep["per_pool"].(bool) {
		t.Error("expected per_pool=true in response")
	}

	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, adminReq(http.MethodGet, "/accounts?page=1&page_size=10", nil))
	var listPayload map[string]any
	_ = json.Unmarshal(listRec.Body.Bytes(), &listPayload)
	items := listPayload["items"].([]any)
	enabledByType := map[string]int{}
	for _, item := range items {
		m := item.(map[string]any)
		if m["enabled"].(bool) {
			pt := m["pool_type"].(string)
			enabledByType[pt]++
		}
	}
	if enabledByType["default"] != 2 {
		t.Errorf("expected 2 enabled default accounts, got %d", enabledByType["default"])
	}
	if enabledByType["no_tools"] != 1 {
		t.Errorf("expected 1 enabled no_tools account, got %d", enabledByType["no_tools"])
	}
}
