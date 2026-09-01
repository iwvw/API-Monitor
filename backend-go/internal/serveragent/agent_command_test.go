package serveragent

import (
	"context"
	"net/http/httptest"
	"testing"
)

// TestAllowDangerousFromAdminAI 锁住「完全批准模式下危险命令放行」的安全边界：
// 只有经服务端内部 ai_caller 写入 request context 的标记才放行；普通 HTTP 请求
// （无该 context 值）一律不放行，杜绝外部登录用户伪造头绕过危险命令拦截。
func TestAllowDangerousFromAdminAI(t *testing.T) {
	s := &Service{}
	plainReq := httptest.NewRequest("POST", "/api/server/agent/command/x", nil)
	fullReq := httptest.NewRequest("POST", "/api/server/agent/command/x", nil).WithContext(WithAdminAIFullApprove(context.Background()))

	if s.allowDangerousFromAdminAI(plainReq) {
		t.Errorf("普通 HTTP 请求不应放行危险命令")
	}
	if !s.allowDangerousFromAdminAI(fullReq) {
		t.Errorf("管理 AI 完全批准 context 应放行危险命令")
	}

	// context 值不可由 HTTP 头伪造：即使外部携带 X-AI-Agent / X-Admin-AI-Full-Approve，
	// 也不会让普通请求获得放行（这些头只是透传，不写入 context）。
	spoofReq := httptest.NewRequest("POST", "/api/server/agent/command/x", nil)
	spoofReq.Header.Set("X-AI-Agent", "api-monitor")
	spoofReq.Header.Set("X-Admin-AI-Full-Approve", "true")
	if s.allowDangerousFromAdminAI(spoofReq) {
		t.Errorf("伪造头部不应放行危险命令")
	}
}
