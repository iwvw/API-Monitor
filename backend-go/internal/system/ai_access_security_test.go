package system

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/apikeys"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func newAISecurityTestService(t *testing.T) *Service {
	t.Helper()
	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db", Version: "test"})
	t.Cleanup(func() { service.Shutdown() })
	return service
}

func decodeEnvelopeData(t *testing.T, res *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var payload map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, res.Body.String())
	}
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data envelope, got %s", res.Body.String())
	}
	return data
}

func TestAIAccessPolicyRejectsAISubject(t *testing.T) {
	service := newAISecurityTestService(t)

	req := httptest.NewRequest(http.MethodPut, "/api/system/ai-access/policy", strings.NewReader(`{"policy":"full"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AI-Agent", "api-monitor")
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("AI subject set policy status = %d body=%s", res.Code, res.Body.String())
	}

	policy, err := service.AIAgentAccessPolicy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if policy != AIAccessPolicyDefault {
		t.Fatalf("policy must stay %q, got %q", AIAccessPolicyDefault, policy)
	}
}

func TestAIAccessAuditClearRejectsAISubject(t *testing.T) {
	service := newAISecurityTestService(t)

	rotateReq := httptest.NewRequest(http.MethodPost, "/api/system/ai-access/key/rotate", nil)
	if _, err := service.rotateAIAgentKey(rotateReq); err != nil {
		t.Fatal(err)
	}

	clearReq := httptest.NewRequest(http.MethodPost, "/api/system/ai-access/audit/clear", nil)
	clearReq.Header.Set("X-AI-Agent", "api-monitor")
	res := httptest.NewRecorder()
	service.ServeHTTP(res, clearReq)
	if res.Code != http.StatusForbidden {
		t.Fatalf("AI subject clear audit status = %d body=%s", res.Code, res.Body.String())
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/api/system/ai-access/audit?page=1&pageSize=10", nil)
	payload, err := service.listAIAuditPage(pageReq)
	if err != nil {
		t.Fatal(err)
	}
	if total, _ := payload["total"].(int); total < 1 {
		t.Fatalf("AI subject must not erase audit, total=%#v", payload["total"])
	}
}

func TestAIAccessAuditClearByHumanRecordsEvent(t *testing.T) {
	service := newAISecurityTestService(t)

	rotateReq := httptest.NewRequest(http.MethodPost, "/api/system/ai-access/key/rotate", nil)
	if _, err := service.rotateAIAgentKey(rotateReq); err != nil {
		t.Fatal(err)
	}

	clearReq := httptest.NewRequest(http.MethodPost, "/api/system/ai-access/audit/clear", nil)
	res := httptest.NewRecorder()
	service.ServeHTTP(res, clearReq)
	if res.Code != http.StatusOK {
		t.Fatalf("human clear audit status = %d body=%s", res.Code, res.Body.String())
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/api/system/ai-access/audit?page=1&pageSize=10", nil)
	payload, err := service.listAIAuditPage(pageReq)
	if err != nil {
		t.Fatal(err)
	}
	records, ok := payload["records"].([]aiAuditEntry)
	if !ok || len(records) != 1 {
		t.Fatalf("expected exactly one clear event, got %#v", payload["records"])
	}
	if records[0].Action != "clear_audit" {
		t.Fatalf("unexpected audit record: %#v", records[0])
	}
}

func TestAIAccessOverviewMasksKeyForAISubject(t *testing.T) {
	service := newAISecurityTestService(t)

	humanReq := httptest.NewRequest(http.MethodGet, "/api/system/ai-access", nil)
	humanRes := httptest.NewRecorder()
	service.ServeHTTP(humanRes, humanReq)
	if humanRes.Code != http.StatusOK {
		t.Fatalf("human overview status = %d body=%s", humanRes.Code, humanRes.Body.String())
	}
	humanData := decodeEnvelopeData(t, humanRes)
	humanKey, _ := humanData["agentKey"].(map[string]interface{})["value"].(string)
	if humanKey == "" || strings.Contains(humanKey, "*") {
		t.Fatalf("human overview should expose real key, got %q", humanKey)
	}
	humanGuide, _ := humanData["guide"].(string)
	if !strings.Contains(humanGuide, humanKey) {
		t.Fatalf("human guide should contain real key")
	}

	aiReq := httptest.NewRequest(http.MethodGet, "/api/system/ai-access", nil)
	aiReq.Header.Set("X-AI-Agent", "api-monitor")
	aiRes := httptest.NewRecorder()
	service.ServeHTTP(aiRes, aiReq)
	if aiRes.Code != http.StatusOK {
		t.Fatalf("AI overview status = %d body=%s", aiRes.Code, aiRes.Body.String())
	}
	aiData := decodeEnvelopeData(t, aiRes)
	agentKey := aiData["agentKey"].(map[string]interface{})
	aiKey, _ := agentKey["value"].(string)
	if aiKey == humanKey || !strings.Contains(aiKey, "*") {
		t.Fatalf("AI overview must mask key, got %q", aiKey)
	}
	if header, _ := agentKey["header"].(string); strings.Contains(header, humanKey) {
		t.Fatalf("AI overview header must not leak key, got %q", header)
	}
	aiGuide, _ := aiData["guide"].(string)
	if strings.Contains(aiGuide, humanKey) {
		t.Fatalf("AI overview guide must not leak key")
	}
}

func TestAIAccessRejectsAIMCPAPIKeySubject(t *testing.T) {
	service := newAISecurityTestService(t)

	created, err := service.apiKeys.Create(context.Background(), apikeys.Input{Name: "ai-caller", Kind: "ai"})
	if err != nil {
		t.Fatal(err)
	}
	bearer := "Bearer " + created.APIKey

	policyReq := httptest.NewRequest(http.MethodPut, "/api/system/ai-access/policy", strings.NewReader(`{"policy":"full"}`))
	policyReq.Header.Set("Content-Type", "application/json")
	policyReq.Header.Set("Authorization", bearer)
	policyRes := httptest.NewRecorder()
	service.ServeHTTP(policyRes, policyReq)
	if policyRes.Code != http.StatusForbidden {
		t.Fatalf("ai:mcp key set policy status = %d body=%s", policyRes.Code, policyRes.Body.String())
	}

	clearReq := httptest.NewRequest(http.MethodPost, "/api/system/ai-access/audit/clear", nil)
	clearReq.Header.Set("Authorization", bearer)
	clearRes := httptest.NewRecorder()
	service.ServeHTTP(clearRes, clearReq)
	if clearRes.Code != http.StatusForbidden {
		t.Fatalf("ai:mcp key clear audit status = %d body=%s", clearRes.Code, clearRes.Body.String())
	}

	overviewReq := httptest.NewRequest(http.MethodGet, "/api/system/ai-access", nil)
	overviewReq.Header.Set("Authorization", bearer)
	overviewRes := httptest.NewRecorder()
	service.ServeHTTP(overviewRes, overviewReq)
	if overviewRes.Code != http.StatusOK {
		t.Fatalf("ai:mcp key overview status = %d body=%s", overviewRes.Code, overviewRes.Body.String())
	}
	overviewData := decodeEnvelopeData(t, overviewRes)
	key, _ := overviewData["agentKey"].(map[string]interface{})["value"].(string)
	if !strings.Contains(key, "*") {
		t.Fatalf("ai:mcp key overview must return masked key, got %q", key)
	}
}
