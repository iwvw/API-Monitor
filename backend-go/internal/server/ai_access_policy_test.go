package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAIAccessPolicyFullAllowsWritesWithoutToggle：full 模式下
// 写操作无需全局写开关即可执行，管理 AI 路由也放开；
// 防自毁拦截（AI 递归、密钥轮换）仍然生效。
func TestAIAccessPolicyFullAllowsWritesWithoutToggle(t *testing.T) {
	handler := testServer(t)
	cookie := loginServerForTest(t, handler)

	getAgentKey := func() (string, string) {
		keyReq := httptest.NewRequest(http.MethodGet, "/api/system/ai-access", nil)
		keyReq.AddCookie(cookie)
		keyRes := httptest.NewRecorder()
		handler.ServeHTTP(keyRes, keyReq)
		var keyPayload map[string]interface{}
		if err := json.Unmarshal(keyRes.Body.Bytes(), &keyPayload); err != nil {
			t.Fatalf("decode ai-access: %v", err)
		}
		overview := keyPayload["data"].(map[string]interface{})
		policy := overview["policy"].(map[string]interface{})
		accessPolicy, _ := policy["accessPolicy"].(string)
		return overview["agentKey"].(map[string]interface{})["value"].(string), accessPolicy
	}

	// 默认 standard 且写未开
	if _, accessPolicy := getAgentKey(); accessPolicy != "standard" {
		t.Fatalf("expected standard policy by default, got %q", accessPolicy)
	}

	// 切换到 full（写开关仍关闭）
	policyReq := httptest.NewRequest(http.MethodPut, "/api/ai-access/policy", strings.NewReader(`{"policy":"full"}`))
	policyReq.AddCookie(cookie)
	policyReq.Header.Set("Content-Type", "application/json")
	policyRes := httptest.NewRecorder()
	handler.ServeHTTP(policyRes, policyReq)
	if policyRes.Code != http.StatusOK {
		t.Fatalf("set policy status = %d body=%s", policyRes.Code, policyRes.Body.String())
	}
	if _, accessPolicy := getAgentKey(); accessPolicy != "full" {
		t.Fatalf("expected accessPolicy=full after toggle, got %q", accessPolicy)
	}

	agentKey, _ := getAgentKey()
	callTools := func(name string, args map[string]interface{}) map[string]interface{} {
		body := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/call",
			"params":  map[string]interface{}{"name": name, "arguments": args},
		}
		encoded, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/ai/mcp", bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		var payload map[string]interface{}
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode mcp response: %v body=%s", err, res.Body.String())
		}
		return payload
	}

	// full 下写操作无需开关：POST /api/backup/run
	payload := callTools("call_api", map[string]interface{}{"method": "POST", "path": "/api/backup/run"})
	if payload["error"] != nil {
		t.Fatalf("full policy should allow writes without toggle, got %#v", payload["error"])
	}

	// 防自毁仍生效：密钥轮换被拒
	payload = callTools("call_api", map[string]interface{}{"method": "POST", "path": "/api/ai-access/key/rotate"})
	if payload["error"] == nil {
		t.Fatal("key rotation must stay blocked in full policy")
	}
	// 递归调用被拒
	payload = callTools("call_api", map[string]interface{}{"method": "GET", "path": "/api/ai/manifest"})
	if payload["error"] == nil {
		t.Fatal("recursive AI routes must stay blocked in full policy")
	}
}

// TestAIAccessPolicyMinimalBlocksWrites：minimal 模式下写操作一律拒绝。
func TestAIAccessPolicyMinimalBlocksWrites(t *testing.T) {
	handler := testServer(t)
	cookie := loginServerForTest(t, handler)

	policyReq := httptest.NewRequest(http.MethodPut, "/api/ai-access/policy", strings.NewReader(`{"policy":"minimal"}`))
	policyReq.AddCookie(cookie)
	policyReq.Header.Set("Content-Type", "application/json")
	policyRes := httptest.NewRecorder()
	handler.ServeHTTP(policyRes, policyReq)
	if policyRes.Code != http.StatusOK {
		t.Fatalf("set policy status = %d body=%s", policyRes.Code, policyRes.Body.String())
	}

	keyReq := httptest.NewRequest(http.MethodGet, "/api/system/ai-access", nil)
	keyReq.AddCookie(cookie)
	keyRes := httptest.NewRecorder()
	handler.ServeHTTP(keyRes, keyReq)
	var keyPayload map[string]interface{}
	if err := json.Unmarshal(keyRes.Body.Bytes(), &keyPayload); err != nil {
		t.Fatal(err)
	}
	overview := keyPayload["data"].(map[string]interface{})
	agentKey := overview["agentKey"].(map[string]interface{})["value"].(string)

	// 开启写开关也无济于事：minimal 由调用侧强制只读
	writeReq := httptest.NewRequest(http.MethodPut, "/api/system/ai-access/write", strings.NewReader(`{"writeEnabled":true}`))
	writeReq.AddCookie(cookie)
	writeReq.Header.Set("Content-Type", "application/json")
	writeRes := httptest.NewRecorder()
	handler.ServeHTTP(writeRes, writeReq)
	if writeRes.Code != http.StatusOK {
		t.Fatalf("enable write status = %d", writeRes.Code)
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"call_api","arguments":{"method":"POST","path":"/api/backup/run"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/ai/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+agentKey)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	var payload map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v body=%s", err, res.Body.String())
	}
	if payload["error"] == nil {
		t.Fatalf("minimal policy must reject writes, got %#v", payload)
	}
	if !strings.Contains(payload["error"].(map[string]interface{})["message"].(string), "只读") {
		t.Fatalf("unexpected minimal rejection message: %#v", payload["error"])
	}

	// 恢复 standard，避免影响其它测试
	restore := httptest.NewRequest(http.MethodPut, "/api/ai-access/policy", strings.NewReader(`{"policy":"standard"}`))
	restore.AddCookie(cookie)
	restore.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(httptest.NewRecorder(), restore)
}
