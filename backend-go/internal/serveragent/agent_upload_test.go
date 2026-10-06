package serveragent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAgentUploadRequiresValidator 锁住二进制直传端点的鉴权边界：未注入
// AI 接入校验器时一律拒绝，注入后由校验器决定放行。
func TestAgentUploadRequiresValidator(t *testing.T) {
	s := &Service{}
	req := httptest.NewRequest(http.MethodPost, "/api/server/agent/upload", nil)
	if s.authorizeAIAgent(req) {
		t.Fatalf("未注入校验器时不应放行")
	}

	called := false
	s.SetAIAgentValidator(func(r *http.Request) bool {
		called = true
		return true
	})
	if !s.authorizeAIAgent(req) {
		t.Fatalf("校验器放行时 authorizeAIAgent 应返回 true")
	}
	if !called {
		t.Fatalf("校验器应被调用")
	}
}

// TestAgentFetchURLValidatesParams 验证 fetch-url 在缺少必填字段时直接返回 400，
// 不会触达 Agent 下发路径。
func TestAgentFetchURLValidatesParams(t *testing.T) {
	s := &Service{}
	body := strings.NewReader(`{"serverId":"host-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/server/agent/fetch-url", body)
	rec := httptest.NewRecorder()
	s.handleAgentFetchURL(rec, req, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺少 url/path 应返回 400，实际 %d", rec.Code)
	}
}
