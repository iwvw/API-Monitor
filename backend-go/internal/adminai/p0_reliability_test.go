package adminai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// P0-1：同一会话并发执行互斥（RunLoop 锁内检查+注册，频道/cron 入站同样受保护）。
func TestRunLoopSessionMutex(t *testing.T) {
	s := newTestService(t)
	// RunLoop 前置校验 aiCaller；mock 仅在需要时被只读工具路径调用，主流程直连 LLM 网关，
	// cfg.Port=0 会使推理 goroutine 快速失败退出，不影响互斥断言
	s.SetAICaller(func(_ context.Context, _ systemmetrics.AICallRequest) (systemmetrics.AICallResponse, error) {
		return systemmetrics.AICallResponse{}, nil
	})
	ctx := context.Background()

	runID, err := s.RunLoop(ctx, "web", "aas_mutex", "第一条消息", "", "", "")
	if err != nil || runID == "" {
		t.Fatalf("first run should start: runID=%q err=%v", runID, err)
	}
	// 第一个 run 的 goroutine 尚未完成时再次提交同一会话 → 必须拒绝
	runID2, err2 := s.RunLoop(ctx, "web", "aas_mutex", "第二条消息", "", "", "")
	if err2 == nil {
		t.Fatalf("second run on same session should conflict, got runID=%q", runID2)
	}
	if !strings.Contains(err2.Error(), "已有执行进行中") {
		t.Fatalf("unexpected conflict error: %v", err2)
	}

	// 不同会话不受影响
	runID3, err3 := s.RunLoop(ctx, "web", "aas_mutex2", "另一会话", "", "", "")
	if err3 != nil || runID3 == "" {
		t.Fatalf("other session should start: runID=%q err=%v", runID3, err3)
	}

	// 等待推理 goroutine 收尾（含 DB 连接归还），避免 TempDir 清理失败
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		_, running1 := s.sessionRuns["aas_mutex"]
		_, running2 := s.sessionRuns["aas_mutex2"]
		s.mu.Unlock()
		if !running1 && !running2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("runs did not finish in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// P0-2：启动恢复把 running 残留执行标记为 error。
func TestRecoverStaleExecutions(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO admin_ai_sessions (id, source, title, write_enabled, created_at, updated_at, last_activity_at) VALUES ('aas_rec', 'web', '', 0, ?, ?, ?)`, now, now, now)
	for _, status := range []string{"running", "completed", "error"} {
		id, _ := randomID("aae_")
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO admin_ai_executions (id, session_id, source, status, started_at) VALUES (?, 'aas_rec', 'web', ?, ?)`,
			id, status, now); err != nil {
			t.Fatalf("insert execution: %v", err)
		}
	}
	db.Close()

	s.recoverStaleExecutions()

	db2, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer db2.Close()
	var runningCount, recovered int
	if err := db2.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_ai_executions WHERE status = 'running'`).Scan(&runningCount); err != nil {
		t.Fatalf("query running: %v", err)
	}
	if runningCount != 0 {
		t.Fatalf("stale running should be cleared, got %d", runningCount)
	}
	var errText string
	if err := db2.QueryRowContext(context.Background(),
		`SELECT COALESCE(error,'') FROM admin_ai_executions WHERE status = 'error' AND error LIKE '进程中断%'`).Scan(&errText); err != nil {
		t.Fatalf("query recovered: %v", err)
	}
	if errText == "" {
		t.Fatalf("recovered execution should carry 进程中断 error text")
	}
	recovered = 0
	_ = recovered
}

// P0-3：工具循环检测——累计计数、call_api 指纹只取 method+path、阈值阻断、run 清理。
func TestToolLoopDetection(t *testing.T) {
	s := newTestService(t)
	runID := "aae_loop"

	tool := func(name string, args map[string]interface{}) {
		t.Helper()
		if allow, count := s.toolLoopCheck(runID, name, args); !allow {
			t.Fatalf("call %d should be allowed before threshold", count)
		}
	}
	// 同工具同参数（仅路径不同 bool 不影响 JSON 序列化）跨轮累计：count 1..8 全允许
	for i := 0; i < toolLoopBlockThreshold-2; i++ {
		tool("call_api", map[string]interface{}{"method": "GET", "path": "/api/cloudflare/zones"})
	}
	// 第 9 次（count=9）仍是阈值内最后一次，允许
	if allow, count := s.toolLoopCheck(runID, "call_api", map[string]interface{}{"method": "GET", "path": "/api/cloudflare/zones"}); !allow || count != toolLoopBlockThreshold-1 {
		t.Fatalf("threshold boundary: allow=%v count=%d", allow, count)
	}
	// 第 10 次（count=10）越线阻断
	if allow, count := s.toolLoopCheck(runID, "call_api", map[string]interface{}{"method": "GET", "path": "/api/cloudflare/zones"}); allow || count != toolLoopBlockThreshold {
		t.Fatalf("should block at threshold: allow=%v count=%d", allow, count)
	}
	// 不同 path 是不同指纹，不受影响
	if allow, _ := s.toolLoopCheck(runID, "call_api", map[string]interface{}{"method": "GET", "path": "/api/cloudflare/accounts"}); !allow {
		t.Fatalf("different path should not be blocked")
	}
	// call_api 指纹剥离 body/headers：同 path 不同 body 视为同一调用（轮询风暴判定）
	pathB := "/api/cloudflare/evergreen"
	if allow, count := s.toolLoopCheck(runID, "call_api", map[string]interface{}{"method": "GET", "path": pathB}); !allow || count != 1 {
		t.Fatalf("fresh path should start count=1: allow=%v count=%d", allow, count)
	}
	if allow, count := s.toolLoopCheck(runID, "call_api", map[string]interface{}{"method": "GET", "path": pathB, "body": map[string]interface{}{"x": 1}}); !allow || count != 2 {
		t.Fatalf("same path different body shares fingerprint: allow=%v count=%d", allow, count)
	}
	// 其他工具走完整参数指纹
	memoryTool := "memory_add"
	for i := 0; i < toolLoopBlockThreshold-2; i++ {
		if allow, _ := s.toolLoopCheck(runID, memoryTool, map[string]interface{}{"content": "测试记忆"}); !allow {
			t.Fatalf("memory_add should be allowed until threshold")
		}
	}
	if allow, _ := s.toolLoopCheck(runID, memoryTool, map[string]interface{}{"content": "测试记忆"}); !allow {
		t.Fatalf("memory_add count=9 boundary should still allow")
	}
	if allow, _ := s.toolLoopCheck(runID, memoryTool, map[string]interface{}{"content": "测试记忆"}); allow {
		t.Fatalf("memory_add should block at count=10")
	}
	// 参数不同 → 新指纹，不受之前计数影响
	if allow, _ := s.toolLoopCheck(runID, memoryTool, map[string]interface{}{"content": "另一条记忆"}); !allow {
		t.Fatalf("different args should start fresh fingerprint")
	}
	// run 结束清理后计数归零
	s.clearToolLoops(runID)
	if allow, count := s.toolLoopCheck(runID, "call_api", map[string]interface{}{"method": "GET", "path": "/api/cloudflare/zones"}); !allow || count != 1 {
		t.Fatalf("after clear, count should reset: allow=%v count=%d", allow, count)
	}
}

// P0-3b：指纹稳定性（map 键顺序不影响序列化结果）。
func TestToolLoopFingerprintStable(t *testing.T) {
	f1 := toolLoopFingerprint("call_api", map[string]interface{}{"path": "/p", "method": "POST"})
	f2 := toolLoopFingerprint("call_api", map[string]interface{}{"method": "POST", "path": "/p"})
	if f1 != f2 {
		t.Fatalf("fingerprint should be order-independent: %q vs %q", f1, f2)
	}
	if toolLoopFingerprint("call_api", map[string]interface{}{"path": "/p"}) != "call_api|GET|/p" {
		t.Fatalf("default method should be GET")
	}
}

// P0-4：错误输出卫生——控制字符剥离、超长截断、干净错误原样返回。
func TestSanitizeToolError(t *testing.T) {
	if sanitizeToolError(nil) != nil {
		t.Fatalf("nil error should stay nil")
	}
	clean := errors.New("HTTP 500 服务器内部错误")
	if sanitizeToolError(clean) != clean {
		t.Fatalf("clean error should return as-is")
	}
	dirty := errors.New("调用失败\x1b[31m红色\x00注入")
	got := sanitizeToolError(dirty)
	if strings.Contains(got.Error(), "\x1b") || strings.Contains(got.Error(), "\x00") {
		t.Fatalf("control chars not stripped: %q", got.Error())
	}
	// ESC 被剥除但其后文本保持不变（可见"红色注入"文本完整）
	if !strings.Contains(got.Error(), "[31m红色注入") {
		t.Fatalf("visible text should survive: %q", got.Error())
	}
	long := errors.New(strings.Repeat("A", toolErrorMaxChars+500))
	if len([]rune(sanitizeToolError(long).Error())) >= toolErrorMaxChars+100 {
		t.Fatalf("long error not truncated")
	}
	// retryable 判定依赖的关键词保留：4xx 文本不被清洗改写
	notRetryable := sanitizeToolError(errors.New("接口返回 HTTP 404 不存在"))
	if retryableToolError(notRetryable) {
		t.Fatalf("sanitized 4xx error should stay non-retryable")
	}
	_ = fmt.Sprintf
}
