package adminai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Config{Version: "test", Host: "127.0.0.1", Port: 0, DataDir: t.TempDir(), DBName: "test.db"}
	s := New(cfg)
	// 核心表（system_config 等）由 database 包创建，再触发 adminai 建表
	db, err := s.store.Open(context.Background())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := database.EnsureCoreSchema(context.Background(), db); err != nil {
		t.Fatalf("core schema: %v", err)
	}
	db.Close()
	db, err = s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Close()
	return s
}

func insertApproval(t *testing.T, s *Service, status, expiresAt string) string {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	// 外键约束：审批依赖会话，先幂等插入测试会话
	_, _ = db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO admin_ai_sessions (id, source, title, write_enabled, created_at, updated_at, last_activity_at) VALUES ('aas_test', 'web', '测试会话', 0, ?, ?, ?)`,
		now, now, now)
	id, err := randomID("aaa_")
	if err != nil {
		t.Fatalf("randomID: %v", err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_approvals (id, session_id, status, plan_summary, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, "aas_test", status, "计划摘要", expiresAt, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("insert approval: %v", err)
	}
	return id
}

// 审批超时清理：过期 pending → expired（PRD-04 验收点）。
func TestApprovalCleanerExpiresOverdue(t *testing.T) {
	s := newTestService(t)
	id := insertApproval(t, s, "pending", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339))

	s.expireOverdueApprovals()

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRowContext(context.Background(), "SELECT status FROM admin_ai_approvals WHERE id = ?", id).Scan(&status); err != nil {
		t.Fatalf("query: %v", err)
	}
	if status != "expired" {
		t.Fatalf("期望 expired，实际 %s", status)
	}
}

// 未过期审批不被清理。
func TestApprovalCleanerKeepsFresh(t *testing.T) {
	s := newTestService(t)
	id := insertApproval(t, s, "pending", time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	s.expireOverdueApprovals()

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var status string
	if err := db.QueryRowContext(context.Background(), "SELECT status FROM admin_ai_approvals WHERE id = ?", id).Scan(&status); err != nil {
		t.Fatalf("query: %v", err)
	}
	if status != "pending" {
		t.Fatalf("期望 pending，实际 %s", status)
	}
}

// 审批状态机：pending → approve → approved；pending → reject → rejected。
func TestResolveApprovalStateMachine(t *testing.T) {
	s := newTestService(t)

	for _, action := range []string{"approve", "reject"} {
		id := insertApproval(t, s, "pending", time.Now().UTC().Add(time.Hour).Format(time.RFC3339))

		body := strings.NewReader(`{"action":"` + action + `"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/admin-ai/approvals/"+id+"/resolve", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("resolve %s 状态码 %d: %s", action, rec.Code, rec.Body.String())
		}

		db, err := s.open(context.Background())
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		var status string
		_ = db.QueryRowContext(context.Background(), "SELECT status FROM admin_ai_approvals WHERE id = ?", id).Scan(&status)
		db.Close()
		if status != action {
			t.Fatalf("期望 %s，实际 %s", action, status)
		}
	}
}

