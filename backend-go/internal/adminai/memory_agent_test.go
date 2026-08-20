package adminai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

// newCaptureTestService 构造带本地 mock LLM 网关的服务（/v1/chat/completions → handler）。
func newCaptureTestService(t *testing.T, handler http.HandlerFunc) (*Service, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	port, _ := strconv.Atoi(u.Port())
	cfg := config.Config{Version: "test", Host: "127.0.0.1", Port: port, DataDir: t.TempDir(), DBName: "test.db", AdminAIDefaultModel: "mock-model"}
	s := New(cfg)
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
	return s, ts
}

func insertWebSessionForCapture(t *testing.T, s *Service, id string, idleMinutes int) {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	idleAt := time.Now().UTC().Add(-time.Duration(idleMinutes) * time.Minute).Format(time.RFC3339)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_sessions (id, source, title, model, write_enabled, identity_json, created_at, updated_at, last_activity_at, memory_extracted_at) VALUES (?, 'web', '测试', '', 0, '', ?, ?, ?, '')`,
		id, now, now, idleAt); err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func insertCaptureMessage(t *testing.T, s *Service, sessionID, role, content string, minutesAgo int) {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	id, _ := randomID("aam_")
	created := time.Now().UTC().Add(-time.Duration(minutesAgo) * time.Minute).Format(time.RFC3339)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_messages (id, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, sessionID, role, content, created); err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

// 提炼端到端：空闲会话被选中 → LLM 返回操作清单 → 记忆落库（source=auto）+ 游标推进；
// 再次扫描无新消息时不再触发。
func TestMemoryCaptureEndToEnd(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)
		// 校验请求携带了 system 提炼指令
		msgs, _ := req["messages"].([]interface{})
		if len(msgs) < 1 {
			t.Errorf("no messages")
		}
		resp := `{"choices":[{"message":{"content":"{\"operations\":[{\"action\":\"add\",\"content\":\"用户偏好：表格优先\",\"importance\":8},{\"action\":\"add\",\"content\":\"环境：默认模型是 deepseek\",\"importance\":6,\"triggers\":\"模型,默认\"}]}"}}]}`
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, resp)
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())
	cfg := config.Config{Version: "test", Host: "127.0.0.1", Port: port, DataDir: t.TempDir(), DBName: "test.db", AdminAIDefaultModel: "mock-model"}
	s := New(cfg)
	db, err := s.store.Open(context.Background())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := database.EnsureCoreSchema(context.Background(), db); err != nil {
		t.Fatalf("core schema: %v", err)
	}
	db.Close()
	if dbx, err := s.open(context.Background()); err != nil {
		t.Fatalf("open db: %v", err)
	} else {
		dbx.Close()
	}

	// 三个会话：空闲合格 + 有新消息；空闲合格但无新消息；有新消息但未空闲
	insertWebSessionForCapture(t, s, "aas_cap1", 30)
	insertCaptureMessage(t, s, "aas_cap1", "user", "以后回复用表格展示", 20)
	insertCaptureMessage(t, s, "aas_cap1", "assistant", "好的，已记住", 19)
	insertWebSessionForCapture(t, s, "aas_cap2", 30) // 无新消息
	insertWebSessionForCapture(t, s, "aas_cap3", 1)  // 未空闲
	insertCaptureMessage(t, s, "aas_cap3", "user", "帮我看看 DNS", 0)

	s.runMemoryCaptureWithIdle(10)

	db2, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db2.Close()
	var count int
	if err := db2.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_ai_memories WHERE source = 'auto'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 auto memories, got %d", count)
	}
	var source, session string
	if err := db2.QueryRowContext(context.Background(),
		`SELECT source, session_id FROM admin_ai_memories ORDER BY importance DESC LIMIT 1`).Scan(&source, &session); err != nil {
		t.Fatalf("query memory: %v", err)
	}
	if source != "auto" || session != "aas_cap1" {
		t.Fatalf("unexpected memory attribution: source=%s session=%s", source, session)
	}
	var extracted string
	if err := db2.QueryRowContext(context.Background(),
		`SELECT COALESCE(memory_extracted_at, '') FROM admin_ai_sessions WHERE id = 'aas_cap1'`).Scan(&extracted); err != nil {
		t.Fatalf("query extracted: %v", err)
	}
	if extracted == "" {
		t.Fatalf("memory_extracted_at should be advanced")
	}

	// 再次扫描：无新消息，不产生新记忆
	s.runMemoryCaptureWithIdle(10)
	if err := db2.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_ai_memories WHERE source = 'auto'`).Scan(&count); err != nil {
		t.Fatalf("count2: %v", err)
	}
	if count != 2 {
		t.Fatalf("second scan should not add memories, got %d", count)
	}

	// 追加新消息并再次 idle → 增量提炼（游标只取新消息）。
	// 等待 1.1s 保证新消息时间戳晚于首轮提炼游标（同秒插入会被游标比较吞掉）。
	time.Sleep(1100 * time.Millisecond)
	insertCaptureMessage(t, s, "aas_cap1", "user", "另一个偏好：通知用 Telegram", 0)
	s.runMemoryCaptureWithIdle(10)
	if err := db2.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_ai_memories WHERE source = 'auto'`).Scan(&count); err != nil {
		t.Fatalf("count3: %v", err)
	}
	if count < 3 {
		t.Fatalf("incremental capture should add more memories, got %d", count)
	}
}

// 提炼调用失败：游标回滚，下次扫描可重试。
func TestMemoryCaptureRollbackOnLLMFailure(t *testing.T) {
	s, ts := newCaptureTestService(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gateway down", http.StatusBadGateway)
	})
	defer ts.Close()

	insertWebSessionForCapture(t, s, "aas_roll", 30)
	insertCaptureMessage(t, s, "aas_roll", "user", "以后用中文回复", 20)

	if err := s.runMemoryCaptureOnce(); err == nil {
		t.Fatalf("expected error from gateway failure")
	}

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var extracted string
	if err := db.QueryRowContext(context.Background(),
		`SELECT COALESCE(memory_extracted_at, '') FROM admin_ai_sessions WHERE id = 'aas_roll'`).Scan(&extracted); err != nil {
		t.Fatalf("query: %v", err)
	}
	if extracted != "" {
		t.Fatalf("extracted_at should be rolled back on failure, got %q", extracted)
	}

	// 失败标记已持久化：failed_at 非空、fail_count 递增（冷却生效，重启后依旧）。
	var failedAt string
	var failCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COALESCE(memory_capture_failed_at, ''), COALESCE(memory_capture_fail_count, 0) FROM admin_ai_sessions WHERE id = 'aas_roll'`).Scan(&failedAt, &failCount); err != nil {
		t.Fatalf("query fail marker: %v", err)
	}
	if failedAt == "" {
		t.Fatalf("memory_capture_failed_at should be set after failure")
	}
	if failCount != 1 {
		t.Fatalf("memory_capture_fail_count should be 1 after first failure, got %d", failCount)
	}

	// 冷却窗口内再次扫描：跳过该会话（不再发起调用，不产生错误、轮到次数不增）。
	if err := s.runMemoryCaptureOnce(); err != nil {
		t.Fatalf("cooldown should skip session without error, got %v", err)
	}
	if err := db.QueryRowContext(context.Background(),
		`SELECT COALESCE(memory_capture_fail_count, 0) FROM admin_ai_sessions WHERE id = 'aas_roll'`).Scan(&failCount); err != nil {
		t.Fatalf("query fail count2: %v", err)
	}
	if failCount != 1 {
		t.Fatalf("fail count should stay 1 while cooling down, got %d", failCount)
	}
}

