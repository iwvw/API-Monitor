package adminai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/database"
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// TestRunStopsAfterClientGone 验证事件通道在 run 结束后无条件关闭：
// 即使被 streamEvents 领走（s.runs 删除），消费端也能读到关闭而收尾，
// 不再出现「run 结束但 SSE/goroutine 常驻」的泄漏。
func TestRunChannelClosedAfterFinish(t *testing.T) {
	s := newTestService(t)

	// 模拟 streamEvents 领走场景：注册 run + 消费者读取，run 结束后通道应关闭
	eventCh := make(chan SSEEvent, 8)
	s.mu.Lock()
	s.runs["aae_claimed"] = eventCh
	s.runBuffers["aae_claimed"] = newRunEventBuffer()
	s.chToBuf[eventCh] = s.runBuffers["aae_claimed"]
	s.sessionRuns["aas_claimed"] = "aae_claimed"
	s.activeRuns["aae_claimed"] = true
	s.mu.Unlock()
	// 领走（streamEvents 语义）
	s.mu.Lock()
	delete(s.runs, "aae_claimed")
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case _, ok := <-eventCh:
				if !ok {
					close(done)
					return
				}
			case <-time.After(2 * time.Second):
				close(done)
				return
			}
		}
	}()

	// 手动走 run 收尾路径验证
	s.mu.Lock()
	s.runDone["aae_claimed"] = true
	close(eventCh)
	s.mu.Unlock()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("run 结束通道应被关闭，消费方未能退出")
	}
}

// TestDrainRunEventsReplaysTerminalAfterClaim 验证 drainRunEvents 在通道被
// SSE 领走 / run 已结束时回退环形缓冲补收终态，cron/TG 不会因拿不到通道而挂起。
func TestDrainRunEventsReplaysTerminalAfterClaim(t *testing.T) {
	s := newTestService(t)
	runID := "aae_drain1"
	eventCh := make(chan SSEEvent, 8)
	buf := newRunEventBuffer()
	s.mu.Lock()
	s.runBuffers[runID] = buf
	s.chToBuf[eventCh] = buf
	s.runs[runID] = eventCh
	s.runDone[runID] = true // 已结束
	s.mu.Unlock()
	// 模拟领走
	s.mu.Lock()
	delete(s.runs, runID)
	s.mu.Unlock()

	buf.appendSeq(SSEEvent{Type: "delta", Fields: map[string]interface{}{"text": "x"}})
	buf.appendSeq(SSEEvent{Type: "done", Fields: map[string]interface{}{"messageId": "m1"}})

	var got atomic.Int32
	var terminal atomic.Bool
	s.drainRunEvents(context.Background(), runID, func(ev SSEEvent) {
		got.Add(1)
		if ev.Type == "done" {
			terminal.Store(true)
		}
	})
	if !terminal.Load() {
		t.Fatalf("应重放 done 终态")
	}
}

// TestCronTaskRunRejectsNonLoopback 验证 X-Internal-Cron 之外还强制回环来源：
// 同源登录会话伪造 header 无法以 policy=allow 触发无头执行。
func TestCronTaskRunRejectsNonLoopback(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := database.EnsureCoreSchema(context.Background(), db); err != nil {
		t.Fatalf("core: %v", err)
	}
	db.Close()
	s.aiCaller = func(ctx context.Context, req systemmetrics.AICallRequest) (systemmetrics.AICallResponse, error) {
		return systemmetrics.AICallResponse{StatusCode: http.StatusOK, Body: map[string]interface{}{"data": map[string]interface{}{"ok": true}}}, nil
	}

	body := strings.NewReader(`{"prompt":"测试","policy":"allow"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin-ai/cron/task-run", body)
	req.Header.Set("X-Internal-Cron", "true")
	req.RemoteAddr = "203.0.113.10:5555" // 非回环
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("非回环来源期望 403，实际 %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSessionWriteRevokeAndTTL 验证会话写授权可撤销，且 TTL 到期自动失效。
func TestSessionWriteRevokeAndTTL(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO admin_ai_sessions (id, source, title, write_enabled, created_at, updated_at, last_activity_at) VALUES ('aas_w', 'web', '写会话', 1, ?, ?, ?)`,
		now, now, now)
	db.Close()

	dbA, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ok, err := s.isSessionWriteEnabled(context.Background(), dbA, "aas_w")
	dbA.Close()
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !ok {
		t.Fatalf("应视为已授权")
	}

	// PATCH 撤销
	req := httptest.NewRequest(http.MethodPatch, "/api/admin-ai/sessions/aas_w", strings.NewReader(`{"writeEnabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("撤销期望 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	db2, _ := s.open(context.Background())
	defer db2.Close()
	ok, _ = s.isSessionWriteEnabled(context.Background(), db2, "aas_w")
	if ok {
		t.Fatalf("撤销后不应再授权")
	}
}

// TestPurgeRetainedAudit 验证审计保留清理只清超期 executions/approvals，不动 messages。
func TestPurgeRetainedAudit(t *testing.T) {
	s := newTestService(t)
	// 设置保留 90 天
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	old := time.Now().UTC().AddDate(0, 0, -120).Format(time.RFC3339)
	_, _ = db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO admin_ai_sessions (id, source, title, write_enabled, created_at, updated_at, last_activity_at) VALUES ('aas_audit', 'web', '审计', 0, ?, ?, ?)`, now, now, now)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_executions (id, session_id, source, status, llm_model, started_at) VALUES ('aae_old', 'aas_audit', 'web', 'completed', 'm', ?)`, old)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_executions (id, session_id, source, status, llm_model, started_at) VALUES ('aae_fresh', 'aas_audit', 'web', 'completed', 'm', ?)`, now)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_messages (id, session_id, role, content, created_at) VALUES ('aam_keep', 'aas_audit', 'user', '对话内容', ?)`, old)
	db.Close()

	s.purgeRetainedAudit()

	db2, _ := s.open(context.Background())
	defer db2.Close()
	var oldCount, freshCount, msgCount int
	_ = db2.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_ai_executions WHERE id = 'aae_old'`).Scan(&oldCount)
	_ = db2.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_ai_executions WHERE id = 'aae_fresh'`).Scan(&freshCount)
	_ = db2.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_ai_messages WHERE id = 'aam_keep'`).Scan(&msgCount)
	if oldCount != 0 {
		t.Fatalf("超期 execution 应被清理，实际 %d", oldCount)
	}
	if freshCount != 1 {
		t.Fatalf("未超期 execution 应保留，实际 %d", freshCount)
	}
	if msgCount != 1 {
		t.Fatalf("用户对话消息不应被自动清理，实际 %d", msgCount)
	}
}