// 批准 + applyToSession：审批置为 approved 且该会话 write_enabled=1。
func TestResolveApprovalApplyToSession(t *testing.T) {
	s := newTestService(t)
	id := insertApproval(t, s, "pending", time.Now().UTC().Add(time.Hour).Format(time.RFC3339))

	body := strings.NewReader(`{"action":"approve","applyToSession":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin-ai/approvals/"+id+"/resolve", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var we int
	if err := db.QueryRowContext(context.Background(), "SELECT write_enabled FROM admin_ai_sessions WHERE id = 'aas_test'").Scan(&we); err != nil {
		t.Fatalf("query session: %v", err)
	}
	if we != 1 {
		t.Fatalf("期望会话写授权 write_enabled=1，实际 %d", we)
	}
}

// reject + reason：原因写入审批记录，供“请求更改”反馈给模型。
func TestResolveApprovalStoresReason(t *testing.T) {
	s := newTestService(t)
	id := insertApproval(t, s, "pending", time.Now().UTC().Add(time.Hour).Format(time.RFC3339))

	body := strings.NewReader(`{"action":"reject","reason":"把端口改成 45654"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin-ai/approvals/"+id+"/resolve", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var status, reason string
	if err := db.QueryRowContext(context.Background(), "SELECT status, COALESCE(reason,'') FROM admin_ai_approvals WHERE id = ?", id).Scan(&status, &reason); err != nil {
		t.Fatalf("query approval: %v", err)
	}
	if status != "reject" || reason != "把端口改成 45654" {
		t.Fatalf("期望 reject + 原因，实际 status=%q reason=%q", status, reason)
	}
}

// cancel 会真正调用已注册的 runCtx 取消函数（订阅后仍可终止执行）。
func TestCancelRunInvokesCancel(t *testing.T) {
	s := newTestService(t)
	called := false
	s.mu.Lock()
	s.cancels["aae_ctx"] = context.CancelFunc(func() { called = true })
	s.mu.Unlock()

	body := strings.NewReader(`{"runId":"aae_ctx"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin-ai/cancel", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("cancel 未触发已注册的取消函数")
	}
}

// 历史消息返回 reasoning_content（会话恢复可展示思考过程）。
func TestListMessagesIncludesReasoning(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO admin_ai_sessions (id, source, title, write_enabled, created_at, updated_at, last_activity_at) VALUES ('aas_test', 'web', '测试会话', 0, ?, ?, ?)`,
		now, now, now)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_messages (id, session_id, role, content, reasoning_content, created_at) VALUES ('aam_rz', 'aas_test', 'assistant', '回答正文', '思考过程', ?)`,
		now)
	db.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/admin-ai/sessions/aas_test/messages", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Items []struct {
				ID               string `json:"id"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	found := false
	for _, it := range resp.Data.Items {
		if it.ID == "aam_rz" {
			found = it.ReasoningContent == "思考过程"
		}
	}
	if !found {
		t.Fatal("未读取到消息的 reasoning_content")
	}
}

// 已 resolve 的审批不可重复操作（WHERE status='pending' 保护）。
func TestResolveApprovalTwiceDenied(t *testing.T) {
	s := newTestService(t)
	id := insertApproval(t, s, "approved", time.Now().UTC().Add(time.Hour).Format(time.RFC3339))

	body := strings.NewReader(`{"action":"reject"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin-ai/approvals/"+id+"/resolve", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("重复处理已生效审批应返回 409，状态码 %d body=%s", rec.Code, rec.Body.String())
	}

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var status string
	_ = db.QueryRowContext(context.Background(), "SELECT status FROM admin_ai_approvals WHERE id = ?", id).Scan(&status)
	if status != "approved" {
		t.Fatalf("已批准审批不应被改为 %s", status)
	}
}

// 审批列表查询：pending 状态过滤。
func TestListApprovalsFilter(t *testing.T) {
	s := newTestService(t)
	insertApproval(t, s, "pending", time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	insertApproval(t, s, "approved", time.Now().UTC().Format(time.RFC3339))

	req := httptest.NewRequest(http.MethodGet, "/api/admin-ai/approvals?status=pending", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Items []approvalItem `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(resp.Data.Items) != 1 || resp.Data.Items[0].Status != "pending" {
		t.Fatalf("期望 1 条 pending，实际 %d 条", len(resp.Data.Items))
	}
}

// 审计查询 API：执行记录可查。
func TestAuditQuery(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO admin_ai_sessions (id, source, title, write_enabled, created_at, updated_at, last_activity_at) VALUES ('aas_test', 'web', '测试会话', 0, ?, ?, ?)`,
		now, now, now)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_executions (id, session_id, source, status, llm_model, started_at) VALUES ('aae_test', 'aas_test', 'web', 'completed', 'm/default', ?)`, now)
	db.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/admin-ai/audit", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Items []auditItem `json:"items"`
			Total int         `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.Data.Total < 1 {
		t.Fatalf("审计至少 1 条，实际 %d", resp.Data.Total)
	}
	found := false
	for _, it := range resp.Data.Items {
		if it.Kind == "execution" && it.ID == "aae_test" {
			found = true
		}
	}
	if !found {
		t.Fatal("未找到插入的执行记录")
	}
}

// settings 多键读写（PRD-04 系统配置）。
func TestSettingsRoundTrip(t *testing.T) {
	s := newTestService(t)

	putBody := strings.NewReader(`{"admin_ai_enabled":"true","admin_ai_tool_call_limit":"20"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin-ai/settings", putBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT 状态码 %d: %s", rec.Code, rec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/admin-ai/settings", nil)
	getRec := httptest.NewRecorder()
	s.ServeHTTP(getRec, getReq)
	var resp struct {
		Data struct {
			Settings map[string]string `json:"settings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("GET 解析失败: %v", err)
	}
	if resp.Data.Settings["admin_ai_tool_call_limit"] != "20" {
		t.Fatalf("期望 tool_call_limit=20，实际 %q", resp.Data.Settings["admin_ai_tool_call_limit"])
	}
	if resp.Data.Settings["admin_ai_enabled"] != "true" {
		t.Fatalf("期望 enabled=true，实际 %q", resp.Data.Settings["admin_ai_enabled"])
	}
	// 未配置键回默认值
	if resp.Data.Settings["admin_ai_timeout_seconds"] != "600" {
		t.Fatalf("期望 timeout 默认 600，实际 %q", resp.Data.Settings["admin_ai_timeout_seconds"])
	}
}

// toolDesc 具体路径也要命中模板路径的中文描述（/api/aliyun/accounts/1/domains
// → 清单里 /api/aliyun/accounts/{id}/domains 的「列出或添加域名」）。
func TestToolDescTemplatePathMatch(t *testing.T) {
	s := newTestService(t)
	s.catalogMu.Lock()
	s.catalogDescs = map[string]string{
		"/api/aliyun/accounts":                     "列出或新增阿里云账号",
		"/api/aliyun/accounts/{id}/domains":        "列出或添加域名",
		"/api/tencent/accounts":                    "列出或新增腾讯云账号",
		"/api/cloudflare/zones":                    "列出 Zone 列表",
		"/api/cloudflare/zones/{zoneId}/dns_records": "管理 DNS 记录",
	}
	s.catalogMu.Unlock()

	cases := []struct {
		name     string
		args     string
		expected string
	}{
		{"无参路径精确命中", `{"method":"GET","path":"/api/aliyun/accounts"}`, "列出或新增阿里云账号"},
		{"具体 id 命中模板", `{"method":"GET","path":"/api/aliyun/accounts/1/domains"}`, "列出或添加域名"},
		{"多层具体 id 命中模板", `{"method":"GET","path":"/api/cloudflare/zones/zone_abc/dns_records"}`, "管理 DNS 记录"},
		{"未知路径不回退（语义视图只展示清单内动作描述）", `{"method":"POST","path":"/api/unknown"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.toolDesc("call_api", c.args); got != c.expected {
				t.Fatalf("期望 %q，实际 %q", c.expected, got)
			}
		})
	}
}

// 完全批准模式：admin_ai_auto_approve=true 时 getAutoApprove 返回 true。
func TestGetAutoApprove(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// 未配置 → 默认 false
	v, err := s.getAutoApprove(context.Background(), db)
	if err != nil || v {
		t.Fatalf("期望默认 false，实际 %v (err=%v)", v, err)
	}
	// 配置 true → true
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO system_config (key, value) VALUES ('admin_ai_auto_approve', 'true')
		 ON CONFLICT(key) DO UPDATE SET value = 'true'`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}
	v, err = s.getAutoApprove(context.Background(), db)
	if err != nil || !v {
		t.Fatalf("期望 true，实际 %v (err=%v)", v, err)
	}
}