// 提炼输出容错解析。
func TestParseMemoryCaptureResponse(t *testing.T) {
	cases := []struct {
		raw   string
		count int
		ok    bool
	}{
		{`{"operations":[{"action":"add","content":"a","importance":7}]}`, 1, true},
		{"```json\n{\"operations\":[]}\n```", 0, true},
		{"前面解释\n{\"operations\":[{\"action\":\"update\",\"id\":\"x\",\"content\":\"b\"}]}\n结尾", 1, true},
		{"完全没有JSON", 0, false},
		{`{"operations":"oops"}`, 0, false},
		{``, 0, false},
	}
	for i, c := range cases {
		ops, err := parseMemoryCaptureResponse(c.raw)
		if (err == nil) != c.ok {
			t.Fatalf("case %d: ok mismatch (err=%v)", i, err)
		}
		if err == nil && len(ops) != c.count {
			t.Fatalf("case %d: expected %d ops, got %d", i, c.count, len(ops))
		}
	}
}

// applyMemoryCaptureOp：add 与 update 落库，未知 action 忽略，update 引用不存在时忽略。
func TestApplyMemoryCaptureOp(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if err := s.applyMemoryCaptureOp(ctx, db, "aas_x", memoryCaptureOperation{Action: "add", Content: "  偏好A  ", Importance: intPtr(9)}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.applyMemoryCaptureOp(ctx, db, "aas_x", memoryCaptureOperation{Action: "bogus", Content: "x"}); err != nil {
		t.Fatalf("unknown action should be ignored: %v", err)
	}
	if err := s.applyMemoryCaptureOp(ctx, db, "aas_x", memoryCaptureOperation{Action: "update", ID: "missing", Content: "x"}); err != nil {
		t.Fatalf("update of missing memory should be ignored: %v", err)
	}
	if err := s.applyMemoryCaptureOp(ctx, db, "aas_x", memoryCaptureOperation{Action: "add", Content: "   "}); err != nil {
		t.Fatalf("blank content should be ignored: %v", err)
	}

	// 真实 update：改写已存在的记忆内容与重要度
	it := insertMemoryTest(t, s, "旧事实：默认模型 A", 5, false)
	v := 3
	if err := s.applyMemoryCaptureOp(ctx, db, "aas_x", memoryCaptureOperation{Action: "update", ID: it.ID, Content: "新事实：默认模型 B", Importance: &v}); err != nil {
		t.Fatalf("update: %v", err)
	}
	var content string
	var importance int
	if err := db.QueryRowContext(ctx, `SELECT content, importance FROM admin_ai_memories WHERE id = ?`, it.ID).Scan(&content, &importance); err != nil {
		t.Fatalf("verify update: %v", err)
	}
	if content != "新事实：默认模型 B" || importance != 3 {
		t.Fatalf("update not applied: %q imp=%d", content, importance)
	}
	// 更新后旧词不可检索（FTS 触发器同步）
	items, err := s.searchMemories(ctx, db, "默认模型 A", 6)
	if err != nil || len(items) != 0 {
		t.Fatalf("stale content searchable: %v %v", items, err)
	}
}

func intPtr(v int) *int { return &v }
